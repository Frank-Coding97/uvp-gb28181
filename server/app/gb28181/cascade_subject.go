package gb28181

import (
	"context"
	"fmt"
	"strings"

	"github.com/emiago/sipgo/sip"
)

// The sender ID and sender sequence identify the media source (2022 annex L).
// Receiver fields are retained as observed facts, not used to merge consumers.
type cascadeMediaSourceIdentity struct {
	SenderID         string `json:"senderId"`
	SenderSequence   string `json:"senderSequence"`
	ReceiverID       string `json:"receiverId"`
	ReceiverSequence string `json:"receiverSequence"`
}

func parseCascadeMediaSourceIdentity(raw string, playback bool) (cascadeMediaSourceIdentity, error) {
	var identity cascadeMediaSourceIdentity
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return identity, fmt.Errorf("invalid Subject")
	}
	left, right := strings.Split(strings.TrimSpace(parts[0]), ":"), strings.Split(strings.TrimSpace(parts[1]), ":")
	if len(left) != 2 || len(right) != 2 {
		return identity, fmt.Errorf("invalid Subject")
	}
	identity = cascadeMediaSourceIdentity{left[0], left[1], right[0], right[1]}
	prefix := byte('0')
	if playback {
		prefix = '1'
	}
	if len(left[0]) != 20 || len(right[0]) != 20 || len(left[1]) == 0 || len(left[1]) > 20 || left[1][0] != prefix || len(right[1]) == 0 || strings.ContainsAny(raw, "\r\n") {
		return cascadeMediaSourceIdentity{}, fmt.Errorf("invalid Subject media source identity")
	}
	return identity, nil
}

// parseCascadeBroadcastSourceIdentity 是**语音广播/对讲音频 INVITE** 的 Subject 解析。
//
// 与 parseCascadeMediaSourceIdentity 只差一处：**不校验序列号的首字符**。
// 国标把序列号首字符当「实时(0) / 回放(1)」标记，但语音广播复用同一个字段；
// EasyGBS 3.7.451 实测发的是
//
//	Subject: 34020000012000000001:1,37010301021180000010:2
//
// 也就是「平台编码:序号,设备编码:序号」——序列号就是 1、2 这种纯序号。
// 用严格解析会直接判 Subject 非法，上级 UI 表现为「点了语音没反应」。
// 序列号只是媒体流标识，不参与鉴权（锚是租约 + 已发布通道 + 设备纪元 + 来源地址），
// 所以这里只要求「长度与字符集合法」，前缀语义留给视频那条路径继续严格校验。
func parseCascadeBroadcastSourceIdentity(raw string) (cascadeMediaSourceIdentity, error) {
	var identity cascadeMediaSourceIdentity
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return identity, fmt.Errorf("invalid Subject")
	}
	left := strings.Split(strings.TrimSpace(parts[0]), ":")
	right := strings.Split(strings.TrimSpace(parts[1]), ":")
	if len(left) != 2 || len(right) != 2 {
		return identity, fmt.Errorf("invalid Subject")
	}
	identity = cascadeMediaSourceIdentity{left[0], left[1], right[0], right[1]}
	if len(left[0]) != 20 || len(right[0]) != 20 ||
		len(left[1]) == 0 || len(left[1]) > 20 || len(right[1]) == 0 || len(right[1]) > 20 ||
		strings.ContainsAny(raw, "\r\n") {
		return cascadeMediaSourceIdentity{}, fmt.Errorf("invalid Subject media source identity")
	}
	return identity, nil
}

type cascadeInviteKey struct {
	CallID          string
	CSeq            uint32
	FromTag, Branch string
}
type cascadeSourceKey struct {
	PlatformID                     uint64
	SenderID, SenderSequence, SSRC string
}

func cascadeInviteTransactionKey(req *sip.Request) cascadeInviteKey {
	tag, _ := req.From().Params.Get("tag")
	branch := ""
	if req.Via() != nil {
		branch, _ = req.Via().Params.Get("branch")
	}
	return cascadeInviteKey{req.CallID().Value(), req.CSeq().SeqNo, tag, branch}
}

// Admission reservations serialize replacements without blocking other sources.
// A failed stop leaves the old dialog and its source lease intact.
func (h *cascadeVideoRuntime) reserveSource(ctx context.Context, platformID uint64, source cascadeMediaSourceIdentity, ssrc string, req *sip.Request) (func(), bool, error) {
	key := cascadeSourceKey{platformID, source.SenderID, source.SenderSequence, ssrc}
	peer, _ := cascadePeerFromRequest(req)
	transaction := cascadeInviteTransactionKey(req)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, false, fmt.Errorf("cascade stopping")
	}
	if h.sourcePending == nil {
		h.sourcePending = map[cascadeSourceKey]cascadeInviteKey{}
	}
	if pending, exists := h.sourcePending[key]; exists {
		h.mu.Unlock()
		if pending == transaction {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("media source request already in progress")
	}
	var old *cascadeVideoSession
	for _, candidate := range h.sessions {
		if candidate.platform.ID != platformID {
			continue
		}
		if candidate.inviteKey == transaction && candidate.source == source && candidate.ssrc == ssrc && candidate.peer.sameSession(peer) {
			h.mu.Unlock()
			return nil, true, nil
		}
		if candidate.source.SenderID == source.SenderID && candidate.source.SenderSequence == source.SenderSequence && candidate.ssrc == ssrc {
			old = candidate
		}
	}
	if old != nil && (!old.acknowledged.Load() || old.stopCurrentSender == nil) {
		h.mu.Unlock()
		return nil, false, fmt.Errorf("old media source request not ready")
	}
	h.sourcePending[key] = transaction
	h.mu.Unlock()
	release := func() { h.mu.Lock(); delete(h.sourcePending, key); h.mu.Unlock() }
	if old != nil {
		if err := old.stopCurrentSender(ctx); err != nil {
			release()
			return nil, false, err
		}
		old.cancel()
		select {
		case <-old.done:
		case <-ctx.Done():
			release()
			return nil, false, ctx.Err()
		}
	}
	return release, false, nil
}
