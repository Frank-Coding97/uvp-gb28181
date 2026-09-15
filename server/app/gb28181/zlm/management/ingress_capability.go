package management

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

// IngressCapabilityReader maps one cached getApiList profile to the family
// states consumed by FFmpeg and RTP services. A family is writable only when
// every typed list/create/delete API is explicitly present.
type IngressCapabilityReader struct {
	profiles ProxyCapabilityReader
}

func NewIngressCapabilityReader(profiles ProxyCapabilityReader) *IngressCapabilityReader {
	return &IngressCapabilityReader{profiles: profiles}
}

func (r *IngressCapabilityReader) FFmpegCapability(ctx context.Context, nodeID int64) (FFmpegCapabilityState, error) {
	state, err := r.family(ctx, nodeID, "listFFmpegSource", "addFFmpegSource", "delFFmpegSource")
	return FFmpegCapabilityState(state), err
}

func (r *IngressCapabilityReader) RTPCapability(ctx context.Context, nodeID int64) (RTPCapabilityState, error) {
	state, err := r.family(ctx, nodeID, "listRtpServer", "openRtpServer", "closeRtpServer")
	return RTPCapabilityState(state), err
}

func (r *IngressCapabilityReader) family(ctx context.Context, nodeID int64, APIs ...string) (zlm.CapabilityState, error) {
	if r == nil || r.profiles == nil {
		return zlm.CapabilityUnknown, nil
	}
	profile, err := r.profiles.GetCapabilityProfile(ctx, nodeID)
	if err != nil {
		return zlm.CapabilityUnknown, err
	}
	state := zlm.CapabilitySupported
	for _, api := range APIs {
		switch profile.Status(api) {
		case zlm.CapabilityUnsupported:
			return zlm.CapabilityUnsupported, nil
		case zlm.CapabilityUnknown:
			state = zlm.CapabilityUnknown
		}
	}
	return state, nil
}
