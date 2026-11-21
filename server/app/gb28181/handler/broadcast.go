package handler

import "context"

type BroadcastInviteRequest struct {
	PeerID   string
	TargetID string
	CallID   string
	CSeq     uint
	SDP      string
}

type BroadcastInviteResponse struct {
	SessionID string
	AnswerSDP string
}

type BroadcastInviteProcessor interface {
	PrepareBroadcastInvite(context.Context, BroadcastInviteRequest) (BroadcastInviteResponse, error)
	OnBroadcastAck(context.Context, string) error
	OnBroadcastBye(context.Context, string) error
}
