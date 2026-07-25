package handler

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// RegisterHandler 处理 REGISTER:401 挑战 → digest 校验(统一密码)→ 自动建档/注销
type RegisterHandler struct {
	cfg               gbconfig.Config
	keepaliveInterval int
	platformVersion   string            // X-GB-Ver returned by the platform
	catalogTrigger    CatalogTrigger    // 可选:首次注册成功后触发 Catalog 查询
	deviceInfoTrigger DeviceInfoTrigger // 可选:首次注册成功后触发 DeviceInfo 查询(拉设备本体元数据)
	subscriptionWaker SubscriptionWaker // 可选:设备恢复在线后恢复已启用订阅
	recorder          metrics.Recorder  // 可选:埋点 SIP 事务
}

// NewRegisterHandler 创建注册处理器
func NewRegisterHandler(cfg gbconfig.Config) *RegisterHandler {
	interval := cfg.Device.KeepaliveInterval
	if interval <= 0 {
		interval = 60
	}
	return &RegisterHandler{cfg: cfg, keepaliveInterval: interval, platformVersion: platformXGBVersion(cfg)}
}

// SetCatalogTrigger 注入 Catalog 触发器(可选;不注入则不触发)
func (h *RegisterHandler) SetCatalogTrigger(t CatalogTrigger) {
	h.catalogTrigger = t
}

// SetDeviceInfoTrigger 注入 DeviceInfo 触发器(可选;不注入则不触发)
func (h *RegisterHandler) SetDeviceInfoTrigger(t DeviceInfoTrigger) {
	h.deviceInfoTrigger = t
}

// SetSubscriptionWaker 注入设备恢复在线后的订阅唤醒器。
func (h *RegisterHandler) SetSubscriptionWaker(w SubscriptionWaker) {
	h.subscriptionWaker = w
}

// SetRecorder 注入指标 Recorder(可选,nil 时所有埋点 no-op)
func (h *RegisterHandler) SetRecorder(r metrics.Recorder) {
	h.recorder = r
}

// recordBegin 安全调用 Recorder.Begin(nil 跳过)
func (h *RegisterHandler) recordBegin(req *sip.Request, kind metrics.TxKind, deviceID string) {
	if h.recorder == nil {
		return
	}
	callID, cseq := sipPairKey(req)
	if callID == "" {
		return
	}
	h.recorder.Begin(metrics.Transaction{
		Kind:      kind,
		Direction: metrics.DirIn,
		CallID:    callID,
		CSeq:      cseq,
		DeviceID:  deviceID,
		StartedAt: time.Now(),
	})
}

// recordEnd 安全调用 Recorder.End(nil 跳过)
func (h *RegisterHandler) recordEnd(req *sip.Request, statusCode int, success bool) {
	if h.recorder == nil {
		return
	}
	callID, cseq := sipPairKey(req)
	if callID == "" {
		return
	}
	h.recorder.End(callID, cseq, statusCode, success)
}

// Handle 处理 REGISTER 请求
func (h *RegisterHandler) Handle(req *sip.Request, tx sip.ServerTransaction) {
	deviceID := ""
	if from := req.From(); from != nil {
		deviceID = from.Address.User
	}
	h.recordBegin(req, metrics.TxRegister, deviceID)
	advertised := advertisedRegisterVersion(req)
	if deviceID == "" {
		_ = tx.Respond(h.newResponse(req, 400, "Missing device id", nil))
		h.recordEnd(req, 400, false)
		return
	}

	// 严格校验:平台 ServerID。国标设备在两个位置标识"上级平台编码":
	//   1. Request-URI 的 userpart(GB28181 行业惯例:sip:<serverID>@<host>)
	//   2. To header 的 userpart(RFC 3261 § 10.2 要求 To.uri 就是 AOR)
	// RFC 3261 允许 REGISTER 的 Request-URI 不带 userpart,只有 To 是可靠来源;
	// 但反过来 GB28181 设备总会在 To 里带上级平台编码。因此策略:
	//   - 若 Request-URI userpart 非空 → 必须等于 ServerID;
	//   - 否则(标准 RFC 客户端)→ 回退到 To header userpart,要求等于 ServerID。
	// 若设备侧配置错服务器 ID,即使 IP/端口/密码都对,放行会造成"假在线"
	//(平台后续下行的 Catalog/DeviceInfo/INVITE 因 From userpart 不匹配全部被设备拒绝)。
	claimedServerID := req.Recipient.User
	if claimedServerID == "" {
		if to := req.To(); to != nil {
			claimedServerID = to.Address.User
		}
	}
	if claimedServerID != h.cfg.SIP.ServerID {
		app.ZapLog.Warn("GB28181 注册拒绝:上级平台编码与平台 ServerID 不匹配",
			zap.String("deviceId", deviceID),
			zap.String("gotServerId", claimedServerID),
			zap.String("wantServerId", h.cfg.SIP.ServerID))
		_ = tx.Respond(h.newResponse(req, 403, "Server ID mismatch", nil))
		h.recordEnd(req, 403, false)
		return
	}

	// 第二步:无 Authorization → 回 401 挑战 digest
	authHeader := req.GetHeader("Authorization")
	if authHeader == nil {
		chal := digest.Challenge{
			Realm:     h.cfg.SIP.Domain,
			Nonce:     fmt.Sprintf("%d", time.Now().UnixMicro()),
			Algorithm: "MD5",
		}
		res := h.newResponse(req, 401, "Unauthorized", nil)
		res.AppendHeader(sip.NewHeader("WWW-Authenticate", chal.String()))
		_ = tx.Respond(res)
		// 401 挑战是正常协议握手,不算失败(后续 Authorization 重发会再走一遍 Handle)
		h.recordEnd(req, 401, true)
		return
	}

	// 第三步:校验 digest(统一接入密码)
	cred, err := digest.ParseCredentials(authHeader.Value())
	if err != nil {
		_ = tx.Respond(h.newResponse(req, 400, "Bad credentials", nil))
		h.recordEnd(req, 400, false)
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
		_ = tx.Respond(h.newResponse(req, 401, "Unauthorized", nil))
		h.recordEnd(req, 401, false)
		return
	}

	// 鉴权通过:判断注册 or 注销
	expires := parseExpires(req)
	ctx := context.Background()
	if expires == 0 {
		// 注销
		if err := device.HandleUnregister(ctx, deviceID); err != nil {
			app.ZapLog.Error("GB28181 注销处理失败", zap.String("deviceId", deviceID), zap.Error(err))
			_ = tx.Respond(h.newResponse(req, 500, "Server error", nil))
			h.recordEnd(req, 500, false)
			return
		}
		app.ZapLog.Info("GB28181 设备注销", zap.String("deviceId", deviceID))
		_ = tx.Respond(h.buildOKWithExpires(req, 0))
		h.recordEnd(req, 200, true)
		return
	}

	// 自动建档 + 在线态
	ip, port := splitHostPort(req.Source())
	info := device.RegisterInfo{
		DeviceID:        deviceID,
		Transport:       req.Transport(),
		IP:              ip,
		Port:            port,
		Expires:         expires,
		ReportedVersion: advertised.Raw,
	}
	isFirst, err := device.HandleRegister(ctx, info, h.keepaliveInterval)
	if err != nil {
		app.ZapLog.Error("GB28181 自动建档失败", zap.String("deviceId", deviceID), zap.Error(err))
		_ = tx.Respond(h.newResponse(req, 500, "Server error", nil))
		h.recordEnd(req, 500, false)
		return
	}
	app.ZapLog.Info("GB28181 设备注册成功",
		zap.String("deviceId", deviceID),
		zap.String("transport", req.Transport()),
		zap.Bool("isFirst", isFirst))
	_ = tx.Respond(h.buildOKWithExpires(req, expires))
	h.recordEnd(req, 200, true)

	// 首次注册(或离线后重连)→ 触发 Catalog / DeviceInfo 查询
	// 放在响应之后:不阻塞 200 OK,失败仅记日志。两个查询独立并行,任一失败不影响另一个。
	// transport 沿用本次 REGISTER 的传输协议,避免 TCP 注册的设备被 UDP 出站发不出去
	if isFirst {
		dest := fmt.Sprintf("%s:%d", ip, port)
		transport := req.Transport()
		if h.subscriptionWaker != nil {
			if err := h.subscriptionWaker.WakeDeviceByCode(ctx, deviceID); err != nil {
				app.ZapLog.Warn("GB28181 设备恢复订阅失败", zap.String("deviceId", deviceID), zap.Error(err))
			}
		}
		if h.catalogTrigger != nil {
			h.catalogTrigger.Trigger(ctx, deviceID, dest, transport)
		}
		if h.deviceInfoTrigger != nil {
			h.deviceInfoTrigger.Trigger(ctx, deviceID, dest, transport)
		}
	}
}

const defaultPlatformXGBVersion = "3.0"

// registerVersion carries both the value received from the wire and the
// resolver decision used for a successful device archive update.
type registerVersion struct {
	Raw        string
	Resolution protocol.Resolution
}

func advertisedRegisterVersion(req *sip.Request) registerVersion {
	raw := ""
	if req != nil {
		if header := req.GetHeader("X-GB-Ver"); header != nil {
			raw = header.Value()
		}
	}
	return registerVersion{
		Raw:        raw,
		Resolution: protocol.Resolve(protocol.ResolveInput{Register: raw}),
	}
}

func platformXGBVersion(cfg gbconfig.Config) string {
	version := strings.TrimSpace(cfg.SIP.XGBVersion)
	if version == "" {
		return defaultPlatformXGBVersion
	}
	// Allow the normalized profile names in configuration while keeping the
	// wire header in the version notation used by GB/T 28181 REGISTER.
	switch version {
	case "2016":
		return "2.0"
	case "2022":
		return "3.0"
	case "1.0", "1.1", "2.0", "3.0":
		return version
	default:
		return defaultPlatformXGBVersion
	}
}

func newRegisterResponse(req *sip.Request, status int, reason string, body []byte, platformVersion string) *sip.Response {
	res := sip.NewResponseFromRequest(req, status, reason, body)
	version := strings.TrimSpace(platformVersion)
	if version == "" {
		version = defaultPlatformXGBVersion
	}
	res.AppendHeader(sip.NewHeader("X-GB-Ver", version))
	return res
}

func (h *RegisterHandler) newResponse(req *sip.Request, status int, reason string, body []byte) *sip.Response {
	version := h.platformVersion
	if version == "" {
		version = platformXGBVersion(h.cfg)
	}
	return newRegisterResponse(req, status, reason, body, version)
}

func (h *RegisterHandler) buildOKWithExpires(req *sip.Request, expires int) *sip.Response {
	res := h.newResponse(req, 200, "OK", nil)
	res.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	res.AppendHeader(sip.NewHeader("Date", time.Now().Format("2006-01-02T15:04:05")))
	return res
}

// parseExpires 解析 REGISTER 的有效期。
// RFC 3261 允许把 expires 放在 Contact 参数中;该参数优先于全局 Expires 头,
// 否则设备用 Contact: <sip:...>;expires=0 注销时会被误当成普通注册。
func parseExpires(req *sip.Request) int {
	if contact := req.Contact(); contact != nil {
		for _, param := range contact.Params {
			if !strings.EqualFold(strings.TrimSpace(param.K), "expires") {
				continue
			}
			if v, err := strconv.Atoi(strings.TrimSpace(param.V)); err == nil {
				return v
			}
		}
	}
	if h := req.GetHeader("Expires"); h != nil {
		if v, err := strconv.Atoi(strings.TrimSpace(h.Value())); err == nil {
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
