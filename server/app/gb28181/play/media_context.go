package play

import (
	"context"
	"errors"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

var (
	ErrPlaybackMediaNotCurrent     = errors.New("playback media is not current")
	ErrPlaybackMediaStateUncertain = errors.New("playback media state is uncertain")
)

func (c *Coordinator) HasTrackedGeneration(deviceID, channelID string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[coordinatorKey{deviceID: deviceID, channelID: channelID}]
	return entry != nil && entry.state != LiveStateIdle
}

// CurrentSession returns a copy of the ready live generation owning streamID.
func (c *Coordinator) CurrentSession(streamID string) (LiveSession, bool) {
	if c == nil {
		return LiveSession{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.entries {
		if entry.state != LiveStateReady || entry.result == nil || entry.result.StreamID != streamID {
			continue
		}
		nodeID := entry.ownerNode
		if entry.result.Node != nil {
			nodeID = entry.result.Node.ID
		}
		return LiveSession{
			DeviceID: key.deviceID, ChannelID: key.channelID,
			StreamID: entry.result.StreamID, SSRC: entry.result.SSRC,
			Generation: entry.result.Generation, NodeID: nodeID,
			ModeAtStart: entry.result.ModeAtStart, State: entry.state,
		}, true
	}
	return LiveSession{}, false
}

// ResolvePlaybackMediaContext produces a trusted token binding from the active
// coordinator, location map, and node registry. It never trusts token claims to
// discover media ownership and never queries the business database.
func (s *Service) ResolvePlaybackMediaContext(appName, streamID, mediaServerID string) (playauth.Binding, error) {
	if s == nil || appName != zlmApp || streamID == "" || mediaServerID == "" || s.registry == nil || s.locationMap == nil {
		return playauth.Binding{}, ErrPlaybackMediaNotCurrent
	}
	session, ok := s.coordinator().CurrentSession(streamID)
	if !ok || session.State != LiveStateReady || session.Generation == 0 || session.NodeID == 0 {
		if deviceID, channelID, parseErr := ParseFixedStreamID(streamID); parseErr == nil &&
			s.coordinator().HasTrackedGeneration(deviceID, channelID) {
			return playauth.Binding{}, ErrPlaybackMediaStateUncertain
		}
		return playauth.Binding{}, ErrPlaybackMediaNotCurrent
	}
	locations, ok := s.locationMap.(interface {
		LookupCurrent(string) (stream.LiveRef, bool)
	})
	if !ok {
		return playauth.Binding{}, ErrPlaybackMediaNotCurrent
	}
	current, ok := locations.LookupCurrent(streamID)
	if !ok || current != session.Ref() {
		return playauth.Binding{}, ErrPlaybackMediaNotCurrent
	}
	mediaNode, ok := s.registry.Get(session.NodeID)
	if !ok || mediaNode == nil || mediaNode.MediaServerUUID == "" || mediaNode.MediaServerUUID != mediaServerID {
		return playauth.Binding{}, ErrPlaybackMediaNotCurrent
	}
	return playauth.Binding{
		DeviceID: session.DeviceID, ChannelID: session.ChannelID,
		App: appName, Stream: session.StreamID, MediaServerID: mediaNode.MediaServerUUID,
		MediaGeneration: session.Generation,
	}, nil
}

// ResolveColdPlaybackMediaContext only authorizes a zero-generation binding
// after the exact managed node confirms the fixed media is absent. Coordinator
// checks on both sides of the probe close the in-process start/stop race.
func (s *Service) ResolveColdPlaybackMediaContext(ctx context.Context, appName, streamID, mediaServerID string) (playauth.Binding, error) {
	if s == nil || ctx == nil || appName != zlmApp || streamID == "" || mediaServerID == "" || !s.useMultiNode() {
		return playauth.Binding{}, ErrPlaybackMediaStateUncertain
	}
	deviceID, channelID, err := ParseFixedStreamID(streamID)
	if err != nil {
		return playauth.Binding{}, ErrPlaybackMediaStateUncertain
	}
	coordinator := s.coordinator()
	if coordinator.HasTrackedGeneration(deviceID, channelID) {
		return playauth.Binding{}, ErrPlaybackMediaStateUncertain
	}
	var owner *node.Node
	for _, candidate := range s.registry.ListActive() {
		if candidate == nil || candidate.MediaServerUUID != mediaServerID {
			continue
		}
		if owner != nil {
			return playauth.Binding{}, ErrPlaybackMediaStateUncertain
		}
		owner = candidate
	}
	if owner == nil {
		return playauth.Binding{}, ErrPlaybackMediaStateUncertain
	}
	online, err := s.clientForNode(owner).IsMediaOnline(ctx, zlmApp, streamID)
	if err != nil || online || coordinator.HasTrackedGeneration(deviceID, channelID) {
		return playauth.Binding{}, ErrPlaybackMediaStateUncertain
	}
	return playauth.Binding{
		DeviceID: deviceID, ChannelID: channelID,
		App: appName, Stream: streamID, MediaServerID: mediaServerID,
	}, nil
}
