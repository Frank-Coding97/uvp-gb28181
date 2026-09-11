package media

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const defaultSenderVHost = "__defaultVhost__"

var (
	ErrInvalidSenderRequest  = errors.New("invalid cascade sender request")
	ErrSenderNodeUnavailable = errors.New("cascade sender node unavailable")
	ErrSenderStartFailed     = errors.New("cascade sender start failed")
	ErrSenderStopFailed      = errors.New("cascade sender stop failed")
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
	Source      StartResult
	SSRC        string
	PayloadType int
	RemoteIP    string
	RemotePort  int
	Transport   zlm.GBSendRTPTransport
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
	LocalPort int
	client    GBSenderClient
	request   zlm.GBSendRTPRequest
	once      sync.Once
	err       error
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
		PayloadType: request.PayloadType, RemoteIP: request.RemoteIP, RemotePort: request.RemotePort, Transport: request.Transport,
	}
	result, err := client.StartGBSendRTP(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSenderStartFailed, redactNodeSecret(err.Error(), mediaNode.APISecret))
	}
	if result == nil || result.LocalPort <= 0 || result.LocalPort > 65535 {
		return nil, fmt.Errorf("%w: invalid local port", ErrSenderStartFailed)
	}
	return &SenderLease{LocalPort: result.LocalPort, client: client, request: params}, nil
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
	return nil
}

func redactNodeSecret(message, secret string) string {
	if secret == "" {
		return message
	}
	return strings.ReplaceAll(message, secret, "***")
}
