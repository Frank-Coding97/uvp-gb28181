package streammonitor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const (
	StatusOnline  = "online"
	defaultVHost  = "__defaultVhost__"
	defaultApp    = "rtp"
	defaultSchema = "rtsp"
)

var (
	ErrNodeUnavailable = errors.New("media node unavailable")
	ErrStreamOffline   = errors.New("stream offline")
)

type LocationStore interface {
	Lookup(string) (int64, bool)
	Bind(string, int64)
}

type NodeRegistry interface {
	Get(int64) (*node.Node, bool)
	ListActive() []*node.Node
}

type MediaClient interface {
	GetMediaInfo(context.Context, string, string, string, string) (*zlm.MediaInfo, error)
}

type ClientFactory func(*node.Node) MediaClient

type Service struct {
	nodes     NodeRegistry
	locations LocationStore
	clientFor ClientFactory
	now       func() time.Time
}

func NewService(nodes NodeRegistry, locations LocationStore, clientFor ClientFactory, now func() time.Time) *Service {
	if clientFor == nil {
		clientFor = func(n *node.Node) MediaClient { return zlm.NewClientForNode(n) }
	}
	if now == nil {
		now = time.Now
	}
	return &Service{nodes: nodes, locations: locations, clientFor: clientFor, now: now}
}

func (s *Service) Get(ctx context.Context, streamID string) (*Snapshot, error) {
	if s == nil || s.nodes == nil || s.locations == nil {
		return nil, fmt.Errorf("%w: service not configured", ErrNodeUnavailable)
	}
	if nodeID, ok := s.locations.Lookup(streamID); ok {
		mediaNode, exists := s.nodes.Get(nodeID)
		if !exists {
			return nil, fmt.Errorf("%w: node %d not found", ErrNodeUnavailable, nodeID)
		}
		return s.read(ctx, streamID, mediaNode)
	}

	active := s.nodes.ListActive()
	if len(active) == 0 {
		return nil, fmt.Errorf("%w: no active nodes", ErrNodeUnavailable)
	}
	failed := 0
	for _, mediaNode := range active {
		info, err := s.clientFor(mediaNode).GetMediaInfo(ctx, defaultSchema, defaultVHost, defaultApp, streamID)
		if err != nil {
			failed++
			continue
		}
		if !info.Online {
			continue
		}
		s.locations.Bind(streamID, mediaNode.ID)
		return s.snapshot(streamID, mediaNode, info), nil
	}
	if failed == len(active) {
		return nil, fmt.Errorf("%w: all active nodes failed", ErrNodeUnavailable)
	}
	return nil, ErrStreamOffline
}

func (s *Service) read(ctx context.Context, streamID string, mediaNode *node.Node) (*Snapshot, error) {
	info, err := s.clientFor(mediaNode).GetMediaInfo(ctx, defaultSchema, defaultVHost, defaultApp, streamID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNodeUnavailable, err)
	}
	if !info.Online {
		return nil, ErrStreamOffline
	}
	return s.snapshot(streamID, mediaNode, info), nil
}

func (s *Service) snapshot(streamID string, mediaNode *node.Node, info *zlm.MediaInfo) *Snapshot {
	tracks := make([]Track, 0, len(info.Tracks))
	for _, source := range info.Tracks {
		kind := "unknown"
		switch source.CodecType {
		case 0:
			kind = "video"
		case 1:
			kind = "audio"
		}
		tracks = append(tracks, Track{
			Kind: kind, Codec: source.CodecIDName, Ready: source.Ready,
			Frames: source.Frames, Duration: source.Duration, Loss: source.Loss,
			Width: source.Width, Height: source.Height, FPS: source.FPS,
			KeyFrames: source.KeyFrames, GOPSize: source.GOPSize, GOPIntervalMS: source.GOPIntervalMS,
			SampleRate: source.SampleRate, Channels: source.Channels, SampleBit: source.SampleBit,
		})
	}
	return &Snapshot{
		StreamID:    streamID,
		CollectedAt: s.now().UTC(),
		Status:      StatusOnline,
		Node:        NodeInfo{ID: mediaNode.ID, Name: mediaNode.Name, Host: mediaNode.Host},
		Quality:     Quality{BitrateKbps: float64(info.BytesSpeed) * 8 / 1000},
		Network: Network{
			BytesSpeed: info.BytesSpeed, TotalBytes: info.TotalBytes,
			ReaderCount: info.ReaderCount, TotalReaderCount: info.TotalReaderCount,
			AliveSecond: info.AliveSecond,
		},
		Tracks:    tracks,
		Recording: Recording{MP4: info.IsRecordingMP4, HLS: info.IsRecordingHLS},
	}
}

type Snapshot struct {
	StreamID    string    `json:"streamId"`
	CollectedAt time.Time `json:"collectedAt"`
	Status      string    `json:"status"`
	Node        NodeInfo  `json:"node"`
	Quality     Quality   `json:"quality"`
	Network     Network   `json:"network"`
	Tracks      []Track   `json:"tracks"`
	Recording   Recording `json:"recording"`
}

type NodeInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
}

type Quality struct {
	BitrateKbps float64 `json:"bitrateKbps"`
}

type Network struct {
	BytesSpeed       uint64 `json:"bytesSpeed"`
	TotalBytes       uint64 `json:"totalBytes"`
	ReaderCount      int    `json:"readerCount"`
	TotalReaderCount int    `json:"totalReaderCount"`
	AliveSecond      uint64 `json:"aliveSecond"`
}

type Track struct {
	Kind          string   `json:"kind"`
	Codec         string   `json:"codec"`
	Ready         bool     `json:"ready"`
	Frames        int64    `json:"frames"`
	Duration      float64  `json:"duration"`
	Loss          *float64 `json:"loss"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	FPS           int      `json:"fps"`
	KeyFrames     int64    `json:"keyFrames"`
	GOPSize       int      `json:"gopSize"`
	GOPIntervalMS int      `json:"gopIntervalMs"`
	SampleRate    int      `json:"sampleRate"`
	Channels      int      `json:"channels"`
	SampleBit     int      `json:"sampleBit"`
}

type Recording struct {
	MP4 bool `json:"mp4"`
	HLS bool `json:"hls"`
}
