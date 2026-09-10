package traffic

import (
	"context"
	"sync"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type blockingSamplerClient struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	close   sync.Once
}

func (c *blockingSamplerClient) GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error) {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return nil, nil
}

func TestLoggingSamplerT12StartDoneWaitsForInFlightSample(t *testing.T) {
	repo := newTrafficTestRepo(t)
	client := &blockingSamplerClient{started: make(chan struct{}), release: make(chan struct{})}
	release := func() { client.close.Do(func() { close(client.release) }) }
	t.Cleanup(release)
	now := time.Unix(3000, 0)
	sampler := NewSampler(
		samplerNodes{nodes: []*node.Node{{ID: 7, MediaServerUUID: "ms-1"}}},
		func(*node.Node) SamplerMediaClient { return client },
		repo,
		NewAttributionResolver(),
		nil,
		func() time.Time { return now },
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := sampler.Start(ctx, time.Hour)
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("sampler did not enter the blocking media query")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("sampler reported done before the in-flight sample returned")
	case <-time.After(20 * time.Millisecond):
	}

	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sampler did not report completion after the sample returned")
	}
}

func TestLoggingSessionPrunerT12StartDoneClosesAfterCancellation(t *testing.T) {
	repo := newTrafficTestRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := StartSessionPruner(ctx, repo, time.Now, nil)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session pruner did not report completion after cancellation")
	}
}

func TestLoggingSessionPrunerT12NilRepositoryReturnsClosedDone(t *testing.T) {
	done := StartSessionPruner(context.Background(), nil, nil, nil)
	select {
	case <-done:
	default:
		t.Fatal("nil repository should return an already-closed completion signal")
	}
}
