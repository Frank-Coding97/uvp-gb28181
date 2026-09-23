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
	// ⛔ 关键次序:服务侧的调用预算必须**严格大于** zlm 的传输层超时。相等或更小的话,
	// addProbe 会先被外层 ctx 掐掉,报出裸 context deadline exceeded,与"传输层自己放弃"
	// 无法区分 —— 这正是 60 秒探针在现场的表现。原来的断言只查 "< 60s",漏掉了这条次序。
	remaining := <-client.deadlineRemaining
	transport := zlm.ProbeHTTPTimeout(60000)
	if remaining <= transport {
		t.Fatalf("调用预算剩余=%s 必须大于传输层超时=%s", remaining, transport)
	}
	if remaining <= 60*time.Second {
		t.Fatalf("deadline remaining=%s", remaining)
	}
}

func TestProbeCallBudgetOutlivesTransportTimeout(t *testing.T) {
	for _, durationMS := range []int{3000, 10000, 60000} {
		budget := probeCallBudget(5*time.Second, durationMS)
		transport := zlm.ProbeHTTPTimeout(durationMS)
		if budget <= transport {
			t.Fatalf("duration=%d 预算=%s 必须大于传输层超时=%s", durationMS, budget, transport)
		}
		if min := time.Duration(durationMS) * time.Millisecond; budget < min {
			t.Fatalf("duration=%d 预算=%s 盖不住采样窗口 %s", durationMS, budget, min)
		}
	}
	// 配置的 base 更大时原样生效(它是兜底下限,不是上限)。
	if got := probeCallBudget(time.Hour, 3000); got != time.Hour {
		t.Fatalf("大 base 应原样生效, got %s", got)
	}
}

func TestProbeTaskDeadlineOutlivesCallBudgetButStaysClaimable(t *testing.T) {
	created := time.Unix(1700000000, 0)
	for _, durationMS := range []int{3000, 10000, 60000} {
		span := probeTaskDeadline(created, durationMS).Sub(created)
		if budget := probeCallBudget(5*time.Second, durationMS); span <= budget {
			t.Fatalf("duration=%d 任务预算 %s 必须大于调用预算 %s", durationMS, span, budget)
		}
		// ⛔ 同时不能超过认领空闲时间,否则一次正常的长探针会被别的 worker 当成死消息抢走。
		if span >= probeClaimIdle {
			t.Fatalf("duration=%d 任务预算 %s 必须小于认领空闲 %s", durationMS, span, probeClaimIdle)
		}
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
