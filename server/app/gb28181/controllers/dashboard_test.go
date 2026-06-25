package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// 测试用 gin 引擎(关闭日志噪音)
func newTestRouter(dc *DashboardController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	app.Response = mockResponse{}
	r := gin.New()
	r.GET("/snapshot", dc.Snapshot)
	return r
}

// mockResponse 实现 ResponseHandler 的最小子集,直接 c.JSON 写出
type mockResponse struct{}

func (m mockResponse) ReturnJson(c *gin.Context, httpCode int, dataCode int, msg string, data interface{}) {
	c.JSON(httpCode, gin.H{"code": dataCode, "data": data, "message": msg})
}
func (m mockResponse) Success(c *gin.Context, data ...interface{}) {
	var payload interface{}
	if len(data) > 0 {
		payload = data[0]
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": payload, "message": "ok"})
}
func (m mockResponse) Fail(c *gin.Context, msg string, data ...interface{}) {
	c.JSON(http.StatusOK, gin.H{"code": 1, "message": msg})
}
func (m mockResponse) ErrorSystem(c *gin.Context, msg string, data interface{}) {
	c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": msg, "data": data})
}

// T1.8-U1: 空 provider 返回默认空快照(8 个 empty transactions)
func TestDashboard_Snapshot_NilProvider(t *testing.T) {
	dc := NewDashboardController(nil)
	r := newTestRouter(dc)

	req := httptest.NewRequest(http.MethodGet, "/snapshot", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp struct {
		Code int                        `json:"code"`
		Data metrics.DashboardSnapshot  `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if resp.Data.Health != metrics.HealthEmpty {
		t.Errorf("health=%v, want HealthEmpty(-1)", resp.Data.Health)
	}
	if len(resp.Data.Transactions) != 8 {
		t.Errorf("transactions len=%d, want 8", len(resp.Data.Transactions))
	}
	if resp.Data.Pulse.Samples == nil {
		t.Error("pulse.samples should be empty array not nil")
	}
}

// T1.8-U2: 灌真实 aggregator 后字段全对齐
func TestDashboard_Snapshot_WithAggregator(t *testing.T) {
	agg := metrics.NewAggregator()
	// 灌 1 个 REGISTER 成功
	tx := metrics.Transaction{
		Kind: metrics.TxRegister, CallID: "c1", CSeq: "1", StartedAt: time.Now(),
	}
	agg.Begin(tx)
	agg.End("c1", "1", 200, true)

	dc := NewDashboardController(func() *metrics.Aggregator { return agg })
	r := newTestRouter(dc)

	req := httptest.NewRequest(http.MethodGet, "/snapshot?window=60m&precision=1m", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var resp struct {
		Code int                        `json:"code"`
		Data metrics.DashboardSnapshot  `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if resp.Data.TodayTotal != 1 {
		t.Errorf("todayTotal=%d, want 1", resp.Data.TodayTotal)
	}
	// REGISTER 100% 成功 → health 100
	if resp.Data.Health != 100.0 {
		t.Errorf("health=%v, want 100", resp.Data.Health)
	}
	if resp.Data.Transactions[0].KindStr != "REGISTER" {
		t.Errorf("first tx kindStr=%q, want REGISTER", resp.Data.Transactions[0].KindStr)
	}
}

// T1.8 parsePulseParams 覆盖
func TestParsePulseParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var gotWin, gotPrec time.Duration
	r.GET("/p", func(c *gin.Context) {
		gotWin, gotPrec = parsePulseParams(c)
		c.Status(200)
	})

	cases := []struct {
		url      string
		wantWin  time.Duration
		wantPrec time.Duration
	}{
		{"/p", 60 * time.Minute, time.Minute},
		{"/p?window=24h&precision=1m", 24 * time.Hour, time.Minute},
		{"/p?window=6h&precision=10s", 6 * time.Hour, 10 * time.Second},
		{"/p?window=garbage", 60 * time.Minute, time.Minute}, // 解析失败回默认
		{"/p?window=3600", time.Hour, time.Minute},           // 纯数字 = 秒
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.url, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if gotWin != c.wantWin {
			t.Errorf("%s win=%v, want %v", c.url, gotWin, c.wantWin)
		}
		if gotPrec != c.wantPrec {
			t.Errorf("%s prec=%v, want %v", c.url, gotPrec, c.wantPrec)
		}
	}
}
