package uac

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func closeScanObservations(t *testing.T, u *UAC) {
	t.Helper()
	u.playbackIntentMu.Lock()
	var observations []*playbackRecoveredObservation
	for _, o := range u.playbackObservations {
		observations = append(observations, o)
	}
	u.playbackIntentMu.Unlock()
	for _, o := range observations {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		require.NoError(t, o.CloseLocal(ctx))
		cancel()
	}
}

func TestPlaybackRecoveryScanPageAndTargetBoundary(t *testing.T) {
	f, _ := recoveredPlaybackUDPFixture(t)
	ctx := context.Background()
	for _, letter := range []string{"1", "2", "3"} {
		id := f.id
		id.OperationID = strings.Repeat(letter, 32)
		_, err := f.store.Reserve(ctx, id)
		require.NoError(t, err)
	}
	// A caller-supplied future epoch cannot cancel current reservations.
	_, err := f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 2)
	require.Error(t, err)
	var count int64
	require.NoError(t, f.db.Model(&playauth.DeviceOperationIntent{}).Where("state = ?", playauth.IntentCancelled).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch = 2 WHERE id = 1").Error)
	p, err := f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 2)
	require.NoError(t, err)
	require.Equal(t, 2, p.Scanned)
	require.Equal(t, 2, p.Cancelled)
	require.Equal(t, strings.Repeat("2", 32), p.NextAfter)
	p, err = f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 2, p.NextAfter, 1)
	require.NoError(t, err)
	require.Equal(t, 1, p.Cancelled)
	// Empty/wrong identity pages cannot advance the device watermark.
	_, err = f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 2, f.id.DeviceCode, 2, "", 1)
	require.Error(t, err)
	p, err = f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 2, strings.Repeat("f", 32), 1)
	require.NoError(t, err)
	require.Zero(t, p.Scanned)
	state, err := playauth.NewDeviceCleanupStore(f.db).Load(ctx, f.id.DeviceCode)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.CleanupCompletedEpoch)
	f.noACK(t)
}

func TestPlaybackRecoveryScanActualMultipleBranchesAndRepeat(t *testing.T) {
	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer closeScanObservations(t, f.u)
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	b := loaded.Steps[0].KnownBranch.Identity
	b.RemoteTag = "second-recovery-branch"
	_, err = f.store.ObserveSIPAdditionalBranch(ctx, f.id, loaded.Intent.RowVersion, b)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch = 2 WHERE id = 1").Error)
	result := make(chan error, 1)
	go func() {
		_, err := f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 1)
		result <- err
	}()
	for _, tag := range []string{"recovery-remote", b.RemoteTag} {
		ack, _ := readCleanupRequest(t, f.peer)
		bye, address := readCleanupRequest(t, f.peer)
		require.Equal(t, sip.ACK, ack.Method)
		require.Equal(t, sip.BYE, bye.Method)
		gotTag, _ := bye.To().Params.Get("tag")
		require.Equal(t, tag, gotTag)
		// A concurrent page, even for a later target, must not overlap.
		_, err := f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 3, "", 1)
		require.Error(t, err)
		_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
		require.NoError(t, err)
	}
	require.ErrorIs(t, <-result, ErrPlaybackCleanupUnknown)
	loaded, err = f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, stepID, loaded.Steps[0].Identity.StepID)
	for _, branch := range playbackObservedBranchRecords(loaded.Steps[0]) {
		require.Len(t, branch.CleanupAttempts, 1)
		require.NotNil(t, branch.CleanupAttempts[0].Response)
		require.NotNil(t, branch.CleanupAttempts[0].LocalQuiescedAt)
	}
	p, err := f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 1)
	require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
	require.Equal(t, 1, p.Pending)
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	require.Error(t, playauth.NewDeviceCleanupStore(f.db).Authorize(ctx, f.id.DeviceCode, 2))
	f.noACK(t)
	// A new fact after the first sweep is handled only by a fresh sweep.
	third := b
	third.RemoteTag = "late-third"
	_, err = f.store.ObserveSIPAdditionalBranch(ctx, f.id, loaded.Intent.RowVersion, third)
	require.NoError(t, err)
	go func() {
		_, err := f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 1)
		result <- err
	}()
	ack, _ := readCleanupRequest(t, f.peer)
	bye, address := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.ACK, ack.Method)
	tag, _ := bye.To().Params.Get("tag")
	require.Equal(t, third.RemoteTag, tag)
	_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
	require.NoError(t, err)
	require.ErrorIs(t, <-result, ErrPlaybackCleanupUnknown)
	f.noACK(t)
}

func TestPlaybackRecoveryScanMissingMaterialAndOriginalOwner(t *testing.T) {
	t.Run("missing-or-unsupported", func(t *testing.T) {
		u, db, store, id, observer := playbackIntentStoreFixture(t)
		ctx := context.Background()
		other := id
		other.OperationID = strings.Repeat("1", 32)
		other.Kind = "live"
		_, err := store.Reserve(ctx, other)
		require.NoError(t, err)
		_, err = store.Dispatch(ctx, other, 1)
		require.NoError(t, err)
		require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch = 2 WHERE id = 1").Error)
		barrier := playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))
		p, err := u.RecoverPlaybackIntents(ctx, store, barrier, 1, id.DeviceCode, 2, "", 1000)
		require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
		require.Equal(t, 2, p.Scanned)
		require.Equal(t, 2, p.Pending)
		require.Zero(t, p.Cancelled)
		require.Nil(t, observer.request)
		require.Empty(t, u.playbackRecoveries)
		require.Empty(t, u.playbackObservations)
	})
	t.Run("original-owner", func(t *testing.T) {
		f := newPlaybackOperationUDPFixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch = 2 WHERE id = 1").Error)
		p, err := f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 1)
		require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
		require.Equal(t, 1, p.Pending)
		require.Empty(t, f.u.playbackRecoveries)
		require.Empty(t, f.u.playbackObservations)
		f.noACK(t)
	})
}

func TestPlaybackRecoveryScanResumesRetainedFactsWithoutResend(t *testing.T) {
	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer closeScanObservations(t, f.u)
	// Fail the response-fact commit once; the concrete owner must survive.
	faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
	faultDB.Statement.ConnPool = &playbackOperationCommitFault{ConnPool: f.db.Statement.ConnPool, failAt: 4}
	r, err := f.u.beginRecoveredPlaybackCleanup(ctx, playauth.NewDeviceOperationIntentStore(faultDB), f.barrier, f.id, stepID, "recovery-remote")
	require.NoError(t, err)
	defer r.CloseLocal(ctx)
	result := make(chan error, 1)
	go func() { result <- r.Run(ctx) }()
	readCleanupRequest(t, f.peer)
	bye, address := readCleanupRequest(t, f.peer)
	_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
	require.NoError(t, err)
	require.Error(t, <-result)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch = 2 WHERE id = 1").Error)
	p, err := f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 1)
	require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
	require.Equal(t, 1, p.Pending)
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	a := loaded.Steps[0].KnownBranch.CleanupAttempts
	require.Len(t, a, 1)
	require.NotNil(t, a[0].Response)
	require.NotNil(t, a[0].LocalQuiescedAt)
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	f.noACK(t)
}

func TestPlaybackRecoveryScanTimeoutRetainsRunningOwner(t *testing.T) {
	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer closeScanObservations(t, f.u)
	r, err := f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
	require.NoError(t, err)
	defer r.CloseLocal(ctx)
	result := make(chan error, 1)
	go func() { result <- r.Run(ctx) }()
	readCleanupRequest(t, f.peer)
	bye, address := readCleanupRequest(t, f.peer)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch = 2 WHERE id = 1").Error)
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = f.u.RecoverPlaybackIntents(short, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 1)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	stop()
	f.u.playbackIntentMu.Lock()
	retained := f.u.playbackRecoveries[f.id.OperationID]
	f.u.playbackIntentMu.Unlock()
	require.Same(t, r, retained)
	short, stop = context.WithTimeout(ctx, 15*time.Millisecond)
	require.ErrorIs(t, f.barrier.WaitBefore(short, 1, 2), context.DeadlineExceeded)
	stop()
	f.noACK(t)
	_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
	require.NoError(t, err)
	require.NoError(t, <-result)
	_, err = f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 1)
	require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	f.noACK(t)
}
