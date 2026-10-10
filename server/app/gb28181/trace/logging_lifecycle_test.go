package trace

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type t12BlockingStore struct {
	started   chan struct{}
	release   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func (s *t12BlockingStore) InsertBatch(context.Context, []StoredEvent) error {
	s.startOnce.Do(func() { close(s.started) })
	<-s.release
	return nil
}

func (s *t12BlockingStore) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

type t12BlockingFactory struct {
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	store     Store
}

func (f *t12BlockingFactory) open(context.Context) (Store, error) {
	f.startOnce.Do(func() { close(f.started) })
	<-f.release
	return f.store, nil
}

type t12HealthyPrunableStore struct {
	pruneCalls atomic.Int64
	closed     chan struct{}
	closeOnce  sync.Once
}

func (s *t12HealthyPrunableStore) InsertBatch(context.Context, []StoredEvent) error {
	return nil
}

func (s *t12HealthyPrunableStore) Prune(ctx context.Context, _ time.Time, _ int) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.pruneCalls.Add(1)
	return 0, nil
}

func (s *t12HealthyPrunableStore) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func TestLoggingPrunableShutdownCompletes(t *testing.T) {
	store := &t12HealthyPrunableStore{closed: make(chan struct{})}
	lifecycleCtx, cancel := context.WithCancel(context.Background())
	prunerDone := make(chan struct{})
	go func() {
		defer close(prunerDone)
		NewTracePruner(store, 7, DefaultTracePruneBatchSize, time.Now).Run(lifecycleCtx, time.Millisecond, nil)
	}()
	t.Cleanup(func() {
		cancel()
		<-prunerDone
	})
	require.Eventually(t, func() bool { return store.pruneCalls.Load() > 0 }, time.Second, time.Millisecond)

	done := make(chan struct{})
	close(done)
	retryDone := make(chan struct{})
	close(retryDone)
	module := &Module{
		health:       newHealthTracker(HealthReady, ""),
		collector:    NewCollector(1, nil),
		store:        store,
		lifecycleCtx: lifecycleCtx,
		cancel:       cancel,
		done:         done,
		retryDone:    retryDone,
		prunerDone:   prunerDone,
		cleanupDone:  make(chan struct{}),
		probeDone:    make(chan struct{}),
	}

	shutdown := make(chan error, 1)
	go func() { shutdown <- module.Shutdown(context.Background()) }()
	select {
	case err := <-shutdown:
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		cancel()
		select {
		case <-shutdown:
		case <-time.After(time.Second):
			t.Fatal("prunable module shutdown did not finish after cancellation")
		}
		t.Fatal("Shutdown waited for pruner before canceling its lifecycle")
	}
	select {
	case <-store.closed:
	case <-time.After(time.Second):
		t.Fatal("healthy prunable store was not closed after shutdown")
	}
}

func TestLoggingCompletedShutdownWinsOverExpiredContext(t *testing.T) {
	store := &t12HealthyPrunableStore{closed: make(chan struct{})}
	module := NewModule(testTraceConfig(1, 1, 1), store, testPayloadCipher())
	require.NoError(t, module.Shutdown(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, module.Shutdown(ctx))
}

func TestLoggingShutdownDeadlineDoesNotCloseBlockingStore(t *testing.T) {
	store := &t12BlockingStore{
		started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{}),
	}
	t.Cleanup(func() { closeT12Channel(store.release) })
	module := NewModule(testTraceConfig(1, 1, 1), store, testPayloadCipher())
	module.WriteObserver(testWriteProps(), []byte("MESSAGE sip:lifecycle SIP/2.0\r\nContent-Length: 0\r\n\r\n"))
	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("writer did not enter the blocking store")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	shutdown := make(chan error, 1)
	go func() { shutdown <- module.Shutdown(ctx) }()
	select {
	case err := <-shutdown:
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(200 * time.Millisecond):
		cancel()
		closeT12Channel(store.release)
		t.Fatal("Shutdown exceeded its caller deadline while the store ignored context cancellation")
	}
	select {
	case <-store.closed:
		t.Fatal("Shutdown closed the store before the in-flight write returned")
	default:
	}

	closeT12Channel(store.release)
	select {
	case <-store.closed:
	case <-time.After(time.Second):
		t.Fatal("background cleanup did not close the store after the writer completed")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	require.NoError(t, module.Shutdown(ctx2))
}

func TestLoggingShutdownWaitsForRelationalHealthProbe(t *testing.T) {
	store := &t12BlockingStore{
		started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{}),
	}
	factory := &t12BlockingFactory{
		started: make(chan struct{}), release: make(chan struct{}), store: store,
	}
	t.Cleanup(func() { closeT12Channel(factory.release) })
	reconnecting := NewReconnectingStore(factory.open, time.Millisecond, time.Second)
	module := NewModule(testTraceConfig(1, 1, 1), reconnecting, testPayloadCipher())
	startRelationalHealthProbe(reconnecting, module)
	select {
	case <-factory.started:
	case <-time.After(time.Second):
		t.Fatal("relational health probe did not enter the blocking factory")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	shutdown := make(chan error, 1)
	go func() { shutdown <- module.Shutdown(ctx) }()
	select {
	case err := <-shutdown:
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(200 * time.Millisecond):
		cancel()
		closeT12Channel(factory.release)
		t.Fatal("Shutdown exceeded its caller deadline while the health probe was blocked")
	}
	select {
	case <-store.closed:
		t.Fatal("Shutdown closed the store before the health probe completed")
	default:
	}

	closeT12Channel(factory.release)
	select {
	case <-store.closed:
	case <-time.After(time.Second):
		t.Fatal("background cleanup did not close the store after the health probe completed")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	require.NoError(t, module.Shutdown(ctx2))
}

func closeT12Channel(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}
