package controllers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/cachehelper"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

// T2 — QRController HTTP 契约.
// 前端(T5/T6)与模拟器(T7)都依赖这里定下的请求/响应形状.
// spec: wiki/projects/uvp-gb28181/specs/qr-sip-provisioning.md §5

// useRealResponseHandler 强制本用例使用生产的 DefaultResponseHandler,并在结束后恢复.
//
// 同包其他测试(dashboard_test / zlm_node_test)在 init 里把全局 app.Response 换成
// mockResponse,而那个 mock 的 Fail 硬编码 c.JSON(http.StatusOK, ...) 忽略 httpCode。
// 本 task 的契约恰好要断言 400 / 410 / 503,必须用真实 handler,否则全部假通过。
// 参照 device_scope_test.go 的既有 save/restore 模式。
func useRealResponseHandler(t *testing.T) {
	t.Helper()
	prev := app.Response
	app.Response = response.NewResponseHandler()
	t.Cleanup(func() { app.Response = prev })
}

func newQRRouter(t *testing.T, seedConfig bool, transport []string) (*gin.Engine, app.CacheInterf) {
	t.Helper()
	useRealResponseHandler(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbsetup.SIPConfig{}))

	if seedConfig {
		row := gbsetup.SIPConfig{
			ID:             gbsetup.SingletonID,
			DeploymentMode: gbsetup.DeploymentLAN,
			ListenIP:       "0.0.0.0",
			AdvertiseIP:    "192.168.1.10",
			Port:           5061,
			Domain:         "3402000000",
			ServerID:       "34020000002000000001",
			Password:       "Str0ng!Passw0rd#2026",
		}
		require.NoError(t, db.Save(&row).Error)
	}

	cache := cachehelper.NewMemoryHelper()
	t.Cleanup(func() { _ = cache.Close() })

	ctrl := gbcontrollers.NewConfiguredQRController(db, cache, transport)

	r := gin.New()
	r.Use(gin.Recovery())
	// 模拟真实注册形状:token 在 protected 组语义下,exchange 在 public 组语义下,
	// 二者路径同为 /api/gb28181/...
	r.POST("/api/gb28181/sip/qr/token", ctrl.GenerateToken)
	r.POST("/api/gb28181/sip/qr/exchange", ctrl.Exchange)
	r.GET("/gb28181/qr", ctrl.Landing)
	return r, cache
}

// envelope 是项目统一响应包装
type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func doJSON(t *testing.T, r *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func mustToken(t *testing.T, r *gin.Engine) string {
	t.Helper()
	rr := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/token", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var env envelope
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &env))
	var data gbcontrollers.QRTokenResponse
	require.NoError(t, json.Unmarshal(env.Data, &data))
	return data.Token
}

// 2.1 生成 token 返回契约字段
func TestQRToken_Contract(t *testing.T) {
	r, _ := newQRRouter(t, true, []string{"udp"})

	rr := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/token", nil)
	require.Equal(t, http.StatusOK, rr.Code)

	var env envelope
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &env))
	require.Equal(t, 0, env.Code)

	var data gbcontrollers.QRTokenResponse
	require.NoError(t, json.Unmarshal(env.Data, &data))
	require.Len(t, data.Token, 22)
	require.Equal(t, 300, data.ExpiresInSeconds, "返回相对秒数,前端以到达时刻起算倒计时")
}

// 2.2 正常兑换
func TestQRExchange_Success(t *testing.T) {
	r, _ := newQRRouter(t, true, []string{"udp"})
	token := mustToken(t, r)

	rr := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/exchange",
		gbcontrollers.QRExchangeRequest{Token: token})
	require.Equal(t, http.StatusOK, rr.Code)

	var env envelope
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &env))
	require.Equal(t, 0, env.Code)

	var payload gbsetup.QRPayload
	require.NoError(t, json.Unmarshal(env.Data, &payload))
	require.Equal(t, "34020000002000000001", payload.ServerID)
	require.Equal(t, "3402000000", payload.Domain)
	require.Equal(t, "192.168.1.10", payload.IP)
	require.Equal(t, 5061, payload.Port)
	require.Equal(t, "udp", payload.Transport)
	require.Equal(t, "Str0ng!Passw0rd#2026", payload.Password)
}

// 2.3 缺 token 字段
func TestQRExchange_MissingToken(t *testing.T) {
	r, _ := newQRRouter(t, true, []string{"udp"})

	rr := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/exchange",
		gbcontrollers.QRExchangeRequest{})
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

// 2.4 格式非法的 token
func TestQRExchange_MalformedToken(t *testing.T) {
	r, _ := newQRRouter(t, true, []string{"udp"})

	rr := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/exchange",
		gbcontrollers.QRExchangeRequest{Token: "abc"})
	require.Equal(t, http.StatusBadRequest, rr.Code)

	var env envelope
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &env))
	require.Contains(t, env.Message, "损坏")
}

// 2.5 二次兑换返 410
func TestQRExchange_SecondTimeGone(t *testing.T) {
	r, _ := newQRRouter(t, true, []string{"udp"})
	token := mustToken(t, r)

	first := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/exchange",
		gbcontrollers.QRExchangeRequest{Token: token})
	require.Equal(t, http.StatusOK, first.Code)

	second := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/exchange",
		gbcontrollers.QRExchangeRequest{Token: token})
	require.Equal(t, http.StatusGone, second.Code)
}

// 2.6 过期与已消费文案完全相同 —— C1 契约
// 后端无法区分二者(cache 里都是 key 不存在),对外必须统一措辞,
// 否则会泄露 token 是否曾有效.
func TestQRExchange_ExpiredAndConsumedSameMessage(t *testing.T) {
	r, _ := newQRRouter(t, true, []string{"udp"})

	// 情况 A:已消费
	consumed := mustToken(t, r)
	doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/exchange",
		gbcontrollers.QRExchangeRequest{Token: consumed})
	rrA := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/exchange",
		gbcontrollers.QRExchangeRequest{Token: consumed})

	// 情况 B:从未存在(等价于过期后 key 已消失)
	rrB := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/exchange",
		gbcontrollers.QRExchangeRequest{Token: "AAAAAAAAAAAAAAAAAAAAAA"})

	require.Equal(t, rrA.Code, rrB.Code)

	var envA, envB envelope
	require.NoError(t, json.Unmarshal(rrA.Body.Bytes(), &envA))
	require.NoError(t, json.Unmarshal(rrB.Body.Bytes(), &envB))
	require.Equal(t, envA.Message, envB.Message,
		"已消费与已过期必须返回完全相同的文案")
}

// 2.7 SIP 未配置时不发码
func TestQRToken_SIPNotConfigured(t *testing.T) {
	r, _ := newQRRouter(t, false, []string{"udp"})

	rr := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/token", nil)
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

// 2.8 兑换响应带 Cache-Control: no-store
func TestQRExchange_NoStoreHeader(t *testing.T) {
	r, _ := newQRRouter(t, true, []string{"udp"})
	token := mustToken(t, r)

	rr := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/exchange",
		gbcontrollers.QRExchangeRequest{Token: token})
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "no-store", rr.Header().Get("Cache-Control"),
		"响应含明文凭据,禁止中间层缓存")
}

// 2.9 引导页可访问且不消费 token
func TestQRLanding_Accessible(t *testing.T) {
	r, _ := newQRRouter(t, true, []string{"udp"})

	req := httptest.NewRequest(http.MethodGet, "/gb28181/qr", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "text/html")
	require.Contains(t, rr.Body.String(), "扫一扫")
}

// 2.10 引导页带 token 查询参数也不消费 —— D6 的核心验证
// v1 设计(GET ?t=)下系统相机一扫就会消费掉 token 并明文显示密码.
func TestQRLanding_DoesNotConsumeToken(t *testing.T) {
	r, _ := newQRRouter(t, true, []string{"udp"})
	token := mustToken(t, r)

	req := httptest.NewRequest(http.MethodGet, "/gb28181/qr?t="+token, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	// token 必须仍然可兑换
	ex := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/exchange",
		gbcontrollers.QRExchangeRequest{Token: token})
	require.Equal(t, http.StatusOK, ex.Code,
		"引导页不得消费 token —— 否则通用扫码 App 一扫就废掉二维码")
}

// 未装配 svc 时返回 503(照 platform controller 惯例)
func TestQRController_NotWired(t *testing.T) {
	useRealResponseHandler(t)
	ctrl := gbcontrollers.NewQRController()
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/gb28181/sip/qr/token", ctrl.GenerateToken)
	r.POST("/api/gb28181/sip/qr/exchange", ctrl.Exchange)

	rr := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/token", nil)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)

	rr2 := doJSON(t, r, http.MethodPost, "/api/gb28181/sip/qr/exchange",
		gbcontrollers.QRExchangeRequest{Token: "AAAAAAAAAAAAAAAAAAAAAA"})
	require.Equal(t, http.StatusServiceUnavailable, rr2.Code)
}
