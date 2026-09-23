package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

// 用真 HTTP 服务器跑 SSE,httptest.NewRecorder 不模拟流式
func newSSEServer(dc *DashboardController) *httptest.Server {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/stream", dc.Stream)
	return httptest.NewServer(r)
}

// T2.1-U1: 客户端连上立刻收到首屏 snapshot 帧
func TestSSE_FirstSnapshotImmediately(t *testing.T) {
	agg := metrics.NewAggregator()
	dc := NewDashboardController(func() *metrics.Aggregator { return agg })
	srv := newSSEServer(dc)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type=%q, want text/event-stream", resp.Header.Get("Content-Type"))
	}

	// 读首帧(应该立刻拿到 snapshot)
	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("read: %v", err)
	}
	body := string(buf[:n])
	if !strings.Contains(body, "event: snapshot") {
		t.Errorf("first frame missing event: snapshot, got: %q", body)
	}
	if !strings.Contains(body, "\"health\"") {
		t.Errorf("first frame missing health field, got: %q", body)
	}
}

// T2.1-U2: 客户端断开自动 unsubscribe(进程不泄漏 goroutine)
// 这个测试比较弱:没有直接探针看 stream goroutine 是否退出
// 我们只验证服务端 close 后再发请求仍正常,handler 不卡死
func TestSSE_ClientDisconnect(t *testing.T) {
	agg := metrics.NewAggregator()
	dc := NewDashboardController(func() *metrics.Aggregator { return agg })
	srv := newSSEServer(dc)
	defer srv.Close()

	// 客户端建连后立刻 cancel
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// 读一帧确认连上,然后 cancel
	buf := make([]byte, 1024)
	_, _ = resp.Body.Read(buf)
	cancel()
	resp.Body.Close()

	// 再发一次,确认服务器没卡死
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	req2, _ := http.NewRequestWithContext(ctx2, http.MethodGet, srv.URL+"/stream", nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("second connect: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Errorf("second status=%d, want 200", resp2.StatusCode)
	}
}

// T2.1-U?: nil provider 也能流(返回 emptySnapshot)
func TestSSE_NilProvider(t *testing.T) {
	dc := NewDashboardController(nil)
	srv := newSSEServer(dc)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "event: snapshot") {
		t.Errorf("nil provider should still send empty snapshot, got: %q", body)
	}
}
