package reconciler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// ============ 手写 fake(不用 gomock) ============

// fakeStopper 记录 Stop 调用.
type fakeStopper struct {
	mu      sync.Mutex
	calls   []string
	stopErr error // 非 nil 则 Stop 返回该 err
}

type conditionalStopCall struct {
	streamID string
	ssrc     string
}

type fakeConditionalStopper struct {
	fakeStopper
	conditional []conditionalStopCall
}

func (s *fakeConditionalStopper) StopIfPersistedCurrent(_ context.Context, streamID, ssrc string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conditional = append(s.conditional, conditionalStopCall{streamID: streamID, ssrc: ssrc})
	return s.stopErr
}

func (s *fakeStopper) Stop(ctx context.Context, streamID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, streamID)
	return s.stopErr
}

func (s *fakeStopper) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	copy(out, s.calls)
	return out
}

// fakeRegistry 按 map 存 node.
type fakeRegistry struct {
	nodes map[int64]*node.Node
}

func (r *fakeRegistry) List() []*node.Node {
	out := make([]*node.Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		out = append(out, n)
	}
	return out
}

func (r *fakeRegistry) Get(id int64) (*node.Node, bool) {
	n, ok := r.nodes[id]
	return n, ok
}

// fakeProbe 用 map 决定每个 (nodeID, streamID) 组合的 online 结果.
type fakeProbe struct {
	mu     sync.Mutex
	online map[string]bool  // key = nodeID|streamID; 单节点场景 nodeID=0
	errs   map[string]error // 同 key
	calls  []probeCall
}

type probeCall struct {
	nodeID   int64
	streamID string
}

func (p *fakeProbe) IsMediaOnline(ctx context.Context, n *node.Node, streamID string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var nodeID int64
	if n != nil {
		nodeID = n.ID
	}
	key := keyFor(nodeID, streamID)
	p.calls = append(p.calls, probeCall{nodeID: nodeID, streamID: streamID})
	if e, ok := p.errs[key]; ok {
		return false, e
	}
	return p.online[key], nil
}

func (p *fakeProbe) Calls() []probeCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]probeCall, len(p.calls))
	copy(out, p.calls)
	return out
}

func keyFor(nodeID int64, streamID string) string {
	// 简单拼接够用,streamID 里不含 |
	return streamID + "|" + itoa(nodeID)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return sign + digits
}

// fakeLister 返回预设的 channel list.
type fakeLister struct {
	channels gbmodels.GbChannelList
	err      error
}

func (l *fakeLister) ListPlayingChannels(ctx context.Context) (gbmodels.GbChannelList, error) {
	return l.channels, l.err
}

// ============ Helpers ============

func init() {
	// 单测环境下 ZapLog 可能没初始化,给个 no-op logger 避免 panic.
	if app.ZapLog == nil {
		app.ZapLog = zap.NewNop()
	}
}

func makeChannel(streamID string) *gbmodels.GbChannel {
	return &gbmodels.GbChannel{
		DeviceID:  "dev-" + streamID,
		ChannelID: "ch-" + streamID,
		StreamID:  streamID,
	}
}

func makeNode(id int64, state node.State) *node.Node {
	return &node.Node{ID: id, State: state}
}

// ============ 测试用例 T6.1-T6.10 ============

// T6.1: 单节点 + online -> 不清
func TestT6_1_SingleNodeOnline(t *testing.T) {
	stopper := &fakeStopper{}
	probe := &fakeProbe{online: map[string]bool{keyFor(0, "ssrc-1"): true}}
	lister := &fakeLister{channels: gbmodels.GbChannelList{makeChannel("ssrc-1")}}

	rec := New(1*time.Millisecond, stopper,
		WithMediaProbe(probe),
		WithChannelLister(lister))

	stats := rec.runOnce(context.Background())
	if stats.Cleaned != 0 {
		t.Errorf("期望 Cleaned=0, 实际 %d", stats.Cleaned)
	}
	if got := stopper.Calls(); len(got) != 0 {
		t.Errorf("期望 stopper 未被调用, 实际调用 %v", got)
	}
}

// T6.2: 单节点 + offline -> 清一次
func TestT6_2_SingleNodeOffline(t *testing.T) {
	stopper := &fakeStopper{}
	probe := &fakeProbe{online: map[string]bool{}}
	lister := &fakeLister{channels: gbmodels.GbChannelList{makeChannel("ssrc-2")}}

	rec := New(1*time.Millisecond, stopper,
		WithMediaProbe(probe),
		WithChannelLister(lister))

	stats := rec.runOnce(context.Background())
	if stats.Cleaned != 1 {
		t.Errorf("期望 Cleaned=1, 实际 %d", stats.Cleaned)
	}
	calls := stopper.Calls()
	if len(calls) != 1 || calls[0] != "ssrc-2" {
		t.Errorf("期望 stopper.Stop 被调用一次(streamID=ssrc-2), 实际 %v", calls)
	}
}

// T6.3: 多节点 + LocationMap 命中 + node active + online -> 不清
func TestT6_3_MultiNodeLocationHitOnline(t *testing.T) {
	stopper := &fakeStopper{}
	n1 := makeNode(1, node.StateActive)
	registry := &fakeRegistry{nodes: map[int64]*node.Node{1: n1}}
	locMap := stream.NewLocationMap()
	locMap.Bind("ssrc-3", 1)
	probe := &fakeProbe{online: map[string]bool{keyFor(1, "ssrc-3"): true}}
	lister := &fakeLister{channels: gbmodels.GbChannelList{makeChannel("ssrc-3")}}

	rec := New(1*time.Millisecond, stopper,
		WithRegistry(registry),
		WithLocationMap(locMap),
		WithMediaProbe(probe),
		WithChannelLister(lister))

	stats := rec.runOnce(context.Background())
	if stats.Cleaned != 0 {
		t.Errorf("期望 Cleaned=0, 实际 %d", stats.Cleaned)
	}
	if len(stopper.Calls()) != 0 {
		t.Errorf("期望 stopper 未被调用, 实际 %v", stopper.Calls())
	}
}

// T6.4: Q4 - LocationMap 命中但 node inactive -> 跳过, 不调 probe 也不调 Stop
func TestT6_4_Q4NodeInactiveSkipped(t *testing.T) {
	stopper := &fakeStopper{}
	n1 := makeNode(1, node.StateMaintenance) // 非 StateActive
	registry := &fakeRegistry{nodes: map[int64]*node.Node{1: n1}}
	locMap := stream.NewLocationMap()
	locMap.Bind("ssrc-4", 1)
	probe := &fakeProbe{}
	lister := &fakeLister{channels: gbmodels.GbChannelList{makeChannel("ssrc-4")}}

	rec := New(1*time.Millisecond, stopper,
		WithRegistry(registry),
		WithLocationMap(locMap),
		WithMediaProbe(probe),
		WithChannelLister(lister))

	stats := rec.runOnce(context.Background())
	if stats.Skipped != 1 {
		t.Errorf("期望 Skipped=1, 实际 %d", stats.Skipped)
	}
	if stats.Cleaned != 0 {
		t.Errorf("期望 Cleaned=0, 实际 %d", stats.Cleaned)
	}
	if len(probe.Calls()) != 0 {
		t.Errorf("Q4 不应该探测, 实际探测 %v", probe.Calls())
	}
	if len(stopper.Calls()) != 0 {
		t.Errorf("Q4 不应该 Stop, 实际 %v", stopper.Calls())
	}
}

// T6.5: Q5 - LocationMap miss + 遍历命中一个节点 online -> 不清
func TestT6_5_Q5LocationMissTraverseHit(t *testing.T) {
	stopper := &fakeStopper{}
	n1 := makeNode(1, node.StateActive)
	n2 := makeNode(2, node.StateActive)
	registry := &fakeRegistry{nodes: map[int64]*node.Node{1: n1, 2: n2}}
	locMap := stream.NewLocationMap() // 空,ssrc-5 miss
	probe := &fakeProbe{online: map[string]bool{
		keyFor(1, "ssrc-5"): false,
		keyFor(2, "ssrc-5"): true, // n2 说 online
	}}
	lister := &fakeLister{channels: gbmodels.GbChannelList{makeChannel("ssrc-5")}}

	rec := New(1*time.Millisecond, stopper,
		WithRegistry(registry),
		WithLocationMap(locMap),
		WithMediaProbe(probe),
		WithChannelLister(lister))

	stats := rec.runOnce(context.Background())
	if stats.Cleaned != 0 {
		t.Errorf("期望 Cleaned=0(某节点 online 就保留), 实际 %d", stats.Cleaned)
	}
	if len(stopper.Calls()) != 0 {
		t.Errorf("期望 stopper 未调用, 实际 %v", stopper.Calls())
	}
}

// T6.6: Q5 - LocationMap miss + 全部节点 offline -> 清一次
func TestT6_6_Q5LocationMissAllOffline(t *testing.T) {
	stopper := &fakeStopper{}
	n1 := makeNode(1, node.StateActive)
	n2 := makeNode(2, node.StateActive)
	registry := &fakeRegistry{nodes: map[int64]*node.Node{1: n1, 2: n2}}
	locMap := stream.NewLocationMap()
	probe := &fakeProbe{online: map[string]bool{
		keyFor(1, "ssrc-6"): false,
		keyFor(2, "ssrc-6"): false,
	}}
	lister := &fakeLister{channels: gbmodels.GbChannelList{makeChannel("ssrc-6")}}

	rec := New(1*time.Millisecond, stopper,
		WithRegistry(registry),
		WithLocationMap(locMap),
		WithMediaProbe(probe),
		WithChannelLister(lister))

	stats := rec.runOnce(context.Background())
	if stats.Cleaned != 1 {
		t.Errorf("期望 Cleaned=1(全 offline), 实际 %d", stats.Cleaned)
	}
	calls := stopper.Calls()
	if len(calls) != 1 || calls[0] != "ssrc-6" {
		t.Errorf("期望 stopper.Stop(ssrc-6) 一次, 实际 %v", calls)
	}
}

func TestReconcilerCarriesPersistedSSRCIntoConditionalStop(t *testing.T) {
	stopper := &fakeConditionalStopper{}
	n1 := makeNode(1, node.StateActive)
	registry := &fakeRegistry{nodes: map[int64]*node.Node{1: n1}}
	locMap := stream.NewLocationMap()
	probe := &fakeProbe{online: map[string]bool{keyFor(1, "fixed-stream"): false}}
	channel := makeChannel("fixed-stream")
	channel.CurrentSSRC = "0200000007"
	rec := New(time.Millisecond, stopper,
		WithRegistry(registry), WithLocationMap(locMap), WithMediaProbe(probe),
		WithChannelLister(&fakeLister{channels: gbmodels.GbChannelList{channel}}))

	stats := rec.runOnce(context.Background())
	if stats.Cleaned != 1 || len(stopper.conditional) != 1 {
		t.Fatalf("stats=%+v conditional=%+v", stats, stopper.conditional)
	}
	if got := stopper.conditional[0]; got.streamID != "fixed-stream" || got.ssrc != "0200000007" {
		t.Fatalf("conditional stop=%+v", got)
	}
	if len(stopper.Calls()) != 0 {
		t.Fatalf("conditional stopper fell back to unsafe Stop: %v", stopper.Calls())
	}
}

// T6.7: ZLM 探测报错 -> 不 Stop, Failed 计数
func TestT6_7_ProbeError(t *testing.T) {
	stopper := &fakeStopper{}
	n1 := makeNode(1, node.StateActive)
	registry := &fakeRegistry{nodes: map[int64]*node.Node{1: n1}}
	locMap := stream.NewLocationMap()
	locMap.Bind("ssrc-7", 1)
	probe := &fakeProbe{errs: map[string]error{keyFor(1, "ssrc-7"): errors.New("zlm 500")}}
	lister := &fakeLister{channels: gbmodels.GbChannelList{makeChannel("ssrc-7")}}

	rec := New(1*time.Millisecond, stopper,
		WithRegistry(registry),
		WithLocationMap(locMap),
		WithMediaProbe(probe),
		WithChannelLister(lister))

	stats := rec.runOnce(context.Background())
	if stats.Failed != 1 {
		t.Errorf("期望 Failed=1, 实际 %d", stats.Failed)
	}
	if stats.Cleaned != 0 {
		t.Errorf("期望 Cleaned=0(不清), 实际 %d", stats.Cleaned)
	}
	if len(stopper.Calls()) != 0 {
		t.Errorf("期望 stopper 未调用, 实际 %v", stopper.Calls())
	}
}

// T6.8: Stop 报错 -> 继续下一条, Failed 计数, 无 panic
func TestT6_8_StopError(t *testing.T) {
	stopper := &fakeStopper{stopErr: errors.New("bye failed")}
	n1 := makeNode(1, node.StateActive)
	registry := &fakeRegistry{nodes: map[int64]*node.Node{1: n1}}
	locMap := stream.NewLocationMap()
	locMap.Bind("ssrc-8a", 1)
	locMap.Bind("ssrc-8b", 1)
	probe := &fakeProbe{online: map[string]bool{
		keyFor(1, "ssrc-8a"): false,
		keyFor(1, "ssrc-8b"): false,
	}}
	lister := &fakeLister{channels: gbmodels.GbChannelList{
		makeChannel("ssrc-8a"), makeChannel("ssrc-8b"),
	}}

	rec := New(1*time.Millisecond, stopper,
		WithRegistry(registry),
		WithLocationMap(locMap),
		WithMediaProbe(probe),
		WithChannelLister(lister))

	stats := rec.runOnce(context.Background())
	if stats.Failed != 2 {
		t.Errorf("期望 Failed=2, 实际 %d", stats.Failed)
	}
	if got := len(stopper.Calls()); got != 2 {
		t.Errorf("期望 stopper 被调用 2 次(继续下一条), 实际 %d", got)
	}
}

// T6.9: 重入保护 - 并发 100 次 runOnce 只实际执行 1 次
func TestT6_9_ReentryGuard(t *testing.T) {
	stopper := &fakeStopper{}
	// probe 里加人为阻塞:第一个 runOnce 会卡在 probe 里,其他都被 CAS 挡掉
	release := make(chan struct{})
	var probeCallCount atomic.Int32
	probe := &blockingProbe{
		release: release,
		called:  &probeCallCount,
	}
	lister := &fakeLister{channels: gbmodels.GbChannelList{makeChannel("ssrc-9")}}

	rec := New(1*time.Millisecond, stopper,
		WithMediaProbe(probe),
		WithChannelLister(lister))

	var wg sync.WaitGroup
	// 先启动 1 个"卡住"的 runOnce
	wg.Add(1)
	go func() {
		defer wg.Done()
		rec.runOnce(context.Background())
	}()

	// 稍等,确保上面 goroutine 已经进入 running 状态
	time.Sleep(20 * time.Millisecond)

	// 并发 99 个 runOnce,全部应该被 CAS 挡掉
	for i := 0; i < 99; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec.runOnce(context.Background())
		}()
	}

	// 稍等所有并发 runOnce 完成 CAS 检查(挡掉的会立即返回)
	time.Sleep(50 * time.Millisecond)

	// 释放阻塞
	close(release)
	wg.Wait()

	// 只有 1 次 probe 调用(阻塞那次),其他 99 次被 CAS 挡掉
	if got := probeCallCount.Load(); got != 1 {
		t.Errorf("期望 probe 调用 1 次(其他被 CAS 挡掉), 实际 %d", got)
	}
}

type blockingProbe struct {
	release chan struct{}
	called  *atomic.Int32
}

func (p *blockingProbe) IsMediaOnline(ctx context.Context, n *node.Node, streamID string) (bool, error) {
	p.called.Add(1)
	<-p.release
	return true, nil // online, 不清
}

// T6.10: interval <= 0 时 Start() 不启动 goroutine
func TestT6_10_DisabledInterval(t *testing.T) {
	stopper := &fakeStopper{}
	rec := New(0, stopper)
	rec.Start(context.Background())
	rec.Stop() // 应该立即返回,不 hang

	// runOnce 不应该被自动调用过(Start 里 interval<=0 直接 return)
	// 手工调 runOnce 应该仍能工作
	rec2 := New(-1*time.Second, stopper)
	rec2.Start(context.Background())
	rec2.Stop()

	// 加一个 timeout guard 确保 Start/Stop 不 hang
	done := make(chan struct{})
	go func() {
		rec3 := New(0, stopper)
		rec3.Start(context.Background())
		rec3.Stop()
		close(done)
	}()
	select {
	case <-done:
		// ok
	case <-time.After(500 * time.Millisecond):
		t.Fatal("interval=0 时 Start/Stop 不应该 hang")
	}
}
