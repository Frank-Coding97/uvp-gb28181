package play

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// SystemLiveEnsurer is the capability passed only to internal recording-plan
// orchestration. HTTP/Hook callers keep the ordinary Service interface and
// must supply their original authorized epoch or queued authorization.
type SystemLiveEnsurer struct {
	service   *Service
	authority *playauth.DeviceSecurityStore
}

func NewSystemLiveEnsurer(service *Service, authority *playauth.DeviceSecurityStore) *SystemLiveEnsurer {
	return &SystemLiveEnsurer{service: service, authority: authority}
}

func (s *SystemLiveEnsurer) EnsureLive(ctx context.Context, req Request) (*Result, error) {
	if s == nil || s.service == nil || s.authority == nil || ctx == nil || ctx.Err() != nil ||
		req.DeviceEpoch != 0 || req.AuthorizationID != "" || req.IsQualified() {
		return nil, ErrPlayAuthorizationUnavailable
	}
	state, err := s.authority.Load(ctx, req.DeviceID)
	if err != nil || state.AccessEpoch <= 0 || state.CleanupCompletedEpoch != state.AccessEpoch {
		return nil, ErrPlayAuthorizationUnavailable
	}
	// This is a new trusted-system intent, not a refresh of a caller's old
	// authority. The owner operation barrier will validate this same snapshot
	// again after waiting; it must not replace it with a newer epoch.
	req.DeviceEpoch = state.AccessEpoch
	return s.service.EnsureLive(ctx, req)
}
