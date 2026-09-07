package uac

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func TestPlaybackIntentLateBranchPersistsAfterLocalRelease(t *testing.T) {
	for _, cleaned := range []bool{false, true} {
		name := "local-close"
		if cleaned {
			name = "completed-cleanup"
		}
		t.Run(name, func(t *testing.T) { testPlaybackIntentLateBranch(t, cleaned) })
	}
}

type quarantineOfflinePool struct {
	gorm.ConnPool
	offline  atomic.Bool
	failures atomic.Int32
}

func (p *quarantineOfflinePool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	if p.offline.Load() {
		p.failures.Add(1)
		return nil, errors.New("fixture quarantine database offline")
	}
	return p.ConnPool.(*sql.DB).BeginTx(ctx, options)
}
func (p *quarantineOfflinePool) GetDBConn() (*sql.DB, error) { return p.ConnPool.(*sql.DB), nil }

func TestPlaybackIntentQuarantineRetriesFactsAfterDatabaseRecovery(t *testing.T) {
	f := newPlaybackOperationUDPFixture(t)
	awaitCleanupFirstBranch(t, f)
	connection := f.op.owned.Transaction().Connection()
	connection.Ref(1)
	t.Cleanup(func() { _, _ = connection.TryClose() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, f.op.CloseLocal(ctx))
	pool := &quarantineOfflinePool{ConnPool: f.db.Statement.ConnPool}
	pool.offline.Store(true)
	faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
	faultDB.Statement.ConnPool = pool
	require.NoError(t, f.op.enter(ctx))
	f.op.store = playauth.NewDeviceOperationIntentStore(faultDB)
	f.op.leave()
	f.op.factMu.Lock()
	late := f.op.first.Clone()
	f.op.factMu.Unlock()
	late.To().Params.Add("tag", "late-during-db-failure")
	_, err := f.peer.WriteTo([]byte(late.String()), f.address)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return pool.failures.Load() > 0 }, time.Second, time.Millisecond)
	require.Len(t, f.op.owned.QuarantinedBranches().Responses, 1)
	stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Empty(t, stored.Steps[0].AdditionalBranches)
	require.Equal(t, playauth.IntentDispatched, stored.Intent.State)
	pool.offline.Store(false)
	require.Eventually(t, func() bool {
		stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
		return err == nil && len(stored.Steps[0].AdditionalBranches) == 1
	}, 3*time.Second, time.Millisecond, "retry must not depend on another response or Close invocation")
	require.ErrorIs(t, f.op.CloseLocal(ctx), ErrPlaybackCleanupUnknown)
	f.noACK(t)
}

func TestPlaybackIntentQuarantineWaitsOutsideFrozenCleanupBatch(t *testing.T) {
	unhandled := make(chan *sip.Response, 1)
	f := newPlaybackOperationUDPFixture(t, sipgo.WithUserAgentTransactionLayerOptions(sip.WithTransactionLayerUnhandledResponseHandler(func(r *sip.Response) { unhandled <- r })))
	awaitCleanupFirstBranch(t, f)
	connection := f.op.owned.Transaction().Connection()
	connection.Ref(1)
	t.Cleanup(func() { _, _ = connection.TryClose() })
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
	_, _ = readCleanupRequest(t, f.peer)
	bye, address := readCleanupRequest(t, f.peer)
	f.op.factMu.Lock()
	late := f.op.first.Clone()
	f.op.factMu.Unlock()
	late.To().Params.Add("tag", "late-during-cleanup")
	_, err := f.peer.WriteTo([]byte(late.String()), f.address)
	require.NoError(t, err)
	select {
	case <-unhandled:
	case <-ctx.Done():
		t.Fatal("late response not received")
	}
	require.Len(t, f.op.owned.QuarantinedBranches().Responses, 1)
	stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Empty(t, stored.Steps[0].AdditionalBranches, "quarantine persistence cannot overlap active cleanup work")
	_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
	require.NoError(t, err)
	require.NoError(t, <-finished)
	require.Eventually(t, func() bool {
		loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
		return err == nil && len(loaded.Steps[0].AdditionalBranches) == 1
	}, time.Second, time.Millisecond)
	stored, err = f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Empty(t, stored.Steps[0].AdditionalBranches[0].CleanupAttempts)
	require.NoError(t, f.op.enter(ctx))
	require.Empty(t, f.op.cleanupPlan, "single-branch batch cannot become multi-branch after freezing")
	f.op.leave()
	require.ErrorIs(t, f.op.CloseLocal(ctx), ErrPlaybackCleanupUnknown)
	f.noACK(t)
}

func TestPlaybackIntentQuarantineRetainsLateFirstValidResponse(t *testing.T) {
	f := newPlaybackOperationUDPFixture(t)
	connection := f.op.owned.Transaction().Connection()
	connection.Ref(1)
	t.Cleanup(func() { _, _ = connection.TryClose() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, f.op.CloseLocal(ctx))
	late := sip.NewResponseFromRequest(f.invite, 200, "OK", nil)
	late.To().Params.Add("tag", "first-after-release")
	late.AppendHeader(f.invite.Contact().Clone())
	_, err := f.peer.WriteTo([]byte(late.String()), f.address)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
		return err == nil && stored.Steps[0].KnownBranch != nil
	}, time.Second, time.Millisecond)
	stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, "first-after-release", stored.Steps[0].KnownBranch.Identity.RemoteTag)
	require.NotEmpty(t, stored.Steps[0].BranchInventoryFault)
	require.Empty(t, stored.Steps[0].KnownBranch.CleanupAttempts)
	require.Empty(t, f.op.owned.ObservedBranches().Responses)
	require.Error(t, f.op.CleanupKnownBranch(ctx))
	f.noACK(t)
}

func testPlaybackIntentLateBranch(t *testing.T, cleaned bool) {
	unhandled := make(chan *sip.Response, 1)
	f := newPlaybackOperationUDPFixture(t, sipgo.WithUserAgentTransactionLayerOptions(
		sip.WithTransactionLayerUnhandledResponseHandler(func(r *sip.Response) { unhandled <- r }),
	))
	awaitCleanupFirstBranch(t, f)
	connection := f.op.owned.Transaction().Connection()
	connection.Ref(1)
	t.Cleanup(func() { _, _ = connection.TryClose() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if cleaned {
		finished := make(chan error, 1)
		go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
		_, _ = readCleanupRequest(t, f.peer)
		bye, address := readCleanupRequest(t, f.peer)
		_, err := f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
		require.NoError(t, err)
		require.NoError(t, <-finished)
	} else {
		require.NoError(t, f.op.CloseLocal(ctx))
	}
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	f.op.factMu.Lock()
	late := f.op.first.Clone()
	f.op.factMu.Unlock()
	late.To().Params.Add("tag", "late-after-release")
	_, err := f.peer.WriteTo([]byte(late.String()), f.address)
	require.NoError(t, err)
	select {
	case r := <-unhandled:
		require.Equal(t, "late-after-release", singlePlaybackBranchTag(r.To().Params))
	case <-ctx.Done():
		t.Fatal("late response did not reach the real receive transport")
	}
	require.Eventually(t, func() bool {
		stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
		return err == nil && len(stored.Steps[0].AdditionalBranches) == 1
	}, time.Second, time.Millisecond, "background observation persists without a new Close/Cleanup call")
	closeErr := f.op.CloseLocal(ctx)
	stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Len(t, stored.Steps[0].AdditionalBranches, 1, "actual late branch remains durable even after local release")
	require.Equal(t, "late-after-release", stored.Steps[0].AdditionalBranches[0].Identity.RemoteTag)
	require.Empty(t, stored.Steps[0].AdditionalBranches[0].CleanupAttempts)
	require.Equal(t, "owned-device", stored.Steps[0].KnownBranch.Identity.RemoteTag)
	require.Equal(t, playauth.IntentDispatched, stored.Intent.State)
	require.ErrorIs(t, closeErr, ErrPlaybackCleanupUnknown)
	require.Error(t, f.op.CleanupKnownBranch(ctx), "observation cannot revive the released owner's sending permission")
	f.noACK(t)
}
