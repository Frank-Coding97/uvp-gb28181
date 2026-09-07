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
	// The bool proves only actual local join plus persisted recovery materials.
	// A true bool may accompany a remote-unknown error; it is not Complete.
	Close(context.Context) (bool, error)
}

type IntentRTPFactory interface {
	PrepareIntent(context.Context, *playauth.DeviceOperationIntentStore, playauth.DeviceOperationIntentIdentity, int64, NodeInfo, RTPRequest) (IntentRTPChild, error)
}

type IntentRTPChild interface {
	Open(context.Context) (RTPAllocation, error)
	// The bool is local quiescence, not media/source/viewer completion.
	Close(context.Context) (bool, error)
	Unbind(context.Context) error
}
