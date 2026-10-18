package traffic

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

type SamplerNodeRegistry interface {
	ListActive() []*node.Node
}

type SamplerMediaClient interface {
	GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error)
	GetMediaPlayerList(context.Context, string, string, string, string) ([]zlm.MediaPlayer, error)
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
	interval    time.Duration
	lastRoundAt time.Time
}

func NewSampler(nodes SamplerNodeRegistry, clientFor SamplerClientFactory, repo *GormRepository, attribution *AttributionResolver, realtime *RealtimeStore, now func() time.Time) *Sampler {
	if clientFor == nil {
		clientFor = func(n *node.Node) SamplerMediaClient { return zlm.NewClientForNode(n) }
	}
	if now == nil {
		now = time.Now
	}
	return &Sampler{nodes: nodes, clientFor: clientFor, repo: repo, attribution: attribution, realtime: realtime, now: now, failures: make(map[int64]int), interval: time.Minute}
}

func (s *Sampler) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	done := make(chan struct{})
	if interval <= 0 {
		interval = time.Minute
	}
	s.mu.Lock()
	s.interval = interval
	s.mu.Unlock()
	go func() {
		defer close(done)
		_ = s.SampleOnce(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
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
	roundSeconds := s.roundSeconds(s.now().UTC())
	for _, mediaNode := range s.nodes.ListActive() {
		if mediaNode == nil {
			continue
		}
		items, err := s.clientFor(mediaNode).GetMediaList(ctx, "__defaultVhost__", "rtp", "")
		if err != nil {
			s.noteFailure(ctx, mediaNode.ID, s.now().UTC())
			failures = append(failures, fmt.Errorf("node %d: %w", mediaNode.ID, err))
			continue
		}
		s.noteSuccess(ctx, mediaNode.ID, s.now().UTC())
		if err := s.observeNode(ctx, mediaNode, items, roundSeconds); err != nil {
			failures = append(failures, err)
		}
	}
	// 收尾失联的下行会话:观看连接已断开但 ZLM 未上报 flow_report
	// (低于 flowThreshold / hook 丢失 / ZLM 宕机),否则会话永远挂在 active
	if _, err := s.repo.SettleStaleDownstream(ctx, s.staleDownstreamCutoff(s.now().UTC()), 200); err != nil {
		failures = append(failures, fmt.Errorf("settle stale downstream: %w", err))
	}
	return errors.Join(failures...)
}

func (s *Sampler) noteFailure(ctx context.Context, nodeID int64, at time.Time) {
	s.mu.Lock()
	s.failures[nodeID]++
	count := s.failures[nodeID]
	s.mu.Unlock()
	if count == 2 {
		_ = s.repo.OpenGap(ctx, nodeID, "api_failed", at)
	}
}

func (s *Sampler) noteSuccess(ctx context.Context, nodeID int64, at time.Time) {
	s.mu.Lock()
	delete(s.failures, nodeID)
	s.mu.Unlock()
	// ⛔ 这里曾经是 `if count >= 2 { CloseGap }`:failures 只活在进程内存里,
	//    后端重启后一律清零 ⇒ 重启前留下的 open 缺口**再也没有机会被关上**
	//    (2026-10-18 实测:节点 5 的 gap#46 从 14:34 一直挂到收盘,首页流量图因此
	//     永久显示「部分数据」,而实际采样早已恢复、账目逐字节对平)。
	//    CloseGap 自带 `state = 'open'` 守卫,没有缺口时是一次空更新,
	//    所以无条件调用既幂等又能自愈历史残留。
	_ = s.repo.CloseGap(ctx, nodeID, "api_failed", at)
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
	// readerItems 是这条流下有观看者的各 schema 媒体项,用于逐个拉取播放器列表
	readerItems []zlm.MediaInfo
}

// roundTiming 记录本轮采样耗时基准:实测下行速率 = 本轮下行增量 / 距上一轮的秒数
func (s *Sampler) roundSeconds(at time.Time) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	seconds := s.interval.Seconds()
	if !s.lastRoundAt.IsZero() {
		if elapsed := at.Sub(s.lastRoundAt).Seconds(); elapsed > 0 {
			seconds = elapsed
		}
	}
	s.lastRoundAt = at
	return seconds
}

// staleDownstreamCutoff 返回下行会话的失联判定线:超过 3 个采样周期未再出现的
// 观看连接视为已断开但丢了 flow_report,需要收尾。flow_report 正常在断开数秒内
// 到达,3 个周期的宽限足够避免误伤。
func (s *Sampler) staleDownstreamCutoff(at time.Time) time.Time {
	s.mu.Lock()
	interval := s.interval
	s.mu.Unlock()
	if interval <= 0 {
		interval = time.Minute
	}
	return at.Add(-3 * interval)
}

func (s *Sampler) observeNode(ctx context.Context, mediaNode *node.Node, items []zlm.MediaInfo, roundSeconds float64) error {
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
		if item.ReaderCount > 0 {
			group.readerItems = append(group.readerItems, item)
		}
	}
	at := s.now().UTC()
	var failures []error
	for _, group := range groups {
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
		downstreamDelta, downstreamMeasured, downstreamErr := s.samplePlayers(ctx, mediaNode, group, attribution, at)
		if downstreamErr != nil {
			failures = append(failures, downstreamErr)
		}
		if s.realtime != nil {
			snapshot := RealtimeSnapshot{
				DeviceCode: attribution.DeviceCode, ChannelCode: attribution.ChannelCode, NodeID: mediaNode.ID, Stream: group.stream,
				InProgressUpstreamBytes: group.maxTotal, UpstreamBytesPerSecond: group.maxSpeed, ReaderCount: group.readerCount,
				EstimatedDownstreamBytesPerSec: group.maxSpeed * uint64(group.readerCount), SampledAt: at,
				DownstreamMeasured: downstreamMeasured,
			}
			if downstreamMeasured {
				snapshot.DownstreamBytesPerSecond = uint64(float64(downstreamDelta) / roundSeconds)
			}
			s.realtime.Put(snapshot)
		}
	}
	return errors.Join(failures...)
}

// samplePlayers 轮询一条流下所有观看连接的实时下行字节,按连接逐条增量入账。
// 与上行同一条「采样累计 + 断开时 flow_report 收尾」路径:
// 这里用 ObserveAbsolute 增量,断开时 CollectFlow 用同一个 businessKey 结算尾差。
// 返回 (本轮下行字节增量, 节点是否支持实时下行计数, 错误)。
// ⛔ TotalBytes 为 nil(旧版 ZLM / HLS 切片会话)时必须跳过,不能与
// HlsMediaSource 按 cookie 聚合上报的 flow_report 重复计数。
func (s *Sampler) samplePlayers(ctx context.Context, mediaNode *node.Node, group *groupedMedia, attribution Attribution, at time.Time) (uint64, bool, error) {
	if len(group.readerItems) == 0 {
		return 0, false, nil
	}
	client := s.clientFor(mediaNode)
	var totalDelta uint64
	measured := false
	var failures []error
	seen := make(map[string]struct{})
	for _, item := range group.readerItems {
		players, err := client.GetMediaPlayerList(ctx, item.Schema, item.VHost, item.App, item.Stream)
		if err != nil {
			failures = append(failures, fmt.Errorf("player list node %d stream %s schema %s: %w", mediaNode.ID, group.stream, item.Schema, err))
			continue
		}
		for _, player := range players {
			if player.Identifier == "" || player.TotalBytes == nil {
				continue
			}
			// 同一连接可能因多 schema 媒体项重复出现(例如 rtsp 会话同时挂在
			// rtp/schema 两棵树上),按 identifier 去重,一轮只入账一次
			if _, dup := seen[player.Identifier]; dup {
				continue
			}
			seen[player.Identifier] = struct{}{}
			measured = true
			result, err := s.repo.Apply(ctx, ApplyRequest{
				BusinessKey: downstreamBusinessKey(mediaNode.ID, player.Identifier),
				NodeID:      mediaNode.ID, MediaServerUUID: mediaNode.MediaServerUUID, ZLMSessionID: player.Identifier,
				Direction:  DirectionDownstream,
				DeviceCode: attribution.DeviceCode, ChannelCode: attribution.ChannelCode, OwnerDeptID: attribution.OwnerDeptID,
				MediaKind: attribution.MediaKind, Schema: item.Schema, VHost: item.VHost, App: item.App, Stream: item.Stream,
				AbsoluteBytes: *player.TotalBytes, At: at,
			})
			if err != nil {
				failures = append(failures, fmt.Errorf("apply player node %d stream %s id %s: %w", mediaNode.ID, group.stream, player.Identifier, err))
				continue
			}
			totalDelta += result.DeltaBytes
		}
	}
	return totalDelta, measured, errors.Join(failures...)
}
