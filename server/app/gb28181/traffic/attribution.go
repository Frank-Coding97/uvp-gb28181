package traffic

import (
	"errors"
	"fmt"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
)

var ErrUnattributed = errors.New("traffic media cannot be attributed")

type MediaKind string

const (
	MediaKindLive     MediaKind = "live"
	MediaKindPlayback MediaKind = "playback"
	MediaKindDownload MediaKind = "download"
)

type Attribution struct {
	NodeID            int64
	StreamID          string
	Generation        uint64
	DeviceCode        string
	ChannelCode       string
	OwnerDeptID       uint
	MediaKind         MediaKind
	PlaybackSessionID string
}

type LiveBinding struct {
	NodeID      int64
	StreamID    string
	Generation  uint64
	DeviceCode  string
	ChannelCode string
	OwnerDeptID uint
}

type PlaybackBinding struct {
	NodeID      int64
	StreamID    string
	SessionID   string
	DeviceCode  string
	ChannelCode string
	OwnerDeptID uint
	MediaKind   MediaKind
}

type AttributionResolver struct {
	mu       sync.RWMutex
	live     map[string]LiveBinding
	playback map[string]PlaybackBinding
}

func NewAttributionResolver() *AttributionResolver {
	return &AttributionResolver{live: make(map[string]LiveBinding), playback: make(map[string]PlaybackBinding)}
}

func resolverKey(nodeID int64, streamID string) string {
	return fmt.Sprintf("%d\x00%s", nodeID, streamID)
}

func (r *AttributionResolver) RegisterLive(binding LiveBinding) {
	if r == nil || binding.NodeID == 0 || binding.StreamID == "" || binding.DeviceCode == "" || binding.ChannelCode == "" {
		return
	}
	r.mu.Lock()
	r.live[resolverKey(binding.NodeID, binding.StreamID)] = binding
	delete(r.playback, resolverKey(binding.NodeID, binding.StreamID))
	r.mu.Unlock()
}

func (r *AttributionResolver) RegisterPlayback(binding PlaybackBinding) {
	if r == nil || binding.NodeID == 0 || binding.StreamID == "" || binding.SessionID == "" || binding.DeviceCode == "" || binding.ChannelCode == "" {
		return
	}
	if binding.MediaKind == "" {
		binding.MediaKind = MediaKindPlayback
	}
	r.mu.Lock()
	r.playback[resolverKey(binding.NodeID, binding.StreamID)] = binding
	delete(r.live, resolverKey(binding.NodeID, binding.StreamID))
	r.mu.Unlock()
}

func (r *AttributionResolver) Resolve(nodeID int64, _ string, streamID string) (Attribution, error) {
	if r == nil || nodeID == 0 || streamID == "" {
		return Attribution{}, ErrUnattributed
	}
	key := resolverKey(nodeID, streamID)
	r.mu.RLock()
	if binding, ok := r.playback[key]; ok {
		r.mu.RUnlock()
		return Attribution{NodeID: binding.NodeID, StreamID: binding.StreamID, DeviceCode: binding.DeviceCode, ChannelCode: binding.ChannelCode, OwnerDeptID: binding.OwnerDeptID, MediaKind: binding.MediaKind, PlaybackSessionID: binding.SessionID}, nil
	}
	if binding, ok := r.live[key]; ok {
		r.mu.RUnlock()
		return Attribution{NodeID: binding.NodeID, StreamID: binding.StreamID, Generation: binding.Generation, DeviceCode: binding.DeviceCode, ChannelCode: binding.ChannelCode, OwnerDeptID: binding.OwnerDeptID, MediaKind: MediaKindLive}, nil
	}
	r.mu.RUnlock()

	deviceCode, channelCode, err := play.ParseFixedStreamID(streamID)
	if err != nil {
		return Attribution{}, ErrUnattributed
	}
	return Attribution{NodeID: nodeID, StreamID: streamID, DeviceCode: deviceCode, ChannelCode: channelCode, MediaKind: MediaKindLive}, nil
}
