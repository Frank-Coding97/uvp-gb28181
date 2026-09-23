package uac

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

type scanRTPResolver func(context.Context, playauth.DeviceRTPResourceIdentity) (playauth.RTPCleanupRuntime, error)

func (f scanRTPResolver) ResolveRTPCleanup(ctx context.Context, id playauth.DeviceRTPResourceIdentity) (playauth.RTPCleanupRuntime, error) {
	return f(ctx, id)
}

type scanRTPRuntime struct {
	resource, ingress func(context.Context) (string, error)
	release           func()
}

func (r *scanRTPRuntime) CloseResource(ctx context.Context) (string, error) { return r.resource(ctx) }
func (r *scanRTPRuntime) CloseIngress(ctx context.Context) (string, error)  { return r.ingress(ctx) }
func (r *scanRTPRuntime) Release()                                          { r.release() }

func playbackRTPScanFixture(t *testing.T, beforeTransfer ...func(*playauth.DeviceOperationBarrier, playauth.DeviceOperationIntentIdentity)) (*UAC, *gorm.DB, *playauth.DeviceOperationIntentStore, *playauth.DeviceOperationBarrier, playauth.DeviceOperationIntentIdentity, playauth.DeviceRTPResourceIdentity) {
	t.Helper()
	u, db, store, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	ctx := context.Background()
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device_operation_intent ADD COLUMN rtp_steps_json TEXT NULL").Error)
	stepID := strings.Repeat("b", 32)
	resourceID, err := playauth.NewDeviceRTPResourceID(id.OperationID, stepID, time.Now())
	require.NoError(t, err)
	identity := playauth.DeviceRTPResourceIdentity{StepID: stepID, NodePK: 7, NodeUUID: "scan-node", NodeRevision: 1, BootNonce: strings.Repeat("c", 32), ResourceID: resourceID, VHost: "__defaultVhost__", App: "rtp", Stream: "scan-original", LocalIP: "127.0.0.1", SSRC: 1234}
	out, err := store.AddRTPResourceStep(ctx, id, 2, identity)
	require.NoError(t, err)
	_, old, err := store.DispatchRTPResourceWork(ctx, id, out.Intent.RowVersion, stepID)
	require.NoError(t, err)
	require.NoError(t, old.Quiesce(ctx))
	b := newAuthorizedBarrierTest(t, db)
	for _, prepare := range beforeTransfer {
		prepare(b, id)
	}
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, u.ShutdownPlaybackIntents(ctx))
	})
	return u, db, store, b, id, identity
}

func TestPlaybackRTPRecoveryScanContinuesSameOwnerAfterSQLFailure(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, db, store, b, id, identity := playbackRTPScanFixture(t)
	ctx := context.Background()
	var resource, ingress, resolved, freed atomic.Int32
	runtime := &scanRTPRuntime{resource: func(context.Context) (string, error) { resource.Add(1); return "close_pending", nil }, ingress: func(context.Context) (string, error) { ingress.Add(1); return "rtp_ingress_drained", nil }, release: func() { freed.Add(1) }}
	require.NoError(t, u.ConfigurePlaybackRTPCleanup(ctx, store, b, scanRTPResolver(func(_ context.Context, i playauth.DeviceRTPResourceIdentity) (playauth.RTPCleanupRuntime, error) {
		require.Equal(t, identity, i)
		resolved.Add(1)
		return runtime, nil
	})))
	require.NoError(t, db.Exec(`CREATE TRIGGER scan_rtp_outcome BEFORE UPDATE ON gb_device_operation_intent WHEN json_extract(NEW.rtp_steps_json,'$.steps[0].cleanup.currentCall.outcome') IS NOT NULL BEGIN SELECT RAISE(FAIL,'fixture outcome SQL failure'); END`).Error)
	p, err := u.RecoverPlaybackIntents(ctx, store, b, 1, id.DeviceCode, 2, "", 16)
	require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
	require.Equal(t, 1, p.Pending, "SIP unknown does not skip RTP")
	require.EqualValues(t, 1, resource.Load())
	require.Zero(t, ingress.Load())
	require.Zero(t, freed.Load())
	require.Len(t, u.playbackRTPCleanup.owners, 1)
	key := id.OperationID + ":" + identity.StepID
	owner := u.playbackRTPCleanup.owners[key]
	_, _ = u.RecoverPlaybackIntents(ctx, store, b, 1, id.DeviceCode, 2, "", 16)
	require.Same(t, owner, u.playbackRTPCleanup.owners[key])
	require.EqualValues(t, 1, resource.Load())
	require.NoError(t, db.Exec("DROP TRIGGER scan_rtp_outcome").Error)
	p, err = u.RecoverPlaybackIntents(ctx, store, b, 1, id.DeviceCode, 2, "", 16)
	require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
	require.Equal(t, 1, p.Pending)
	require.EqualValues(t, 1, resource.Load())
	require.EqualValues(t, 1, ingress.Load())
	require.EqualValues(t, 1, resolved.Load())
	require.EqualValues(t, 1, freed.Load())
	require.Empty(t, u.playbackRTPCleanup.owners)
	out, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.EqualValues(t, 1, out.Steps[0].Recovery.Generation)
	require.Equal(t, playauth.IntentDispatched, out.Intent.State)
	state, err := playauth.NewDeviceCleanupStore(db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.EqualValues(t, 1, state.CleanupCompletedEpoch)
}

func TestPlaybackRTPRecoveryWorkerStopJoinsAndRestartKeepsOwner(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, db, store, b, id, identity := playbackRTPScanFixture(t)
	ctx := context.Background()
	entered, release := make(chan struct{}), make(chan struct{})
	var resource, ingress, resolved, freed atomic.Int32
	runtime := &scanRTPRuntime{resource: func(context.Context) (string, error) {
		resource.Add(1)
		close(entered)
		<-release
		return "close_pending", nil
	}, ingress: func(context.Context) (string, error) { ingress.Add(1); return "rtp_ingress_drained", nil }, release: func() { freed.Add(1) }}
	require.NoError(t, u.ConfigurePlaybackRTPCleanup(ctx, store, b, scanRTPResolver(func(context.Context, playauth.DeviceRTPResourceIdentity) (playauth.RTPCleanupRuntime, error) {
		resolved.Add(1)
		return runtime, nil
	})))
	devices := playauth.NewDeviceCleanupStore(db)
	stop, err := u.StartPlaybackRecovery(ctx, devices, store, b, nil)
	require.NoError(t, err)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		require.NoError(t, stop(ctx))
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("RTP worker did not start")
	}
	short, cancel := context.WithTimeout(ctx, 15*time.Millisecond)
	require.ErrorIs(t, stop(short), context.DeadlineExceeded)
	cancel()
	_, err = u.StartPlaybackRecovery(ctx, devices, store, b, nil)
	require.Error(t, err)
	require.Zero(t, freed.Load())
	close(release)
	require.NoError(t, stop(ctx))
	key := id.OperationID + ":" + identity.StepID
	require.NotNil(t, u.playbackRTPCleanup.owners[key])
	require.NoError(t, u.playbackRTPCleanup.ctx.Err(), "worker stop cannot cancel controller lifetime")
	ticked := make(chan struct{}, 1)
	stopAgain, err := u.StartPlaybackRecovery(ctx, devices, store, b, func(PlaybackRecoveryTick, error) {
		select {
		case ticked <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)
	defer stopAgain(ctx)
	select {
	case <-ticked:
	case <-time.After(3 * time.Second):
		t.Fatal("replacement worker did not tick")
	}
	require.NoError(t, stopAgain(ctx))
	require.Empty(t, u.playbackRTPCleanup.owners)
	require.EqualValues(t, 1, resource.Load())
	require.EqualValues(t, 1, ingress.Load())
	require.EqualValues(t, 1, resolved.Load())
	require.EqualValues(t, 1, freed.Load())
}

func TestPlaybackRTPRecoveryShutdownRetainsFailedFinalFacts(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, db, store, b, id, _ := playbackRTPScanFixture(t)
	ctx := context.Background()
	var calls, freed atomic.Int32
	runtime := &scanRTPRuntime{resource: func(context.Context) (string, error) { calls.Add(1); return "close_pending", nil }, ingress: func(context.Context) (string, error) { calls.Add(1); return "rtp_ingress_drained", nil }, release: func() { freed.Add(1) }}
	resolver := scanRTPResolver(func(context.Context, playauth.DeviceRTPResourceIdentity) (playauth.RTPCleanupRuntime, error) {
		return runtime, nil
	})
	require.NoError(t, u.ConfigurePlaybackRTPCleanup(ctx, store, b, resolver))
	require.NoError(t, db.Exec(`CREATE TRIGGER scan_rtp_quiesce BEFORE UPDATE ON gb_device_operation_intent WHEN json_extract(NEW.rtp_steps_json,'$.steps[0].cleanup.localQuiescedAt') IS NOT NULL BEGIN SELECT RAISE(FAIL,'fixture final SQL failure'); END`).Error)
	_, _ = u.RecoverPlaybackIntents(ctx, store, b, 1, id.DeviceCode, 2, "", 16)
	require.EqualValues(t, 2, calls.Load())
	require.EqualValues(t, 1, freed.Load())
	require.Error(t, u.ShutdownPlaybackIntents(ctx))
	require.Len(t, u.playbackRTPCleanup.owners, 1)
	require.Error(t, u.ConfigurePlaybackRTPCleanup(ctx, store, b, resolver))
	require.NoError(t, db.Exec("DROP TRIGGER scan_rtp_quiesce").Error)
	require.NoError(t, u.ShutdownPlaybackIntents(ctx))
	require.Empty(t, u.playbackRTPCleanup.owners)
	require.EqualValues(t, 2, calls.Load())
	require.EqualValues(t, 1, freed.Load())
	require.NoError(t, b.WaitBefore(ctx, 1, 2))
}

func TestPlaybackRTPRecoveryPageTimeoutDoesNotCancelController(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, _, store, b, id, _ := playbackRTPScanFixture(t)
	ctx := context.Background()
	var resource, ingress, freed atomic.Int32
	runtime := &scanRTPRuntime{
		resource: func(ctx context.Context) (string, error) { resource.Add(1); <-ctx.Done(); return "", ctx.Err() },
		ingress:  func(context.Context) (string, error) { ingress.Add(1); return "rtp_ingress_drained", nil },
		release:  func() { freed.Add(1) },
	}
	require.NoError(t, u.ConfigurePlaybackRTPCleanup(ctx, store, b, scanRTPResolver(func(context.Context, playauth.DeviceRTPResourceIdentity) (playauth.RTPCleanupRuntime, error) {
		return runtime, nil
	})))
	page, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	_, err := u.RecoverPlaybackIntents(page, store, b, 1, id.DeviceCode, 2, "", 16)
	require.Error(t, err)
	cancel()
	require.EqualValues(t, 1, resource.Load())
	require.Len(t, u.playbackRTPCleanup.owners, 1)
	require.NoError(t, u.playbackRTPCleanup.ctx.Err())
	_, _ = u.RecoverPlaybackIntents(ctx, store, b, 1, id.DeviceCode, 2, "", 16)
	require.EqualValues(t, 1, resource.Load())
	require.EqualValues(t, 1, ingress.Load())
	require.EqualValues(t, 1, freed.Load())
	require.Empty(t, u.playbackRTPCleanup.owners)
}

func TestPlaybackRTPRecoveryResolverFailureQuiescesAndCannotRebind(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, db, store, b, id, _ := playbackRTPScanFixture(t)
	ctx := context.Background()
	var calls atomic.Int32
	resolver := scanRTPResolver(func(context.Context, playauth.DeviceRTPResourceIdentity) (playauth.RTPCleanupRuntime, error) {
		calls.Add(1)
		return nil, errors.New("untrusted fixture")
	})
	require.NoError(t, u.ConfigurePlaybackRTPCleanup(ctx, store, b, resolver))
	require.Error(t, u.ConfigurePlaybackRTPCleanup(ctx, store, b, resolver))
	_, err := u.RecoverPlaybackIntents(ctx, playauth.NewDeviceOperationIntentStore(db), b, 1, id.DeviceCode, 2, "", 16)
	require.Error(t, err)
	require.Zero(t, calls.Load(), "a different factory cannot replace the bound store")
	_, err = u.RecoverPlaybackIntents(ctx, store, b, 1, id.DeviceCode, 2, "", 16)
	require.Error(t, err)
	require.EqualValues(t, 1, calls.Load())
	require.Empty(t, u.playbackRTPCleanup.owners, "cached resolver error must not strand a run-phase owner")
	require.NoError(t, b.WaitBefore(ctx, 1, 2))
}

func TestPlaybackRTPRecoveryControllerCapacityDoesNotEvictCreators(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, _, store, b, id, identity := playbackRTPScanFixture(t)
	ctx := context.Background()
	resolver := scanRTPResolver(func(context.Context, playauth.DeviceRTPResourceIdentity) (playauth.RTPCleanupRuntime, error) {
		t.Error("capacity granted resolution")
		return nil, errors.New("unexpected")
	})
	require.NoError(t, u.ConfigurePlaybackRTPCleanup(ctx, store, b, resolver))
	c := u.playbackRTPCleanup
	// Capacity unit fixture: real opaque reservations on another device,
	// deliberately before SQL preparation. No fake network authority is minted.
	for n := 1; n <= 64; n++ {
		other := id
		other.OperationID, other.DevicePK = fmt.Sprintf("%032x", n), 2
		work, err := b.ReserveRTPCleanup(c.ctx, store, other, identity.StepID)
		require.NoError(t, err)
		key := other.OperationID + ":" + identity.StepID
		c.owners[key] = &playbackRTPOwner{id: other, key: key, work: work, phase: playbackRTPQuiesce}
	}
	_, err := u.RecoverPlaybackIntents(ctx, store, b, 1, id.DeviceCode, 2, "", 16)
	require.Error(t, err)
	require.Len(t, c.owners, 64)
	require.NotContains(t, c.owners, id.OperationID+":"+identity.StepID)
	require.NoError(t, u.ShutdownPlaybackIntents(ctx))
	require.Empty(t, c.owners)
}

func TestPlaybackRTPRecoveryCancelledPrepareQuiescesBeforeNewBatch(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	ctx := context.Background()
	var lease playauth.DeviceOperationLease
	u, _, store, b, id, _ := playbackRTPScanFixture(t, func(b *playauth.DeviceOperationBarrier, id playauth.DeviceOperationIntentIdentity) {
		var err error
		lease, err = b.BeginEpoch(ctx, id.DeviceCode, id.DeviceEpoch)
		require.NoError(t, err)
	})
	defer lease.Release()
	var resolved atomic.Int32
	runtime := &scanRTPRuntime{resource: func(context.Context) (string, error) { return "close_pending", nil }, ingress: func(context.Context) (string, error) { return "rtp_ingress_drained", nil }, release: func() {}}
	require.NoError(t, u.ConfigurePlaybackRTPCleanup(ctx, store, b, scanRTPResolver(func(context.Context, playauth.DeviceRTPResourceIdentity) (playauth.RTPCleanupRuntime, error) {
		resolved.Add(1)
		return runtime, nil
	})))
	page, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	_, err := u.RecoverPlaybackIntents(page, store, b, 1, id.DeviceCode, 2, "", 16)
	require.Error(t, err)
	cancel()
	require.Zero(t, resolved.Load())
	require.Len(t, u.playbackRTPCleanup.owners, 1)
	lease.Release()
	_, _ = u.RecoverPlaybackIntents(ctx, store, b, 1, id.DeviceCode, 2, "", 16)
	require.Empty(t, u.playbackRTPCleanup.owners, "failed Prepare safely retires before any new authorization")
	require.EqualValues(t, 1, resolved.Load(), "retired failed Prepare must allow new admission in this page, before SIP can consume another sweep")
}

func TestPlaybackRTPRecoveryShutdownJoinsLateResolverAndRetainsRuntime(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, _, store, b, id, _ := playbackRTPScanFixture(t)
	ctx := context.Background()
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var freed atomic.Int32
	runtime := &scanRTPRuntime{resource: func(context.Context) (string, error) { t.Error("shutdown dispatched RTP"); return "", nil }, ingress: func(context.Context) (string, error) { t.Error("shutdown dispatched ingress"); return "", nil }, release: func() { freed.Add(1) }}
	require.NoError(t, u.ConfigurePlaybackRTPCleanup(ctx, store, b, scanRTPResolver(func(context.Context, playauth.DeviceRTPResourceIdentity) (playauth.RTPCleanupRuntime, error) {
		close(entered)
		<-release
		return runtime, nil
	})))
	go func() {
		_, err := u.RecoverPlaybackIntents(ctx, store, b, 1, id.DeviceCode, 2, "", 16)
		finished <- err
	}()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("resolver not reached")
	}
	short, cancel := context.WithTimeout(ctx, 15*time.Millisecond)
	require.ErrorIs(t, u.ShutdownPlaybackIntents(short), context.DeadlineExceeded)
	cancel()
	require.Zero(t, freed.Load())
	close(release)
	require.Error(t, <-finished)
	require.NoError(t, u.ShutdownPlaybackIntents(ctx))
	require.EqualValues(t, 1, freed.Load())
	require.Empty(t, u.playbackRTPCleanup.owners)
	require.NoError(t, b.WaitBefore(ctx, 1, 2))
}

func TestPlaybackRTPRecoverySIPTimeoutCannotStarveRTPNextPage(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f, _ := recoveredPlaybackUDPFixture(t)
	ctx := context.Background()
	defer func() { closeScanObservations(t, f.u); require.NoError(t, f.u.ShutdownPlaybackIntents(ctx)) }()
	require.NoError(t, f.db.Exec("ALTER TABLE gb_device_operation_intent ADD COLUMN rtp_steps_json TEXT NULL").Error)
	out, err := f.store.LoadRTPResourceSteps(ctx, f.id)
	require.NoError(t, err)
	stepID := strings.Repeat("d", 32)
	resourceID, err := playauth.NewDeviceRTPResourceID(f.id.OperationID, stepID, time.Now())
	require.NoError(t, err)
	i := playauth.DeviceRTPResourceIdentity{StepID: stepID, NodePK: 7, NodeUUID: "scan-sip-node", NodeRevision: 1, BootNonce: strings.Repeat("e", 32), ResourceID: resourceID, VHost: "__defaultVhost__", App: "rtp", Stream: "original-sip-rtp", LocalIP: "127.0.0.1", SSRC: 1234}
	out, err = f.store.AddRTPResourceStep(ctx, f.id, out.Intent.RowVersion, i)
	require.NoError(t, err)
	_, original, err := f.store.DispatchRTPResourceWork(ctx, f.id, out.Intent.RowVersion, stepID)
	require.NoError(t, err)
	require.NoError(t, original.Quiesce(ctx))
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	var resolved, freed atomic.Int32
	runtime := &scanRTPRuntime{resource: func(context.Context) (string, error) { return "close_pending", nil }, ingress: func(context.Context) (string, error) { return "rtp_ingress_drained", nil }, release: func() { freed.Add(1) }}
	require.NoError(t, f.u.ConfigurePlaybackRTPCleanup(ctx, f.store, f.barrier, scanRTPResolver(func(context.Context, playauth.DeviceRTPResourceIdentity) (playauth.RTPCleanupRuntime, error) {
		resolved.Add(1)
		return runtime, nil
	})))
	page, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.u.RecoverPlaybackIntents(page, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 16)
		done <- err
	}()
	ack, _ := readCleanupRequest(t, f.peer)
	bye, _ := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.ACK, ack.Method)
	require.Equal(t, sip.BYE, bye.Method)
	// Deliberately omit a BYE response. SIP consumes its half-page budget.
	require.ErrorIs(t, <-done, ErrPlaybackCleanupUnknown)
	if resolved.Load() == 0 {
		next, cancelNext := context.WithTimeout(ctx, time.Second)
		defer cancelNext()
		_, err = f.u.RecoverPlaybackIntents(next, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 16)
		require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
	}
	require.EqualValues(t, 1, resolved.Load(), "SIP timeout/resume must not recreate the same starvation cycle")
	require.EqualValues(t, 1, freed.Load())
	require.Empty(t, f.u.playbackRTPCleanup.owners)
	out, err = f.store.LoadRTPResourceSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, playauth.IntentDispatched, out.Intent.State)
}
