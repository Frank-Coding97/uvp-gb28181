package playback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/sdp"
)

var (
	ErrNodeUnavailable = errors.New("playback node unavailable")
	ErrRTPUnavailable  = errors.New("playback RTP unavailable")
	ErrMediaWait       = errors.New("playback media wait failed")
)

type NodeInfo struct {
	ID, DeviceID, ServerID string
	Destination, Transport string
	RecvIP                 string
	TCPMode                bool
}

type PickRequest struct {
	OwnerID, DeviceID, ChannelID, RecordKey string
}

type NodePicker interface {
	Pick(context.Context, PickRequest) (NodeInfo, error)
}

type RTPRequest struct {
	NodeID, StreamID, SSRC string
	Port                   int
	TCPMode                bool
}

type RTPAllocation struct {
	StreamID string
	SSRC     string
	Port     int
	Bind     func() error
	Close    func(context.Context) error
	Unbind   func() error
}

type RTPOpener interface {
	Open(context.Context, RTPRequest) (RTPAllocation, error)
}

type UACInvite struct {
	DeviceID, ChannelID, Destination, Transport, SSRC, SDP string
}

type DialogInfo struct {
	CallID string
}

type PlaybackInviter interface {
	Invite(context.Context, UACInvite) (DialogInfo, error)
	Teardown(context.Context, string) error
}

type MediaReady struct {
	URLs     map[string]string
	HasAudio bool
}

type MediaWaiter interface {
	Wait(context.Context, string) (MediaReady, error)
}

type ServiceConfig struct {
	ServerID string
}

type Service struct {
	registry *Registry
	picker   NodePicker
	rtp      RTPOpener
	inviter  PlaybackInviter
	media    MediaWaiter
	config   ServiceConfig
}

func NewService(registry *Registry, picker NodePicker, rtp RTPOpener, inviter PlaybackInviter, media MediaWaiter, config ServiceConfig) *Service {
	return &Service{registry: registry, picker: picker, rtp: rtp, inviter: inviter, media: media, config: config}
}

func randomPlaybackValue(prefix string) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return prefix + hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
}

type allocationResources struct {
	allocation RTPAllocation
	teardown   func(context.Context) error
}

func (r allocationResources) Teardown(ctx context.Context) error {
	if r.teardown == nil {
		return nil
	}
	return r.teardown(ctx)
}
func (r allocationResources) CloseRTP(ctx context.Context) error {
	if r.allocation.Close == nil {
		return nil
	}
	return r.allocation.Close(ctx)
}
func (r allocationResources) Unbind(context.Context) error {
	if r.allocation.Unbind == nil {
		return nil
	}
	return r.allocation.Unbind()
}

func (s *Service) fail(ctx context.Context, sessionID, stage, code string, cause error, resources CleanupResources) error {
	_ = s.registry.Update(sessionID, func(session *Session) error {
		session.ErrorStage, session.ErrorCode, session.Error = stage, code, cause
		return nil
	})
	cleanupErr := CleanupRunner{}.Run(ctx, resources)
	finalizeErr := s.registry.Finalize(sessionID, StateFailed, stage+": "+cause.Error())
	return errors.Join(cause, cleanupErr, finalizeErr)
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (CreateResult, error) {
	if s == nil || s.registry == nil || s.picker == nil || s.rtp == nil || s.inviter == nil || s.media == nil {
		return CreateResult{}, ErrRTPUnavailable
	}
	created, err := s.registry.Create(ctx, request)
	if err != nil {
		return CreateResult{}, err
	}
	if created.Existing {
		return CreateResult{Session: created.Session}, nil
	}
	session := created.Session
	node, err := s.picker.Pick(ctx, PickRequest{OwnerID: request.OwnerID, DeviceID: request.DeviceID, ChannelID: request.ChannelID, RecordKey: request.RecordKey})
	if err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "node", "unavailable", fmt.Errorf("%w: %v", ErrNodeUnavailable, err), nil)
	}
	if strings.TrimSpace(node.RecvIP) == "" {
		return CreateResult{}, s.fail(ctx, session.ID, "node", "invalid", fmt.Errorf("%w: receive IP missing", ErrNodeUnavailable), nil)
	}
	streamID, ssrc := randomPlaybackValue("pb-"), "1"+randomPlaybackValue("")[:9]
	allocation, err := s.rtp.Open(ctx, RTPRequest{NodeID: node.ID, StreamID: streamID, SSRC: ssrc, TCPMode: node.TCPMode})
	if err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "rtp", "unavailable", fmt.Errorf("%w: %v", ErrRTPUnavailable, err), nil)
	}
	if allocation.StreamID != "" {
		streamID = allocation.StreamID
	}
	if allocation.SSRC != "" {
		ssrc = allocation.SSRC
	}
	resources := allocationResources{allocation: allocation}
	if allocation.Bind != nil {
		if err := allocation.Bind(); err != nil {
			return CreateResult{}, s.fail(ctx, session.ID, "rtp", "bind_failed", err, resources)
		}
	}
	start, end := session.SegmentStart, session.SegmentEnd
	playFrom := session.PlayFrom
	if playFrom.IsZero() {
		playFrom = start
	}
	serverID := node.ServerID
	if serverID == "" {
		serverID = s.config.ServerID
	}
	body, err := sdp.BuildPlaybackSDP(sdp.PlaybackParams{ServerID: serverID, ChannelID: request.ChannelID, RecvIP: node.RecvIP,
		RecvPort: allocation.Port, SSRC: ssrc, TCPMode: node.TCPMode, Start: start, End: end, PlayFrom: playFrom})
	if err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "invite", "invalid_sdp", err, resources)
	}
	dialog, err := s.inviter.Invite(ctx, UACInvite{DeviceID: request.DeviceID, ChannelID: request.ChannelID, Destination: node.Destination,
		Transport: node.Transport, SSRC: ssrc, SDP: body})
	if err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "invite", "failed", err, resources)
	}
	resources.teardown = func(teardownCtx context.Context) error { return s.inviter.Teardown(teardownCtx, dialog.CallID) }
	if err := s.registry.Update(session.ID, func(value *Session) error {
		value.NodeID, value.StreamID, value.SSRC, value.CallID = node.ID, streamID, ssrc, dialog.CallID
		value.PlayFrom, value.State, value.Resources = playFrom, StateBuffering, resources
		return nil
	}); err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "invite", "session_update", err, resources)
	}
	ready, err := s.media.Wait(ctx, streamID)
	if err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "media_wait", "timeout", fmt.Errorf("%w: %v", ErrMediaWait, err), resources)
	}
	if err := s.registry.Update(session.ID, func(value *Session) error {
		value.MediaURLs, value.HasAudio, value.State = ready.URLs, ready.HasAudio, StatePlaying
		return nil
	}); err != nil {
		return CreateResult{}, err
	}
	result, ok := s.registry.Get(session.ID)
	if !ok {
		return CreateResult{}, ErrPlaybackNotFound
	}
	return CreateResult{Session: result}, nil
}

func (s *Service) Stop(ctx context.Context, sessionID, reason string) error {
	if s == nil || s.registry == nil {
		return ErrPlaybackNotFound
	}
	return s.registry.Stop(ctx, sessionID, reason)
}
