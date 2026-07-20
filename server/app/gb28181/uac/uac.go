package uac

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

// UAC 平台主叫客户端:向下级设备发起 SIP 请求(MESSAGE 查询 / INVITE 点播)
type UAC struct {
	client   *sipgo.Client
	dialogUA *sipgo.DialogClientCache // 管理 INVITE 对话(Ack/Bye)
	serverID string
	domain   string
	recorder metrics.Recorder // 可选:埋点出向事务

	// outCSeq 给本端构造的 MESSAGE/INVITE 生成稳定 CSeq,
	// 配合 generated Call-ID 用于 metrics 配对
	outCSeq uint64
}

// New 创建 UAC,复用 server 的 UserAgent
// 关键:不要 WithClientPort 抢 server 已绑定的 5061,否则 client 走备选 socket
// 设备应答会回到 server 端口但 client dialog 收不到 → WaitAnswer 永久阻塞
// 让 sipgo 默认共享 server 的 transport;Contact 头我们手动写明 sipIP:sipPort
func New(ua *sipgo.UserAgent, serverID, domain, advertiseIP string, sipPort int) (*UAC, error) {
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname(advertiseIP))
	if err != nil {
		return nil, fmt.Errorf("创建 UAC client 失败: %w", err)
	}
	// Contact 头:平台自身地址,设备回包/BYE 用(端口写 server 监听端口,确保设备应答能回)
	contact := platformContact(serverID, advertiseIP, sipPort)
	dialogUA := sipgo.NewDialogClientCache(client, contact)
	return &UAC{client: client, dialogUA: dialogUA, serverID: serverID, domain: domain}, nil
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
		bytes.Contains(body, []byte("CruiseTrackQuery")) || bytes.Contains(body, []byte("PTZPreciseStatusQuery")) {
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
	req.SetDestination(in.Destination)
	req.SetTransport(normalizeTransport(in.Transport))
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
	if u == nil || u.client == nil {
		return TrackedMessageResult{}, fmt.Errorf("SIP UAC 未就绪")
	}
	req, result, err := u.buildTrackedMessageRequest(TrackedMessageRequest{DeviceID: deviceID, Destination: dest, Transport: transport, Body: body})
	if err != nil {
		return result, err
	}
	kind := detectMessageKind(body)
	u.recordBegin(kind, result.CallID, result.CSeq, deviceID)
	resp, err := u.client.Do(ctx, req)
	if err != nil {
		u.recordEnd(result.CallID, result.CSeq, 0, false)
		return result, fmt.Errorf("发送 MESSAGE 失败: %w", err)
	}
	result.StatusCode = int(resp.StatusCode)
	result.ResponseBody = append([]byte(nil), resp.Body()...)
	if resp.StatusCode != 200 {
		u.recordEnd(result.CallID, result.CSeq, int(resp.StatusCode), false)
		return result, fmt.Errorf("MESSAGE 应答非200: %d %s", resp.StatusCode, resp.Reason)
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
	req.SetDestination(in.Destination)
	req.SetTransport(normalizeTransport(in.Transport))
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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
	DeviceID  string
	ChannelID string
	SSRC      string
	StreamID  string
	Dest      string
	Transport string // 传输协议(UDP/TCP),对应 gb_device.transport;空值兜底 UDP
	State     SessionState
	dialog    *sipgo.DialogClientSession
	createdAt time.Time
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

func (m *SessionManager) put(s *Session) {
	m.mu.Lock()
	m.sessions[s.StreamID] = s
	m.mu.Unlock()
}

func (m *SessionManager) remove(streamID string) {
	m.mu.Lock()
	delete(m.sessions, streamID)
	m.mu.Unlock()
}

func (u *UAC) buildInviteRequest(s *Session, sdpBody string) *sip.Request {
	req := sip.NewRequest(sip.INVITE, u.deviceURI(s.ChannelID))
	req.SetBody([]byte(sdpBody))
	req.AppendHeader(sip.NewHeader("Subject", fmt.Sprintf("%s:%s,%s:0", s.ChannelID, s.SSRC, u.serverID)))
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.AppendHeader(u.platformFromHeader())
	req.SetDestination(s.Dest)
	req.SetTransport(normalizeTransport(s.Transport))
	return req
}

// Invite 发起点播:INVITE → 等应答 → ACK,会话建立
// 关键 1:sipgo v1.4 的 WaitAnswer 内部 select 不响应外部 ctx.Done(),
//
//	这里用 channel + select 包一层强制超时,ctx 到期主动 Close dialog
//
// 关键 2:Request-URI 的 userpart 使用可播放通道编码,不能使用设备根编码;
//
//	host 是国标域(如 3402000000),不可路由,仍需 SetDestination 指定设备真实 IP:port
func (u *UAC) Invite(ctx context.Context, m *SessionManager, s *Session, sdpBody string) error {
	s.State = StateInviting
	s.createdAt = time.Now()

	// 自己构造 INVITE request,显式 SetDestination(避免 sipgo 默认按 URI 域名解析)
	req := u.buildInviteRequest(s, sdpBody)

	callID, cseq := u.extractKeyFromRequest(req)
	u.recordBegin(metrics.TxInvite, callID, cseq, s.DeviceID)

	dialog, err := u.dialogUA.WriteInvite(ctx, req)
	if err != nil {
		s.State = StateIdle
		u.recordEnd(callID, cseq, 0, false)
		return fmt.Errorf("INVITE 失败: %w", err)
	}

	// WaitAnswer 不听 ctx,自己加超时控制
	answered := make(chan error, 1)
	go func() {
		answered <- dialog.WaitAnswer(ctx, sipgo.AnswerOptions{})
	}()
	select {
	case waitErr := <-answered:
		if waitErr != nil {
			s.State = StateIdle
			_ = dialog.Close()
			u.recordEnd(callID, cseq, 0, false)
			return fmt.Errorf("等待 INVITE 应答失败: %w", waitErr)
		}
	case <-ctx.Done():
		s.State = StateIdle
		_ = dialog.Close()
		u.recordEnd(callID, cseq, 0, false)
		return fmt.Errorf("等待 INVITE 应答超时: %w", ctx.Err())
	}

	if err := dialog.Ack(ctx); err != nil {
		s.State = StateIdle
		u.recordEnd(callID, cseq, 0, false)
		return fmt.Errorf("发送 ACK 失败: %w", err)
	}
	s.dialog = dialog
	s.State = StateEstablished
	m.put(s)
	u.recordEnd(callID, cseq, 200, true)
	return nil
}

// Bye 停止点播
func (u *UAC) Bye(ctx context.Context, m *SessionManager, streamID string) error {
	s := m.Get(streamID)
	if s == nil || s.dialog == nil {
		return nil
	}
	// 为 BYE 单独记一次出向事务(用临时 key,跟 INVITE 的 dialog Call-ID 区分)
	callID := fmt.Sprintf("bye-%s-%d", streamID, time.Now().UnixNano())
	cseq := u.nextCSeq()
	u.recordBegin(metrics.TxBye, callID, cseq, s.DeviceID)

	err := s.dialog.Bye(ctx)
	s.State = StateBye
	m.remove(streamID)
	if err != nil {
		u.recordEnd(callID, cseq, 0, false)
		return fmt.Errorf("BYE 失败: %w", err)
	}
	u.recordEnd(callID, cseq, 200, true)
	return nil
}
