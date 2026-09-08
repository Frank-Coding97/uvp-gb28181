package uac

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

// ErrStaleInviteGeneration 本 INVITE 的 ACK 已完成,但较新的代次已占据同一
// streamID,结果被 SessionManager 拒绝写入。设备端会话已 BYE,本地 dialog 已关。
var ErrStaleInviteGeneration = errors.New("uac: stale invite generation")

// ErrStaleInviteCleanupFailed stale 会话的设备端清理(BYE/Close)未确认成功,
// 设备可能仍按旧代次推流 —— 调用方需隔离 SSRC,防止回池后被新代次复用造成双流冲突。
var ErrStaleInviteCleanupFailed = errors.New("uac: stale invite cleanup failed")

// InviteOutcome reports the protocol facts observed while establishing an
// INVITE dialog. It deliberately does not classify playback failures.
type InviteOutcome struct {
	RequestID string
	CallID    string
	CSeq      string

	RequestSent bool
	SentAt      time.Time

	FinalStatus     int
	FinalResponseAt time.Time
	AckSucceeded    bool
	AckAt           time.Time
	Error           error
}

// inviteDialog is the narrow dialog contract needed by the live INVITE path.
// Keeping it local makes the outcome contract testable without a SIP socket.
type inviteDialog interface {
	WaitAnswer(context.Context) error
	Ack(context.Context) error
	Bye(context.Context) error
	Close() error
	FinalStatus() int
}

type inviteDialogTransport interface {
	WriteInvite(context.Context, *sip.Request) (inviteDialog, error)
}

type sipgoInviteDialogTransport struct {
	client *sipgo.Client
}

func (t *sipgoInviteDialogTransport) WriteInvite(ctx context.Context, req *sip.Request) (inviteDialog, error) {
	if t == nil || t.client == nil {
		return nil, fmt.Errorf("SIP INVITE UAC 未就绪")
	}
	contact := req.Contact()
	if contact == nil {
		return nil, fmt.Errorf("INVITE 缺少 Contact")
	}
	cache := sipgo.NewDialogClientCache(t.client, *contact)
	dialog, err := cache.WriteInvite(ctx, req)
	if err != nil {
		return nil, err
	}
	// Direct GB devices may return a numeric SIP realm as their Contact host.
	// Keep in-dialog packets on the responding peer; Record-Route still wins.
	dialog.UA.RewriteContact = true
	return &sipgoInviteDialog{session: dialog}, nil
}

type sipgoInviteDialog struct {
	session *sipgo.DialogClientSession
}

func (d *sipgoInviteDialog) WaitAnswer(ctx context.Context) error {
	return d.session.WaitAnswer(ctx, sipgo.AnswerOptions{})
}

func (d *sipgoInviteDialog) Ack(ctx context.Context) error { return d.session.Ack(ctx) }
func (d *sipgoInviteDialog) Bye(ctx context.Context) error { return d.session.Bye(ctx) }
func (d *sipgoInviteDialog) Close() error                  { return d.session.Close() }

func (d *sipgoInviteDialog) FinalStatus() int {
	if d == nil || d.session == nil || d.session.InviteResponse == nil {
		return 0
	}
	status := int(d.session.InviteResponse.StatusCode)
	if status < 200 {
		return 0
	}
	return status
}

func newInviteRequestID(s *Session) string {
	if s != nil && s.RequestID != "" {
		return s.RequestID
	}
	return fmt.Sprintf("invite-%d", time.Now().UnixNano())
}

func (o *InviteOutcome) captureFinalStatus(dialog inviteDialog) {
	if o == nil || dialog == nil || o.FinalStatus != 0 {
		return
	}
	if status := dialog.FinalStatus(); status >= 200 {
		o.FinalStatus = status
		o.FinalResponseAt = time.Now()
	}
}

// InviteTracked is the structured-result form of Invite. Every path returns
// the correlation fields that are known at that point, together with the
// original wrapped error for callers that still use error semantics.
func (u *UAC) InviteTracked(ctx context.Context, m *SessionManager, s *Session, sdpBody string) (InviteOutcome, error) {
	outcome := InviteOutcome{RequestID: newInviteRequestID(s)}
	ctx, cancel := withSIPCommandTimeout(ctx)
	defer cancel()

	if s == nil {
		err := fmt.Errorf("INVITE 会话为空")
		outcome.Error = err
		return outcome, err
	}
	s.State = StateInviting
	s.createdAt = time.Now()

	req, err := u.buildInviteRequest(s, sdpBody)
	if err != nil {
		s.State = StateIdle
		wrapped := fmt.Errorf("构造 INVITE 失败: %w", err)
		outcome.Error = wrapped
		return outcome, wrapped
	}

	callID, cseq := u.ensureInviteCorrelation(req)
	outcome.CallID = callID
	outcome.CSeq = cseq
	u.recordBegin(metrics.TxInvite, callID, cseq, s.DeviceID)

	transport := u.inviteTransport
	if transport == nil && u.client != nil {
		transport = &sipgoInviteDialogTransport{client: u.client}
	}
	if transport == nil {
		s.State = StateIdle
		err := fmt.Errorf("发送 INVITE 失败: SIP UAC 未就绪")
		outcome.Error = err
		u.recordEnd(callID, cseq, 0, false)
		return outcome, err
	}

	dialog, err := transport.WriteInvite(ctx, req)
	if err != nil {
		s.State = StateIdle
		wrapped := fmt.Errorf("INVITE 失败: %w", err)
		outcome.Error = wrapped
		u.recordEnd(callID, cseq, 0, false)
		return outcome, wrapped
	}
	if dialog == nil {
		s.State = StateIdle
		err := fmt.Errorf("INVITE 失败: transport 返回空 dialog")
		outcome.Error = err
		u.recordEnd(callID, cseq, 0, false)
		return outcome, err
	}
	outcome.RequestSent = true
	outcome.SentAt = time.Now()

	// WaitAnswer 不保证监听外部 ctx，使用 channel + select 保持总超时边界。
	answered := make(chan error, 1)
	go func() {
		answered <- dialog.WaitAnswer(ctx)
	}()
	select {
	case waitErr := <-answered:
		outcome.captureFinalStatus(dialog)
		if waitErr != nil {
			s.State = StateIdle
			_ = dialog.Close()
			u.recordEnd(callID, cseq, outcome.FinalStatus, false)
			wrapped := fmt.Errorf("等待 INVITE 应答失败: %w", waitErr)
			outcome.Error = wrapped
			return outcome, wrapped
		}
	case <-ctx.Done():
		outcome.captureFinalStatus(dialog)
		s.State = StateIdle
		_ = dialog.Close()
		u.recordEnd(callID, cseq, outcome.FinalStatus, false)
		wrapped := fmt.Errorf("等待 INVITE 应答超时: %w", ctx.Err())
		outcome.Error = wrapped
		return outcome, wrapped
	}

	if err := dialog.Ack(ctx); err != nil {
		outcome.captureFinalStatus(dialog)
		s.State = StateIdle
		_ = dialog.Close()
		u.recordEnd(callID, cseq, outcome.FinalStatus, false)
		wrapped := fmt.Errorf("发送 ACK 失败: %w", err)
		outcome.Error = wrapped
		return outcome, wrapped
	}
	outcome.AckSucceeded = true
	outcome.AckAt = time.Now()
	s.dialog = dialog
	s.State = StateEstablished
	if m != nil && !m.PutIfCurrent(s) {
		// 较新的代次已占据 streamID:本对话框在设备端仍然活着,必须 BYE 回收,
		// 只本地 Close 会留下设备端推流无人收
		byeCtx, byeCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer byeCancel()
		var cleanupErrs []error
		if err := dialog.Bye(byeCtx); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
		if err := dialog.Close(); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
		s.dialog = nil
		s.State = StateIdle
		u.recordEnd(callID, cseq, outcome.FinalStatus, false)
		wrapped := fmt.Errorf("%w: INVITE 结果晚于更新代次,设备端会话已回收", ErrStaleInviteGeneration)
		if len(cleanupErrs) > 0 {
			// 清理失败不能无声 —— 上层需要知道设备端会话可能仍活着,可重试或走补偿
			wrapped = errors.Join(wrapped,
				errors.Join(ErrStaleInviteCleanupFailed, errors.Join(cleanupErrs...)))
		}
		outcome.Error = wrapped
		return outcome, wrapped
	}
	u.recordEnd(callID, cseq, outcome.FinalStatus, true)
	return outcome, nil
}

// ensureInviteCorrelation makes the identifiers available before transport
// execution; sipgo otherwise adds them internally after the request is sent.
func (u *UAC) ensureInviteCorrelation(req *sip.Request) (string, string) {
	callID, cseq := u.extractKeyFromRequest(req)
	if req.CallID() == nil {
		header := sip.CallIDHeader(callID)
		req.AppendHeader(&header)
	}
	if req.CSeq() == nil {
		seq, _ := strconv.ParseUint(cseq, 10, 32)
		req.AppendHeader(&sip.CSeqHeader{SeqNo: uint32(seq), MethodName: sip.INVITE})
	}
	return callID, cseq
}
