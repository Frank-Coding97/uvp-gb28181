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
