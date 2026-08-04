package uac

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

type TalkInviteRequest struct {
	DeviceID    string
	ChannelID   string
	Destination string
	Transport   string
	SSRC        string
	SDP         string
}

type TalkDialogMetadata struct {
	CallID     string
	CSeq       uint32
	StatusCode int
	DeviceID   string
	ChannelID  string
	SSRC       string
}

type TalkByeHandler func(TalkDialogMetadata)

type talkDialog interface {
	WaitAnswer(context.Context) error
	Ack(context.Context) error
	Bye(context.Context) error
	Close() error
	StatusCode() int
	ReadBye(*sip.Request, sip.ServerTransaction) error
}

type talkDialogTransport interface {
	WriteInvite(context.Context, *sip.Request) (talkDialog, error)
}

type sipgoTalkDialogTransport struct {
	client *sipgo.Client
}

func (t *sipgoTalkDialogTransport) WriteInvite(ctx context.Context, req *sip.Request) (talkDialog, error) {
	contact := req.Contact()
	if contact == nil {
		return nil, fmt.Errorf("TALK INVITE 缺少 Contact")
	}
	cache := sipgo.NewDialogClientCache(t.client, *contact)
	dialog, err := cache.WriteInvite(ctx, req)
	if err != nil {
		return nil, err
	}
	return &sipgoTalkDialog{session: dialog}, nil
}

type sipgoTalkDialog struct {
	session *sipgo.DialogClientSession
}

func (d *sipgoTalkDialog) WaitAnswer(ctx context.Context) error {
	return d.session.WaitAnswer(ctx, sipgo.AnswerOptions{})
}

func (d *sipgoTalkDialog) Ack(ctx context.Context) error { return d.session.Ack(ctx) }
func (d *sipgoTalkDialog) Bye(ctx context.Context) error { return d.session.Bye(ctx) }
func (d *sipgoTalkDialog) Close() error                  { return d.session.Close() }

func (d *sipgoTalkDialog) StatusCode() int {
	if d.session.InviteResponse == nil {
		return 0
	}
	return int(d.session.InviteResponse.StatusCode)
}

func (d *sipgoTalkDialog) ReadBye(req *sip.Request, tx sip.ServerTransaction) error {
	return d.session.ReadBye(req, tx)
}

type talkDialogRecord struct {
	mu       sync.Mutex
	dialog   talkDialog
	metadata TalkDialogMetadata
	acked    bool
	ended    bool
}

type talkDialogStore struct {
	mu        sync.RWMutex
	transport talkDialogTransport
	dialogs   map[string]*talkDialogRecord
	handler   TalkByeHandler
}

func newTalkDialogStore(transport talkDialogTransport) *talkDialogStore {
	return &talkDialogStore{transport: transport, dialogs: make(map[string]*talkDialogRecord)}
}

func (s *talkDialogStore) get(callID string) *talkDialogRecord {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dialogs[callID]
}

func (s *talkDialogStore) put(record *talkDialogRecord) {
	s.mu.Lock()
	s.dialogs[record.metadata.CallID] = record
	s.mu.Unlock()
}

func (s *talkDialogStore) remove(callID string, record *talkDialogRecord) {
	s.mu.Lock()
	if s.dialogs[callID] == record {
		delete(s.dialogs, callID)
	}
	s.mu.Unlock()
}

func (s *talkDialogStore) setHandler(handler TalkByeHandler) {
	s.mu.Lock()
	s.handler = handler
	s.mu.Unlock()
}

func (s *talkDialogStore) byeHandler() TalkByeHandler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.handler
}

func (u *UAC) buildTalkInviteRequest(in TalkInviteRequest) (*sip.Request, TalkDialogMetadata, error) {
	if u == nil || strings.TrimSpace(in.DeviceID) == "" || strings.TrimSpace(in.ChannelID) == "" ||
		strings.TrimSpace(in.Destination) == "" || strings.TrimSpace(in.SSRC) == "" || strings.TrimSpace(in.SDP) == "" {
		return nil, TalkDialogMetadata{}, fmt.Errorf("TALK INVITE 缺少设备、通道、目的地址、SSRC 或 SDP")
	}
	req := sip.NewRequest(sip.INVITE, u.deviceURI(in.ChannelID))
	req.SetBody([]byte(in.SDP))
	req.AppendHeader(sip.NewHeader("Subject", fmt.Sprintf("%s:%s,%s:0", in.ChannelID, in.SSRC, u.serverID)))
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.AppendHeader(u.platformFromHeader())
	if err := u.prepareOutboundRequest(req, in.Destination, in.Transport, true); err != nil {
		return nil, TalkDialogMetadata{}, err
	}

	cseqText := u.nextCSeq()
	cseq, err := strconv.ParseUint(cseqText, 10, 32)
	if err != nil || cseq == 0 {
		return nil, TalkDialogMetadata{}, fmt.Errorf("TALK INVITE CSeq 不合法: %q", cseqText)
	}
	callID := fmt.Sprintf("talk-%s-%s-%d", u.serverID, sip.GenerateTagN(16), time.Now().UnixNano())
	callIDHeader := sip.CallIDHeader(callID)
	req.AppendHeader(&callIDHeader)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: uint32(cseq), MethodName: sip.INVITE})
	return req, TalkDialogMetadata{
		CallID: callID, CSeq: uint32(cseq), DeviceID: in.DeviceID,
		ChannelID: in.ChannelID, SSRC: in.SSRC,
	}, nil
}

// InviteTalk establishes and ACKs a TALK dialog that is tracked separately
// from live-play sessions.
func (u *UAC) InviteTalk(ctx context.Context, in TalkInviteRequest) (TalkDialogMetadata, error) {
	if u == nil || u.talkDialogs == nil || u.talkDialogs.transport == nil {
		return TalkDialogMetadata{}, fmt.Errorf("SIP TALK UAC 未就绪")
	}
	req, metadata, err := u.buildTalkInviteRequest(in)
	if err != nil {
		return metadata, err
	}
	u.recordBegin(metrics.TxInvite, metadata.CallID, strconv.FormatUint(uint64(metadata.CSeq), 10), metadata.DeviceID)
	dialog, err := u.talkDialogs.transport.WriteInvite(ctx, req)
	if err != nil {
		u.recordEnd(metadata.CallID, strconv.FormatUint(uint64(metadata.CSeq), 10), 0, false)
		return metadata, fmt.Errorf("TALK INVITE 失败: %w", err)
	}

	answered := make(chan error, 1)
	go func() { answered <- dialog.WaitAnswer(ctx) }()
	select {
	case waitErr := <-answered:
		metadata.StatusCode = dialog.StatusCode()
		if waitErr != nil {
			_ = dialog.Close()
			u.recordEnd(metadata.CallID, strconv.FormatUint(uint64(metadata.CSeq), 10), metadata.StatusCode, false)
			return metadata, fmt.Errorf("TALK INVITE 应答失败: %w", waitErr)
		}
	case <-ctx.Done():
		_ = dialog.Close()
		u.recordEnd(metadata.CallID, strconv.FormatUint(uint64(metadata.CSeq), 10), 0, false)
		return metadata, fmt.Errorf("TALK INVITE 应答超时: %w", ctx.Err())
	}

	metadata.StatusCode = dialog.StatusCode()
	record := &talkDialogRecord{dialog: dialog, metadata: metadata}
	u.talkDialogs.put(record)
	if err := u.AckTalk(ctx, metadata.CallID); err != nil {
		u.talkDialogs.remove(metadata.CallID, record)
		_ = dialog.Close()
		u.recordEnd(metadata.CallID, strconv.FormatUint(uint64(metadata.CSeq), 10), metadata.StatusCode, false)
		return metadata, err
	}
	u.recordEnd(metadata.CallID, strconv.FormatUint(uint64(metadata.CSeq), 10), metadata.StatusCode, true)
	return metadata, nil
}

// AckTalk is idempotent for an already acknowledged TALK dialog.
func (u *UAC) AckTalk(ctx context.Context, callID string) error {
	if u == nil || u.talkDialogs == nil {
		return fmt.Errorf("SIP TALK UAC 未就绪")
	}
	record := u.talkDialogs.get(callID)
	if record == nil {
		return fmt.Errorf("TALK dialog 不存在: %s", callID)
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	if record.acked {
		return nil
	}
	if err := record.dialog.Ack(ctx); err != nil {
		return fmt.Errorf("发送 TALK ACK 失败: %w", err)
	}
	record.acked = true
	return nil
}

// ByeTalk is idempotent: once cleanup starts, later calls do not send another
// BYE even when the first network attempt fails.
func (u *UAC) ByeTalk(ctx context.Context, callID string) error {
	if u == nil || u.talkDialogs == nil {
		return nil
	}
	record := u.talkDialogs.get(callID)
	if record == nil {
		return nil
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	if record.ended {
		return nil
	}
	record.ended = true
	u.talkDialogs.remove(callID, record)
	if err := record.dialog.Bye(ctx); err != nil {
		return fmt.Errorf("发送 TALK BYE 失败: %w", err)
	}
	return nil
}

func (u *UAC) SetTalkByeHandler(handler TalkByeHandler) {
	if u != nil && u.talkDialogs != nil {
		u.talkDialogs.setHandler(handler)
	}
}

// HandleTalkBye is the primitive used by the SIP Server OnBye registration.
// A BYE for an unknown TALK Call-ID receives RFC 3261 status 481.
func (u *UAC) HandleTalkBye(req *sip.Request, tx sip.ServerTransaction) (bool, error) {
	if req == nil || tx == nil {
		return false, fmt.Errorf("TALK BYE 请求或事务为空")
	}
	callID := ""
	if header := req.CallID(); header != nil {
		callID = string(*header)
	}
	if u == nil || u.talkDialogs == nil {
		err := tx.Respond(sip.NewResponseFromRequest(req, sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist", nil))
		return false, err
	}
	record := u.talkDialogs.get(callID)
	if record == nil {
		err := tx.Respond(sip.NewResponseFromRequest(req, sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist", nil))
		return false, err
	}

	record.mu.Lock()
	if record.ended {
		record.mu.Unlock()
		err := tx.Respond(sip.NewResponseFromRequest(req, sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist", nil))
		return false, err
	}
	record.ended = true
	u.talkDialogs.remove(callID, record)
	err := record.dialog.ReadBye(req, tx)
	metadata := record.metadata
	record.mu.Unlock()
	if err != nil {
		return true, err
	}
	if handler := u.talkDialogs.byeHandler(); handler != nil {
		handler(metadata)
	}
	return true, nil
}
