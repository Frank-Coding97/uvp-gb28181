package traffic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAttributionResolverPrefersRegisteredLiveGeneration(t *testing.T) {
	r := NewAttributionResolver()
	r.RegisterLive(LiveBinding{NodeID: 7, StreamID: "dynamic-1", Generation: 3, DeviceCode: "device-1", ChannelCode: "channel-1"})

	got, err := r.Resolve(7, "rtp", "dynamic-1")
	require.NoError(t, err)
	require.Equal(t, "device-1", got.DeviceCode)
	require.Equal(t, "channel-1", got.ChannelCode)
	require.EqualValues(t, 3, got.Generation)
}

func TestAttributionResolverUsesFixedStreamOnlyWhenUnregistered(t *testing.T) {
	r := NewAttributionResolver()
	stream := "34020000001320000001_34020000001310000001"

	got, err := r.Resolve(7, "rtp", stream)
	require.NoError(t, err)
	require.Equal(t, "34020000001320000001", got.DeviceCode)
	require.Equal(t, "34020000001310000001", got.ChannelCode)
	require.Equal(t, MediaKindLive, got.MediaKind)

	r.RegisterLive(LiveBinding{NodeID: 7, StreamID: stream, Generation: 9, DeviceCode: "device-override", ChannelCode: "channel-override"})
	got, err = r.Resolve(7, "rtp", stream)
	require.NoError(t, err)
	require.Equal(t, "device-override", got.DeviceCode)
}

func TestAttributionResolverRejectsUnknownStream(t *testing.T) {
	_, err := NewAttributionResolver().Resolve(7, "rtp", "unknown")
	require.ErrorIs(t, err, ErrUnattributed)
}

func TestAttributionResolverPlaybackUsesSessionIdentity(t *testing.T) {
	r := NewAttributionResolver()
	r.RegisterPlayback(PlaybackBinding{NodeID: 9, StreamID: "playback-1", SessionID: "pb-1", DeviceCode: "device-2", ChannelCode: "channel-2"})

	got, err := r.Resolve(9, "rtp", "playback-1")
	require.NoError(t, err)
	require.Equal(t, MediaKindPlayback, got.MediaKind)
	require.Equal(t, "pb-1", got.PlaybackSessionID)
}
