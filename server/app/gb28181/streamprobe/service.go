package streamprobe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const DefaultDurationMS = 3000

// probeLocateGrace 覆盖 locate() 的节点探测:位置缓存未命中时要逐个节点问 GetMediaInfo,
// 这段耗时与采样窗口是串行的,必须算进调用预算。
const probeLocateGrace = 10 * time.Second

// probeCallBudget 是一次探测在服务侧的总预算,刻意由 zlm 的传输层超时派生。
//
// ⛔ 次序不能反。外层 ctx 若比传输层先到期,会有两个后果:
//  1. 超时被报成不带 "(Client.Timeout exceeded while awaiting headers)" 后缀的裸
//     context deadline exceeded,和"调用方取消 / 节点不可达"混在一起,现场分不出来;
//  2. 外层收得比传输层还早,白白浪费掉传输层剩下的窗口。原来的 durationMS+2s 就是这种
//     情形:2 秒余量覆盖不了节点定位往返 + 近 1MB 探针报文的聚合与传输,60 秒探针必挂。
func probeCallBudget(base time.Duration, durationMS int) time.Duration {
	return max(base, zlm.ProbeHTTPTimeout(durationMS)+probeLocateGrace)
}

func IsSupportedDuration(durationMS int) bool {
	return durationMS == 3000 || durationMS == 10000 || durationMS == 60000
}

var (
	ErrNodeUnavailable = errors.New("media node unavailable")
	ErrStreamOffline   = errors.New("stream offline")
	ErrProbeFailed     = errors.New("probe failed")
)

type ProbeClient interface {
	GetMediaInfo(context.Context, string, string, string, string) (*zlm.MediaInfo, error)
	AddProbe(context.Context, string, string, string, int) ([]zlm.ProbeFrame, error)
}

type ProbeNodeRegistry interface {
	Get(int64) (*node.Node, bool)
	ListActive() []*node.Node
}

type ProbeLocations interface {
	Lookup(string) (int64, bool)
	Bind(string, int64)
}

type ProbeSnapshot struct {
	NodeID      int64     `json:"nodeId"`
	NodeName    string    `json:"nodeName"`
	CompletedAt time.Time `json:"completedAt"`
	Result
}

type probeCall struct {
	done   chan struct{}
	result *ProbeSnapshot
	err    error
}

type probeKey struct {
	streamID   string
	durationMS int
}

type Service struct {
	nodes     ProbeNodeRegistry
	locations ProbeLocations
	clientFor func(*node.Node) ProbeClient
	timeout   time.Duration
	now       func() time.Time
	mu        sync.Mutex
	running   map[probeKey]*probeCall
}

func NewService(nodes ProbeNodeRegistry, locations ProbeLocations, clientFor func(*node.Node) ProbeClient, timeout time.Duration, now func() time.Time) *Service {
	if clientFor == nil {
		clientFor = func(n *node.Node) ProbeClient { return zlm.NewClientForNode(n) }
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &Service{nodes: nodes, locations: locations, clientFor: clientFor, timeout: timeout, now: now, running: make(map[probeKey]*probeCall)}
}

func (s *Service) Run(ctx context.Context, streamID string, durationMS int) (*ProbeSnapshot, error) {
	key := probeKey{streamID: streamID, durationMS: durationMS}
	s.mu.Lock()
	call := s.running[key]
	if call == nil {
		call = &probeCall{done: make(chan struct{})}
		s.running[key] = call
		go s.execute(key, call)
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-call.done:
		return call.result, call.err
	}
}

func (s *Service) execute(key probeKey, call *probeCall) {
	ctx, cancel := context.WithTimeout(context.Background(), probeCallBudget(s.timeout, key.durationMS))
	defer cancel()
	mediaNode, client, err := s.locate(ctx, key.streamID)
	if err == nil {
		var frames []zlm.ProbeFrame
		frames, err = client.AddProbe(ctx, "__defaultVhost__", "rtp", key.streamID, key.durationMS)
		if err != nil {
			err = fmt.Errorf("%w: %w", ErrProbeFailed, err)
		}
		if err == nil {
			call.result = &ProbeSnapshot{NodeID: mediaNode.ID, NodeName: mediaNode.Name, CompletedAt: s.now().UTC(), Result: Analyze(frames)}
		}
	}
	call.err = err
	s.mu.Lock()
	delete(s.running, key)
	close(call.done)
	s.mu.Unlock()
}

func (s *Service) locate(ctx context.Context, streamID string) (*node.Node, ProbeClient, error) {
	if s.nodes == nil || s.locations == nil {
		return nil, nil, ErrNodeUnavailable
	}
	if id, ok := s.locations.Lookup(streamID); ok {
		n, exists := s.nodes.Get(id)
		if !exists {
			return nil, nil, ErrNodeUnavailable
		}
		return n, s.clientFor(n), nil
	}
	nodes := s.nodes.ListActive()
	failed := 0
	for _, n := range nodes {
		client := s.clientFor(n)
		info, err := client.GetMediaInfo(ctx, "rtsp", "__defaultVhost__", "rtp", streamID)
		if err != nil {
			failed++
			continue
		}
		if info.Online {
			s.locations.Bind(streamID, n.ID)
			return n, client, nil
		}
	}
	if len(nodes) == 0 || failed == len(nodes) {
		return nil, nil, ErrNodeUnavailable
	}
	return nil, nil, ErrStreamOffline
}
