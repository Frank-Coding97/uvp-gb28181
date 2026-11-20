package play

import (
	"context"

	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
)

// SystemLiveEnsurer authorizes trusted platform-originated intents against the
// current device epoch. It must not be exposed to arbitrary HTTP callers;
// external requests must retain their original epoch or queued authorization.
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

// AutoOnDemandEnsurer fills the current device epoch only for fixed-address
// Hook requests that do not carry caller authorization. Authenticated requests
// retain their original authorization snapshot.
type AutoOnDemandEnsurer struct {
	service *Service
	system  *SystemLiveEnsurer
}

func NewAutoOnDemandEnsurer(service *Service, authority *playauth.DeviceSecurityStore) *AutoOnDemandEnsurer {
	return &AutoOnDemandEnsurer{
		service: service,
		system:  NewSystemLiveEnsurer(service, authority),
	}
}

func (s *AutoOnDemandEnsurer) EnsureLive(ctx context.Context, req Request) (*Result, error) {
	if s == nil || s.service == nil || s.system == nil {
		return nil, ErrPlayAuthorizationUnavailable
	}
	if req.DeviceEpoch == 0 && req.AuthorizationID == "" {
		return s.system.EnsureLive(ctx, req)
	}
	return s.service.EnsureLive(ctx, req)
}
