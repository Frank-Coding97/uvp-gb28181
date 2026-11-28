package management

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
)

const (
	// DefaultAudienceCollectTimeout 是观众聚合的独立预算。它不占用节点采集的
	// 3s，避免「有大量观众」时把节点的统计/会话读取一起拖超时。
	DefaultAudienceCollectTimeout = 2 * time.Second
	DefaultAudienceWorkers        = 8
	// MaxAudienceTargets 限制单轮查询的播放器目标数。极端部署（几百路同时有人看）
	// 下宁可标记 truncated，也不让一次 overview 变成无界 fan-out。
	MaxAudienceTargets = 128
	// DefaultAudienceCacheTTL 用来吸收「首页 HTTP」与「后台采样器」在同一时刻的
	// 重复采集；只影响数字新鲜度最多 3 秒。
	DefaultAudienceCacheTTL = 3 * time.Second
)

type audienceCacheEntry struct {
	value     AudienceSummary
	expiresAt time.Time
}

// OverviewMediaPlayerReader 是观众聚合需要的最小能力面：只读一条流的播放器列表。
// 刻意不放通用的 API 名或原始响应，保持 management 边界只暴露类型化读取。
type OverviewMediaPlayerReader interface {
	GetMediaPlayerList(context.Context, int64, zlm.StreamTarget) ([]zlm.MediaPlayer, error)
}

// AudienceConnection 是一条「播放器连接」的稳定描述，仅用于前端展开明细与强退定位。
// Media 必须完整带上（含 schema），否则前端无法拼出强退请求。
type AudienceConnection struct {
	NodeID     int64         `json:"nodeId"`
	Media      MediaIdentity `json:"media"`
	Identifier string        `json:"identifier"`
	PeerIP     string        `json:"peerIp"`
	TypeID     string        `json:"typeId,omitempty"`
	PeerPort   int           `json:"peerPort"`
	LocalIP    string        `json:"localIp,omitempty"`
	LocalPort  int           `json:"localPort"`
}

// AudienceStreamRef 是观众正在观看的一条业务流（不含 schema：同一路流的多协议
// 入口在「看了几路」里只能算一路）。
type AudienceStreamRef struct {
	NodeID int64         `json:"nodeId"`
	Media  MediaIdentity `json:"media"`
}

// AudienceViewer 是按远端 IP 聚合后的一位观众。Connected 数量可能大于 Streams
// 长度：同一个人把同一路流开两个窗口，或者一路流同时走了 FLV 与 HLS 两个入口。
type AudienceViewer struct {
	PeerIP      string               `json:"peerIp"`
	Connections int                  `json:"connections"`
	Streams     []AudienceStreamRef  `json:"streams"`
	Details     []AudienceConnection `json:"details"`
}

// AudienceSummary 是「按远端 IP 去重」的观众口径。
//
// ⛔ 这里的 UniqueIPs 不是「账号数」：平台在 ZLM 侧拿不到账号，只能看到
// 对端 IP。同一出口 NAT 后多人会合并成 1；同一人换网络会算成 2。
// Available=false 表示运行时读取能力未装配或整轮不可用，前端必须回退旧口径，
// 不能把 0 当成「没人在看」。
type AudienceSummary struct {
	Available   bool                 `json:"available"`
	UniqueIPs   int                  `json:"uniqueIps"`
	Connections int                  `json:"connections"`
	Viewers     []AudienceViewer     `json:"viewers"`
	Partial     bool                 `json:"partial"`
	Truncated   bool                 `json:"truncated"`
	Errors      []NodeRuntimeFailure `json:"errors,omitempty"`
	AsOf        time.Time            `json:"asOf"`
}

type audienceTarget struct {
	nodeID int64
	media  MediaIdentity
	target zlm.StreamTarget
}

type audienceTargetResult struct {
	target  audienceTarget
	players []zlm.MediaPlayer
	err     error
}

// collectAudience 带 3 秒短缓存 + 单飞：overview 有两个调用方（首页 HTTP 与
// 后台采样器），同一时刻往往一起打进来，没必要对 ZLM 重复采一遍播放器列表。
// 缓存只影响数字的"新鲜度"最多 3 秒，对首页 5 秒刷新无感。
func (s *OverviewService) collectAudience(ctx context.Context, streams []RuntimeMedia) AudienceSummary {
	if s.players == nil {
		return AudienceSummary{Viewers: make([]AudienceViewer, 0), Errors: make([]NodeRuntimeFailure, 0), AsOf: s.clock()}
	}
	now := s.clock()
	s.audienceMu.Lock()
	cached := s.audienceCache
	s.audienceMu.Unlock()
	if cached != nil && now.Before(cached.expiresAt) {
		return cloneAudience(cached.value)
	}
	value, _, _ := s.audienceGroup.Do("audience", func() (interface{}, error) {
		s.audienceMu.Lock()
		cached := s.audienceCache
		s.audienceMu.Unlock()
		if cached != nil && now.Before(cached.expiresAt) {
			return cloneAudience(cached.value), nil
		}
		result := s.sampleAudience(ctx, streams)
		s.audienceMu.Lock()
		s.audienceCache = &audienceCacheEntry{value: result, expiresAt: now.Add(DefaultAudienceCacheTTL)}
		s.audienceMu.Unlock()
		return result, nil
	})
	summary, ok := value.(AudienceSummary)
	if !ok {
		return AudienceSummary{Viewers: make([]AudienceViewer, 0), Errors: make([]NodeRuntimeFailure, 0), AsOf: now}
	}
	return cloneAudience(summary)
}

// cloneAudience 让缓存值不把内部切片暴露给调用方。
func cloneAudience(summary AudienceSummary) AudienceSummary {
	cloned := summary
	cloned.Errors = append([]NodeRuntimeFailure(nil), summary.Errors...)
	cloned.Viewers = make([]AudienceViewer, len(summary.Viewers))
	for index, viewer := range summary.Viewers {
		cloned.Viewers[index] = viewer
		cloned.Viewers[index].Streams = append([]AudienceStreamRef(nil), viewer.Streams...)
		cloned.Viewers[index].Details = append([]AudienceConnection(nil), viewer.Details...)
	}
	return cloned
}

// sampleAudience 按远端 IP 聚合当前所有在线流的播放连接。
//
// 取数路径：overview 已经采到的流明细（ReaderCount>0）→ 逐条 getMediaPlayerList。
// 只对有读者的流发起请求，所以常规部署下每轮只有个位数到十几条调用。
func (s *OverviewService) sampleAudience(ctx context.Context, streams []RuntimeMedia) AudienceSummary {
	summary := AudienceSummary{
		Viewers: make([]AudienceViewer, 0),
		Errors:  make([]NodeRuntimeFailure, 0),
		AsOf:    s.clock(),
	}
	if s.players == nil {
		return summary
	}
	targets, truncated := s.audienceTargets(streams)
	summary.Truncated = truncated
	if len(targets) == 0 {
		summary.Available = true
		return summary
	}

	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), s.effectiveAudienceTimeout())
	defer cancel()

	results := make([]audienceTargetResult, len(targets))
	workers := s.audienceWorkers
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for index := range targets {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			target := targets[index]
			players, err := s.players.GetMediaPlayerList(operationCtx, target.nodeID, target.target)
			results[index] = audienceTargetResult{target: target, players: players, err: err}
		}(index)
	}
	wg.Wait()

	// 同一会话可能同时挂在多个 schema 树上（traffic 采样器也踩过这一点），
	// 必须按 (节点, identifier) 去重，否则一个人一路流会被算成多条连接。
	seenConnection := make(map[string]struct{}, len(targets))
	byPeer := make(map[string]*AudienceViewer)
	failedNodes := make(map[int64]struct{})
	readable := false
	for _, result := range results {
		if result.err != nil {
			if errorsIsMediaNotFound(result.err) {
				// 流在这轮采样和读播放器之间结束了：不算失败，也不贡献观众。
				readable = true
				continue
			}
			if _, failed := failedNodes[result.target.nodeID]; !failed {
				failedNodes[result.target.nodeID] = struct{}{}
				summary.Partial = true
				summary.Errors = append(summary.Errors, NodeRuntimeFailure{
					NodeID: result.target.nodeID,
					Error:  *s.nodeError(result.target.nodeID, "audience", result.err),
				})
			}
			continue
		}
		readable = true
		streamRef := AudienceStreamRef{NodeID: result.target.nodeID, Media: result.target.media}
		streamRef.Media.Schema = ""
		for _, player := range result.players {
			identifier := strings.TrimSpace(player.Identifier)
			if identifier == "" {
				continue
			}
			connectionKey := connectionIdentity(result.target.nodeID, identifier)
			if _, duplicate := seenConnection[connectionKey]; duplicate {
				continue
			}
			seenConnection[connectionKey] = struct{}{}
			peerIP := strings.TrimSpace(player.PeerIP)
			peerKey := peerIP
			if peerKey == "" {
				// 没有对端 IP 时不能把所有空值合并成一位观众，退化成按连接计数。
				peerKey = "unknown:" + identifier
			}
			viewer := byPeer[peerKey]
			if viewer == nil {
				viewer = &AudienceViewer{
					PeerIP:  peerIP,
					Streams: make([]AudienceStreamRef, 0, 1),
					Details: make([]AudienceConnection, 0, 1),
				}
				byPeer[peerKey] = viewer
			}
			viewer.Connections++
			viewer.Streams = appendUniqueStream(viewer.Streams, streamRef)
			viewer.Details = append(viewer.Details, AudienceConnection{
				NodeID:     result.target.nodeID,
				Media:      result.target.media,
				Identifier: identifier,
				PeerIP:     sanitizeAudienceLabel(peerIP),
				TypeID:     sanitizeAudienceLabel(player.TypeID),
				PeerPort:   player.PeerPort,
				LocalIP:    sanitizeAudienceLabel(player.LocalIP),
				LocalPort:  player.LocalPort,
			})
		}
	}

	summary.Available = readable
	viewers := make([]AudienceViewer, 0, len(byPeer))
	for _, viewer := range byPeer {
		summary.Connections += viewer.Connections
		viewers = append(viewers, *viewer)
	}
	sort.Slice(viewers, func(i, j int) bool {
		if viewers[i].Connections != viewers[j].Connections {
			return viewers[i].Connections > viewers[j].Connections
		}
		if len(viewers[i].Streams) != len(viewers[j].Streams) {
			return len(viewers[i].Streams) > len(viewers[j].Streams)
		}
		return viewers[i].PeerIP < viewers[j].PeerIP
	})
	summary.UniqueIPs = len(viewers)
	summary.Viewers = viewers
	sort.Slice(summary.Errors, func(i, j int) bool { return summary.Errors[i].NodeID < summary.Errors[j].NodeID })
	return summary
}

func (s *OverviewService) audienceTargets(streams []RuntimeMedia) ([]audienceTarget, bool) {
	seen := make(map[string]struct{}, len(streams))
	targets := make([]audienceTarget, 0, len(streams))
	truncated := false
	for _, stream := range streams {
		if !stream.Online || stream.ReaderCount <= 0 {
			continue
		}
		key := mediaTargetKey(stream.NodeID, stream.Media)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if len(targets) >= MaxAudienceTargets {
			truncated = true
			break
		}
		targets = append(targets, audienceTarget{
			nodeID: stream.NodeID,
			media:  stream.Media,
			target: zlm.StreamTarget{
				Schema: stream.Media.Schema,
				VHost:  stream.Media.Vhost,
				App:    stream.Media.App,
				Stream: stream.Media.Stream,
			},
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].nodeID != targets[j].nodeID {
			return targets[i].nodeID < targets[j].nodeID
		}
		return mediaTargetKey(targets[i].nodeID, targets[i].media) < mediaTargetKey(targets[j].nodeID, targets[j].media)
	})
	return targets, truncated
}

func (s *OverviewService) effectiveAudienceTimeout() time.Duration {
	timeout := s.audienceTimeout
	if timeout <= 0 {
		timeout = DefaultAudienceCollectTimeout
	}
	return timeout
}

func mediaTargetKey(nodeID int64, media MediaIdentity) string {
	return nodeIDString(nodeID) + "\u001f" + media.Schema + "\u001f" + media.Vhost + "\u001f" + media.App + "\u001f" + media.Stream
}

func connectionIdentity(nodeID int64, identifier string) string {
	return nodeIDString(nodeID) + "\u001f" + identifier
}

func appendUniqueStream(streams []AudienceStreamRef, candidate AudienceStreamRef) []AudienceStreamRef {
	for _, existing := range streams {
		if existing.NodeID == candidate.NodeID &&
			existing.Media.Vhost == candidate.Media.Vhost &&
			existing.Media.App == candidate.Media.App &&
			existing.Media.Stream == candidate.Media.Stream {
			return streams
		}
	}
	return append(streams, candidate)
}

// sanitizeAudienceLabel 只做展示用的裁剪：这些字段会直接进前端表格，
// 不允许把上游的异常长串或控制字符带出去。
func sanitizeAudienceLabel(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) > 128 {
		trimmed = trimmed[:128]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, trimmed)
}

func errorsIsMediaNotFound(err error) bool {
	return err != nil && (errors.Is(err, ErrMediaNotFound) || errors.Is(err, zlm.ErrMediaNotFound))
}
