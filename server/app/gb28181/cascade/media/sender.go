package media

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

const defaultSenderVHost = "__defaultVhost__"

var (
	ErrInvalidSenderRequest  = errors.New("invalid cascade sender request")
	ErrSenderNodeUnavailable = errors.New("cascade sender node unavailable")
	ErrSenderStartFailed     = errors.New("cascade sender start failed")
	ErrSenderStopFailed      = errors.New("cascade sender stop failed")
	ErrSenderCheckFailed     = errors.New("cascade sender status check failed")
)

type SenderNodeLookup interface {
	Get(int64) (*node.Node, bool)
}

type GBSenderClient interface {
	StartGBSendRTP(context.Context, zlm.GBSendRTPRequest) (*zlm.GBSendRTPResult, error)
	StopSendRtp(context.Context, string, string, string, string) error
}

type SenderClientFactory func(*node.Node) GBSenderClient

type SenderRequest struct {
	Source StartResult
	SSRC   string
	// RTPInfoURL is the local media resource URL advertised in MANSRTSP.
	// RemoteIP/RemotePort remain the upstream RTP receiver address.
	RTPInfoURL    string
	PayloadType   int
	RemoteIP      string
	RemotePort    int
	Transport     zlm.GBSendRTPTransport
	RTCPKeepalive bool
}

// Sender starts a PS-over-RTP sender on the same ZLM node as the acquired
// source stream. It does not own the source lease lifecycle.
type Sender struct {
	nodes     SenderNodeLookup
	clientFor SenderClientFactory
}

func NewSender(nodes SenderNodeLookup, clientFor SenderClientFactory) *Sender {
	if clientFor == nil {
		clientFor = func(value *node.Node) GBSenderClient { return zlm.NewClientForNode(value) }
	}
	return &Sender{nodes: nodes, clientFor: clientFor}
}

type SenderLease struct {
	LocalPort  int
	RTPInfo    *zlm.GBRTPInfo
	client     GBSenderClient
	request    zlm.GBSendRTPRequest
	nodeSecret string
	rtpInfoMu  sync.Mutex
	once       sync.Once
	err        error
}

type gbRTPInfoReader interface {
	GetGBSendRTPInfo(context.Context, zlm.GBSendRTPRequest) (*zlm.GBRTPInfo, error)
}

type gbRTPInfoResetter interface {
	ResetGBSendRTPInfo(context.Context, zlm.GBSendRTPRequest) error
}

// CanSampleRTPInfo reports whether the backing media node can both reset and
// read a sender-scoped first-packet sample. Legacy nodes should not add a wait
// to playback control when they cannot provide this optional metadata.
func (l *SenderLease) CanSampleRTPInfo() bool {
	if l == nil || l.client == nil {
		return false
	}
	_, canRead := l.client.(gbRTPInfoReader)
	_, canReset := l.client.(gbRTPInfoResetter)
	return canRead && canReset
}

// RefreshRTPInfo obtains the first packet sample for this sender when the
// backing ZLM implementation exposes it. Older nodes simply leave the cached
// value unchanged and callers omit the optional RTP-Info header.
func (l *SenderLease) RefreshRTPInfo(ctx context.Context) (*zlm.GBRTPInfo, error) {
	if l == nil || l.client == nil {
		return nil, nil
	}
	l.rtpInfoMu.Lock()
	defer l.rtpInfoMu.Unlock()
	reader, ok := l.client.(gbRTPInfoReader)
	if ok {
		info, err := reader.GetGBSendRTPInfo(ctx, l.request)
		if err != nil {
			return nil, fmt.Errorf("refresh sender RTP info: %s", redactNodeSecret(err.Error(), l.nodeSecret))
		}
		if info != nil {
			l.RTPInfo = info
		}
	}
	return cloneRTPInfo(l.RTPInfo), nil
}

// ResetRTPInfo clears the cached sample and asks an enhanced ZLM sender to
// capture the next packet. The endpoint is optional so a legacy node remains
// usable; the caller may then omit RTP-Info until a fresh value is available.
func (l *SenderLease) ResetRTPInfo(ctx context.Context) error {
	if l == nil || l.client == nil {
		return nil
	}
	var err error
	if resetter, ok := l.client.(gbRTPInfoResetter); ok {
		err = resetter.ResetGBSendRTPInfo(ctx, l.request)
	}
	l.rtpInfoMu.Lock()
	l.RTPInfo = nil
	l.rtpInfoMu.Unlock()
	if err != nil {
		return fmt.Errorf("reset sender RTP info: %s", redactNodeSecret(err.Error(), l.nodeSecret))
	}
	return nil
}

func cloneRTPInfo(info *zlm.GBRTPInfo) *zlm.GBRTPInfo {
	if info == nil {
		return nil
	}
	copy := *info
	return &copy
}

// IsActive checks the leased SSRC without changing other senders or the source.
func (l *SenderLease) IsActive(ctx context.Context) (bool, error) {
	if l == nil || l.client == nil {
		return false, nil
	}
	checker, ok := l.client.(interface {
		IsGBSendRTPActive(context.Context, zlm.GBSendRTPRequest) (bool, error)
	})
	if !ok {
		return false, fmt.Errorf("%w: client does not support status checks", ErrSenderCheckFailed)
	}
	active, err := checker.IsGBSendRTPActive(ctx, l.request)
	if err != nil {
		return false, fmt.Errorf("%w: %s", ErrSenderCheckFailed, redactNodeSecret(err.Error(), l.nodeSecret))
	}
	return active, nil
}

func (l *SenderLease) Stop(ctx context.Context) error {
	if l == nil || l.client == nil {
		return nil
	}
	l.once.Do(func() {
		if err := l.client.StopSendRtp(ctx, l.request.VHost, l.request.App, l.request.Stream, l.request.SSRC); err != nil {
			l.err = fmt.Errorf("%w: %v", ErrSenderStopFailed, err)
		}
	})
	return l.err
}

func (s *Sender) Start(ctx context.Context, request SenderRequest) (*SenderLease, error) {
	if err := validateSenderRequest(request); err != nil {
		return nil, err
	}
	if s == nil || s.nodes == nil || s.clientFor == nil {
		return nil, ErrSenderNodeUnavailable
	}
	mediaNode, ok := s.nodes.Get(request.Source.NodeID)
	if !ok || mediaNode == nil {
		return nil, fmt.Errorf("%w: node %d not found", ErrSenderNodeUnavailable, request.Source.NodeID)
	}
	client := s.clientFor(mediaNode)
	if client == nil {
		return nil, fmt.Errorf("%w: node %d client unavailable", ErrSenderNodeUnavailable, request.Source.NodeID)
	}
	params := zlm.GBSendRTPRequest{
		VHost: defaultSenderVHost, App: request.Source.App, Stream: request.Source.StreamID, SSRC: request.SSRC,
		RTPInfoURL:  request.RTPInfoURL,
		PayloadType: request.PayloadType, RemoteIP: request.RemoteIP, RemotePort: request.RemotePort, Transport: request.Transport,
		RTCPKeepalive: request.RTCPKeepalive,
	}
	result, err := client.StartGBSendRTP(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSenderStartFailed, redactNodeSecret(err.Error(), mediaNode.APISecret))
	}
	if result == nil || result.LocalPort <= 0 || result.LocalPort > 65535 {
		return nil, fmt.Errorf("%w: invalid local port", ErrSenderStartFailed)
	}
	return &SenderLease{LocalPort: result.LocalPort, RTPInfo: result.RTPInfo, client: client, request: params, nodeSecret: mediaNode.APISecret}, nil
}

func validateSenderRequest(request SenderRequest) error {
	if strings.TrimSpace(request.Source.StreamID) == "" || strings.TrimSpace(request.Source.App) == "" || request.Source.NodeID <= 0 || strings.TrimSpace(request.SSRC) == "" {
		return ErrInvalidSenderRequest
	}
	if request.PayloadType < 0 || request.PayloadType > 127 {
		return ErrInvalidSenderRequest
	}
	if request.Transport != zlm.GBSendRTPUDP && request.Transport != zlm.GBSendRTPTCPActive && request.Transport != zlm.GBSendRTPTCPPassive {
		return ErrInvalidSenderRequest
	}
	if request.Transport == zlm.GBSendRTPTCPPassive {
		return nil
	}
	ip := net.ParseIP(strings.TrimSpace(request.RemoteIP))
	if ip == nil || ip.IsUnspecified() || request.RemotePort <= 0 || request.RemotePort > 65535 {
		return ErrInvalidSenderRequest
	}
	if request.Transport == zlm.GBSendRTPUDP && request.RTCPKeepalive && request.RemotePort == 65535 {
		return ErrInvalidSenderRequest
	}
	return nil
}

func redactNodeSecret(message, secret string) string {
	if secret == "" {
		return message
	}
	return strings.ReplaceAll(message, secret, "***")
}
