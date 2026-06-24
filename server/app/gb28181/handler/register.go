package handler

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// RegisterHandler 处理 REGISTER:401 挑战 → digest 校验(统一密码)→ 自动建档/注销
type RegisterHandler struct {
	cfg               gbconfig.Config
	keepaliveInterval int
	catalogTrigger    CatalogTrigger // 可选:首次注册成功后触发 Catalog 查询
}

// NewRegisterHandler 创建注册处理器
func NewRegisterHandler(cfg gbconfig.Config) *RegisterHandler {
	interval := cfg.Device.KeepaliveInterval
	if interval <= 0 {
		interval = 60
	}
	return &RegisterHandler{cfg: cfg, keepaliveInterval: interval}
}

// SetCatalogTrigger 注入 Catalog 触发器(可选;不注入则不触发)
func (h *RegisterHandler) SetCatalogTrigger(t CatalogTrigger) {
	h.catalogTrigger = t
}

// Handle 处理 REGISTER 请求
func (h *RegisterHandler) Handle(req *sip.Request, tx sip.ServerTransaction) {
	deviceID := ""
	if from := req.From(); from != nil {
		deviceID = from.Address.User
	}
	if deviceID == "" {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 400, "Missing device id", nil))
		return
	}

	// 第一步:无 Authorization → 回 401 挑战
	authHeader := req.GetHeader("Authorization")
	if authHeader == nil {
		chal := digest.Challenge{
			Realm:     h.cfg.SIP.Domain,
			Nonce:     fmt.Sprintf("%d", time.Now().UnixMicro()),
			Algorithm: "MD5",
		}
		res := sip.NewResponseFromRequest(req, 401, "Unauthorized", nil)
		res.AppendHeader(sip.NewHeader("WWW-Authenticate", chal.String()))
		_ = tx.Respond(res)
		return
	}

	// 第二步:校验 digest(统一接入密码)
	cred, err := digest.ParseCredentials(authHeader.Value())
	if err != nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 400, "Bad credentials", nil))
		return
	}
	chal := digest.Challenge{
		Realm:     cred.Realm,
		Nonce:     cred.Nonce,
		Algorithm: cred.Algorithm,
		Opaque:    cred.Opaque,
		QOP:       splitQOP(cred.QOP),
	}
	expected, err := digest.Digest(&chal, digest.Options{
		Method:   string(req.Method),
		URI:      cred.URI,
		Username: cred.Username,
		Password: h.cfg.SIP.Password,
		Count:    cred.Nc,
		Cnonce:   cred.Cnonce,
	})
	if err != nil || expected.Response != cred.Response {
		app.ZapLog.Warn("GB28181 注册鉴权失败", zap.String("deviceId", deviceID))
		_ = tx.Respond(sip.NewResponseFromRequest(req, 401, "Unauthorized", nil))
		return
	}

	// 鉴权通过:判断注册 or 注销
	expires := parseExpires(req)
	ctx := context.Background()
	if expires == 0 {
		// 注销
		if err := device.HandleUnregister(ctx, deviceID); err != nil {
			app.ZapLog.Error("GB28181 注销处理失败", zap.String("deviceId", deviceID), zap.Error(err))
		}
		app.ZapLog.Info("GB28181 设备注销", zap.String("deviceId", deviceID))
		_ = tx.Respond(buildOKWithExpires(req, 0))
		return
	}

	// 自动建档 + 在线态
	ip, port := splitHostPort(req.Source())
	info := device.RegisterInfo{
		DeviceID:  deviceID,
		Transport: req.Transport(),
		IP:        ip,
		Port:      port,
		Expires:   expires,
	}
	isFirst, err := device.HandleRegister(ctx, info, h.keepaliveInterval)
	if err != nil {
		app.ZapLog.Error("GB28181 自动建档失败", zap.String("deviceId", deviceID), zap.Error(err))
		_ = tx.Respond(sip.NewResponseFromRequest(req, 500, "Server error", nil))
		return
	}
	app.ZapLog.Info("GB28181 设备注册成功",
		zap.String("deviceId", deviceID),
		zap.String("transport", req.Transport()),
		zap.Bool("isFirst", isFirst))
	_ = tx.Respond(buildOKWithExpires(req, expires))

	// 首次注册(或离线后重连)→ 触发一次 Catalog 查询拉通道树
	// 放在响应之后:不阻塞 200 OK,失败仅记日志
	if isFirst && h.catalogTrigger != nil {
		dest := fmt.Sprintf("%s:%d", ip, port)
		h.catalogTrigger.Trigger(ctx, deviceID, dest)
	}
}

// buildOKWithExpires 构造 200 OK 并回带 Expires/Date
func buildOKWithExpires(req *sip.Request, expires int) *sip.Response {
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	res.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	res.AppendHeader(sip.NewHeader("Date", time.Now().Format("2006-01-02T15:04:05")))
	return res
}

// parseExpires 从请求取 Expires(优先 Expires 头)
func parseExpires(req *sip.Request) int {
	if h := req.GetHeader("Expires"); h != nil {
		if v, err := strconv.Atoi(h.Value()); err == nil {
			return v
		}
	}
	return 3600
}

func splitHostPort(src string) (string, int) {
	host, portStr, err := net.SplitHostPort(src)
	if err != nil {
		return src, 0
	}
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func splitQOP(qop string) []string {
	if qop == "" {
		return nil
	}
	return []string{qop}
}
