package uac

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// UAC 平台主叫客户端:向下级设备发起 SIP 请求(MESSAGE 查询 / INVITE 点播)
type UAC struct {
	client   *sipgo.Client
	dialogUA *sipgo.DialogClientCache // 管理 INVITE 对话(Ack/Bye)
	serverID string
	domain   string
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

func (u *UAC) deviceURI(deviceID string) sip.Uri {
	uri := sip.Uri{}
	sip.ParseUri(fmt.Sprintf("sip:%s@%s", deviceID, u.domain), &uri)
	return uri
}

// SendMessage 向设备发 MESSAGE(承载 MANSCDP XML,如 Catalog 查询)
func (u *UAC) SendMessage(ctx context.Context, deviceID, dest string, body []byte) error {
	req := sip.NewRequest(sip.MESSAGE, u.deviceURI(deviceID))
	req.SetBody(body)
	req.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSCDP+xml"))
	req.SetDestination(dest)
	req.SetTransport("UDP")
	resp, err := u.client.Do(ctx, req)
	if err != nil {
		return fmt.Errorf("发送 MESSAGE 失败: %w", err)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("MESSAGE 应答非200: %d %s", resp.StatusCode, resp.Reason)
	}
	return nil
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
// 关键:sipgo v1.4 的 WaitAnswer 内部 select 不响应外部 ctx.Done(),
// 这里用 channel + select 包一层强制超时,ctx 到期主动 Close dialog
func (u *UAC) Invite(ctx context.Context, m *SessionManager, s *Session, sdpBody string) error {
	s.State = StateInviting
	s.createdAt = time.Now()

	subject := sip.NewHeader("Subject", fmt.Sprintf("%s:%s,%s:0", s.ChannelID, s.SSRC, u.serverID))
	ctype := sip.NewHeader("Content-Type", "application/sdp")
	dialog, err := u.dialogUA.Invite(ctx, u.deviceURI(s.DeviceID), []byte(sdpBody), ctype, subject)
	if err != nil {
		s.State = StateIdle
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
			return fmt.Errorf("等待 INVITE 应答失败: %w", waitErr)
		}
	case <-ctx.Done():
		s.State = StateIdle
		_ = dialog.Close()
		return fmt.Errorf("等待 INVITE 应答超时: %w", ctx.Err())
	}

	if err := dialog.Ack(ctx); err != nil {
		s.State = StateIdle
		return fmt.Errorf("发送 ACK 失败: %w", err)
	}
	s.dialog = dialog
	s.State = StateEstablished
	m.put(s)
	return nil
}

// Bye 停止点播
func (u *UAC) Bye(ctx context.Context, m *SessionManager, streamID string) error {
	s := m.Get(streamID)
	if s == nil || s.dialog == nil {
		return nil
	}
	err := s.dialog.Bye(ctx)
	s.State = StateBye
	m.remove(streamID)
	if err != nil {
		return fmt.Errorf("BYE 失败: %w", err)
	}
	return nil
}
