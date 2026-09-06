package streammonitor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

const (
	StatusOnline = "online"
	defaultVHost = "__defaultVhost__"
	defaultApp   = "rtp"
)

var (
	ErrNodeUnavailable = errors.New("media node unavailable")
	ErrStreamOffline   = errors.New("stream offline")
)

type LocationStore interface {
	Lookup(string) (int64, bool)
	Bind(string, int64)
	Unbind(string)
	// UnbindIfNode 仅当当前绑定仍是该节点时解绑,防删并发建立的新代次绑定
	UnbindIfNode(string, int64) bool
}

type NodeRegistry interface {
	Get(int64) (*node.Node, bool)
	ListActive() []*node.Node
}

type MediaClient interface {
	GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error)
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
		if exists {
			if snap, err := s.read(ctx, streamID, mediaNode); err == nil {
				return snap, nil
			}
		}
		// 绑定指向的节点不存在/不可达/已无此流:条件解绑旧绑定并扫描其余活跃节点,
		// 否则节点故障转移或流迁移后监控会持续误报离线。
		// 条件解绑防把并发建立的新代次绑定一并删掉
		s.locations.UnbindIfNode(streamID, nodeID)
	}

	active := s.nodes.ListActive()
	if len(active) == 0 {
		return nil, fmt.Errorf("%w: no active nodes", ErrNodeUnavailable)
	}
	failed := 0
	for _, mediaNode := range active {
		list, err := s.clientFor(mediaNode).GetMediaList(ctx, defaultVHost, defaultApp, streamID)
		if err != nil {
			failed++
			continue
		}
		if len(list) == 0 {
			continue
		}
		s.locations.Bind(streamID, mediaNode.ID)
		return s.snapshot(streamID, mediaNode, list), nil
	}
	if failed == len(active) {
		return nil, fmt.Errorf("%w: all active nodes failed", ErrNodeUnavailable)
	}
	return nil, ErrStreamOffline
}

func (s *Service) read(ctx context.Context, streamID string, mediaNode *node.Node) (*Snapshot, error) {
	list, err := s.clientFor(mediaNode).GetMediaList(ctx, defaultVHost, defaultApp, streamID)
	if err != nil {
		app.Log(ctx).Named("streammonitor").Warn("读取 ZLM 流概况失败",
			zap.String("event", "streammonitor.media_read_failed"),
			zap.String("streamId", streamID),
			zap.Int64("nodeId", mediaNode.ID),
			zap.String("host", mediaNode.Host),
			zap.Int("apiPort", mediaNode.APIPort),
			zap.Error(err))
		return nil, fmt.Errorf("%w: %v", ErrNodeUnavailable, err)
	}
	if len(list) == 0 {
		return nil, ErrStreamOffline
	}
	return s.snapshot(streamID, mediaNode, list), nil
}

// snapshot 把同一路流在 ZLM 里的多个 schema 副本聚合成一个视图:
//   - reader 相关字段跨 schema 求和(浏览器走 rtmp/flv,rtsp 客户端走 rtsp,分别计数)
//   - 码率、字节数、存活时间跨 schema 取最大值(各 schema 独立打包,原始上行速率相同)
//   - 轨道元数据、录制标志取第一个副本即可(同源不会不一致)
func (s *Service) snapshot(streamID string, mediaNode *node.Node, list []zlm.MediaInfo) *Snapshot {
	base := list[0]
	var (
		readerSum      int
		totalReaderMax int
		bytesSpeedMax  uint64
		totalBytesMax  uint64
		aliveMax       uint64
		recordingMP4   bool
		recordingHLS   bool
	)
	for _, item := range list {
		readerSum += item.ReaderCount
		if item.TotalReaderCount > totalReaderMax {
			totalReaderMax = item.TotalReaderCount
		}
		if item.BytesSpeed > bytesSpeedMax {
			bytesSpeedMax = item.BytesSpeed
		}
		if item.TotalBytes > totalBytesMax {
			totalBytesMax = item.TotalBytes
		}
		if item.AliveSecond > aliveMax {
			aliveMax = item.AliveSecond
		}
		recordingMP4 = recordingMP4 || item.IsRecordingMP4
		recordingHLS = recordingHLS || item.IsRecordingHLS
	}
	tracks := make([]Track, 0, len(base.Tracks))
	for _, source := range base.Tracks {
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
		Quality:     Quality{BitrateKbps: float64(bytesSpeedMax) * 8 / 1000},
		Network: Network{
			BytesSpeed: bytesSpeedMax, TotalBytes: totalBytesMax,
			ReaderCount: readerSum, TotalReaderCount: totalReaderMax,
			AliveSecond: aliveMax,
		},
		Tracks:    tracks,
		Recording: Recording{MP4: recordingMP4, HLS: recordingHLS},
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
	FPS           float64  `json:"fps"`
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
