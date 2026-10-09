package play

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
)

type sharedMockZLM struct {
	mockZLM
	sharedOpen, sharedClose int
	sharedErr               error
	closed                  string
}

func (m *sharedMockZLM) OpenSinglePortReceiver(_ context.Context, ssrc string, port int) (zlm.SinglePortReceiver, error) {
	m.sharedOpen++
	id, err := zlm.SinglePortStreamID(ssrc)
	return zlm.SinglePortReceiver{StreamID: id, Port: port}, err
}
func (m *sharedMockZLM) CloseSinglePortReceiver(_ context.Context, id string) error {
	m.sharedClose++
	m.closed = id
	return m.sharedErr
}

func TestSinglePortLiveUsesFixedPortAndKeepsCleanupAfterModeSwitch(t *testing.T) {
	for _, transport := range []string{"UDP", "TCP-Passive"} {
		t.Run(transport, func(t *testing.T) {
			withFixedAddressPlaybackSettings(t, false, false)
			z := &sharedMockZLM{}
			inv := &mockInviter{}
			ch := aChannel()
			ch.StreamTransport = transport
			s, notifier, _ := newFixedSvc(t, z, inv, onlineDevice(), ch)
			selected, _ := s.registry.Get(1)
			selected.RTPReceiveMode = "single"
			selected.RTPProxyPort = 10000
			inv.onInvite = func(session *uac.Session) { z.online.Store(true); go notifier.Publish(session.StreamID) }
			result, err := s.Start(context.Background(), onlineDevice().DeviceID, ch.ChannelID)
			require.NoError(t, err)
			expected, err := zlm.SinglePortStreamID(result.SSRC)
			require.NoError(t, err)
			require.Equal(t, expected, result.StreamID)
			require.Contains(t, inv.lastBody, "10000")
			require.Equal(t, int32(0), z.openCalls.Load())
			selected.RTPReceiveMode = "multi"
			require.NoError(t, s.Stop(context.Background(), result.StreamID, onlineDevice().DeviceID, ch.ChannelID))
			require.Equal(t, 1, z.sharedClose)
			require.Equal(t, result.StreamID, z.closed)
			require.Equal(t, int32(0), z.closeCalls.Load())
		})
	}
}

func TestSinglePortLiveRejectsFixedAddressBeforeInvite(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	z := &sharedMockZLM{}
	inv := &mockInviter{}
	s, _, _ := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
	selected, _ := s.registry.Get(1)
	selected.RTPReceiveMode = "single"
	selected.RTPProxyPort = 10000
	_, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	require.ErrorContains(t, err, "固定播放地址")
	require.Zero(t, z.sharedOpen)
	require.Zero(t, inv.inviteCalls.Load())
}

func TestSinglePortLiveInviteFailureNeverClosesSharedListener(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	z := &sharedMockZLM{}
	inv := &mockInviter{inviteErr: errors.New("invite failed")}
	s, _, _ := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
	selected, _ := s.registry.Get(1)
	selected.RTPReceiveMode = "single"
	selected.RTPProxyPort = 10000
	_, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	require.Error(t, err)
	require.Equal(t, 1, z.sharedClose)
	require.Zero(t, z.closeCalls.Load())
	require.False(t, strings.Contains(z.closed, "-"))
}

func TestSinglePortStopRetainsIdentityUntilCleanupConfirmed(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	z := &sharedMockZLM{sharedErr: errors.New("cleanup pending")}
	inv := &mockInviter{}
	ch := aChannel()
	ch.StreamID = "3B9ACA01"
	ch.CurrentSSRC = "1000000001"
	s, _, channels := newFixedSvc(t, z, inv, onlineDevice(), ch)
	s.locationMap.Bind(ch.StreamID, 1)
	require.ErrorContains(t, s.stopDirect(context.Background(), ch.StreamID), "cleanup pending")
	require.Equal(t, "3B9ACA01", channels.c.StreamID)
	_, ok := s.locationMap.Lookup(ch.StreamID)
	require.True(t, ok)
	z.sharedErr = nil
	require.NoError(t, s.stopDirect(context.Background(), ch.StreamID))
	require.Empty(t, channels.c.StreamID)
}

func TestSinglePortRecoveryRestoresHexStreamAfterSwitchToMulti(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	ch := aChannel()
	ch.CurrentSSRC = "0200000007"
	ch.StreamID, _ = zlm.SinglePortStreamID(ch.CurrentSSRC)
	z := &sharedMockZLM{}
	z.online.Store(true)
	s, _, _ := newFixedSvc(t, z, &mockInviter{}, onlineDevice(), ch)
	s.BeginRecovery()
	stats, err := s.RecoverLiveSessions(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, stats.Restored)
	s.FinishRecovery()
	require.NoError(t, s.stopDirect(context.Background(), ch.StreamID))
	require.Equal(t, 1, z.sharedClose)
	require.Zero(t, z.closeCalls.Load())
}
