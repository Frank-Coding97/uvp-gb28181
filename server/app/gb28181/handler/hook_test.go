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
	return e
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

// TestHookOnStreamChangedRegistPublishes T7 兼测: regist=true 触发 Notifier.Publish
func TestHookOnStreamChangedRegistPublishes(t *testing.T) {
	n := stream.NewNotifier()
	h := handler.NewHookController(n)
	e := newHookEngine(t, h)

	ch := n.Subscribe("stream-pub-test")
	defer n.Unsubscribe("stream-pub-test")

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
	defer n.Unsubscribe("stream-unreg")

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
