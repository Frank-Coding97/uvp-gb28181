package play

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
)

func TestBackendOriginResolvesAfterMediaSelectionAndBindsObservedIP(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	withPlayAuthorization(t, true, true)
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, notifier, _ := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
	inv.onInvite = func(session *uac.Session) { z.online.Store(true); go notifier.Publish(session.StreamID) }
	leases := NewSourceLeaseRegistry()
	req := AuthorizedRequest{DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, DeviceEpoch: 1}
	_, err := s.StartAuthorized(context.Background(), req)
	require.ErrorIs(t, err, ErrPlayAuthorizationUnavailable)
	require.False(t, z.online.Load(), "ordinary missing-IP requests must not start media")
	calls := 0
	result, lease, err := s.StartAuthorizedWithBackendOrigin(context.Background(), req, leases, "share", func(ctx context.Context, result *Result) (string, error) {
		calls++
		require.NotNil(t, result.Node)
		require.NotZero(t, result.Generation)
		require.True(t, leases.HasLease(result.StreamID))
		return "203.0.113.42", nil
	})
	require.NoError(t, err)
	defer lease.Release()
	require.Equal(t, 1, calls)
	u, err := url.Parse(valueOrEmpty(result.URLs.HTTPFLV))
	require.NoError(t, err)
	binding, err := s.ResolvePlaybackMediaContext("rtp", result.StreamID, "node-a")
	require.NoError(t, err)
	binding.BindClientIP = true
	binding.ClientIP = "203.0.113.42"
	_, err = s.tokenIssuer.(playauth.Verifier).Verify(u.Query().Get(playauth.QueryParameter), binding)
	require.NoError(t, err)
	binding.ClientIP = "127.0.0.1"
	_, err = s.tokenIssuer.(playauth.Verifier).Verify(u.Query().Get(playauth.QueryParameter), binding)
	require.Error(t, err)
	_, failedLease, err := s.StartAuthorizedWithBackendOrigin(context.Background(), req, leases, "failed", func(context.Context, *Result) (string, error) { return "", errors.New("proof failed") })
	require.Error(t, err)
	require.Nil(t, failedLease)
	require.NoError(t, lease.Release())
	require.False(t, leases.HasLease(result.StreamID))
}
