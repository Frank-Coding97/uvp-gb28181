// Package media contains the cascade-specific source lease boundary.
// It deliberately does not own the browser playback lifecycle.
package media

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"uvplatform.com/uvp-gb28181/app/gb28181/play"
)

var (
	ErrInvalidAcquire = errors.New("invalid cascade source acquire request")
	ErrProviderClosed = errors.New("cascade source provider closed")
)

type AcquireRequest struct {
	ConsumerKey string
	DeviceID    string
	ChannelID   string
	DeviceEpoch int64
}

type StartResult struct {
	StreamID string
	SSRC     string
	App      string
	NodeID   int64
	Reused   bool
}

type SourceStarter interface {
	Start(context.Context, AcquireRequest) (StartResult, error)
}

type liveEnsurer interface {
	EnsureLive(context.Context, play.Request) (*play.Result, error)
}

// PlayServiceAdapter bridges the established device-side playback service.
// The adapter exposes no new public playback API and never writes GbChannel.StreamID itself.
type PlayServiceAdapter struct{ service liveEnsurer }

func NewPlayServiceAdapter(service liveEnsurer) *PlayServiceAdapter {
	return &PlayServiceAdapter{service: service}
}

func (a *PlayServiceAdapter) Start(ctx context.Context, request AcquireRequest) (StartResult, error) {
	if a == nil || a.service == nil {
		return StartResult{}, ErrProviderClosed
	}
	if request.DeviceEpoch <= 0 {
		return StartResult{}, play.ErrPlayAuthorizationUnavailable
	}
	result, err := a.service.EnsureLive(ctx, play.Request{
		DeviceID: request.DeviceID, ChannelID: request.ChannelID,
		DeviceEpoch: request.DeviceEpoch, Trigger: "cascade",
	})
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
}

// Provider tracks only cascade leases. Every acquire passes through the playback
// service, which validates the caller's epoch and deduplicates source acquisition.
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
	if p == nil || p.starter == nil || request.ConsumerKey == "" || request.DeviceID == "" || request.ChannelID == "" || request.DeviceEpoch <= 0 {
		return nil, ErrInvalidAcquire
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrProviderClosed
	}
	p.mu.Unlock()

	result, err := p.starter.Start(ctx, request)
	if err != nil {
		return nil, err
	}
	key := sourceKey(request.DeviceID, request.ChannelID) + "\x00" + result.StreamID
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, ErrProviderClosed
	}
	entry := p.sources[key]
	if entry == nil {
		entry = &sourceEntry{result: result, consumers: make(map[string]struct{})}
		p.sources[key] = entry
	}
	if _, exists := entry.consumers[request.ConsumerKey]; exists {
		return nil, fmt.Errorf("consumer lease already exists: %s", request.ConsumerKey)
	}
	entry.consumers[request.ConsumerKey] = struct{}{}
	return &Lease{Source: result, Consumer: request.ConsumerKey, provider: p, streamKey: key}, nil
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
