package uac

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func TestPlaybackRecoveryWorkerFairGlobalCursor(t *testing.T) {
	u, db, store, id, _ := playbackIntentStoreFixture(t)
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	for n := 2; n <= 101; n++ {
		require.NoError(t, db.Exec("INSERT INTO gb_device VALUES (?,?,2,1,NULL)", n, fmt.Sprintf("3402000000132%07d", n)).Error)
	}
	w := newPlaybackRecoveryWorker(u, playauth.NewDeviceCleanupStore(db), store, playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db)))
	w.deviceLimit, w.intentLimit = 100, 100
	// Model only the page scheduler here; real SQL discovery is not mocked.
	// The separate worker UDP test exercises the actual page executor.
	items := map[int64][]string{101: {strings.Repeat("f", 32)}}
	for n := 1; n <= 201; n++ {
		items[1] = append(items[1], fmt.Sprintf("%032x", n*2))
	}
	seen, visits := make(map[string]bool), make(map[int64]int)
	w.recoverPage = func(_ context.Context, pk int64, code string, epoch int64, after string, limit int) (PlaybackRecoveryPage, error) {
		visits[pk]++
		require.Equal(t, int64(2), epoch)
		if pk == 1 {
			require.Equal(t, id.DeviceCode, code)
		}
		if pk == 2 {
			return PlaybackRecoveryPage{}, context.DeadlineExceeded
		}
		var p PlaybackRecoveryPage
		for _, item := range items[pk] {
			if item <= after || p.Scanned == limit {
				continue
			}
			seen[item] = true
			p.Scanned++
			p.Pending++
			p.NextAfter = item
		}
		if p.Pending != 0 {
			return p, ErrPlaybackCleanupUnknown
		}
		return p, nil
	}
	ctx := context.Background()
	_, err := w.Tick(ctx)
	require.Error(t, err)
	require.Zero(t, visits[101])
	// A larger newly inserted PK cannot extend the fixed first round.
	require.NoError(t, db.Exec("INSERT INTO gb_device VALUES (102,?,2,1,NULL)", "34020000001320000102").Error)
	r, _ := w.Tick(ctx)
	require.True(t, r.RoundEnded)
	require.Equal(t, 1, visits[101])
	require.Zero(t, visits[102])
	late := fmt.Sprintf("%032x", 1)
	items[1] = append([]string{late}, items[1]...)
	for n := 0; n < 20; n++ {
		_, _ = w.Tick(ctx)
	}
	for _, group := range items {
		for _, item := range group {
			require.True(t, seen[item], "missing %s", item)
		}
	}
	require.Positive(t, visits[102])
	state, err := playauth.NewDeviceCleanupStore(db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.CleanupCompletedEpoch)
}

func TestPlaybackRecoveryWorkerActualUDPStopAndResumeRetainedOwner(t *testing.T) {
	f, _ := recoveredPlaybackUDPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var blocked atomic.Bool
	require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register("fixture:worker-facts", func(db *gorm.DB) {
		if blocked.Load() {
			db.AddError(errors.New("fixture facts unavailable"))
		}
	}))
	defer func() {
		blocked.Store(false)
		closeScanObservations(t, f.u)
		_ = f.db.Callback().Update().Remove("fixture:worker-facts")
	}()
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	devices := playauth.NewDeviceCleanupStore(f.db)
	stop, err := f.u.StartPlaybackRecovery(ctx, devices, f.store, f.barrier, nil)
	require.NoError(t, err)
	defer stop(context.Background())
	ack, _ := readCleanupRequest(t, f.peer)
	bye, _ := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.ACK, ack.Method)
	require.Equal(t, sip.BYE, bye.Method)
	f.u.playbackIntentMu.Lock()
	owner := f.u.playbackRecoveries[f.id.OperationID]
	f.u.playbackIntentMu.Unlock()
	require.NotNil(t, owner)
	blocked.Store(true)
	require.NoError(t, stop(ctx), "stopping joins the loop, not remote completion")
	f.u.playbackIntentMu.Lock()
	retained, observations := f.u.playbackRecoveries[f.id.OperationID], len(f.u.playbackObservations)
	f.u.playbackIntentMu.Unlock()
	require.Same(t, owner, retained)
	require.Equal(t, 1, observations)
	short, stopWait := context.WithTimeout(ctx, 15*time.Millisecond)
	require.ErrorIs(t, f.barrier.WaitBefore(short, 1, 2), context.DeadlineExceeded)
	stopWait()
	blocked.Store(false)
	ticked := make(chan struct{}, 2)
	stopAgain, err := f.u.StartPlaybackRecovery(ctx, devices, f.store, f.barrier, func(PlaybackRecoveryTick, error) { ticked <- struct{}{} })
	require.NoError(t, err)
	defer stopAgain(context.Background())
	select {
	case <-ticked:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, stopAgain(ctx))
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Len(t, loaded.Steps[0].KnownBranch.CleanupAttempts, 1)
	require.NotNil(t, loaded.Steps[0].KnownBranch.CleanupAttempts[0].LocalQuiescedAt)
	require.Nil(t, loaded.Steps[0].KnownBranch.CleanupAttempts[0].Response)
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	f.noACK(t)
}

func TestPlaybackRecoveryWorkerStopTimeoutKeepsSingleRunner(t *testing.T) {
	f, _ := recoveredPlaybackUDPFixture(t)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("fixture:worker-blocked", func(*gorm.DB) {
		once.Do(func() { close(entered); <-release })
	}))
	defer f.db.Callback().Query().Remove("fixture:worker-blocked")
	devices := playauth.NewDeviceCleanupStore(f.db)
	stop, err := f.u.StartPlaybackRecovery(ctx, devices, f.store, f.barrier, nil)
	require.NoError(t, err)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		_ = stop(context.Background())
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	short, cancelShort := context.WithTimeout(ctx, 15*time.Millisecond)
	require.ErrorIs(t, stop(short), context.DeadlineExceeded)
	cancelShort()
	_, err = f.u.StartPlaybackRecovery(ctx, devices, f.store, f.barrier, nil)
	require.Error(t, err, "cancel does not release a still-running worker")
	close(release)
	require.NoError(t, stop(ctx))
	f.noACK(t)
}

func TestPlaybackRecoveryWorkerDiscoveryFailureDoesNotStarve(t *testing.T) {
	u, db, store, id, _ := playbackIntentStoreFixture(t)
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_device VALUES (2,'malformed',2,1,NULL), (3,?,2,1,NULL)", "34020000001320000003").Error)
	w := newPlaybackRecoveryWorker(u, playauth.NewDeviceCleanupStore(db), store, playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db)))
	w.deviceLimit = 1
	var visited []int64
	w.recoverPage = func(_ context.Context, pk int64, _ string, _ int64, _ string, _ int) (PlaybackRecoveryPage, error) {
		visited = append(visited, pk)
		return PlaybackRecoveryPage{}, nil
	}
	ctx := context.Background()
	_, err := w.Tick(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), w.deviceAfter)
	require.NoError(t, db.Exec("ALTER TABLE gb_device RENAME TO fixture_device_offline").Error)
	_, err = w.Tick(ctx)
	require.Error(t, err)
	require.Equal(t, int64(1), w.deviceAfter)
	require.Equal(t, int64(3), w.deviceThrough)
	require.NoError(t, db.Exec("ALTER TABLE fixture_device_offline RENAME TO gb_device").Error)
	r, err := w.Tick(ctx)
	require.Error(t, err)
	require.Equal(t, 1, r.Invalid)
	require.Equal(t, int64(2), w.deviceAfter)
	r, err = w.Tick(ctx)
	require.NoError(t, err)
	require.True(t, r.RoundEnded)
	require.Equal(t, []int64{1, 3}, visited)
	state, err := playauth.NewDeviceCleanupStore(db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.CleanupCompletedEpoch)
}
