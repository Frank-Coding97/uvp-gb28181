package zlm

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type cacheFetchStub struct {
	mu     sync.Mutex
	values map[int64]node.ServerConfig
	err    error
}

func (s *cacheFetchStub) fetch(_ context.Context, nodeID int64) (node.ServerConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return node.ServerConfig{}, s.err
	}
	return s.values[nodeID], nil
}

func (s *cacheFetchStub) set(nodeID int64, cfg node.ServerConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[nodeID] = cfg
}

func (s *cacheFetchStub) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func TestServerConfigCacheRefreshReplacesCachedValue(t *testing.T) {
	stub := &cacheFetchStub{values: map[int64]node.ServerConfig{7: {HTTPPort: 18080}}}
	cache := NewServerConfigCache(stub.fetch)

	first, err := cache.Get(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 18080, first.HTTPPort)

	stub.set(7, node.ServerConfig{HTTPPort: 28080})
	fresh, err := cache.Refresh(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 28080, fresh.HTTPPort)

	cached, err := cache.Get(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 28080, cached.HTTPPort)
}

func TestServerConfigCacheRefreshFailurePreservesPreviousValue(t *testing.T) {
	stub := &cacheFetchStub{values: map[int64]node.ServerConfig{7: {HTTPPort: 18080}}}
	cache := NewServerConfigCache(stub.fetch)
	_, err := cache.Get(context.Background(), 7)
	require.NoError(t, err)

	stub.setError(errors.New("zlm unavailable"))
	_, err = cache.Refresh(context.Background(), 7)
	require.EqualError(t, err, "zlm unavailable")

	cached, err := cache.Get(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 18080, cached.HTTPPort)
}

func TestServerConfigCacheRefreshKeepsNodesIsolated(t *testing.T) {
	stub := &cacheFetchStub{values: map[int64]node.ServerConfig{
		1: {HTTPPort: 18080},
		2: {HTTPPort: 28080},
	}}
	cache := NewServerConfigCache(stub.fetch)

	_, err := cache.Get(context.Background(), 1)
	require.NoError(t, err)
	_, err = cache.Get(context.Background(), 2)
	require.NoError(t, err)

	stub.set(1, node.ServerConfig{HTTPPort: 38080})
	_, err = cache.Refresh(context.Background(), 1)
	require.NoError(t, err)

	nodeOne, err := cache.Get(context.Background(), 1)
	require.NoError(t, err)
	nodeTwo, err := cache.Get(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, 38080, nodeOne.HTTPPort)
	require.Equal(t, 28080, nodeTwo.HTTPPort)
}

func TestServerConfigCacheConcurrentRefreshAndGet(t *testing.T) {
	stub := &cacheFetchStub{values: map[int64]node.ServerConfig{7: {HTTPPort: 18080}}}
	cache := NewServerConfigCache(stub.fetch)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			stub.set(7, node.ServerConfig{HTTPPort: port})
			_, _ = cache.Refresh(context.Background(), 7)
			_, _ = cache.Get(context.Background(), 7)
		}(20000 + i)
	}
	wg.Wait()

	cached, err := cache.Get(context.Background(), 7)
	require.NoError(t, err)
	require.GreaterOrEqual(t, cached.HTTPPort, 20000)
	require.LessOrEqual(t, cached.HTTPPort, 20019)
}
