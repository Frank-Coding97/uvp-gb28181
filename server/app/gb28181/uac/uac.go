package uac

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
)

const trackedMessageSummaryLimit = 4 * 1024

type messageDoFunc func(context.Context, *sip.Request) (*sip.Response, error)
type localIPResolver func(destination string) (string, error)

// UAC 平台主叫客户端:向下级设备发起 SIP 请求(MESSAGE 查询 / INVITE 点播)
type UAC struct {
	client           *sipgo.Client
	talkDialogs      *talkDialogStore
	playbackDialogs  *PlaybackDialogStore
	serverID         string
	domain           string
	sipPort          int
	advertiseIP      string
	dynamicAdvertise bool
	resolveLocalIP   localIPResolver
	recorder         metrics.Recorder // 可选:埋点出向事务
	doMessage        messageDoFunc    // MESSAGE 专用测试 seam;生产绑定 client.Do
	inviteTransport  inviteDialogTransport
	playbackEndMu    sync.RWMutex
	playbackEndHook  func(context.Context, PlaybackDialogMetadata, string) error

	// outCSeq 给本端构造的 MESSAGE/INVITE 生成稳定 CSeq,
	// 配合 generated Call-ID 用于 metrics 配对
	outCSeq uint64
}

// New 创建 UAC,复用 server 的 UserAgent
// 关键:不要 WithClientPort 抢 server 已绑定的 5061,否则 client 走备选 socket
// 设备应答会回到 server 端口但 client dialog 收不到 → WaitAnswer 永久阻塞
// 让 sipgo 默认共享 server 的 transport;Contact 头我们手动写明 sipIP:sipPort
func New(ua *sipgo.UserAgent, serverID, domain, advertiseIP string, sipPort int, dynamicAdvertise bool) (*UAC, error) {
	clientOptions := make([]sipgo.ClientOption, 0, 1)
	if !dynamicAdvertise {
		clientOptions = append(clientOptions, sipgo.WithClientHostname(advertiseIP))
	}
	client, err := sipgo.NewClient(ua, clientOptions...)
	if err != nil {
		return nil, fmt.Errorf("创建 UAC client 失败: %w", err)
	}
	u := &UAC{
		client:           client,
		talkDialogs:      newTalkDialogStore(&sipgoTalkDialogTransport{client: client}),
		playbackDialogs:  NewPlaybackDialogStore(&sipgoPlaybackDialogTransport{client: client}),
		serverID:         serverID,
		domain:           domain,
		sipPort:          sipPort,
		advertiseIP:      advertiseIP,
		dynamicAdvertise: dynamicAdvertise,
		resolveLocalIP:   resolveRouteLocalIP,
	}
	u.inviteTransport = &sipgoInviteDialogTransport{client: client}
	u.doMessage = func(ctx context.Context, req *sip.Request) (*sip.Response, error) {
		return client.Do(ctx, req)
	}
	return u, nil
}

func resolveRouteLocalIP(destination string) (string, error) {
	connection, err := net.DialTimeout("udp", destination, time.Second)
	if err != nil {
		return "", fmt.Errorf("解析 SIP 目的地址路由失败: %w", err)
	}
	defer connection.Close()
	host, _, err := net.SplitHostPort(connection.LocalAddr().String())
	if err != nil {
		return "", fmt.Errorf("解析 SIP 本地路由地址失败: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() {
		return "", fmt.Errorf("SIP 本地路由没有可用 IPv4 地址: %s", host)
	}
	return ip.To4().String(), nil
}

func isLoopbackIPv4Destination(destination string) bool {
	host, _, err := net.SplitHostPort(destination)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() != nil && ip.IsLoopback()
}

func (u *UAC) outboundIP(destination string) (string, error) {
	if !u.dynamicAdvertise {
		ip := net.ParseIP(u.advertiseIP)
		if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsLoopback() {
			return "", fmt.Errorf("SIP 宣告地址不可用: %q", u.advertiseIP)
		}
		return ip.To4().String(), nil
	}
	if u.resolveLocalIP == nil {
		return "", fmt.Errorf("SIP 本地路由解析器未配置")
	}
	ipText, err := u.resolveLocalIP(destination)
	if err != nil {
		return "", err
	}
	ip := net.ParseIP(ipText)
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() ||
		(ip.IsLoopback() && !isLoopbackIPv4Destination(destination)) {
		return "", fmt.Errorf("SIP 本地路由返回不可用 IPv4 地址: %q", ipText)
	}
	return ip.To4().String(), nil
}

func (u *UAC) prepareOutboundRequest(req *sip.Request, destination, transport string, includeContact bool) error {
	req.SetDestination(destination)
	req.SetTransport(normalizeTransport(transport))
	localIP, err := u.outboundIP(destination)
	if err != nil {
		return err
	}
	params := sip.NewParams()
	params.Add("branch", sip.GenerateBranchN(16))
	req.AppendHeader(&sip.ViaHeader{
		ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: req.Transport(),
		Host: localIP, Port: u.sipPort, Params: params,
	})
	if includeContact {
		contact := platformContact(u.serverID, localIP, u.sipPort)
		req.AppendHeader(&contact)
	}
	return nil
}

func platformContact(serverID, advertiseIP string, sipPort int) sip.ContactHeader {
	return sip.ContactHeader{
		Address: sip.Uri{User: serverID, Host: advertiseIP, Port: sipPort},
	}
}

// SetRecorder 注入指标 Recorder(可选)
func (u *UAC) SetRecorder(r metrics.Recorder) {
	u.recorder = r
}

func (u *UAC) SetPlaybackEndHook(hook func(context.Context, PlaybackDialogMetadata, string) error) {
	u.playbackEndMu.Lock()
	u.playbackEndHook = hook
	u.playbackEndMu.Unlock()
}

func (u *UAC) playbackEnded(ctx context.Context, metadata PlaybackDialogMetadata, reason string) error {
	u.playbackEndMu.RLock()
	hook := u.playbackEndHook
	u.playbackEndMu.RUnlock()
	if hook == nil {
		return nil
	}
	return hook(ctx, metadata, reason)
}

// nextCSeq 生成单调递增 CSeq(metrics 配对 key 的一部分)
func (u *UAC) nextCSeq() string {
	return strconv.FormatUint(atomic.AddUint64(&u.outCSeq, 1), 10)
}

// detectMessageKind 通过 MANSCDP body 判断本次 MESSAGE 属于哪类事务
// Catalog / RecordInfo / DeviceControl(PTZ) 三类我们主动发起
func detectMessageKind(body []byte) metrics.TxKind {
	if bytes.Contains(body, []byte("Catalog")) {
		return metrics.TxCatalog
	}
	if bytes.Contains(body, []byte("RecordInfo")) {
		return metrics.TxRecord
	}
	if bytes.Contains(body, []byte("DeviceControl")) || bytes.Contains(body, []byte("PTZCmd")) ||
		bytes.Contains(body, []byte("PTZPrecise")) || bytes.Contains(body, []byte("HomePositionQuery")) ||
		bytes.Contains(body, []byte("CruiseTrackQuery")) || bytes.Contains(body, []byte("PTZPreciseStatusQuery")) ||
		bytes.Contains(body, []byte("PTZPosition")) {
		return metrics.TxPTZ
	}
	return metrics.TxUnknown
}

func (u *UAC) deviceURI(deviceID string) sip.Uri {
	uri := sip.Uri{}
	sip.ParseUri(fmt.Sprintf("sip:%s@%s", deviceID, u.domain), &uri)
	return uri
}

// platformFromHeader 构造平台端 From 头(GB28181 § 9.1.1 要求)
// From URI userpart 必须是平台国标编码(serverID),设备端会据此校验上级平台身份。
// 之前不显式设置时,sipgo client 会用 UserAgent 名(如 "UVP-GB28181")兜底填 User,
// 结果被合规设备/模拟器判为"未授权来源"直接丢弃。
func (u *UAC) platformFromHeader() *sip.FromHeader {
	h := &sip.FromHeader{
		Address: sip.Uri{User: u.serverID, Host: u.domain},
		Params:  sip.NewParams(),
	}
	h.Params.Add("tag", sip.GenerateTagN(16))
	return h
}

// recordBegin / recordEnd 给 UAC 出向事务埋点
// callID 由调用方传入(用真实 SIP Call-ID 头);若 recorder nil 则 no-op
func (u *UAC) recordBegin(kind metrics.TxKind, callID, cseq, deviceID string) {
	if u.recorder == nil || callID == "" {
		return
	}
	u.recorder.Begin(metrics.Transaction{
		Kind:      kind,
		Direction: metrics.DirOut,
		CallID:    callID,
		CSeq:      cseq,
		DeviceID:  deviceID,
		StartedAt: time.Now(),
	})
}

func (u *UAC) recordEnd(callID, cseq string, statusCode int, success bool) {
	if u.recorder == nil || callID == "" {
		return
	}
	u.recorder.End(callID, cseq, statusCode, success)
}

// normalizeTransport 归一化 transport,兜底 UDP
// 允许空/大小写混用;非 UDP/TCP 一律按 UDP(sipgo 里 transport 头大小写敏感)
func normalizeTransport(transport string) string {
	switch strings.ToUpper(strings.TrimSpace(transport)) {
	case "TCP":
		return "TCP"
	default:
		return "UDP"
	}
}

type TrackedMessageRequest struct {
	DeviceID    string
	Destination string
	Transport   string
	Body        []byte
	CallID      string
	CSeq        string
}

type TrackedMessageResult struct {
	CallID       string
	CSeq         string
	StatusCode   int
	ResponseBody []byte
	Attempted    bool
	ErrorSummary string
}

func is2xx(statusCode int) bool {
	return statusCode >= 200 && statusCode < 300
}

func trackedMessageResponseSummary(body []byte) []byte {
	summary := gbtrace.RedactSIP(body)
	if len(summary) > trackedMessageSummaryLimit {
		summary = summary[:trackedMessageSummaryLimit]
	}
	return append([]byte(nil), summary...)
}

func trackedMessageErrorSummary(err error) string {
	if err == nil {
		return ""
	}
	return string(trackedMessageResponseSummary([]byte(err.Error())))
}

func (u *UAC) buildTrackedMessageRequest(in TrackedMessageRequest) (*sip.Request, TrackedMessageResult, error) {
	if u == nil {
		return nil, TrackedMessageResult{}, fmt.Errorf("SIP UAC 未就绪")
	}
	if strings.TrimSpace(in.DeviceID) == "" || strings.TrimSpace(in.Destination) == "" {
		return nil, TrackedMessageResult{}, fmt.Errorf("MESSAGE 缺少设备或目的地址")
	}
	req := sip.NewRequest(sip.MESSAGE, u.deviceURI(in.DeviceID))
	req.SetBody(in.Body)
	req.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSCDP+xml"))
	req.AppendHeader(u.platformFromHeader())
	if err := u.prepareOutboundRequest(req, in.Destination, in.Transport, false); err != nil {
		return nil, TrackedMessageResult{}, err
	}
	callID := strings.TrimSpace(in.CallID)
	if callID == "" {
		callID = fmt.Sprintf("message-%d", time.Now().UnixNano())
	}
	cseq := strings.TrimSpace(in.CSeq)
	if cseq == "" {
		cseq = u.nextCSeq()
	}
	seq, err := strconv.ParseUint(cseq, 10, 32)
	if err != nil || seq == 0 {
		return nil, TrackedMessageResult{}, fmt.Errorf("MESSAGE CSeq 不合法: %q", cseq)
	}
	callIDHeader := sip.CallIDHeader(callID)
	req.AppendHeader(&callIDHeader)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: uint32(seq), MethodName: sip.MESSAGE})
	return req, TrackedMessageResult{CallID: callID, CSeq: cseq}, nil
}

// SendMessageTracked sends a MESSAGE and returns SIP correlation metadata.
func (u *UAC) SendMessageTracked(ctx context.Context, deviceID, dest, transport string, body []byte) (TrackedMessageResult, error) {
	if u == nil || u.doMessage == nil {
		err := fmt.Errorf("SIP UAC 未就绪")
		return TrackedMessageResult{ErrorSummary: trackedMessageErrorSummary(err)}, err
	}
	ctx, cancel := withSIPCommandTimeout(ctx)
	defer cancel()
	req, result, err := u.buildTrackedMessageRequest(TrackedMessageRequest{DeviceID: deviceID, Destination: dest, Transport: transport, Body: body})
	if err != nil {
		result.ErrorSummary = trackedMessageErrorSummary(err)
		return result, err
	}
	kind := detectMessageKind(body)
	u.recordBegin(kind, result.CallID, result.CSeq, deviceID)
	result.Attempted = true
	resp, err := u.doMessage(ctx, req)
	if err != nil {
		u.recordEnd(result.CallID, result.CSeq, 0, false)
		err = fmt.Errorf("发送 MESSAGE 失败: %w", err)
		result.ErrorSummary = trackedMessageErrorSummary(err)
		return result, err
	}
	result.StatusCode = int(resp.StatusCode)
	result.ResponseBody = trackedMessageResponseSummary(resp.Body())
	if !is2xx(resp.StatusCode) {
		u.recordEnd(result.CallID, result.CSeq, int(resp.StatusCode), false)
		err = fmt.Errorf("MESSAGE 应答非2xx: %d %s", resp.StatusCode, resp.Reason)
		result.ErrorSummary = trackedMessageErrorSummary(err)
		return result, err
	}
	u.recordEnd(result.CallID, result.CSeq, int(resp.StatusCode), true)
	return result, nil
}

// SendMessage 向设备发 MESSAGE(承载 MANSCDP XML,如 Catalog 查询)
// transport 从设备注册记录(gb_device.transport)读出来,避免硬编码 UDP 导致 TCP 设备发不出去
func (u *UAC) SendMessage(ctx context.Context, deviceID, dest, transport string, body []byte) error {
	_, err := u.SendMessageTracked(ctx, deviceID, dest, transport, body)
	return err
}

// SubscriptionRequest contains the durable dialog metadata needed to establish, renew, or cancel a SUBSCRIBE dialog.
type SubscriptionRequest struct {
	DeviceID    string
	Destination string
	Transport   string
	Event       string
	Body        []byte
	Expires     int
	CallID      string
	LocalTag    string
	RemoteTag   string
	CSeq        uint
}

type SubscriptionResponse struct {
	StatusCode int
	Expires    int
	CallID     string
	LocalTag   string
	RemoteTag  string
	CSeq       uint
}

func (u *UAC) buildSubscribeRequest(in SubscriptionRequest) (*sip.Request, error) {
	if strings.TrimSpace(in.DeviceID) == "" || strings.TrimSpace(in.Destination) == "" || strings.TrimSpace(in.Event) == "" {
		return nil, fmt.Errorf("SUBSCRIBE 缺少设备、目的地址或 Event")
	}
	req := sip.NewRequest(sip.SUBSCRIBE, u.deviceURI(in.DeviceID))
	req.SetBody(in.Body)
	if err := u.prepareOutboundRequest(req, in.Destination, in.Transport, false); err != nil {
		return nil, err
	}
	req.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSCDP+xml"))
	req.AppendHeader(sip.NewHeader("Event", in.Event))
	req.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(max(in.Expires, 0))))

	from := u.platformFromHeader()
	if in.LocalTag != "" {
		from.Params.Add("tag", in.LocalTag)
	}
	to := &sip.ToHeader{Address: u.deviceURI(in.DeviceID), Params: sip.NewParams()}
	if in.RemoteTag != "" {
		to.Params.Add("tag", in.RemoteTag)
	}
	req.AppendHeader(from)
	req.AppendHeader(to)

	callID := in.CallID
	if callID == "" {
		callID = fmt.Sprintf("subscription-%d", time.Now().UnixNano())
	}
	callIDHeader := sip.CallIDHeader(callID)
	req.AppendHeader(&callIDHeader)
	cseq := in.CSeq
	if cseq == 0 {
		n, err := strconv.ParseUint(u.nextCSeq(), 10, 32)
		if err != nil {
			return nil, err
		}
		cseq = uint(n)
	}
	req.AppendHeader(&sip.CSeqHeader{SeqNo: uint32(cseq), MethodName: sip.SUBSCRIBE})
	return req, nil
}

// SendSubscribe sends one SUBSCRIBE transaction and returns the dialog metadata needed for renew/cancel.
func (u *UAC) SendSubscribe(ctx context.Context, in SubscriptionRequest) (SubscriptionResponse, error) {
	if u == nil || u.client == nil {
		return SubscriptionResponse{}, fmt.Errorf("SIP UAC 未就绪")
	}
	ctx, cancel := withSIPCommandTimeout(ctx)
	defer cancel()
	req, err := u.buildSubscribeRequest(in)
	if err != nil {
		return SubscriptionResponse{}, err
	}
	resp, err := u.client.Do(ctx, req)
	if err != nil {
		return SubscriptionResponse{}, fmt.Errorf("发送 SUBSCRIBE 失败: %w", err)
	}
	out := SubscriptionResponse{StatusCode: int(resp.StatusCode), Expires: in.Expires, CallID: in.CallID, LocalTag: in.LocalTag, RemoteTag: in.RemoteTag, CSeq: in.CSeq}
	if h := resp.To(); h != nil {
		if tag, ok := h.Params.Get("tag"); ok {
			out.RemoteTag = tag
		}
	}
	if h := req.CallID(); h != nil {
		out.CallID = string(*h)
	}
	if h := req.From(); h != nil {
		out.LocalTag, _ = h.Params.Get("tag")
	}
	if h := req.CSeq(); h != nil {
		out.CSeq = uint(h.SeqNo)
	}
	if header := resp.GetHeaders("Expires"); len(header) > 0 {
		if n, parseErr := strconv.Atoi(header[0].Value()); parseErr == nil && n >= 0 {
			out.Expires = n
		}
	}
	if !is2xx(resp.StatusCode) {
		return out, fmt.Errorf("SUBSCRIBE 应答非2xx: %d %s", resp.StatusCode, resp.Reason)
	}
	return out, nil
}

// extractKeyFromRequest 从已构造的请求里取 Call-ID + CSeq 作为 metrics 配对 key
// sipgo 在 client.Do 内部会补 Call-ID/CSeq,这里我们提前读;若不存在补一对
func (u *UAC) extractKeyFromRequest(req *sip.Request) (string, string) {
	var callID, cseq string
	if h := req.CallID(); h != nil {
		callID = string(*h)
	}
	if h := req.CSeq(); h != nil {
		cseq = strconv.FormatUint(uint64(h.SeqNo), 10)
	}
	if callID == "" {
		// 没有 Call-ID(请求还没被 sipgo 加工)→ 用时间戳生成一个临时 key,
		// 仅为 metrics 配对用,不影响 SIP transport(transport 会补真实的)
		callID = fmt.Sprintf("uac-%d", time.Now().UnixNano())
	}
	if cseq == "" {
		cseq = u.nextCSeq()
	}
	return callID, cseq
}

// ===== 点播会话 =====

type SessionState int

const (
	StateIdle SessionState = iota
	StateInviting
	StateEstablished
	StateBye
)

// Session 一路点播会话
type Session struct {
	DeviceID   string
	ChannelID  string
	RequestID  string
	SSRC       string
	StreamID   string
	Generation uint64
	NodeID     int64
	Dest       string
	Transport  string // 传输协议(UDP/TCP),对应 gb_device.transport;空值兜底 UDP
	State      SessionState
	dialog     inviteDialog
	createdAt  time.Time
}

type SessionRef struct {
	StreamID   string
	SSRC       string
	Generation uint64
	NodeID     int64
}

func (s *Session) Ref() SessionRef {
	if s == nil {
		return SessionRef{}
	}
	return SessionRef{StreamID: s.StreamID, SSRC: s.SSRC, Generation: s.Generation, NodeID: s.NodeID}
}

// SessionManager 会话管理(内存)
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]*Session)}
}

func (m *SessionManager) Get(streamID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[streamID]
}

func (m *SessionManager) GetCurrent(streamID string) (*Session, bool) {
	session := m.Get(streamID)
	return session, session != nil
}

// PutIfCurrent rejects stale generations and conflicting writes for the same
// generation. Generation zero retains the legacy last-write behavior.
func (m *SessionManager) PutIfCurrent(s *Session) bool {
	if s == nil || s.StreamID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.sessions[s.StreamID]
	if ok && current.Generation > 0 && s.Generation == 0 {
		return false
	}
	if ok && s.Generation > 0 && current.Generation > 0 {
		if current.Generation > s.Generation {
			return false
		}
		if current.Generation == s.Generation && current.Ref() != s.Ref() {
			return false
		}
	}
	m.sessions[s.StreamID] = s
	return true
}

func (m *SessionManager) remove(streamID string) {
	m.mu.Lock()
	delete(m.sessions, streamID)
	m.mu.Unlock()
}

func (m *SessionManager) RemoveIfCurrent(ref SessionRef) bool {
	_, ok := m.TakeIfCurrent(ref)
	return ok
}

// TakeIfCurrent atomically removes and returns exactly one matching live
// session. A stale generation cannot remove a newer dialog stored under the
// same fixed stream ID.
func (m *SessionManager) TakeIfCurrent(ref SessionRef) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.sessions[ref.StreamID]
	if !ok || current.Ref() != ref {
		return nil, false
	}
	delete(m.sessions, ref.StreamID)
	return current, true
}

func (u *UAC) buildInviteRequest(s *Session, sdpBody string) (*sip.Request, error) {
	req := sip.NewRequest(sip.INVITE, u.deviceURI(s.ChannelID))
	req.SetBody([]byte(sdpBody))
	req.AppendHeader(sip.NewHeader("Subject", fmt.Sprintf("%s:%s,%s:0", s.ChannelID, s.SSRC, u.serverID)))
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.AppendHeader(u.platformFromHeader())
	if err := u.prepareOutboundRequest(req, s.Dest, s.Transport, true); err != nil {
		return nil, err
	}
	return req, nil
}

// Invite preserves the legacy error-only contract. New callers should use
// InviteTracked when they need correlation and SIP-stage facts.
func (u *UAC) Invite(ctx context.Context, m *SessionManager, s *Session, sdpBody string) error {
	_, err := u.InviteTracked(ctx, m, s, sdpBody)
	return err
}

// Bye 停止点播
func (u *UAC) Bye(ctx context.Context, m *SessionManager, streamID string) error {
	s := m.Get(streamID)
	if s == nil {
		return nil
	}
	_, err := u.ByeIfCurrent(ctx, m, s.Ref())
	return err
}

// ByeIfCurrent sends BYE only for the exact live generation identified by
// ref. The session is atomically taken before network I/O so duplicate or late
// cleanup events cannot target a newer dialog.
func (u *UAC) ByeIfCurrent(ctx context.Context, m *SessionManager, ref SessionRef) (bool, error) {
	s, ok := m.TakeIfCurrent(ref)
	if !ok {
		return false, nil
	}
	if s.dialog == nil {
		s.State = StateBye
		return true, nil
	}
	// 为 BYE 单独记一次出向事务(用临时 key,跟 INVITE 的 dialog Call-ID 区分)
	callID := fmt.Sprintf("bye-%s-%d", ref.StreamID, time.Now().UnixNano())
	cseq := u.nextCSeq()
	u.recordBegin(metrics.TxBye, callID, cseq, s.DeviceID)

	err := s.dialog.Bye(ctx)
	s.State = StateBye
	if err != nil {
		u.recordEnd(callID, cseq, 0, false)
		return true, fmt.Errorf("BYE 失败: %w", err)
	}
	u.recordEnd(callID, cseq, 200, true)
	return true, nil
}
