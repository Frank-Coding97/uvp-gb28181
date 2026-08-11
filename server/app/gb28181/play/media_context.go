package play

import (
	"errors"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

var ErrPlaybackMediaNotCurrent = errors.New("playback media is not current")

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
