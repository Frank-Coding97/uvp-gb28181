package handler

import (
	"context"
	"errors"
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
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
	"uvplatform.cn/uvp-gb28181/app/gb28181/trace/diagnosis"
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
	security          RegisterSecurity
	diagnosticSink    diagnosis.DiagnosticSink
	attempts          *registerAttemptTracker
	transactions      *registerTransactionLedger
	now               func() time.Time
	handleRegister    func(context.Context, device.RegisterInfo, int) (bool, error)
	handleUnregister  func(context.Context, string) error
}

// RegisterSecurity keeps authentication protection independent from the SIP
// transport implementation. Runtime supplies persistence, scoring and bans;
// the standalone fallback still provides signed, expiring nonces in tests and
// non-bootstrap uses.
type RegisterSecurity interface {
	IssueNonce() (string, error)
	ValidateNonce(nonce, nonceCount string) error
	Record(gbsecurity.Event) error
	TrustEndpoint(deviceID, transport, address string, expires time.Duration) error
}

type standaloneRegisterSecurity struct{ nonce *gbsecurity.NonceManager }

func newStandaloneRegisterSecurity() RegisterSecurity {
	policy := gbsecurity.DefaultPolicy()
	return &standaloneRegisterSecurity{nonce: gbsecurity.NewNonceManager(nil, policy.NonceTTL, gbsecurity.RealClock())}
}

func (s *standaloneRegisterSecurity) IssueNonce() (string, error) {
	return s.nonce.Issue()
}
func (s *standaloneRegisterSecurity) ValidateNonce(nonce, nonceCount string) error {
	return s.nonce.Validate(nonce, nonceCount)
}
func (s *standaloneRegisterSecurity) ValidateNonceForTransaction(nonce, nonceCount, transactionFingerprint string) error {
	return s.nonce.ValidateForTransaction(nonce, nonceCount, transactionFingerprint)
}
func (s *standaloneRegisterSecurity) Record(gbsecurity.Event) error { return nil }
func (s *standaloneRegisterSecurity) TrustEndpoint(string, string, string, time.Duration) error {
	return nil
}

// NewRegisterHandler 创建注册处理器
func NewRegisterHandler(cfg gbconfig.Config) *RegisterHandler {
	interval := cfg.Device.KeepaliveInterval
	if interval <= 0 {
		interval = 60
	}
	sink := diagnosis.DiagnosticSink(diagnosis.NoopSink{})
	handler := &RegisterHandler{
		cfg: cfg, keepaliveInterval: interval, platformVersion: platformXGBVersion(cfg),
		security: newStandaloneRegisterSecurity(), diagnosticSink: sink, now: time.Now,
		handleRegister: device.HandleRegister, handleUnregister: device.HandleUnregister,
	}
	handler.attempts = newRegisterAttemptTracker(sink, func() time.Time { return handler.now() })
	handler.transactions = newRegisterTransactionLedger(defaultRegisterTransactionTTL, defaultMaxRegisterTransactions, func() time.Time { return handler.now() })
	return handler
}

func (h *RegisterHandler) SetDiagnosticSink(sink diagnosis.DiagnosticSink) {
	if sink == nil {
		sink = diagnosis.NoopSink{}
	}
	h.diagnosticSink = sink
	h.attempts.setSink(sink)
}

// SetSecurity installs the shared SIP security runtime before the server starts.
func (h *RegisterHandler) SetSecurity(security RegisterSecurity) {
	if security != nil {
		h.security = security
	}
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
		h.emitRegisterFailure(req, deviceID, "", diagnosis.CodeInvalidRequest, 400)
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
		h.recordSecurity(req, deviceID, gbsecurity.ReasonServerMismatch)
		app.ZapLog.Warn("GB28181 注册拒绝:上级平台编码与平台 ServerID 不匹配",
			zap.String("deviceId", deviceID),
			zap.String("gotServerId", claimedServerID),
			zap.String("wantServerId", h.cfg.SIP.ServerID))
		_ = tx.Respond(h.newResponse(req, 403, "Server ID mismatch", nil))
		h.recordEnd(req, 403, false)
		h.emitRegisterFailure(req, deviceID, "", diagnosis.CodeServerIDMismatch, 403)
		return
	}

	transactionKey := registerTransactionKey(req, deviceID)
	result, finishTransaction, execute := h.transactions.begin(transactionKey)
	if !execute {
		_ = tx.Respond(result.response(req, h.platformVersion))
		h.recordEnd(req, result.status, result.status == sip.StatusUnauthorized || result.status >= 200 && result.status < 300)
		return
	}
	capture := &capturingRegisterTransaction{ServerTransaction: tx}
	tx = capture
	defer func() {
		finishTransaction(capture.responseResult, capture.captured)
	}()

	// 第二步:无 Authorization → 回 401 挑战 digest
	authHeader := req.GetHeader("Authorization")
	if authHeader == nil {
		status, nonce := h.respondChallenge(req, tx)
		// 401 挑战是正常协议握手,不算失败(后续 Authorization 重发会再走一遍 Handle)
		h.recordEnd(req, status, status == sip.StatusUnauthorized)
		if status == sip.StatusUnauthorized {
			h.trackRegisterChallenge(req, deviceID, nonce)
		} else {
			h.emitRegisterFailure(req, deviceID, "", diagnosis.CodeInternalError, status)
		}
		return
	}

	// 第三步:校验 digest(统一接入密码)
	cred, err := digest.ParseCredentials(authHeader.Value())
	if err != nil {
		h.recordSecurity(req, deviceID, gbsecurity.ReasonDigestFailure)
		status, _ := h.respondChallenge(req, tx)
		h.recordEnd(req, status, false)
		h.attempts.failByCallID(registerCallID(req))
		h.emitRegisterFailure(req, deviceID, "", diagnosis.CodeDigestFailure, status)
		return
	}
	algorithm := strings.ToUpper(strings.TrimSpace(cred.Algorithm))
	if algorithm == "" {
		algorithm = "MD5"
	}
	if cred.Realm != h.cfg.SIP.Domain || cred.Username != deviceID || algorithm != "MD5" {
		h.recordSecurity(req, deviceID, gbsecurity.ReasonDigestFailure)
		status, _ := h.respondChallenge(req, tx)
		h.recordEnd(req, status, false)
		h.failAndEmitRegister(req, deviceID, cred.Nonce, diagnosis.CodeDigestFailure, status)
		return
	}
	chal := digest.Challenge{
		Realm:     cred.Realm,
		Nonce:     cred.Nonce,
		Algorithm: algorithm,
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
		h.recordSecurity(req, deviceID, gbsecurity.ReasonDigestFailure)
		app.ZapLog.Warn("GB28181 注册鉴权失败", zap.String("deviceId", deviceID))
		status, _ := h.respondChallenge(req, tx)
		h.recordEnd(req, status, false)
		h.failAndEmitRegister(req, deviceID, cred.Nonce, diagnosis.CodeDigestFailure, status)
		return
	}
	if err := h.validateNonce(cred.Nonce, digestNonceCount(cred.Nc), transactionKey, deviceID); err != nil {
		h.recordSecurity(req, deviceID, nonceFailureReason(err))
		status, freshNonce := h.respondChallenge(req, tx)
		h.recordEnd(req, status, false)
		h.failAndEmitRegister(req, deviceID, cred.Nonce, diagnosisCodeForNonceError(err), status)
		if status == sip.StatusUnauthorized {
			h.trackRegisterChallenge(req, deviceID, freshNonce)
		}
		return
	}

	// 鉴权通过:判断注册 or 注销
	expires := parseExpires(req)
	ctx := context.Background()
	if expires == 0 {
		// 注销
		if err := h.handleUnregister(ctx, deviceID); err != nil {
			app.ZapLog.Error("GB28181 注销处理失败", zap.String("deviceId", deviceID), zap.Error(err))
			_ = tx.Respond(h.newResponse(req, 500, "Server error", nil))
			h.recordEnd(req, 500, false)
			h.failAndEmitRegister(req, deviceID, cred.Nonce, diagnosis.CodeInternalError, 500)
			return
		}
		app.ZapLog.Info("GB28181 设备注销", zap.String("deviceId", deviceID))
		_ = tx.Respond(h.buildOKWithExpires(req, 0))
		h.recordEnd(req, 200, true)
		h.finishRegisterAttempt(req, deviceID, cred.Nonce)
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
	isFirst, err := h.handleRegister(ctx, info, h.keepaliveInterval)
	if err != nil {
		status, reason := registerFailureResponse(err)
		app.ZapLog.Error("GB28181 注册状态更新失败", zap.String("deviceId", deviceID), zap.Error(err))
		_ = tx.Respond(h.newResponse(req, status, reason, nil))
		h.recordEnd(req, status, false)
		code := diagnosis.CodeInternalError
		if errors.Is(err, device.ErrDeviceNotPreallocated) {
			code = diagnosis.CodeDeviceNotPreallocated
		}
		h.failAndEmitRegister(req, deviceID, cred.Nonce, code, status)
		return
	}
	if err := h.security.TrustEndpoint(deviceID, req.Transport(), ip, time.Duration(expires)*time.Second); err != nil {
		app.ZapLog.Warn("GB28181 注册安全端点更新失败", zap.String("deviceId", deviceID), zap.Error(err))
	}
	app.ZapLog.Info("GB28181 设备注册成功",
		zap.String("deviceId", deviceID),
		zap.String("transport", req.Transport()),
		zap.Bool("isFirst", isFirst))
	_ = tx.Respond(h.buildOKWithExpires(req, expires))
	h.recordEnd(req, 200, true)
	h.finishRegisterAttempt(req, deviceID, cred.Nonce)

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
		if h.catalogTrigger != nil && gbconfig.SyncChannelsOnOnline() {
			h.catalogTrigger.Trigger(ctx, deviceID, dest, transport)
		}
		if h.deviceInfoTrigger != nil {
			h.deviceInfoTrigger.Trigger(ctx, deviceID, dest, transport)
		}
	}
}

func registerFailureResponse(err error) (int, string) {
	if errors.Is(err, device.ErrDeviceNotPreallocated) {
		return sip.StatusForbidden, "Device not preallocated"
	}
	return sip.StatusInternalServerError, "Server error"
}

func digestNonceCount(count int) string {
	if count <= 0 {
		return ""
	}
	return fmt.Sprintf("%08x", count)
}

type transactionNonceValidator interface {
	ValidateNonceForTransaction(nonce, nonceCount, transactionFingerprint string) error
}

func (h *RegisterHandler) validateNonce(nonce, nonceCount, transactionFingerprint, deviceID string) error {
	var err error
	if validator, ok := h.security.(transactionNonceValidator); ok {
		err = validator.ValidateNonceForTransaction(nonce, nonceCount, transactionFingerprint)
	} else {
		err = h.security.ValidateNonce(nonce, nonceCount)
	}
	if errors.Is(err, gbsecurity.ErrNonceExpired) && h.attempts.hasChallenge(deviceID, nonce) {
		return gbsecurity.ErrNonceStale
	}
	return err
}

func (h *RegisterHandler) respondChallenge(req *sip.Request, tx sip.ServerTransaction) (int, string) {
	nonce, err := h.security.IssueNonce()
	if err != nil {
		_ = tx.Respond(h.newResponse(req, sip.StatusInternalServerError, "Server error", nil))
		return sip.StatusInternalServerError, ""
	}
	challenge := digest.Challenge{Realm: h.cfg.SIP.Domain, Nonce: nonce, Algorithm: "MD5"}
	res := h.newResponse(req, sip.StatusUnauthorized, "Unauthorized", nil)
	res.AppendHeader(sip.NewHeader("WWW-Authenticate", challenge.String()))
	_ = tx.Respond(res)
	return sip.StatusUnauthorized, nonce
}

func (h *RegisterHandler) recordSecurity(req *sip.Request, deviceID string, reason gbsecurity.Reason) {
	sourceIP, _ := splitHostPort(req.Source())
	_ = h.security.Record(gbsecurity.Event{
		SourceIP: sourceIP, Transport: req.Transport(), Method: string(req.Method),
		DeviceID: deviceID, Reason: reason, Action: gbsecurity.ActionSample,
	})
}

func nonceFailureReason(err error) gbsecurity.Reason {
	switch {
	case errors.Is(err, gbsecurity.ErrNonceExpired):
		return gbsecurity.ReasonNonceExpired
	case errors.Is(err, gbsecurity.ErrNonceReplay):
		return gbsecurity.ReasonNonceReplay
	case errors.Is(err, gbsecurity.ErrNonceStale):
		return gbsecurity.ReasonNonceStale
	default:
		return gbsecurity.ReasonNonceInvalid
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
