package heartbeat_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/heartbeat"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type blockingUpdateRepo struct {
	*memoryRepo
	started chan struct{}
	release chan struct{}
	once    sync.Once
	close   sync.Once
}

func (r *blockingUpdateRepo) Update(ctx context.Context, n node.Node) error {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.memoryRepo.Update(ctx, n)
}

type blockingFetch struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	close   sync.Once
}

func (f *blockingFetch) GetThreadsLoad(context.Context, *node.Node) (float64, error) {
	f.once.Do(func() { close(f.started) })
	<-f.release
	return 0.4, nil
}

func (f *blockingFetch) GetWorkThreadsLoad(context.Context, *node.Node) (float64, error) {
	return 0.2, nil
}

func TestWatcherT12StartDoneWaitsForInFlightTick(t *testing.T) {
	baseRepo := newMemoryRepo()
	repo := &blockingUpdateRepo{
		memoryRepo: baseRepo,
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	releaseRepo := func() { repo.close.Do(func() { close(repo.release) }) }
	t.Cleanup(releaseRepo)
	reg := node.NewRegistry(repo)
	clock := newFakeClock(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	added, err := reg.Add(context.Background(), node.Node{
		Name:            "zlm-a",
		MediaServerUUID: "uuid-a",
		State:           node.StateActive,
	})
	require.NoError(t, err)
	reg.UpdateStats("uuid-a", node.Stats{LastHeartbeatAt: clock.Now()})
	clock.Advance(2 * time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	done := heartbeat.NewWatcher(reg, clock, time.Millisecond, time.Second).Start(ctx)
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("watcher did not enter the blocking update")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("watcher reported done before the in-flight Tick returned")
	case <-time.After(20 * time.Millisecond):
	}

	releaseRepo()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher did not report completion after Tick returned")
	}
	_, ok := reg.Get(added.ID)
	require.True(t, ok)
}

func TestThreadLoadPollerT12StartDoneWaitsForTickFetch(t *testing.T) {
	reg, _ := setupRegistry(t, "uuid-1")
	fetcher := &blockingFetch{started: make(chan struct{}), release: make(chan struct{})}
	releaseFetcher := func() { fetcher.close.Do(func() { close(fetcher.release) }) }
	t.Cleanup(releaseFetcher)
	poller := heartbeat.NewThreadLoadPoller(reg, fetcher, time.Hour)

	// Tick remains asynchronous. Start's returned done must include this
	// already-admitted fetch even when cancellation arrives before its first
	// scheduled five-second poll.
	poller.Tick(context.Background())
	select {
	case <-fetcher.started:
	case <-time.After(time.Second):
		t.Fatal("poller did not enter the blocking fetch")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := poller.Start(ctx)
	cancel()
	select {
	case <-done:
		t.Fatal("poller reported done before the in-flight fetch returned")
	case <-time.After(20 * time.Millisecond):
	}

	releaseFetcher()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("poller did not report completion after fetch returned")
	}
}
