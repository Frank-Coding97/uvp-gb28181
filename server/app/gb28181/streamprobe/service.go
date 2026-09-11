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

const ProbeDurationMS = 3000

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

type Service struct {
	nodes     ProbeNodeRegistry
	locations ProbeLocations
	clientFor func(*node.Node) ProbeClient
	timeout   time.Duration
	now       func() time.Time
	mu        sync.Mutex
	running   map[string]*probeCall
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
	return &Service{nodes: nodes, locations: locations, clientFor: clientFor, timeout: timeout, now: now, running: make(map[string]*probeCall)}
}

func (s *Service) Run(ctx context.Context, streamID string) (*ProbeSnapshot, error) {
	s.mu.Lock()
	call := s.running[streamID]
	if call == nil {
		call = &probeCall{done: make(chan struct{})}
		s.running[streamID] = call
		go s.execute(streamID, call)
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-call.done:
		return call.result, call.err
	}
}

func (s *Service) execute(streamID string, call *probeCall) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	mediaNode, client, err := s.locate(ctx, streamID)
	if err == nil {
		var frames []zlm.ProbeFrame
		frames, err = client.AddProbe(ctx, "__defaultVhost__", "rtp", streamID, ProbeDurationMS)
		if err != nil {
			err = fmt.Errorf("%w: %w", ErrProbeFailed, err)
		}
		if err == nil {
			call.result = &ProbeSnapshot{NodeID: mediaNode.ID, NodeName: mediaNode.Name, CompletedAt: s.now().UTC(), Result: Analyze(frames)}
		}
	}
	call.err = err
	s.mu.Lock()
	delete(s.running, streamID)
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
