package uac

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
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
func New(ua *sipgo.UserAgent, serverID, domain, sipIP string, sipPort int) (*UAC, error) {
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname(sipIP))
	if err != nil {
		return nil, fmt.Errorf("创建 UAC client 失败: %w", err)
	}
	// Contact 头:平台自身地址,设备回包/BYE 用(端口写 server 监听端口,确保设备应答能回)
	contact := sip.ContactHeader{
		Address: sip.Uri{User: serverID, Host: sipIP, Port: sipPort},
	}
	dialogUA := sipgo.NewDialogClientCache(client, contact)
	return &UAC{client: client, dialogUA: dialogUA, serverID: serverID, domain: domain}, nil
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
	if bytes.Contains(body, []byte("DeviceControl")) || bytes.Contains(body, []byte("PTZCmd")) {
		return metrics.TxPTZ
	}
	return metrics.TxUnknown
}

func (u *UAC) deviceURI(deviceID string) sip.Uri {
	uri := sip.Uri{}
	sip.ParseUri(fmt.Sprintf("sip:%s@%s", deviceID, u.domain), &uri)
	return uri
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

// SendMessage 向设备发 MESSAGE(承载 MANSCDP XML,如 Catalog 查询)
func (u *UAC) SendMessage(ctx context.Context, deviceID, dest string, body []byte) error {
	req := sip.NewRequest(sip.MESSAGE, u.deviceURI(deviceID))
	req.SetBody(body)
	req.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSCDP+xml"))
	req.SetDestination(dest)
	req.SetTransport("UDP")

	kind := detectMessageKind(body)
	callID, cseq := u.extractKeyFromRequest(req)
	u.recordBegin(kind, callID, cseq, deviceID)

	resp, err := u.client.Do(ctx, req)
	if err != nil {
		u.recordEnd(callID, cseq, 0, false)
		return fmt.Errorf("发送 MESSAGE 失败: %w", err)
	}
	if resp.StatusCode != 200 {
		u.recordEnd(callID, cseq, int(resp.StatusCode), false)
		return fmt.Errorf("MESSAGE 应答非200: %d %s", resp.StatusCode, resp.Reason)
	}
	u.recordEnd(callID, cseq, int(resp.StatusCode), true)
	return nil
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

// Invite 发起点播:INVITE → 等应答 → ACK,会话建立
// 关键 1:sipgo v1.4 的 WaitAnswer 内部 select 不响应外部 ctx.Done(),
//        这里用 channel + select 包一层强制超时,ctx 到期主动 Close dialog
// 关键 2:Request-URI 的 host 是国标域(如 3402000000),不可路由;
//        必须 SetDestination 显式指定设备真实 IP:port,否则 INVITE 发不出去
func (u *UAC) Invite(ctx context.Context, m *SessionManager, s *Session, sdpBody string) error {
	s.State = StateInviting
	s.createdAt = time.Now()

	// 自己构造 INVITE request,显式 SetDestination(避免 sipgo 默认按 URI 域名解析)
	req := sip.NewRequest(sip.INVITE, u.deviceURI(s.DeviceID))
	req.SetBody([]byte(sdpBody))
	req.AppendHeader(sip.NewHeader("Subject", fmt.Sprintf("%s:%s,%s:0", s.ChannelID, s.SSRC, u.serverID)))
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.SetDestination(s.Dest)
	req.SetTransport("UDP")

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
