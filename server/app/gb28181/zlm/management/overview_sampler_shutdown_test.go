package management

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type shutdownOverviewSource struct {
	started        chan struct{}
	release        chan struct{}
	cancelObserved chan struct{}
	startOnce      sync.Once
	cancelOnce     sync.Once
	calls          atomic.Int32
}

func (source *shutdownOverviewSource) GetOverview(ctx context.Context) (OverviewResult, error) {
	source.calls.Add(1)
	source.startOnce.Do(func() { close(source.started) })
	go func() {
		<-ctx.Done()
		source.cancelOnce.Do(func() { close(source.cancelObserved) })
	}()
	<-source.release
	return OverviewResult{}, nil
}

func (source *shutdownOverviewSource) GetNodeRuntime(context.Context, int64) (NodeRuntimeView, error) {
	return NodeRuntimeView{}, nil
}

func (source *shutdownOverviewSource) ListStreams(context.Context, StreamFilter, PageRequest) (StreamDistribution, error) {
	return StreamDistribution{}, nil
}

type shutdownOverviewCache struct {
	*overviewSamplerCacheFake
	started        chan struct{}
	release        chan struct{}
	cancelObserved chan struct{}
	startOnce      sync.Once
	cancelOnce     sync.Once
}

func (cache *shutdownOverviewCache) Get(ctx context.Context, key string) (string, error) {
	cache.startOnce.Do(func() { close(cache.started) })
	go func() {
		<-ctx.Done()
		cache.cancelOnce.Do(func() { close(cache.cancelObserved) })
	}()
	<-cache.release
	return "", app.ErrKeyNotFound
}

func TestOverviewSamplerShutdownCancelsBlockedSourceAndWaits(t *testing.T) {
	source := &shutdownOverviewSource{
		started:        make(chan struct{}),
		release:        make(chan struct{}),
		cancelObserved: make(chan struct{}),
	}
	sampler := NewOverviewSampler(source, nil, WithOverviewSamplerInterval(time.Millisecond))
	sampler.Start(nil)
	<-source.started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, sampler.Shutdown(ctx), context.DeadlineExceeded)
	select {
	case <-source.cancelObserved:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel the blocked source context")
	}

	close(source.release)
	require.NoError(t, sampler.Shutdown(context.Background()))
	calls := source.calls.Load()
	sampler.Start(nil)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, calls, source.calls.Load())
}

func TestOverviewSamplerShutdownCancelsBlockedCacheAndWaits(t *testing.T) {
	cache := &shutdownOverviewCache{
		overviewSamplerCacheFake: newOverviewSamplerCacheFake(),
		started:                  make(chan struct{}),
		release:                  make(chan struct{}),
		cancelObserved:           make(chan struct{}),
	}
	source := &overviewSamplerSourceFake{}
	sampler := NewOverviewSampler(source, cache)
	sampler.Start(nil)
	<-cache.started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, sampler.Shutdown(ctx), context.DeadlineExceeded)
	select {
	case <-cache.cancelObserved:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel the blocked cache context")
	}

	close(cache.release)
	require.NoError(t, sampler.Shutdown(context.Background()))
}

func TestOverviewSamplerShutdownBeforeStartAndConcurrentCallsAreIdempotent(t *testing.T) {
	source := &shutdownOverviewSource{
		started:        make(chan struct{}),
		release:        make(chan struct{}),
		cancelObserved: make(chan struct{}),
	}
	sampler := NewOverviewSampler(source, nil)
	require.NoError(t, sampler.Shutdown(context.Background()))
	sampler.Start(nil)
	select {
	case <-source.started:
		t.Fatal("shutdown-before-start sampler started sampling")
	case <-time.After(30 * time.Millisecond):
	}

	const callers = 4
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- sampler.Shutdown(context.Background())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	sampler.Close()
}
