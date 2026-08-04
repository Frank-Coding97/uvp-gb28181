package playback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	OwnerID, DeviceID, ChannelID, SIPChannelID, RecordKey string
	StreamID, Destination, Transport                      string
	TCPMode                                               bool
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

// PlaybackActioner is implemented by the SIP dialog owner. Keeping it separate
// from PlaybackInviter lets the create path retain its narrow invite contract.
type PlaybackActioner interface {
	Action(context.Context, string, string, float64, float64, time.Duration) (float64, float64, error)
}

type MediaReady struct {
	URLs     map[string]string
	HasAudio bool
}

type MediaWaiter interface {
	Wait(context.Context, string) (MediaReady, error)
}

type ServiceConfig struct {
	ServerID  string
	Metrics   *Metrics
	MediaWait time.Duration
}

type Service struct {
	registry      *Registry
	picker        NodePicker
	rtp           RTPOpener
	inviter       PlaybackInviter
	media         MediaWaiter
	config        ServiceConfig
	sweeperOnce   sync.Once
	sweeperCancel context.CancelFunc
	sweeperWG     sync.WaitGroup
	lifecycle     context.Context
	lifecycleStop context.CancelFunc
}

func NewService(registry *Registry, picker NodePicker, rtp RTPOpener, inviter PlaybackInviter, media MediaWaiter, config ServiceConfig) *Service {
	lifecycle, cancel := context.WithCancel(context.Background())
	return &Service{registry: registry, picker: picker, rtp: rtp, inviter: inviter, media: media, config: config,
		lifecycle: lifecycle, lifecycleStop: cancel}
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
		if resources != nil {
			session.Resources = resources
		}
		return nil
	})
	started, finalizeErr := s.registry.FinalizeContextOnce(ctx, sessionID, StateFailed, stage+": "+cause.Error())
	if started && s.config.Metrics != nil {
		s.config.Metrics.Failed.Add(1)
		s.config.Metrics.Cleaned.Add(1)
	}
	return &ServiceError{Stage: stage, Code: code, Err: errors.Join(cause, finalizeErr)}
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (CreateResult, error) {
	if s == nil || s.registry == nil || s.picker == nil || s.rtp == nil || s.inviter == nil || s.media == nil {
		return CreateResult{}, ErrRTPUnavailable
	}
	operationCtx, operationCancel := context.WithCancel(ctx)
	stopLifecycleCancel := context.AfterFunc(s.lifecycle, operationCancel)
	defer func() {
		stopLifecycleCancel()
		operationCancel()
	}()
	ctx = operationCtx
	created, err := s.registry.Create(ctx, request)
	if err != nil {
		return CreateResult{}, err
	}
	if s.config.Metrics != nil && !created.Existing {
		s.config.Metrics.Created.Add(1)
	}
	if created.Existing {
		return CreateResult{Session: created.Session, Existing: true}, nil
	}
	session := created.Session
	streamID, ssrc := randomPlaybackValue("pb-"), "1"+randomPlaybackValue("")[:9]
	node, err := s.picker.Pick(ctx, PickRequest{OwnerID: request.OwnerID, DeviceID: request.DeviceID, ChannelID: request.ChannelID,
		SIPChannelID: request.SIPChannelID, RecordKey: request.RecordKey, StreamID: streamID,
		Destination: request.Destination, Transport: request.Transport, TCPMode: request.TCPMode})
	if err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "node", "unavailable", fmt.Errorf("%w: %w", ErrNodeUnavailable, err), nil)
	}
	if strings.TrimSpace(node.RecvIP) == "" {
		return CreateResult{}, s.fail(ctx, session.ID, "node", "invalid", fmt.Errorf("%w: receive IP missing", ErrNodeUnavailable), nil)
	}
	tcpMode := request.TCPMode || node.TCPMode
	allocation, err := s.rtp.Open(ctx, RTPRequest{NodeID: node.ID, StreamID: streamID, SSRC: ssrc, TCPMode: tcpMode})
	if err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "rtp", "unavailable", fmt.Errorf("%w: %w", ErrRTPUnavailable, err), nil)
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
	sipChannelID := strings.TrimSpace(request.SIPChannelID)
	if sipChannelID == "" {
		sipChannelID = request.ChannelID
	}
	body, err := sdp.BuildPlaybackSDP(sdp.PlaybackParams{ServerID: serverID, ChannelID: sipChannelID, RecvIP: node.RecvIP,
		RecvPort: allocation.Port, SSRC: ssrc, TCPMode: tcpMode, Start: start, End: end, PlayFrom: playFrom})
	if err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "invite", "invalid_sdp", err, resources)
	}
	destination := strings.TrimSpace(request.Destination)
	if destination == "" {
		destination = node.Destination
	}
	transport := strings.TrimSpace(request.Transport)
	if transport == "" {
		transport = node.Transport
	}
	dialog, err := s.inviter.Invite(ctx, UACInvite{DeviceID: request.DeviceID, ChannelID: sipChannelID, Destination: destination,
		Transport: transport, SSRC: ssrc, SDP: body})
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
	mediaCtx := ctx
	mediaCancel := func() {}
	if s.config.MediaWait > 0 {
		mediaCtx, mediaCancel = context.WithTimeout(ctx, s.config.MediaWait)
	}
	ready, err := s.media.Wait(mediaCtx, streamID)
	mediaCancel()
	if err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "media_wait", "timeout", fmt.Errorf("%w: %w", ErrMediaWait, err), resources)
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
	return CreateResult{Session: result, Existing: created.Existing}, nil
}

func (s *Service) Stop(ctx context.Context, sessionID, reason string) error {
	if s == nil || s.registry == nil {
		return ErrPlaybackNotFound
	}
	started, err := s.registry.StopOnce(ctx, sessionID, reason)
	if err == nil && started && s.config.Metrics != nil {
		s.config.Metrics.Cleaned.Add(1)
	}
	return err
}

func (s *Service) GetForOwner(sessionID, ownerID string) (*Session, bool) {
	if s == nil || s.registry == nil {
		return nil, false
	}
	session, ok := s.registry.GetForOwner(sessionID, ownerID)
	if ok && !session.State.IsTerminal() {
		_ = s.registry.Touch(sessionID, s.registry.now())
		session, ok = s.registry.GetForOwner(sessionID, ownerID)
	}
	return session, ok
}

func (s *Service) StopForOwner(ctx context.Context, sessionID, ownerID, reason string) error {
	if s == nil || s.registry == nil {
		return ErrPlaybackNotFound
	}
	started, err := s.registry.StopForOwnerOnce(ctx, sessionID, ownerID, reason)
	if err == nil && started && s.config.Metrics != nil {
		s.config.Metrics.Cleaned.Add(1)
	}
	return err
}

func (s *Service) Close(ctx context.Context) error {
	if s == nil || s.registry == nil {
		return nil
	}
	if s.lifecycleStop != nil {
		s.lifecycleStop()
	}
	if s.sweeperCancel != nil {
		s.sweeperCancel()
		s.sweeperWG.Wait()
	}
	cleaned, err := s.registry.CloseOnce(ctx)
	if cleaned > 0 && s.config.Metrics != nil {
		s.config.Metrics.Cleaned.Add(int64(cleaned))
	}
	return err
}

func (s *Service) StartSweeper(parent context.Context, interval time.Duration) {
	if s == nil || s.registry == nil || interval <= 0 {
		return
	}
	s.sweeperOnce.Do(func() {
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithCancel(s.lifecycle)
		stopParent := context.AfterFunc(parent, cancel)
		s.sweeperCancel = cancel
		s.sweeperWG.Add(1)
		go func() {
			defer s.sweeperWG.Done()
			defer stopParent()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case at := <-ticker.C:
					cleaned, _ := s.registry.SweepOnce(ctx, at)
					if cleaned > 0 && s.config.Metrics != nil {
						s.config.Metrics.Cleaned.Add(int64(cleaned))
					}
				}
			}
		}()
	})
}

func (s *Service) MetricsSnapshot() MetricsSnapshot {
	if s == nil || s.registry == nil {
		return MetricsSnapshot{}
	}
	if s.config.Metrics == nil {
		return MetricsSnapshot{Active: s.registry.ActiveCount()}
	}
	return s.config.Metrics.Snapshot(s.registry.ActiveCount())
}

func (s *Service) OnPlaybackFileToEnd(ctx context.Context, callID, deviceID string, _ []byte) error {
	if s == nil || s.registry == nil {
		return ErrPlaybackNotFound
	}
	session, ok := s.registry.GetByCallID(callID)
	if !ok || (deviceID != "" && session.DeviceID != deviceID) {
		return ErrPlaybackNotFound
	}
	if session.State.IsTerminal() {
		return nil
	}
	started, err := s.registry.FinalizeContextOnce(ctx, session.ID, StateEnded, "file-to-end")
	if err == nil && started && s.config.Metrics != nil {
		s.config.Metrics.Ended.Add(1)
		s.config.Metrics.Cleaned.Add(1)
	}
	return err
}

func (s *Service) OnPlaybackMediaEnded(ctx context.Context, callID, deviceID, reason string) error {
	if s == nil || s.registry == nil {
		return ErrPlaybackNotFound
	}
	session, ok := s.registry.GetByCallID(callID)
	if !ok || (deviceID != "" && session.DeviceID != deviceID) {
		return ErrPlaybackNotFound
	}
	if session.State.IsTerminal() {
		return nil
	}
	started, err := s.registry.FinalizeContextOnce(ctx, session.ID, StateEnded, reason)
	if err == nil && started && s.config.Metrics != nil {
		s.config.Metrics.Ended.Add(1)
		s.config.Metrics.Cleaned.Add(1)
	}
	return err
}

func (s *Service) OnPlaybackStreamEnded(ctx context.Context, streamID, reason string) error {
	if s == nil || s.registry == nil {
		return ErrPlaybackNotFound
	}
	session, ok := s.registry.GetByStreamID(streamID)
	if !ok {
		return ErrPlaybackNotFound
	}
	if session.State.IsTerminal() {
		return nil
	}
	started, err := s.registry.FinalizeContextOnce(ctx, session.ID, StateEnded, reason)
	if err == nil && started && s.config.Metrics != nil {
		s.config.Metrics.Ended.Add(1)
		s.config.Metrics.Cleaned.Add(1)
	}
	return err
}

func (s *Service) Action(ctx context.Context, sessionID, ownerID string, request ActionRequest) (*Session, error) {
	if s == nil || s.registry == nil {
		return nil, ErrPlaybackNotFound
	}
	session, ok := s.registry.GetForOwner(sessionID, ownerID)
	if !ok || session.State.IsTerminal() {
		return nil, ErrPlaybackNotFound
	}
	duration := session.SegmentEnd.Sub(session.SegmentStart)
	if duration <= 0 {
		return nil, ErrInvalidSession
	}
	if request.Action == "seek" && (request.PositionSeconds < 0 || request.PositionSeconds >= duration.Seconds()) {
		return nil, ErrInvalidSession
	}
	if request.Action == "scale" && !validScale(request.Scale) {
		return nil, ErrInvalidSession
	}
	if request.Action != "pause" && request.Action != "resume" && request.Action != "seek" && request.Action != "scale" {
		return nil, ErrInvalidSession
	}
	actioner, ok := s.inviter.(PlaybackActioner)
	if !ok {
		return nil, ErrRTPUnavailable
	}
	position, scale, err := actioner.Action(ctx, session.CallID, request.Action, request.PositionSeconds, request.Scale, duration)
	if err != nil {
		return nil, err
	}
	if s.config.Metrics != nil {
		s.config.Metrics.Actions.Add(1)
	}
	if err := s.registry.Update(sessionID, func(value *Session) error {
		switch request.Action {
		case "pause":
			if value.State != StatePlaying {
				return ErrInvalidTransition
			}
			value.State = StatePaused
		case "resume":
			if value.State != StatePaused {
				return ErrInvalidTransition
			}
			value.State = StatePlaying
		case "seek":
			value.PositionSeconds = position
		case "scale":
			value.Scale = scale
		default:
			return ErrInvalidSession
		}
		return nil
	}); err != nil {
		return nil, err
	}
	_ = s.registry.Touch(sessionID, s.registry.now())
	updated, ok := s.registry.GetForOwner(sessionID, ownerID)
	if !ok {
		return nil, ErrPlaybackNotFound
	}
	return updated, nil
}

func validScale(scale float64) bool {
	return scale == 0.25 || scale == 0.5 || scale == 1 || scale == 2 || scale == 4
}
