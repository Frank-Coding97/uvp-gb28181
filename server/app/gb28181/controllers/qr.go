package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/controllers"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	gbsetup "uvplatform.com/uvp-gb28181/app/gb28181/setup"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/middleware"
)

// 扫码回填 SIP 接入信息 —— 平台侧 HTTP 端点.
// spec: wiki/projects/uvp-gb28181/specs/qr-sip-provisioning.md

// QRTokenResponse 生成接入二维码所需的一次性 token.
//
// ExpiresInSeconds 是相对秒数而非绝对时间戳:前端以响应到达时刻起算倒计时,
// 免受客户端时钟偏移影响.
type QRTokenResponse struct {
	Token            string `json:"token"`
	ExpiresInSeconds int    `json:"expiresInSeconds"`
	// BaseURL 设备可达的接入基址,**由后端下发**。
	//
	// ⛔⛔ 2026-10-08 之前前端拿 `window.location.origin` 当默认值,
	// 于是「用户从 nginx 自签 HTTPS 打开平台 → 二维码里带 HTTPS 地址 →
	// 设备侧证书校验失败 → 报『连不上平台,请检查 Wi-Fi』」, 排查全跑偏。
	// 现在地址由平台明确告诉前端, 前端不再自行推断 —— 二维码是给**设备**用的,
	// 不是给浏览器用的, 两者可达地址本来就可能不同。
	BaseURL string `json:"baseUrl"`
}

// QRExchangeRequest 设备端兑换请求.
//
// token 走 POST body 而不是 GET query:query 会被 gin.Logger() 写进访问日志,
// 而且通用扫码 App 打开 http URL 会自动 GET,当场消费掉一次性 token 并在
// 浏览器里明文显示平台密码.
type QRExchangeRequest struct {
	Token string `json:"token"`
}

// QRController 提供接入二维码的 token 生成与兑换.
type QRController struct {
	controllers.Common
	svc *gbsetup.QRService
}

func NewQRController() *QRController {
	return &QRController{}
}

// NewConfiguredQRController 装配 QRController.
// cache 由 bootstrap 传入(app.Cache)而非在此直接读全局变量 —— 便于测试注入内存实现.
//
// baseURLFn 返回设备可达的接入基址;为 nil 时回落读全局 gb28181 配置,
// 避免装配顺序把配置读空。
func NewConfiguredQRController(db *gorm.DB, cache app.CacheInterf, transport []string, providers ...gbsetup.InterfaceProvider) *QRController {
	service := gbsetup.NewQRService(
		cache,
		gbsetup.NewSIPConfigService(db),
		func() []string { return transport },
	)
	if len(providers) > 0 {
		service.SetNetworkProvider(providers[0])
	}
	service.SetBaseURLProvider(gbconfig.CurrentQRProvisionBaseURL)
	return &QRController{svc: service}
}

// SetBaseURLProvider 覆盖设备可达基址的解析(测试注入用).
func (qc *QRController) SetBaseURLProvider(fn func() (string, error)) {
	if qc.svc != nil {
		qc.svc.SetBaseURLProvider(fn)
	}
}

// GenerateToken POST /api/gb28181/sip/qr/token
// 受保护端点,权限点 gb28181:sip:config:view —— 能看明文密码的人才能生成接入码.
func (qc *QRController) GenerateToken(c *gin.Context) {
	if qc.svc == nil {
		qc.Fail(c, "接入二维码服务尚未装配", nil, http.StatusServiceUnavailable)
		return
	}

	token, expiresIn, err := qc.svc.GenerateToken(c.Request.Context())
	if err != nil {
		if errors.Is(err, gbsetup.ErrSIPNotConfigured) {
			qc.Fail(c, "平台 SIP 尚未配置,请先完成 SIP 接入配置", err)
			return
		}
		if errors.Is(err, gbsetup.ErrBaseURLUnavailable) {
			// ⛔ 别退化成一个扫了必失败的码 —— 设备侧只会报「连不上平台」,
			// 排查成本全落在用户身上。直接把真实原因带出去。
			qc.Fail(c, "扫码接入地址未配置,请检查平台 gb28181.qr_provision 配置", err,
				http.StatusServiceUnavailable)
			return
		}
		qc.Fail(c, "生成接入二维码失败", err, http.StatusInternalServerError)
		return
	}

	qc.Success(c, QRTokenResponse{
		Token:            token,
		ExpiresInSeconds: expiresIn,
		BaseURL:          qc.svc.BaseURL(),
	})
}

// Exchange POST /api/gb28181/sip/qr/exchange
//
// 免鉴权端点(挂 public 组):设备端没有登录态,一次性 token 是唯一凭据.
// 响应含平台统一密码明文,故带 Cache-Control: no-store.
func (qc *QRController) Exchange(c *gin.Context) {
	if qc.svc == nil {
		qc.Fail(c, "接入二维码服务尚未装配", nil, http.StatusServiceUnavailable)
		return
	}
	// 请求体携带一次性接入 token(免鉴权端点的唯一凭据):
	// 标记敏感操作,审计中间件只记录资源与结果元数据
	middleware.MarkSensitiveOperation(c, map[string]any{"resource": "gb28181_qr_exchange", "operation": "exchange"})

	var req QRExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Token == "" {
		qc.Fail(c, "二维码内容已损坏", err)
		return
	}

	payload, err := qc.svc.Exchange(c.Request.Context(), req.Token)
	if err != nil {
		switch {
		case errors.Is(err, gbsetup.ErrTokenMalformed):
			qc.Fail(c, "二维码内容已损坏", err)
		case errors.Is(err, gbsetup.ErrTooManyAttempts):
			qc.Fail(c, "尝试过于频繁,请稍后再试", err, http.StatusTooManyRequests)
		case errors.Is(err, gbsetup.ErrTokenInvalid):
			// 过期与已消费刻意不区分 —— 用户处置动作相同(回平台重新生成),
			// 区分反而泄露 token 是否曾有效
			qc.Fail(c, "二维码已失效,请在平台重新生成", err, http.StatusGone)
		default:
			qc.Fail(c, "兑换接入信息失败", err, http.StatusInternalServerError)
		}
		return
	}

	// 响应含明文凭据,禁止任何中间层缓存
	c.Header("Cache-Control", "no-store")
	qc.Success(c, payload)
}

// qrLandingHTML 是普通扫码 App 打开二维码 URL 时看到的引导页.
// 二维码把 token 放在 fragment(#t=...),fragment 不会发给服务端,所以这个页面
// 拿不到也不需要 token —— 它的唯一职责是告诉误扫的人该用什么 App.
const qrLandingHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>UVP 国标平台接入</title>
<style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:#f5f6f8;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#1d2129}
.card{max-width:420px;padding:32px 28px;background:#fff;border-radius:16px;
box-shadow:0 4px 24px rgba(0,0,0,.08);text-align:center}
h1{margin:0 0 12px;font-size:18px;font-weight:600}
p{margin:0;font-size:14px;line-height:1.7;color:#4e5969}
</style>
</head>
<body>
<div class="card">
<h1>请使用国标设备模拟器扫描</h1>
<p>这是 UVP 国标平台的设备接入二维码。<br>请在 UVP 国标模拟器 App 内点击"扫一扫"来完成 SIP 参数自动配置。</p>
</div>
</body>
</html>`

// Landing GET /gb28181/qr
// 挂在 engine 根(非 /api),无鉴权,不读 token 不做任何兑换.
func (qc *QRController) Landing(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(qrLandingHTML))
}
