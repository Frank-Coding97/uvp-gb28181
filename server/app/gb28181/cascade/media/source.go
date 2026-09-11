// Package media contains the cascade-specific source lease boundary.
// It deliberately does not own the browser playback lifecycle.
package media

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
)

var (
	ErrInvalidAcquire = errors.New("invalid cascade source acquire request")
	ErrProviderClosed = errors.New("cascade source provider closed")
)

type AcquireRequest struct {
	ConsumerKey string
	DeviceID    string
	ChannelID   string
}

type StartResult struct {
	StreamID string
	SSRC     string
	App      string
	NodeID   int64
	Reused   bool
}

type SourceStarter interface {
	Start(context.Context, string, string) (StartResult, error)
}

// PlayServiceAdapter bridges the established device-side playback service.
// The adapter exposes no new public playback API and never writes GbChannel.StreamID itself.
type PlayServiceAdapter struct{ service *play.Service }

func NewPlayServiceAdapter(service *play.Service) *PlayServiceAdapter {
	return &PlayServiceAdapter{service: service}
}

func (a *PlayServiceAdapter) Start(ctx context.Context, deviceID, channelID string) (StartResult, error) {
	if a == nil || a.service == nil {
		return StartResult{}, ErrProviderClosed
	}
	result, err := a.service.Start(ctx, deviceID, channelID)
	if err != nil {
		return StartResult{}, err
	}
	if result == nil || result.StreamID == "" {
		return StartResult{}, errors.New("play service returned an empty source handle")
	}
	var nodeID int64
	if result.Node != nil {
		nodeID = result.Node.ID
	}
	return StartResult{StreamID: result.StreamID, SSRC: result.SSRC, App: result.App, NodeID: nodeID, Reused: result.Reused}, nil
}

// Lease is an idempotent consumer hold on one source stream.
type Lease struct {
	Source    StartResult
	Consumer  string
	provider  *Provider
	streamKey string
	once      sync.Once
	err       error
}

func (l *Lease) Release(ctx context.Context) error {
	if l == nil || l.provider == nil {
		return nil
	}
	l.once.Do(func() { l.err = l.provider.release(ctx, l.streamKey, l.Consumer) })
	return l.err
}

type sourceEntry struct {
	result    StartResult
	consumers map[string]struct{}
	owned     bool
	// starting 期间 result/consumers 未就绪;done 在启动完成后关闭
	starting bool
	done     chan struct{}
}

// Provider deduplicates upstream source acquisition and tracks only cascade leases.
// It intentionally has no stop callback: the existing ZLM none-reader policy owns
// the final source cleanup and can observe browser readers.
type Provider struct {
	mu      sync.Mutex
	starter SourceStarter
	closed  bool
	sources map[string]*sourceEntry
}

func NewProvider(starter SourceStarter) *Provider {
	return &Provider{starter: starter, sources: make(map[string]*sourceEntry)}
}

func sourceKey(deviceID, channelID string) string { return deviceID + "\x00" + channelID }

func (p *Provider) Acquire(ctx context.Context, request AcquireRequest) (*Lease, error) {
	if p == nil || p.starter == nil || request.ConsumerKey == "" || request.DeviceID == "" || request.ChannelID == "" {
		return nil, ErrInvalidAcquire
	}
	key := sourceKey(request.DeviceID, request.ChannelID)

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrProviderClosed
	}
	if entry := p.sources[key]; entry != nil {
		if entry.starting {
			// 同一源正在锁外启动:等待完成,避免重复 Start
			done := entry.done
			p.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
			}
			return p.acquireExisting(ctx, key, request)
		}
		if _, exists := entry.consumers[request.ConsumerKey]; exists {
			p.mu.Unlock()
			return nil, fmt.Errorf("consumer lease already exists: %s", request.ConsumerKey)
		}
		entry.consumers[request.ConsumerKey] = struct{}{}
		p.mu.Unlock()
		return &Lease{Source: entry.result, Consumer: request.ConsumerKey, provider: p, streamKey: key}, nil
	}
	// 占位防重复启动:Start 在锁外执行,阻塞的通道启动不再拖垮所有 Acquire/Release
	entry := &sourceEntry{starting: true, done: make(chan struct{})}
	p.sources[key] = entry
	p.mu.Unlock()

	result, err := p.starter.Start(ctx, request.DeviceID, request.ChannelID)

	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil || p.closed {
		delete(p.sources, key)
		close(entry.done)
		if err != nil {
			return nil, err
		}
		return nil, ErrProviderClosed
	}
	entry.result = result
	entry.starting = false
	entry.consumers = map[string]struct{}{request.ConsumerKey: {}}
	entry.owned = !result.Reused
	close(entry.done)
	return &Lease{Source: result, Consumer: request.ConsumerKey, provider: p, streamKey: key}, nil
}

// acquireExisting 复用已有就绪 entry(锁内路径,由 Acquire 的等待分支调用)
func (p *Provider) acquireExisting(ctx context.Context, key string, request AcquireRequest) (*Lease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, ErrProviderClosed
	}
	entry := p.sources[key]
	if entry == nil || entry.starting {
		// 启动失败或仍占位:视为竞争失败,让上层重试
		return nil, fmt.Errorf("source became unavailable: %s", key)
	}
	if _, exists := entry.consumers[request.ConsumerKey]; exists {
		return nil, fmt.Errorf("consumer lease already exists: %s", request.ConsumerKey)
	}
	entry.consumers[request.ConsumerKey] = struct{}{}
	return &Lease{Source: entry.result, Consumer: request.ConsumerKey, provider: p, streamKey: key}, nil
}

func (p *Provider) release(_ context.Context, key, consumer string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.sources[key]
	if entry == nil {
		return nil
	}
	delete(entry.consumers, consumer)
	if len(entry.consumers) == 0 {
		delete(p.sources, key)
	}
	return nil
}

func (p *Provider) HasLease(streamID string) bool {
	if p == nil || streamID == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range p.sources {
		if entry.result.StreamID == streamID && len(entry.consumers) > 0 {
			return true
		}
	}
	return false
}

func (p *Provider) LeaseCount(streamID string) int {
	if p == nil || streamID == "" {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range p.sources {
		if entry.result.StreamID == streamID {
			return len(entry.consumers)
		}
	}
	return 0
}

func (p *Provider) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.closed = true
	p.sources = make(map[string]*sourceEntry)
	p.mu.Unlock()
}

// NoneReaderPolicy keeps a source alive while any cascade lease remains, then
// delegates to the existing channel/on-demand policy for browser-aware cleanup.
type NoneReaderPolicy struct {
	Leases   interface{ HasLease(string) bool }
	Delegate interface {
		ShouldCloseOnNoneReader(context.Context, string) (bool, error)
	}
}

func (p NoneReaderPolicy) ShouldCloseOnNoneReader(ctx context.Context, streamID string) (bool, error) {
	if p.Leases != nil && p.Leases.HasLease(streamID) {
		return false, nil
	}
	if p.Delegate == nil {
		return true, nil
	}
	return p.Delegate.ShouldCloseOnNoneReader(ctx, streamID)
}
