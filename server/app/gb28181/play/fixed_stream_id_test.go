package play

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFixedStreamIDRoundTrip(t *testing.T) {
	const deviceID = "34020000001320000001"
	const channelID = "34020000001310000001"
	streamID, err := FixedStreamID(deviceID, channelID)
	require.NoError(t, err)
	require.Equal(t, deviceID+"_"+channelID, streamID)
	require.Len(t, streamID, 41)

	gotDevice, gotChannel, err := ParseFixedStreamID(streamID)
	require.NoError(t, err)
	require.Equal(t, deviceID, gotDevice)
	require.Equal(t, channelID, gotChannel)
}

func TestFixedStreamIDRejectsInvalidIDs(t *testing.T) {
	validDevice := "34020000001320000001"
	validChannel := "34020000001310000001"
	tests := []struct {
		name     string
		deviceID string
		channel  string
	}{
		{name: "empty device", channel: validChannel},
		{name: "short device", deviceID: "340200", channel: validChannel},
		{name: "long channel", deviceID: validDevice, channel: validChannel + "1"},
		{name: "non digit", deviceID: validDevice, channel: "3402000000131000000x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FixedStreamID(tt.deviceID, tt.channel)
			require.ErrorIs(t, err, ErrInvalidFixedStreamID)
		})
	}

	for _, value := range []string{
		"", validDevice, validDevice + "_", "_" + validChannel,
		validDevice + "_" + validChannel + "_extra",
		validDevice + "-" + validChannel,
	} {
		_, _, err := ParseFixedStreamID(value)
		require.ErrorIs(t, err, ErrInvalidFixedStreamID, value)
	}
}

func TestFixedStreamIDIncludesDeviceIdentity(t *testing.T) {
	first, err := FixedStreamID("34020000001320000001", "34020000001310000001")
	require.NoError(t, err)
	second, err := FixedStreamID("34020000001320000002", "34020000001310000001")
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}
