package media

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestRevocationMaintenanceProcessesAndStops(t *testing.T) {
	f := newRevocationWorkerFixture(t, time.Second)
	viewer := f.seed(t, 1, models.ViewerStateRevokePending)
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	close(ticks)
	reports := 0
	f.worker(1).runMaintenance(context.Background(), ticks, func(result RevocationTickResult, err error) {
		require.NoError(t, err)
		require.Equal(t, 1, result.Pending)
		reports++
	})
	require.Equal(t, 1, reports)
	require.Equal(t, RevocationErrorAwaitingLateSession, f.loadViewer(t, viewer.ID).LastErrorClass)
	var nilWorker *RevocationWorker
	nilWorker.RunMaintenance(context.Background(), nil)
}

func TestRevocationMaintenanceReportsSanitizedErrorsAndContinues(t *testing.T) {
	f := newRevocationWorkerFixture(t, time.Second)
	require.NoError(t, f.db.Migrator().DropTable(&models.Viewer{}))
	ticks := make(chan time.Time, 2)
	ticks <- time.Now()
	ticks <- time.Now()
	close(ticks)
	reports := 0
	f.worker(1).runMaintenance(context.Background(), ticks, func(result RevocationTickResult, err error) {
		require.ErrorIs(t, err, ErrRevocationWorkerUnavailable)
		require.NotContains(t, err.Error(), "SELECT")
		reports++
	})
	require.Equal(t, 2, reports)
}

func TestRevocationMaintenanceCancellationWaitsForRunningCall(t *testing.T) {
	f := newRevocationWorkerFixture(t, time.Second)
	f.seed(t, 1, models.ViewerStateRevokePending)
	f.control.blockPlayers = true
	f.control.playersEntered = make(chan struct{})
	f.control.releasePlayers = make(chan struct{})
	ticks := make(chan time.Time, 2)
	ticks <- time.Now()
	ticks <- time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.worker(1).runMaintenance(ctx, ticks, nil)
	}()
	released := false
	defer func() {
		if !released {
			close(f.control.releasePlayers)
		}
		<-done
	}()
	select {
	case <-f.control.playersEntered:
	case <-done:
		t.Fatal("runner exited before processing its first tick")
	case <-time.After(time.Second):
		t.Fatal("runner never entered media control")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("runner detached an ongoing non-cooperative call")
	default:
	}
	close(f.control.releasePlayers)
	released = true
	<-done
	players, sessions := f.control.calls()
	require.Equal(t, 1, players, "buffered next tick must not overlap")
	require.Zero(t, sessions)
}

func TestRevocationMaintenanceCanceledBeforeTickDoesNoWork(t *testing.T) {
	f := newRevocationWorkerFixture(t, time.Second)
	f.seed(t, 1, models.ViewerStateRevokePending)
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.worker(1).runMaintenance(ctx, ticks, nil)
	players, sessions := f.control.calls()
	require.Zero(t, players)
	require.Zero(t, sessions)
	f.worker(1).RunMaintenance(ctx, nil)
}
