package traffic

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type SamplerNodeRegistry interface {
	ListActive() []*node.Node
}

type SamplerMediaClient interface {
	GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error)
}

type SamplerClientFactory func(*node.Node) SamplerMediaClient

type Sampler struct {
	nodes       SamplerNodeRegistry
	clientFor   SamplerClientFactory
	repo        *GormRepository
	attribution *AttributionResolver
	realtime    *RealtimeStore
	now         func() time.Time
	mu          sync.Mutex
	failures    map[int64]int
}

func NewSampler(nodes SamplerNodeRegistry, clientFor SamplerClientFactory, repo *GormRepository, attribution *AttributionResolver, realtime *RealtimeStore, now func() time.Time) *Sampler {
	if clientFor == nil {
		clientFor = func(n *node.Node) SamplerMediaClient { return zlm.NewClientForNode(n) }
	}
	if now == nil {
		now = time.Now
	}
	return &Sampler{nodes: nodes, clientFor: clientFor, repo: repo, attribution: attribution, realtime: realtime, now: now, failures: make(map[int64]int)}
}

func (s *Sampler) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	done := make(chan struct{})
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		defer close(done)
		if ctx.Err() != nil {
			return
		}
		_ = s.SampleOnce(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				_ = s.SampleOnce(ctx)
			}
		}
	}()
	return done
}

func (s *Sampler) SampleOnce(ctx context.Context) error {
	if s == nil || s.nodes == nil || s.repo == nil || s.attribution == nil {
		return errors.New("traffic sampler unavailable")
	}
	var failures []error
	for _, mediaNode := range s.nodes.ListActive() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if mediaNode == nil {
			continue
		}
		items, err := s.clientFor(mediaNode).GetMediaList(ctx, "__defaultVhost__", "rtp", "")
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			s.noteFailure(ctx, mediaNode.ID, s.now().UTC())
			failures = append(failures, fmt.Errorf("node %d: %w", mediaNode.ID, err))
			continue
		}
		s.noteSuccess(ctx, mediaNode.ID, s.now().UTC())
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.observeNode(ctx, mediaNode, items); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *Sampler) noteFailure(ctx context.Context, nodeID int64, at time.Time) {
	s.mu.Lock()
	s.failures[nodeID]++
	count := s.failures[nodeID]
	s.mu.Unlock()
	if count == 2 && ctx.Err() == nil {
		_ = s.repo.OpenGap(ctx, nodeID, "api_failed", at)
	}
}

func (s *Sampler) noteSuccess(ctx context.Context, nodeID int64, at time.Time) {
	s.mu.Lock()
	count := s.failures[nodeID]
	delete(s.failures, nodeID)
	s.mu.Unlock()
	if count >= 2 && ctx.Err() == nil {
		_ = s.repo.CloseGap(ctx, nodeID, "api_failed", at)
	}
}

type groupedMedia struct {
	maxTotal    uint64
	maxSpeed    uint64
	readerCount int
	createStamp uint64
	schema      string
	stream      string
	vhost       string
	app         string
}

func (s *Sampler) observeNode(ctx context.Context, mediaNode *node.Node, items []zlm.MediaInfo) error {
	groups := make(map[string]*groupedMedia)
	for _, item := range items {
		if item.App != "rtp" || item.Stream == "" {
			continue
		}
		group := groups[item.Stream]
		if group == nil {
			group = &groupedMedia{stream: item.Stream, vhost: item.VHost, app: item.App, schema: item.Schema, createStamp: item.CreateStamp}
			groups[item.Stream] = group
		}
		if item.TotalBytes > group.maxTotal {
			group.maxTotal = item.TotalBytes
			group.schema = item.Schema
			group.createStamp = item.CreateStamp
		}
		if item.BytesSpeed > group.maxSpeed {
			group.maxSpeed = item.BytesSpeed
		}
		group.readerCount += item.ReaderCount
	}
	at := s.now().UTC()
	var failures []error
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			return errors.Join(failures...)
		}
		attribution, err := s.attribution.Resolve(mediaNode.ID, group.app, group.stream)
		if err != nil {
			failures = append(failures, fmt.Errorf("node %d stream %s: %w", mediaNode.ID, group.stream, err))
			continue
		}
		_, err = s.repo.Apply(ctx, ApplyRequest{
			BusinessKey: fmt.Sprintf("sample:%d:%s:%d", mediaNode.ID, group.stream, group.createStamp),
			NodeID:      mediaNode.ID, MediaServerUUID: mediaNode.MediaServerUUID, Direction: DirectionUpstream,
			DeviceCode: attribution.DeviceCode, ChannelCode: attribution.ChannelCode, OwnerDeptID: attribution.OwnerDeptID,
			MediaKind: attribution.MediaKind, Schema: group.schema, VHost: group.vhost, App: group.app, Stream: group.stream,
			CreateStamp: group.createStamp, AbsoluteBytes: group.maxTotal, At: at,
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("apply node %d stream %s: %w", mediaNode.ID, group.stream, err))
			continue
		}
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			return errors.Join(failures...)
		}
		if s.realtime != nil {
			s.realtime.Put(RealtimeSnapshot{
				DeviceCode: attribution.DeviceCode, ChannelCode: attribution.ChannelCode, NodeID: mediaNode.ID, Stream: group.stream,
				InProgressUpstreamBytes: group.maxTotal, UpstreamBytesPerSecond: group.maxSpeed, ReaderCount: group.readerCount,
				EstimatedDownstreamBytesPerSec: group.maxSpeed * uint64(group.readerCount), SampledAt: at,
			})
		}
	}
	return errors.Join(failures...)
}
