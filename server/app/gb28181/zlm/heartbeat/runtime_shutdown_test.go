package heartbeat_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/heartbeat"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type shutdownWatcherRepo struct {
	*memoryRepo
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	updates atomic.Int32
}

func (r *shutdownWatcherRepo) Update(ctx context.Context, n node.Node) error {
	r.once.Do(func() { close(r.entered) })
	<-r.release
	r.updates.Add(1)
	return r.memoryRepo.Update(ctx, n)
}

func TestWatcherStartDoneWaitsForInFlightPersistence(t *testing.T) {
	repo := &shutdownWatcherRepo{
		memoryRepo: newMemoryRepo(),
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	reg := node.NewRegistry(repo)
	added, err := reg.Add(context.Background(), node.Node{
		Name:            "zlm-shutdown",
		MediaServerUUID: "uuid-shutdown",
		State:           node.StateActive,
	})
	require.NoError(t, err)
	clock := newFakeClock(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	reg.UpdateStats("uuid-shutdown", node.Stats{LastHeartbeatAt: clock.Now().Add(-2 * time.Second)})
	watcher := heartbeat.NewWatcher(reg, clock, time.Millisecond, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	done := watcher.Start(ctx)
	t.Cleanup(func() {
		cancel()
		select {
		case <-repo.release:
		default:
			close(repo.release)
		}
	})
	select {
	case <-repo.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not enter persistence")
	}

	cancel()
	select {
	case <-done:
		t.Fatal("watcher reported done while persistence was blocked")
	case <-time.After(20 * time.Millisecond):
	}
	close(repo.release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not report done after persistence was released")
	}
	require.Equal(t, int32(1), repo.updates.Load())
	got, ok := reg.Get(added.ID)
	require.True(t, ok)
	require.Equal(t, node.StateOffline, got.State)
}

type shutdownThreadLoadFetcher struct {
	entered   chan struct{}
	release   chan struct{}
	once      sync.Once
	netCalls  atomic.Int32
	workCalls atomic.Int32
}

func (f *shutdownThreadLoadFetcher) GetThreadsLoad(context.Context, *node.Node) (float64, error) {
	f.netCalls.Add(1)
	f.once.Do(func() { close(f.entered) })
	<-f.release
	return 0.4, nil
}

func (f *shutdownThreadLoadFetcher) GetWorkThreadsLoad(context.Context, *node.Node) (float64, error) {
	f.workCalls.Add(1)
	return 0.3, nil
}

func TestThreadLoadPollerStartDoneWaitsForInFlightFetch(t *testing.T) {
	reg, id := setupRegistry(t, "uuid-thread-shutdown")
	fetcher := &shutdownThreadLoadFetcher{entered: make(chan struct{}), release: make(chan struct{})}
	poller := heartbeat.NewThreadLoadPoller(reg, fetcher, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	done := poller.Start(ctx)
	t.Cleanup(func() {
		cancel()
		select {
		case <-fetcher.release:
		default:
			close(fetcher.release)
		}
	})
	select {
	case <-fetcher.entered:
	case <-time.After(7 * time.Second):
		t.Fatal("thread load poller did not enter fetch")
	}

	cancel()
	select {
	case <-done:
		t.Fatal("thread load poller reported done while fetch was blocked")
	case <-time.After(20 * time.Millisecond):
	}
	close(fetcher.release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("thread load poller did not report done after fetch was released")
	}
	require.Equal(t, int32(1), fetcher.netCalls.Load())
	require.Zero(t, fetcher.workCalls.Load(), "cancelled poll must not start the second fetch")
	got, ok := reg.Get(id)
	require.True(t, ok)
	require.Zero(t, got.Stats.NetThreadLoadAvg, "cancelled fetch must not update registry")
	require.Zero(t, got.Stats.WorkThreadLoadAvg, "cancelled fetch must not update registry")
}
