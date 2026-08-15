package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// mockStopper 计数版,验证 hook 是否触发停播
type mockStopper struct {
	calls atomic.Int32
	last  atomic.Value // string
	err   error
}

type mockGenerationStopper struct {
	mockStopper
	current         stream.LiveRef
	pending         stream.LiveRef
	conditional     atomic.Int32
	noneReaderCalls atomic.Int32
}

func (m *mockGenerationStopper) CurrentLiveRef(streamID string) (stream.LiveRef, bool) {
	return m.current, m.current.StreamID == streamID
}

func (m *mockGenerationStopper) CleanupPendingLiveRef(streamID string) (stream.LiveRef, bool) {
	return m.pending, m.pending.StreamID == streamID
}

func (m *mockGenerationStopper) StopIfCurrent(context.Context, stream.LiveRef) (bool, error) {
	m.conditional.Add(1)
	return true, nil
}

func (m *mockGenerationStopper) StopOnNoneReader(context.Context, stream.LiveRef) (bool, error) {
	m.noneReaderCalls.Add(1)
	return true, nil
}

type mockNodeResolver struct {
	uuid   string
	nodeID int64
}

func (m mockNodeResolver) IDForUUID(uuid string) (int64, bool) {
	return m.nodeID, uuid == m.uuid
}

type mockNoneReaderPolicy struct {
	close bool
	err   error
}

type mockSourceLeaseChecker struct{ active bool }

func (m mockSourceLeaseChecker) HasLease(string) bool { return m.active }

type mockStreamObserver struct {
	calls      atomic.Int32
	registered atomic.Bool
	received   chan struct{}
	release    chan struct{}
}

type mockPlaybackMediaSink struct {
	calls atomic.Int32
	last  atomic.Value
}

type mockFlowCollector struct {
	last handler.FlowReport
	err  error
}

func (m *mockFlowCollector) CollectFlow(_ context.Context, report handler.FlowReport) error {
	m.last = report
	return m.err
}

func (m *mockPlaybackMediaSink) OnPlaybackStreamEnded(_ context.Context, streamID, reason string) error {
	m.calls.Add(1)
	m.last.Store(streamID + ":" + reason)
	return nil
}

func (m *mockStreamObserver) ObserveStream(context.Context, string, bool) error {
	m.calls.Add(1)
	m.registered.Store(true)
	close(m.received)
	<-m.release
	return nil
}

func (m *mockNoneReaderPolicy) ShouldCloseOnNoneReader(context.Context, string) (bool, error) {
	return m.close, m.err
}

func (m *mockStopper) Stop(ctx context.Context, streamID string) error {
	m.calls.Add(1)
	m.last.Store(streamID)
	return m.err
}

// newHookEngine 构造一个挂了 hook 路由的测试 engine
func newHookEngine(t *testing.T, h *handler.HookController) *gin.Engine {
	t.Helper()
	if app.ZapLog == nil {
		app.ZapLog = zap.NewNop()
	}
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.POST("/index/hook/on_stream_changed", h.OnStreamChanged)
	e.POST("/index/hook/on_stream_none_reader", h.OnStreamNoneReader)
	e.POST("/index/hook/on_rtp_server_timeout", h.OnRtpServerTimeout)
	e.POST("/index/hook/on_flow_report", h.OnFlowReport)
	return e
}

func TestHookOnFlowReportForwardsNormalizedPayloadAndFailsOpen(t *testing.T) {
	collector := &mockFlowCollector{err: context.DeadlineExceeded}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetFlowCollector(collector)
	e := newHookEngine(t, h)

	rr := postJSON(t, e, "/index/hook/on_flow_report", map[string]interface{}{
		"id": "sid-1", "mediaServerId": "ms-1", "schema": "ws", "vhost": "__defaultVhost__",
		"app": "rtp", "stream": "stream-1", "player": true, "totalBytes": 1234,
		"duration": 9, "ip": "10.0.0.8", "port": 4567,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	assertHookCode(t, rr.Code, rr.Body.Bytes(), 0)
	if collector.last.ID != "sid-1" || !collector.last.Player || collector.last.TotalBytes != 1234 || collector.last.Duration != 9 {
		t.Fatalf("flow report 未正确转发: %#v", collector.last)
	}
}

func postJSON(t *testing.T, e *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	return rr
}

// TestHookOnStreamNoneReaderTriggersStop T7-测1: 无人观看 hook → close=true + 异步发 BYE
func TestHookOnStreamNoneReaderTriggersStop(t *testing.T) {
	n := stream.NewNotifier()
	stopper := &mockStopper{}
	h := handler.NewHookController(n)
	h.SetPlayStopper(stopper)
	e := newHookEngine(t, h)

	rr := postJSON(t, e, "/index/hook/on_stream_none_reader", gin.H{
		"app":    "rtp",
		"stream": "0123456781",
		"schema": "rtsp",
	})
	if rr.Code != 200 {
		t.Fatalf("hook 应返 200,实际 %d", rr.Code)
	}

	var body map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body["code"].(float64) != 0 {
		t.Errorf("应返 code=0,实际 %v", body["code"])
	}
	if close, ok := body["close"].(bool); !ok || !close {
		t.Errorf("应返 close=true 让 ZLM 立即关流,实际 %v", body["close"])
	}

	// stopper 是 goroutine 调用,等一下
	if !waitInt32(&stopper.calls, 1, 500*time.Millisecond) {
		t.Fatalf("应触发 stopper.Stop 一次,实际 %d", stopper.calls.Load())
	}
	if got, _ := stopper.last.Load().(string); got != "0123456781" {
		t.Errorf("Stop 收到 streamID 不符: %q", got)
	}
}

func TestHookFixedNoneReaderDefersCloseToConditionalStop(t *testing.T) {
	streamID := "37010301021320000014_37010301021320000001"
	stopper := &mockGenerationStopper{current: stream.LiveRef{
		StreamID: streamID, SSRC: "0200000007", Generation: 7, NodeID: 22,
	}}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayStopper(stopper)
	h.SetNoneReaderPolicy(&mockNoneReaderPolicy{close: true})
	e := newHookEngine(t, h)
	rr := postJSON(t, e, "/index/hook/on_stream_none_reader", gin.H{"app": "rtp", "stream": streamID, "schema": "fmp4"})
	var body map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if closeStream, ok := body["close"].(bool); !ok || closeStream {
		t.Fatalf("fixed none-reader must return close=false: %v", body)
	}
	if !waitInt32(&stopper.noneReaderCalls, 1, 500*time.Millisecond) {
		t.Fatalf("conditional none-reader calls=%d", stopper.noneReaderCalls.Load())
	}
	if stopper.calls.Load() != 0 {
		t.Fatalf("fixed none-reader used unsafe Stop %d times", stopper.calls.Load())
	}
}

func TestHookFixedNoneReaderRetriesCleanupPendingGeneration(t *testing.T) {
	streamID := "37010301021320000014_37010301021320000001"
	stopper := &mockGenerationStopper{pending: stream.LiveRef{
		StreamID: streamID, SSRC: "0200000007", Generation: 7, NodeID: 22,
	}}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayStopper(stopper)
	h.SetNoneReaderPolicy(&mockNoneReaderPolicy{close: true})
	e := newHookEngine(t, h)
	response := postJSON(t, e, "/index/hook/on_stream_none_reader", gin.H{"app": "rtp", "stream": streamID})
	var body map[string]interface{}
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	if closeStream, ok := body["close"].(bool); !ok || closeStream {
		t.Fatalf("fixed cleanup-pending none-reader must return close=false: %v", body)
	}
	if !waitInt32(&stopper.conditional, 1, 500*time.Millisecond) {
		t.Fatalf("cleanup-pending conditional calls=%d", stopper.conditional.Load())
	}
	if stopper.noneReaderCalls.Load() != 0 || stopper.calls.Load() != 0 {
		t.Fatalf("cleanup-pending used reader probe or unsafe stop: noneReader=%d unsafe=%d", stopper.noneReaderCalls.Load(), stopper.calls.Load())
	}
}

func TestHookStreamOfflineAndRTPTimeoutNotifyPlaybackFinalizer(t *testing.T) {
	h := handler.NewHookController(stream.NewNotifier())
	sink := &mockPlaybackMediaSink{}
	h.SetPlaybackMediaSink(sink)
	e := newHookEngine(t, h)
	postJSON(t, e, "/index/hook/on_stream_changed", gin.H{"app": "rtp", "stream": "pb-1", "regist": false})
	postJSON(t, e, "/index/hook/on_rtp_server_timeout", gin.H{"app": "rtp", "stream_id": "pb-2"})
	if !waitInt32(&sink.calls, 2, 500*time.Millisecond) {
		t.Fatalf("playback finalizer calls=%d", sink.calls.Load())
	}
}

// TestHookOnStreamNoneReaderNoStopper T7-测2: 未注入 stopper 也能正常返回(降级)
func TestHookOnStreamNoneReaderNoStopper(t *testing.T) {
	n := stream.NewNotifier()
	h := handler.NewHookController(n) // 不调 SetPlayStopper
	e := newHookEngine(t, h)
	rr := postJSON(t, e, "/index/hook/on_stream_none_reader", gin.H{"stream": "any"})
	if rr.Code != 200 {
		t.Fatalf("hook 应 200,实际 %d", rr.Code)
	}
}

func TestHookOnStreamNoneReaderDisabledPolicyKeepsStream(t *testing.T) {
	n := stream.NewNotifier()
	stopper := &mockStopper{}
	h := handler.NewHookController(n)
	h.SetPlayStopper(stopper)
	h.SetNoneReaderPolicy(&mockNoneReaderPolicy{close: false})
	e := newHookEngine(t, h)

	rr := postJSON(t, e, "/index/hook/on_stream_none_reader", gin.H{"stream": "stream-keep"})
	if rr.Code != 200 {
		t.Fatalf("hook 应 200,实际 %d", rr.Code)
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if close, ok := body["close"].(bool); !ok || close {
		t.Errorf("关闭按需直播时应返 close=false,实际 %v", body["close"])
	}
	time.Sleep(50 * time.Millisecond)
	if got := stopper.calls.Load(); got != 0 {
		t.Errorf("关闭按需直播时不应触发 Stop,实际 %d", got)
	}
}

func TestHookOnStreamNoneReaderKeepsCascadeLeaseBeforeBrowserPolicy(t *testing.T) {
	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayStopper(&mockStopper{})
	h.SetNoneReaderPolicy(&mockNoneReaderPolicy{close: true})
	h.SetSourceLeaseChecker(mockSourceLeaseChecker{active: true})
	e := newHookEngine(t, h)

	rr := postJSON(t, e, "/index/hook/on_stream_none_reader", gin.H{"app": "rtp", "stream": "cascade-stream"})
	if rr.Code != http.StatusOK {
		t.Fatalf("hook 应返 200,实际 %d", rr.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析 hook 响应失败: %v", err)
	}
	if close, ok := body["close"].(bool); !ok || close {
		t.Fatalf("有级联 lease 时应保持 close=false,实际 %v", body["close"])
	}
}

func TestHookOnStreamNoneReaderPolicyErrorKeepsLegacyClose(t *testing.T) {
	n := stream.NewNotifier()
	h := handler.NewHookController(n)
	h.SetNoneReaderPolicy(&mockNoneReaderPolicy{err: context.Canceled})
	e := newHookEngine(t, h)

	rr := postJSON(t, e, "/index/hook/on_stream_none_reader", gin.H{"stream": "stream-error"})
	var body map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if close, ok := body["close"].(bool); !ok || !close {
		t.Errorf("策略查询失败时应保持 close=true,实际 %v", body["close"])
	}
}

// TestHookOnRtpServerTimeoutTriggersStop T7-测3: RTP 超时 hook → 自动清理会话
func TestHookOnRtpServerTimeoutTriggersStop(t *testing.T) {
	n := stream.NewNotifier()
	stopper := &mockStopper{}
	h := handler.NewHookController(n)
	h.SetPlayStopper(stopper)
	e := newHookEngine(t, h)

	rr := postJSON(t, e, "/index/hook/on_rtp_server_timeout", gin.H{
		"stream_id": "0123456782",
		"app":       "rtp",
		"ssrc":      "0123456782",
	})
	if rr.Code != 200 {
		t.Fatalf("hook 应 200,实际 %d", rr.Code)
	}
	if !waitInt32(&stopper.calls, 1, 500*time.Millisecond) {
		t.Fatalf("RTP 超时应触发 Stop,实际 %d", stopper.calls.Load())
	}
	if got, _ := stopper.last.Load().(string); got != "0123456782" {
		t.Errorf("Stop 收到 stream_id 不符: %q", got)
	}
}

func TestHookFixedRTPTimeoutRequiresMatchingNodeAndNormalizedSSRC(t *testing.T) {
	streamID := "37010301021320000014_37010301021320000001"
	stopper := &mockGenerationStopper{current: stream.LiveRef{
		StreamID: streamID, SSRC: "0200000007", Generation: 7, NodeID: 22,
	}}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayStopper(stopper)
	h.SetMultiNode(mockNodeResolver{uuid: "node-a", nodeID: 22}, nil)
	e := newHookEngine(t, h)
	postJSON(t, e, "/index/hook/on_rtp_server_timeout", gin.H{
		"stream_id": streamID, "app": "rtp", "ssrc": 200000007, "mediaServerId": "node-a",
	})
	if !waitInt32(&stopper.conditional, 1, 500*time.Millisecond) {
		t.Fatalf("matching timeout conditional calls=%d", stopper.conditional.Load())
	}
	postJSON(t, e, "/index/hook/on_rtp_server_timeout", gin.H{
		"stream_id": streamID, "app": "rtp", "ssrc": 200000008, "mediaServerId": "node-a",
	})
	time.Sleep(50 * time.Millisecond)
	if stopper.conditional.Load() != 1 || stopper.calls.Load() != 0 {
		t.Fatalf("mismatched timeout changed stream: conditional=%d unsafe=%d", stopper.conditional.Load(), stopper.calls.Load())
	}
}

func TestHookFixedRTPTimeoutRetriesMatchingCleanupPendingGeneration(t *testing.T) {
	streamID := "37010301021320000014_37010301021320000001"
	stopper := &mockGenerationStopper{pending: stream.LiveRef{
		StreamID: streamID, SSRC: "0200000007", Generation: 7, NodeID: 22,
	}}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayStopper(stopper)
	h.SetMultiNode(mockNodeResolver{uuid: "node-a", nodeID: 22}, nil)
	e := newHookEngine(t, h)
	postJSON(t, e, "/index/hook/on_rtp_server_timeout", gin.H{
		"stream_id": streamID, "app": "rtp", "ssrc": 200000007, "mediaServerId": "node-a",
	})
	if !waitInt32(&stopper.conditional, 1, 500*time.Millisecond) {
		t.Fatalf("cleanup-pending timeout conditional calls=%d", stopper.conditional.Load())
	}
}

func TestHookStreamOfflineRetriesMatchingCleanupPendingGeneration(t *testing.T) {
	streamID := "37010301021320000014_37010301021320000001"
	stopper := &mockGenerationStopper{pending: stream.LiveRef{
		StreamID: streamID, SSRC: "0200000007", Generation: 7, NodeID: 22,
	}}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayStopper(stopper)
	h.SetMultiNode(mockNodeResolver{uuid: "node-a", nodeID: 22}, nil)
	e := newHookEngine(t, h)
	postJSON(t, e, "/index/hook/on_stream_changed", gin.H{
		"app": "rtp", "stream": streamID, "regist": false, "mediaServerId": "node-a",
	})
	if !waitInt32(&stopper.conditional, 1, 500*time.Millisecond) {
		t.Fatalf("cleanup-pending offline conditional calls=%d", stopper.conditional.Load())
	}
}

// TestHookOnStreamChangedRegistPublishes T7 兼测: regist=true 触发 Notifier.Publish
func TestHookOnStreamChangedRegistPublishes(t *testing.T) {
	n := stream.NewNotifier()
	h := handler.NewHookController(n)
	e := newHookEngine(t, h)

	ch := n.Subscribe("stream-pub-test")
	defer n.Unsubscribe("stream-pub-test", ch)

	rr := postJSON(t, e, "/index/hook/on_stream_changed", gin.H{
		"app":    "rtp",
		"stream": "stream-pub-test",
		"schema": "rtsp",
		"regist": true,
	})
	if rr.Code != 200 {
		t.Fatalf("hook 应 200,实际 %d", rr.Code)
	}
	select {
	case <-ch:
	case <-time.After(200 * time.Millisecond):
		t.Error("regist=true 应触发 notifier.Publish")
	}
}

// TestHookOnStreamChangedRegistFalseNoPublish T7 兼测: regist=false 不发 Publish
func TestHookOnStreamChangedRegistFalseNoPublish(t *testing.T) {
	n := stream.NewNotifier()
	h := handler.NewHookController(n)
	e := newHookEngine(t, h)

	ch := n.Subscribe("stream-unreg")
	defer n.Unsubscribe("stream-unreg", ch)

	rr := postJSON(t, e, "/index/hook/on_stream_changed", gin.H{
		"app": "rtp", "stream": "stream-unreg", "regist": false,
	})
	if rr.Code != 200 {
		t.Fatal("hook 应 200")
	}
	select {
	case <-ch:
		t.Error("regist=false 不应触发 Publish")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestHookOnStreamChangedDispatchesObserverAsynchronously(t *testing.T) {
	observer := &mockStreamObserver{received: make(chan struct{}), release: make(chan struct{})}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetStreamObserver(observer)
	e := newHookEngine(t, h)

	start := time.Now()
	rr := postJSON(t, e, "/index/hook/on_stream_changed", gin.H{
		"app": "rtp", "stream": "stream-recording", "regist": true,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("hook 应 200,实际 %d", rr.Code)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("hook 不应同步等待录像对账")
	}
	select {
	case <-observer.received:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("应异步投递 stream observer")
	}
	close(observer.release)
}

func waitInt32(c *atomic.Int32, target int32, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c.Load() >= target {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return c.Load() >= target
}
