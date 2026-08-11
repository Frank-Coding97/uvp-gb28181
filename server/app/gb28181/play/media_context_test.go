package play

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestResolvePlaybackMediaContextUsesCurrentCoordinatorGeneration(t *testing.T) {
	locationMap := stream.NewLocationMap()
	mediaNode := &node.Node{ID: 7, MediaServerUUID: "node-a"}
	service := &Service{registry: fixedTestRegistry{mediaNode}, locationMap: locationMap}
	result := &Result{
		StreamID: "0200000001", SSRC: "0200000001", App: "rtp", Generation: 9,
		Node: &ResultNode{ID: mediaNode.ID}, ModeAtStart: LiveModeDynamic,
	}
	req := Request{DeviceID: "37010301021320000014", ChannelID: "37010301021320000001", RequiredNode: mediaNode.ID}
	require.True(t, service.coordinator().Restore(req, result))
	require.True(t, locationMap.BindCurrent(stream.LiveRef{StreamID: result.StreamID, SSRC: result.SSRC, Generation: result.Generation, NodeID: mediaNode.ID}))

	binding, err := service.ResolvePlaybackMediaContext("rtp", result.StreamID, "node-a")
	require.NoError(t, err)
	require.Equal(t, req.DeviceID, binding.DeviceID)
	require.Equal(t, req.ChannelID, binding.ChannelID)
	require.Equal(t, uint64(9), binding.MediaGeneration)

	_, err = service.ResolvePlaybackMediaContext("rtp", result.StreamID, "node-b")
	require.ErrorIs(t, err, ErrPlaybackMediaNotCurrent)
}

func TestResolvePlaybackMediaContextRejectsStaleOrMissingRegistryState(t *testing.T) {
	locationMap := stream.NewLocationMap()
	mediaNode := &node.Node{ID: 7, MediaServerUUID: "node-a"}
	service := &Service{registry: fixedTestRegistry{mediaNode}, locationMap: locationMap}
	result := &Result{StreamID: "fixed", SSRC: "0200000002", App: "rtp", Generation: 2, Node: &ResultNode{ID: 7}, ModeAtStart: LiveModeFixed}
	require.True(t, service.coordinator().Restore(Request{DeviceID: "37010301021320000014", ChannelID: "37010301021320000001", RequiredNode: 7}, result))
	require.True(t, locationMap.BindCurrent(stream.LiveRef{StreamID: "fixed", SSRC: "0200000001", Generation: 1, NodeID: 7}))

	_, err := service.ResolvePlaybackMediaContext("rtp", "fixed", "node-a")
	require.True(t, errors.Is(err, ErrPlaybackMediaNotCurrent))
	_, err = service.ResolvePlaybackMediaContext("rtp", "missing", "node-a")
	require.ErrorIs(t, err, ErrPlaybackMediaNotCurrent)
}
