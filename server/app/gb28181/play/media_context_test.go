package play

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type coldProbeZLM struct {
	*mockZLM
	onProbe func()
}

func (z *coldProbeZLM) IsMediaOnline(ctx context.Context, appName, streamID string) (bool, error) {
	if z.onProbe != nil {
		z.onProbe()
	}
	return z.mockZLM.IsMediaOnline(ctx, appName, streamID)
}

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

func TestResolveColdPlaybackMediaContextRequiresExactNodeToConfirmOffline(t *testing.T) {
	deviceID := "37010301021320000014"
	channelID := "37010301021320000001"
	streamID := deviceID + "_" + channelID
	for _, test := range []struct {
		name     string
		online   bool
		probeErr error
		wantOK   bool
	}{
		{name: "offline", wantOK: true},
		{name: "online", online: true},
		{name: "probe failure", probeErr: errors.New("probe failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			z := &mockZLM{onlineErr: test.probeErr}
			z.online.Store(test.online)
			service, _, _ := newFixedSvc(t, z, &mockInviter{}, onlineDevice(), aChannel())
			binding, err := service.ResolveColdPlaybackMediaContext(context.Background(), "rtp", streamID, "node-a")
			if test.wantOK {
				require.NoError(t, err)
				require.Equal(t, deviceID, binding.DeviceID)
				require.Equal(t, channelID, binding.ChannelID)
				require.Zero(t, binding.MediaGeneration)
				return
			}
			require.ErrorIs(t, err, ErrPlaybackMediaStateUncertain)
		})
	}
}

func TestResolveColdPlaybackMediaContextRechecksCoordinatorAfterProbe(t *testing.T) {
	deviceID := "37010301021320000014"
	channelID := "37010301021320000001"
	streamID := deviceID + "_" + channelID
	z := &coldProbeZLM{mockZLM: &mockZLM{}}
	service, _, _ := newFixedSvc(t, z, &mockInviter{}, onlineDevice(), aChannel())
	z.onProbe = func() {
		z.onProbe = nil
		require.True(t, service.coordinator().Restore(Request{
			DeviceID: deviceID, ChannelID: channelID, RequiredNode: 1,
		}, &Result{
			StreamID: streamID, SSRC: "0200000001", App: "rtp", Generation: 9,
			Node: &ResultNode{ID: 1}, ModeAtStart: LiveModeFixed,
		}))
	}

	_, err := service.ResolveColdPlaybackMediaContext(context.Background(), "rtp", streamID, "node-a")
	require.ErrorIs(t, err, ErrPlaybackMediaStateUncertain)
}
