package trace

import (
	"context"
	"sync"
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

func TestModuleT12ShutdownDeadlineDoesNotCloseBlockingStore(t *testing.T) {
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

func TestModuleT12ShutdownWaitsForRelationalHealthProbe(t *testing.T) {
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
