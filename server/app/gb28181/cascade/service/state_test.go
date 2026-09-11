package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
)

type fakeClock struct{ now time.Time }

func (c fakeClock) Now() time.Time { return c.now }

func TestDerivePlatformStateKeepsRegistrationHeartbeatAndOnlineIndependent(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	platform := model.GbCascadePlatform{Enabled: true, KeepaliveInterval: 30}
	registeredAt := now.Add(-time.Minute)
	expiresAt := now.Add(time.Minute)
	heartbeatAt := now.Add(-20 * time.Second)
	platform.RegisterAt = &registeredAt
	platform.RegisterExpiresAt = &expiresAt
	platform.HeartbeatAt = &heartbeatAt

	state := DerivePlatformState(platform, fakeClock{now: now}, 0)
	require.Equal(t, RegistrationStateRegistered, state.Registration)
	require.Equal(t, HeartbeatStateHealthy, state.Heartbeat)
	require.Equal(t, PlatformStateOnline, state.Overall)

	expired := now.Add(-time.Second)
	platform.RegisterExpiresAt = &expired
	state = DerivePlatformState(platform, fakeClock{now: now}, 0)
	require.Equal(t, RegistrationStateExpired, state.Registration)
	require.Equal(t, HeartbeatStateHealthy, state.Heartbeat)
	require.Equal(t, PlatformStateOffline, state.Overall)

	platform.RegisterExpiresAt = &expiresAt
	staleHeartbeat := now.Add(-61 * time.Second)
	platform.HeartbeatAt = &staleHeartbeat
	state = DerivePlatformState(platform, fakeClock{now: now}, 0)
	require.Equal(t, RegistrationStateRegistered, state.Registration)
	require.Equal(t, HeartbeatStateStale, state.Heartbeat)
	require.Equal(t, PlatformStateOffline, state.Overall)

	platform.Enabled = false
	state = DerivePlatformState(platform, fakeClock{now: now}, 0)
	require.Equal(t, RegistrationStateRegistered, state.Registration)
	require.Equal(t, HeartbeatStateStale, state.Heartbeat)
	require.Equal(t, PlatformStateOffline, state.Overall)
}
