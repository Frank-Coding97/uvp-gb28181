// Package service contains cascade domain behavior that is independent of SIP transport and GORM.
package service

import (
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
)

type Clock interface {
	Now() time.Time
}

type RegistrationState string

const (
	RegistrationStateUnregistered RegistrationState = "unregistered"
	RegistrationStateRegistered   RegistrationState = "registered"
	RegistrationStateExpired      RegistrationState = "expired"
)

type HeartbeatState string

const (
	HeartbeatStateUnknown HeartbeatState = "unknown"
	HeartbeatStateHealthy HeartbeatState = "healthy"
	HeartbeatStateStale   HeartbeatState = "stale"
)

type PlatformState string

const (
	PlatformStateOnline  PlatformState = "online"
	PlatformStateOffline PlatformState = "offline"
)

type DerivedPlatformState struct {
	Registration RegistrationState
	Heartbeat    HeartbeatState
	Overall      PlatformState
}

// DerivePlatformState computes product state from independent persisted facts.
// A zero heartbeat window uses twice the configured keepalive interval.
func DerivePlatformState(platform model.GbCascadePlatform, clock Clock, heartbeatWindow time.Duration) DerivedPlatformState {
	now := clock.Now()
	registration := deriveRegistrationState(platform, now)
	heartbeat := deriveHeartbeatState(platform, now, heartbeatWindow)
	overall := PlatformStateOffline
	if platform.Enabled && registration == RegistrationStateRegistered && heartbeat == HeartbeatStateHealthy {
		overall = PlatformStateOnline
	}
	return DerivedPlatformState{Registration: registration, Heartbeat: heartbeat, Overall: overall}
}

func deriveRegistrationState(platform model.GbCascadePlatform, now time.Time) RegistrationState {
	if platform.RegisterAt == nil || platform.RegisterExpiresAt == nil {
		return RegistrationStateUnregistered
	}
	if !now.Before(*platform.RegisterExpiresAt) {
		return RegistrationStateExpired
	}
	return RegistrationStateRegistered
}

func deriveHeartbeatState(platform model.GbCascadePlatform, now time.Time, window time.Duration) HeartbeatState {
	if platform.HeartbeatAt == nil {
		return HeartbeatStateUnknown
	}
	if window <= 0 {
		interval := platform.KeepaliveInterval
		if interval <= 0 {
			interval = 60
		}
		window = time.Duration(interval*2) * time.Second
	}
	if !now.Before(platform.HeartbeatAt.Add(window)) {
		return HeartbeatStateStale
	}
	return HeartbeatStateHealthy
}
