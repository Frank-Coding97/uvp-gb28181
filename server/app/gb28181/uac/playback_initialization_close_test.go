package uac

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

// Inject at the real transport's synchronous connection-preparation boundary,
// after the durable snapshot exists but before the constructor compares it.
type playbackInitializationLogHook struct{ handle func(slog.Record) }

func (h playbackInitializationLogHook) Enabled(context.Context, slog.Level) bool { return true }
func (h playbackInitializationLogHook) Handle(_ context.Context, r slog.Record) error {
	h.handle(r)
	return nil
}
func (h playbackInitializationLogHook) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h playbackInitializationLogHook) WithGroup(string) slog.Handler      { return h }

func TestPlaybackShutdownCancelsSnapshotMismatchSelfCleanup(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	var u *UAC
	var original playauth.DeviceSIPInviteIdentity
	var injected, blocked atomic.Bool
	hook := playbackInitializationLogHook{handle: func(r slog.Record) {
		if r.Message != "Creating connection" || !injected.CompareAndSwap(false, true) {
			return
		}
		// This callback runs on the initializer itself, before ready. No other
		// goroutine reads these fields until ready has been published.
		u.playbackIntentMu.Lock()
		for _, o := range u.playbackIntents {
			original = o.invite
			o.invite.CallID = "fixture-snapshot-mismatch"
		}
		u.playbackIntentMu.Unlock()
	}}
	var db *gorm.DB
	var store *playauth.DeviceOperationIntentStore
	var id playauth.DeviceOperationIntentIdentity
	u, db, store, id, _ = playbackIntentStoreFixture(t, sipgo.WithUserAgentTransportLayerOptions(sip.WithTransportLayerLogger(slog.New(hook))))
	u.client.TxRequester = nil
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	barrier := newAuthorizedBarrierTest(t, db)
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	in := validPlaybackInvite()
	in.Transport, in.Destination = "UDP", peer.LocalAddr().String()
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fixture:self-cleanup", func(tx *gorm.DB) {
		if tx.Statement.Table != "gb_device_operation_intent" || !injected.Load() || !blocked.CompareAndSwap(false, true) {
			return
		}
		close(entered)
		select {
		case <-tx.Statement.Context.Done():
			close(cancelled)
		case <-release:
		}
		tx.AddError(errors.New("fixture self-cleanup query interrupted"))
	}))
	finished := make(chan error, 1)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		_, err := u.beginPlaybackIntentOperation(context.Background(), store, barrier, id, 2, strings.Repeat("b", 32), in)
		finished <- err
	}()
	defer func() {
		close(release)
		<-joined
		_ = db.Callback().Query().Remove("fixture:self-cleanup")
		u.playbackIntentMu.Lock()
		o := u.playbackIntents[id.OperationID]
		u.playbackIntentMu.Unlock()
		if o != nil {
			o.invite = original
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = u.ShutdownPlaybackIntents(ctx)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("snapshot mismatch did not enter its real self-cleanup query")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.Error(t, u.ShutdownPlaybackIntents(ctx)) // Injected identity/SQL failure is not success.
	select {
	case <-cancelled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("shutdown failed to cancel snapshot mismatch self-cleanup")
	}
	select {
	case err := <-finished:
		require.ErrorIs(t, err, errPlaybackIntentSnapshot)
	case <-time.After(time.Second):
		t.Fatal("failed initializer did not publish ready")
	}
	u.playbackIntentMu.Lock()
	o := u.playbackIntents[id.OperationID]
	u.playbackIntentMu.Unlock()
	require.NotNil(t, o, "failed drain must retain its original concrete owner")
	o.invite = original // Remove only the injected snapshot fault after all readers joined.
	require.NoError(t, u.ShutdownPlaybackIntents(context.Background()))
	require.NoError(t, barrier.WaitBefore(context.Background(), 1, 2))
	requirePlaybackNoPacket(t, peer)
}

// Keep the actual initialization query in flight, even after Close's deadline.
func blockPlaybackInitialization(t *testing.T, db *gorm.DB, fail bool) (<-chan struct{}, func()) {
	t.Helper()
	entered, released := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var blocked atomic.Bool
	release := func() { once.Do(func() { close(released) }) }
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fixture:initialization", func(tx *gorm.DB) {
		if tx.Statement.Table != "gb_device_operation_intent" || !blocked.CompareAndSwap(false, true) {
			return
		}
		close(entered)
		<-released
		if fail {
			tx.AddError(errors.New("fixture initialization failed"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove("fixture:initialization") })
	t.Cleanup(release)
	return entered, release
}

func TestPlaybackObservationCloseDuringInitialization(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("fail=%v", fail), func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}

			f, stepID := recoveredPlaybackUDPFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			entered, release := blockPlaybackInitialization(t, f.db, fail)
			finished := make(chan error, 1)
			joined := make(chan struct{})
			go func() {
				defer close(joined)
				_, err := f.u.beginRecoveredPlaybackObservation(ctx, f.store, f.barrier, f.id, stepID)
				finished <- err
			}()
			defer func() { release(); <-joined; closeScanObservations(t, f.u) }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("initialization did not enter the real query")
			}
			f.u.playbackIntentMu.Lock()
			o := f.u.playbackObservations[f.id.OperationID+":"+stepID]
			f.u.playbackIntentMu.Unlock()
			require.NotNil(t, o)
			closeCtx, stop := context.WithTimeout(ctx, 15*time.Millisecond)
			defer stop()
			require.NotPanics(t, func() {
				require.ErrorIs(t, o.CloseLocal(closeCtx), context.DeadlineExceeded)
			}, "published reservation is not a fully initialized observation")
			release()
			err := <-finished
			if fail {
				require.Error(t, err)
				require.Error(t, o.CloseLocal(ctx))
			} else {
				require.NoError(t, err)
				require.NoError(t, o.CloseLocal(ctx))
				select {
				case <-o.done:
				default:
					t.Fatal("successful Close must join the observation reader")
				}
			}
			requirePlaybackNoPacket(t, f.peer)
		})
	}
}

func TestPlaybackOriginalCloseDuringInitialization(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("fail=%v", fail), func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}

			u, db, store, id, _ := playbackIntentStoreFixture(t)
			u.client.TxRequester = nil
			require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
			barrier := newAuthorizedBarrierTest(t, db)
			peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
			require.NoError(t, err)
			defer peer.Close()
			in := validPlaybackInvite()
			in.Transport, in.Destination = "UDP", peer.LocalAddr().String()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			entered, release := blockPlaybackInitialization(t, db, fail)
			finished := make(chan error, 1)
			joined := make(chan struct{})
			go func() {
				defer close(joined)
				_, err := u.beginPlaybackIntentOperation(ctx, store, barrier, id, 2, strings.Repeat("b", 32), in)
				finished <- err
			}()
			defer func() { release(); <-joined }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("initialization did not enter the real query")
			}
			u.playbackIntentMu.Lock()
			o := u.playbackIntents[id.OperationID]
			u.playbackIntentMu.Unlock()
			require.NotNil(t, o)
			closeCtx, stop := context.WithTimeout(ctx, 15*time.Millisecond)
			defer stop()
			require.ErrorIs(t, o.CloseLocal(closeCtx), context.DeadlineExceeded)
			release()
			err = <-finished
			if fail {
				require.Error(t, err)
				require.Error(t, o.CloseLocal(ctx))
			} else {
				require.NoError(t, err)
				require.Error(t, o.Start(ctx), "a timed-out Close request still forbids later business sends")
				require.NoError(t, o.CloseLocal(ctx))
			}
			require.NoError(t, barrier.WaitBefore(ctx, 1, 2))
			requirePlaybackNoPacket(t, peer)
		})
	}
}
