package playback

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// IntentSIPFactory prepares a real retained SIP child under the existing
// parent lease. Preparation alone does not dispatch an INVITE.
type IntentSIPFactory interface {
	PrepareIntent(context.Context, *playauth.DeviceOperationIntentStore, *playauth.DeviceOperationBarrier, playauth.DeviceOperationLease,
		playauth.DeviceOperationIntentIdentity, int64, string, UACInvite) (IntentSIPChild, error)
}

type IntentSIPChild interface {
	Invite(context.Context) (DialogInfo, error)
	SendINFO(context.Context, playauth.DeviceSIPINFOCommand) error
	Close(context.Context) (IntentCloseResult, error)
}

// IntentCloseResult separates local lifecycle from remote recovery. A nil
// error plus LocalQuiesced proves actual IO joined and its final facts and
// recovery handoff are durable. RemotePending is not a local failure and must
// never be interpreted as remote success or device Complete. Errors retain the
// original owner for retry, even if a buggy implementation also returns true.
type IntentCloseResult struct {
	LocalQuiesced bool
	RemotePending bool
}

type IntentRTPFactory interface {
	PrepareIntent(context.Context, *playauth.DeviceOperationIntentStore, playauth.DeviceOperationIntentIdentity, int64, NodeInfo, RTPRequest) (IntentRTPChild, error)
}

type IntentRTPChild interface {
	Open(context.Context) (RTPAllocation, error)
	Close(context.Context) (IntentCloseResult, error)
	Unbind(context.Context) error
}
