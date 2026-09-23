package uac

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func TestPlaybackShutdownDrainsOriginalButDoesNotCompleteDevice(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := acceptedPlaybackINFOFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	require.NoError(t, f.u.ShutdownPlaybackIntents(ctx))
	require.NoError(t, f.u.ShutdownPlaybackIntents(ctx))
	select {
	case <-f.op.quarantineReaderDone:
	default:
		t.Fatal("shutdown must join the final SQL reader")
	}
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
	require.Equal(t, playauth.SIPBranchObserverIncomplete, loaded.Steps[0].BranchInventoryFault)
	require.Error(t, playauth.NewDeviceCleanupStore(f.db).Authorize(ctx, f.id.DeviceCode, 2))
	_, err = f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 1)
	require.Error(t, err)
	_, err = f.u.StartPlaybackRecovery(ctx, playauth.NewDeviceCleanupStore(f.db), f.store, f.barrier, nil)
	require.Error(t, err)
	requirePlaybackNoPacket(t, f.peer)
}

func TestPlaybackShutdownConcurrentWithOriginalStart(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := newPlaybackOperationPreparedUDPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	start := make(chan struct{})
	closed := make(chan error, 20)
	for n := 0; n < 20; n++ {
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _ = f.op.Start(ctx) }()
		go func() { defer wg.Done(); <-start; closed <- f.u.ShutdownPlaybackIntents(ctx) }()
	}
	close(start)
	wg.Wait()
	close(closed)
	for err := range closed {
		require.NoError(t, err)
	}
	select {
	case <-f.op.owned.Quiesced():
	default:
		t.Fatal("all shutdown waiters must join actual network work")
	}
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Len(t, loaded.Steps, 1)
	require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
	require.Nil(t, loaded.Steps[0].KnownBranch)
	// Any queued packets may be legitimate retransmissions of the single
	// winning original INVITE. No teardown or newly authorized request exists.
	f.noACK(t)
	requirePlaybackNoPacket(t, f.peer)
}

func TestPlaybackShutdownWaitsForEveryPublishedInitializerAndRunner(t *testing.T) {
	for _, kind := range []string{"original", "observation", "recovery", "scan", "worker"} {
		t.Run(kind, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}

			f, stepID := recoveredPlaybackUDPFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if kind == "scan" || kind == "worker" {
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
			}
			entered, release := blockPlaybackInitialization(t, f.db, false)
			finished := make(chan error, 1)
			joined := make(chan struct{})
			go func() {
				defer close(joined)
				var err error
				switch kind {
				case "original":
					in := validPlaybackInvite()
					in.Transport, in.Destination = "UDP", f.peer.LocalAddr().String()
					_, err = f.u.beginPlaybackIntentOperation(ctx, f.store, f.barrier, f.id, 5, strings.Repeat("c", 32), in)
				case "observation":
					_, err = f.u.beginRecoveredPlaybackObservation(ctx, f.store, f.barrier, f.id, stepID)
				case "recovery":
					_, err = f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
				case "scan":
					_, err = f.u.RecoverPlaybackIntents(ctx, f.store, f.barrier, 1, f.id.DeviceCode, 2, "", 1)
				case "worker":
					_, err = f.u.StartPlaybackRecovery(ctx, playauth.NewDeviceCleanupStore(f.db), f.store, f.barrier, nil)
					if err == nil {
						f.u.playbackIntentMu.Lock()
						w := f.u.playbackRecoveryWorker
						f.u.playbackIntentMu.Unlock()
						if w != nil {
							<-w.done
						}
					}
				}
				finished <- err
			}()
			defer func() { release(); <-joined; _ = f.u.ShutdownPlaybackIntents(context.Background()) }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("initializer/runner did not reach its query")
			}
			closeCtx, stop := context.WithTimeout(ctx, 15*time.Millisecond)
			defer stop()
			require.ErrorIs(t, f.u.ShutdownPlaybackIntents(closeCtx), context.DeadlineExceeded)
			select {
			case <-joined:
				t.Fatal("a deadline cannot substitute for the actual query returning")
			default:
			}
			release()
			if kind != "worker" {
				require.Error(t, <-finished, "initialization cancellation must reach the real SQL call")
			} else {
				require.NoError(t, <-finished)
			}
			require.NoError(t, f.u.ShutdownPlaybackIntents(ctx))
			_, err := f.u.beginRecoveredPlaybackObservation(ctx, f.store, f.barrier, f.id, stepID)
			require.ErrorIs(t, err, ErrPlaybackUnavailable)
			_, err = f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
			require.ErrorIs(t, err, ErrPlaybackUnavailable)
			requirePlaybackNoPacket(t, f.peer)
		})
	}
}

func TestPlaybackShutdownPreparedOriginalHasNoRemoteGap(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := newPlaybackOperationPreparedUDPFixture(t)
	ctx := context.Background()
	before, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.NoError(t, f.u.ShutdownPlaybackIntents(ctx))
	after, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	require.Error(t, f.op.Start(ctx))
	requirePlaybackNoPacket(t, f.peer)
}

func TestPlaybackShutdownInterruptsActiveINFOAndCleanup(t *testing.T) {
	t.Run("info", func(t *testing.T) {
		if !authoritytest.InProcess(t) {
			return
		}

		f := acceptedPlaybackINFOFixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		finished, joined := make(chan error, 1), make(chan struct{})
		go func() {
			defer close(joined)
			finished <- f.op.SendINFO(ctx, playauth.DeviceSIPINFOCommand{Action: "pause"})
		}()
		defer func() { cancel(); <-joined }()
		request, _ := readCleanupRequest(t, f.peer)
		require.Equal(t, sip.INFO, request.Method)
		require.NoError(t, f.u.ShutdownPlaybackIntents(ctx))
		require.Error(t, <-finished)
		loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
		require.NoError(t, err)
		steps := loaded.Steps[0].KnownBranch.InfoSteps
		require.Len(t, steps, 1)
		require.Nil(t, steps[0].Response)
		require.NotNil(t, steps[0].LocalQuiescedAt)
		requirePlaybackNoPacket(t, f.peer)
	})
	t.Run("cleanup", func(t *testing.T) {
		if !authoritytest.InProcess(t) {
			return
		}

		f, stepID := recoveredPlaybackUDPFixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
		_, err := f.u.beginRecoveredPlaybackObservation(ctx, f.store, f.barrier, f.id, stepID)
		require.NoError(t, err)
		r, err := f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
		require.NoError(t, err)
		finished, joined := make(chan error, 1), make(chan struct{})
		go func() { defer close(joined); finished <- r.Run(ctx) }()
		defer func() { cancel(); <-joined; _ = f.u.ShutdownPlaybackIntents(context.Background()) }()
		ack, _ := readCleanupRequest(t, f.peer)
		require.Equal(t, sip.ACK, ack.Method)
		bye, _ := readCleanupRequest(t, f.peer)
		require.Equal(t, sip.BYE, bye.Method)
		require.NoError(t, f.u.ShutdownPlaybackIntents(ctx))
		require.Error(t, <-finished)
		loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
		require.NoError(t, err)
		attempts := loaded.Steps[0].KnownBranch.CleanupAttempts
		require.Len(t, attempts, 1)
		require.Nil(t, attempts[0].Response)
		require.NotNil(t, attempts[0].LocalQuiescedAt)
		require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
		requirePlaybackNoPacket(t, f.peer)
	})
}

func TestPlaybackShutdownRetriesFinalFactsAfterReadersExit(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := acceptedPlaybackINFOFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var fail atomic.Bool
	fail.Store(true)
	require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register("fixture:shutdown", func(tx *gorm.DB) {
		if fail.Load() && tx.Statement.Table == "gb_device_operation_intent" {
			tx.AddError(errors.New("fixture final SQL unavailable"))
		}
	}))
	defer func() {
		fail.Store(false)
		_ = f.u.ShutdownPlaybackIntents(context.Background())
		_ = f.db.Callback().Update().Remove("fixture:shutdown")
	}()
	require.Error(t, f.u.ShutdownPlaybackIntents(ctx))
	f.u.playbackIntentMu.Lock()
	retained := f.u.playbackIntents[f.id.OperationID]
	f.u.playbackIntentMu.Unlock()
	require.Same(t, f.op, retained)
	select {
	case <-f.op.quarantineReaderDone:
	default:
		t.Fatal("reader is joined even when its final persistence failed")
	}
	fail.Store(false)
	require.NoError(t, f.u.ShutdownPlaybackIntents(ctx))
	requirePlaybackNoPacket(t, f.peer)
}
