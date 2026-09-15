package streamprobe

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type serviceLocations struct{ ids map[string]int64 }

func (f *serviceLocations) Lookup(stream string) (int64, bool) {
	id, ok := f.ids[stream]
	return id, ok
}
func (f *serviceLocations) Bind(stream string, id int64) { f.ids[stream] = id }

type serviceNodes struct{ n *node.Node }

func (f serviceNodes) Get(id int64) (*node.Node, bool) { return f.n, f.n != nil && f.n.ID == id }
func (f serviceNodes) ListActive() []*node.Node {
	if f.n == nil {
		return nil
	}
	return []*node.Node{f.n}
}

type countingProbeClient struct {
	calls             atomic.Int32
	durations         chan int
	deadlineRemaining chan time.Duration
	delay             time.Duration
	started           chan struct{}
	once              sync.Once
}

func (f *countingProbeClient) GetMediaInfo(context.Context, string, string, string, string) (*zlm.MediaInfo, error) {
	return &zlm.MediaInfo{Online: true}, nil
}
func (f *countingProbeClient) AddProbe(ctx context.Context, _, _, _ string, durationMS int) ([]zlm.ProbeFrame, error) {
	f.calls.Add(1)
	if f.durations != nil {
		f.durations <- durationMS
	}
	if f.deadlineRemaining != nil {
		deadline, _ := ctx.Deadline()
		f.deadlineRemaining <- time.Until(deadline)
	}
	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(f.delay):
		return []zlm.ProbeFrame{{Codec: "H264", TrackType: "video", RecvStamp: 1, FrameSize: 100, KeyFrame: true}}, nil
	}
}

func newProbeService(client ProbeClient) *Service {
	locations := &serviceLocations{ids: map[string]int64{"stream": 1, "other": 1}}
	return NewService(serviceNodes{n: &node.Node{ID: 1, Name: "edge"}}, locations, func(*node.Node) ProbeClient { return client }, 5*time.Second, time.Now)
}

func TestServiceSingleFlightSameStream(t *testing.T) {
	client := &countingProbeClient{delay: 30 * time.Millisecond}
	service := newProbeService(client)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Run(context.Background(), "stream", DefaultDurationMS); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if client.calls.Load() != 1 {
		t.Fatalf("calls=%d", client.calls.Load())
	}
}

func TestCallerCancellationDoesNotCancelSharedProbe(t *testing.T) {
	client := &countingProbeClient{delay: 50 * time.Millisecond, started: make(chan struct{})}
	service := newProbeService(client)
	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { _, err := service.Run(ctx, "stream", DefaultDurationMS); firstDone <- err }()
	<-client.started
	secondDone := make(chan error, 1)
	go func() { _, err := service.Run(context.Background(), "stream", DefaultDurationMS); secondDone <- err }()
	cancel()
	if err := <-firstDone; err == nil {
		t.Fatal("first caller should be canceled")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("shared probe failed: %v", err)
	}
	if client.calls.Load() != 1 {
		t.Fatalf("calls=%d", client.calls.Load())
	}
}

func TestDifferentStreamsRunIndependently(t *testing.T) {
	client := &countingProbeClient{delay: 20 * time.Millisecond}
	service := newProbeService(client)
	var wg sync.WaitGroup
	for _, stream := range []string{"stream", "other"} {
		wg.Add(1)
		go func(stream string) {
			defer wg.Done()
			_, _ = service.Run(context.Background(), stream, DefaultDurationMS)
		}(stream)
	}
	wg.Wait()
	if client.calls.Load() != 2 {
		t.Fatalf("calls=%d", client.calls.Load())
	}
}

func TestServicePassesSelectedDurationAndExtendsDeadline(t *testing.T) {
	client := &countingProbeClient{
		durations:         make(chan int, 1),
		deadlineRemaining: make(chan time.Duration, 1),
	}
	service := newProbeService(client)

	_, err := service.Run(context.Background(), "stream", 60000)
	if err != nil {
		t.Fatal(err)
	}
	if duration := <-client.durations; duration != 60000 {
		t.Fatalf("duration=%d", duration)
	}
	if remaining := <-client.deadlineRemaining; remaining < 60*time.Second {
		t.Fatalf("deadline remaining=%s", remaining)
	}
}

func TestDifferentDurationsRunIndependently(t *testing.T) {
	client := &countingProbeClient{delay: 30 * time.Millisecond}
	service := newProbeService(client)
	var wg sync.WaitGroup
	for _, durationMS := range []int{3000, 10000} {
		wg.Add(1)
		go func(durationMS int) {
			defer wg.Done()
			_, _ = service.Run(context.Background(), "stream", durationMS)
		}(durationMS)
	}
	wg.Wait()
	if client.calls.Load() != 2 {
		t.Fatalf("calls=%d", client.calls.Load())
	}
}
