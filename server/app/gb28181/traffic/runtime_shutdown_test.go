package traffic

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type shutdownSamplerClient struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func (c *shutdownSamplerClient) GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error) {
	c.calls.Add(1)
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return nil, errors.New("released after cancellation")
}

func TestSamplerStartDoneWaitsForInFlightClient(t *testing.T) {
	repo := newTrafficTestRepo(t)
	resolver := NewAttributionResolver()
	client := &shutdownSamplerClient{entered: make(chan struct{}), release: make(chan struct{})}
	sampler := NewSampler(samplerNodes{nodes: []*node.Node{
		{ID: 7, MediaServerUUID: "ms-shutdown-1"},
		{ID: 8, MediaServerUUID: "ms-shutdown-2"},
	}}, func(*node.Node) SamplerMediaClient {
		return client
	}, repo, resolver, nil, time.Now)

	ctx, cancel := context.WithCancel(context.Background())
	done := sampler.Start(ctx, time.Hour)
	t.Cleanup(func() {
		cancel()
		select {
		case <-client.release:
		default:
			close(client.release)
		}
	})
	select {
	case <-client.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("sampler did not enter client")
	}

	cancel()
	select {
	case <-done:
		t.Fatal("sampler reported done while client was blocked")
	case <-time.After(20 * time.Millisecond):
	}
	close(client.release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sampler did not report done after client was released")
	}
	require.Equal(t, int32(1), client.calls.Load(), "cancelled sample must not start another node fetch")
}

func TestSessionPrunerStartDoneWaitsForInFlightRepositoryCall(t *testing.T) {
	repo := newTrafficTestRepo(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	require.NoError(t, repo.db.Callback().Query().Before("gorm:query").Register("traffic:shutdown_block_query", func(*gorm.DB) {
		once.Do(func() {
			close(entered)
			<-release
		})
	}))

	ctx, cancel := context.WithCancel(context.Background())
	done := StartSessionPruner(ctx, repo, time.Now, nil)
	t.Cleanup(func() {
		cancel()
		select {
		case <-release:
		default:
			close(release)
		}
	})
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("session pruner did not enter repository")
	}

	cancel()
	select {
	case <-done:
		t.Fatal("session pruner reported done while repository call was blocked")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("session pruner did not report done after repository call was released")
	}
}
