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

func TestSinglePortRecoveryRestoresHexStreamAfterSwitchToMulti(t *testing.T) {	withFixedAddressPlaybackSettings(t, false, false)
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

// mediaListSharedMockZLM 是"既能单端口收流、又能查媒体列表"的节点客户端，
// 对应生产里的 *zlm.Client。
type mediaListSharedMockZLM struct {
	sharedMockZLM
	media     []zlm.MediaInfo
	mediaErr  error
	mediaArgs [3]string
}

func (m *mediaListSharedMockZLM) GetMediaList(_ context.Context, vhost, app, stream string) ([]zlm.MediaInfo, error) {
	m.mediaArgs = [3]string{vhost, app, stream}
	return m.media, m.mediaErr
}

// ⛔ 回归防守：receiveModeClient 内嵌的是 ZLM 接口（只有收流三方法），包装后
// 底层 *zlm.Client 的 GetMediaList 会消失。StopOnNoneReader 靠
// client.(mediaListClient) 断言查 readerCount，断言一失败就变成
// ErrReaderCountUnavailable ⇒ 无人观看时永不断流 ⇒ 实时流一直挂在 ZLM 上。
func TestReceiveModeClientForwardsMediaListForReaderCount(t *testing.T) {
	base := &mediaListSharedMockZLM{media: []zlm.MediaInfo{{ReaderCount: 0}}}
	wrapped := wrapReceiveModeClient(base)

	lister, ok := wrapped.(mediaListClient)
	require.True(t, ok, "包装后必须仍能查询 readerCount，否则 on_stream_none_reader 永远关不掉流")

	got, err := lister.GetMediaList(context.Background(), "__defaultVhost__", "rtp", "S")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, [3]string{"__defaultVhost__", "rtp", "S"}, base.mediaArgs)
}

// 底层确实查不到列表时必须如实报"不可用"，不能伪装成 0 个观众——
// 那会让上层以为无人观看，直接把正在被观看的流关掉。
func TestReceiveModeClientReportsUnavailableWhenBackendCannotListMedia(t *testing.T) {
	wrapped := wrapReceiveModeClient(&sharedMockZLM{})

	lister, ok := wrapped.(mediaListClient)
	require.True(t, ok)
	_, err := lister.GetMediaList(context.Background(), "__defaultVhost__", "rtp", "S")
	require.ErrorIs(t, err, ErrReaderCountUnavailable)
}
