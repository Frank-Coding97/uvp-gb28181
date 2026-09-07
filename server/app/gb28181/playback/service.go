package playback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playurl"
	"uvplatform.cn/uvp-gb28181/app/gb28181/sdp"
)

var (
	ErrNodeUnavailable = errors.New("playback node unavailable")
	ErrRTPUnavailable  = errors.New("playback RTP unavailable")
	ErrMediaWait       = errors.New("playback media wait failed")
)

type NodeInfo struct {
	ID, DeviceID, ServerID string
	NodeUUID               string
	NodeRevision           uint64
	Destination, Transport string
	RecvIP                 string
	TCPMode                bool
}

type PickRequest struct {
	OwnerID, DeviceID, ChannelID, SIPChannelID, RecordKey string
	StreamID, Destination, Transport                      string
	PreferredNodeID                                       int64
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
	// On error, only this explicit marker identifies a retained cleanup-only
	// dialog. A generated CallID alone does not prove such a resource exists.
	CleanupRequired bool
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
	// The application shares this barrier. Without Intents this is only the
	// original-epoch preflight; Intents additionally requires both child factories.
	DeviceOperations *playauth.DeviceOperationBarrier
	Intents          *playauth.DeviceOperationIntentStore
	ServerID         string
	Metrics          *Metrics
	MediaWait        time.Duration
}

type Service struct {
	registry       *Registry
	picker         NodePicker
	rtp            RTPOpener
	inviter        PlaybackInviter
	media          MediaWaiter
	config         ServiceConfig
	producerMu     sync.Mutex
	closing        bool
	producers      int
	producersDone  chan struct{}
	sweeperStarted bool
	lifecycle      context.Context
	lifecycleStop  context.CancelFunc
	mediaReady     func(Session)
}

func NewService(registry *Registry, picker NodePicker, rtp RTPOpener, inviter PlaybackInviter, media MediaWaiter, config ServiceConfig) *Service {
	lifecycle, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	close(idle)
	return &Service{registry: registry, picker: picker, rtp: rtp, inviter: inviter, media: media, config: config,
		lifecycle: lifecycle, lifecycleStop: cancel, producersDone: idle}
}

// Admission and the last-producer signal share a lock. A closing service cannot
// miss a partially initialized Create/Action or a concurrently started sweeper.
func (s *Service) addProducerLocked() {
	if s.producers == 0 {
		s.producersDone = make(chan struct{})
	}
	s.producers++
}

func (s *Service) finishProducer() {
	s.producerMu.Lock()
	defer s.producerMu.Unlock()
	s.producers--
	if s.producers == 0 {
		close(s.producersDone)
	}
}

func (s *Service) beginOperation(ctx context.Context) (context.Context, func(), error) {
	s.producerMu.Lock()
	if s.closing {
		s.producerMu.Unlock()
		return nil, nil, ErrRegistryClosed
	}
	s.addProducerLocked()
	s.producerMu.Unlock()
	operationCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.lifecycle, cancel)
	return operationCtx, func() {
		stop()
		cancel()
		s.finishProducer()
	}, nil
}

// SetMediaReadyObserver reports a successfully established playback stream.
// The observer is optional and accounting failures must stay outside playback.
func (s *Service) SetMediaReadyObserver(observer func(Session)) {
	if s != nil {
		s.mediaReady = observer
	}
}

func randomPlaybackValue(prefix string) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return prefix + hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
}

func randomPlaybackSSRC() string {
	value, err := rand.Int(rand.Reader, big.NewInt(1_000_000_000))
	if err == nil {
		return fmt.Sprintf("1%09d", value.Int64())
	}
	fallback := time.Now().UnixNano() % 1_000_000_000
	if fallback < 0 {
		fallback = -fallback
	}
	return fmt.Sprintf("1%09d", fallback)
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
		_, managed := session.Resources.(*playbackIntentOwner)
		if resources != nil && !managed {
			session.Resources = resources
		}
		return nil
	})
	if session, ok := s.registry.Get(sessionID); ok {
		if owner, managed := session.Resources.(*playbackIntentOwner); managed && owner.initializing() {
			// Create's deferred join publishes all children before finalization;
			// waiting here would make initialization wait on its own ready signal.
			return &ServiceError{Stage: stage, Code: code, Err: cause}
		}
	}
	terminal := StateFailed
	// Close cancels in-flight work before closing the registry. Both racing
	// paths must classify that cancellation as a stop, not a business failure.
	if s.lifecycle != nil && s.lifecycle.Err() != nil && errors.Is(cause, context.Canceled) {
		terminal = StateStopped
	}
	started, finalizeErr := s.registry.FinalizeContextOnce(ctx, sessionID, terminal, stage+": "+cause.Error())
	if started && s.config.Metrics != nil {
		if terminal == StateFailed {
			s.config.Metrics.Failed.Add(1)
		}
		s.config.Metrics.Cleaned.Add(1)
	}
	return &ServiceError{Stage: stage, Code: code, Err: errors.Join(cause, finalizeErr)}
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (out CreateResult, createErr error) {
	if s == nil || s.registry == nil || s.picker == nil || s.rtp == nil || s.inviter == nil || s.media == nil {
		return CreateResult{}, ErrRTPUnavailable
	}
	if !gbconfig.IsSupportedPlaybackProtocol(request.DefaultProtocol) {
		request.DefaultProtocol = gbconfig.DefaultPlaybackProtocol
	}
	operationCtx, finish, err := s.beginOperation(ctx)
	if err != nil {
		return CreateResult{}, err
	}
	defer finish()
	ctx = operationCtx
	// Older component-only callers have no authorization snapshot. A real
	// controller request or configured runtime must never fall back to that path.
	if request.Authorization != (AuthorizationSnapshot{}) || s.config.DeviceOperations != nil {
		a := request.Authorization
		if s.config.DeviceOperations == nil || a.DevicePK <= 0 || a.ChannelPK <= 0 || a.DeviceEpoch <= 0 ||
			a.CleanupCompletedEpoch != a.DeviceEpoch || a.DeviceCode != request.DeviceID || a.ChannelCode != request.SIPChannelID ||
			len(a.ChannelCode) != 20 || strings.Trim(a.ChannelCode, "0123456789") != "" ||
			strconv.FormatInt(a.ChannelPK, 10) != request.ChannelID {
			return CreateResult{}, playauth.ErrDeviceOperationUnavailable
		}
		if err := s.config.DeviceOperations.AuthorizeEpoch(ctx, a.DeviceCode, a.DeviceEpoch); err != nil {
			return CreateResult{}, err
		}
	}
	var owner *playbackIntentOwner
	if s.config.Intents != nil {
		if _, ok := s.rtp.(IntentRTPFactory); !ok {
			return CreateResult{}, ErrRTPUnavailable
		}
		if _, ok := s.inviter.(IntentSIPFactory); !ok {
			return CreateResult{}, ErrRTPUnavailable
		}
		owner, err = newPlaybackIntentOwner(s.lifecycle, s.config.Intents, s.config.DeviceOperations, request)
		if err != nil {
			return CreateResult{}, err
		}
		request.Resources = owner
	}
	created, err := s.registry.Create(ctx, request)
	if err != nil {
		if owner != nil {
			owner.cancel()
			owner.finishInitialization()
		}
		return CreateResult{}, err
	}
	if s.config.Metrics != nil && !created.Existing {
		s.config.Metrics.Created.Add(1)
	}
	if created.Existing {
		if owner != nil {
			owner.cancel()
			owner.finishInitialization()
		}
		return CreateResult{Session: created.Session, Existing: true}, nil
	}
	session := created.Session
	if owner != nil {
		defer func() {
			owner.finishInitialization()
			if createErr != nil {
				owner.cancel()
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				terminal := StateFailed
				if s.lifecycle.Err() != nil && errors.Is(createErr, context.Canceled) {
					terminal = StateStopped
				}
				started, cleanupErr := s.registry.FinalizeContextOnce(cleanupCtx, session.ID, terminal, "initialization failed")
				if started && s.config.Metrics != nil {
					if terminal == StateFailed {
						s.config.Metrics.Failed.Add(1)
					}
					s.config.Metrics.Cleaned.Add(1)
				}
				createErr = errors.Join(createErr, cleanupErr)
			}
		}()
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		stopOwner := context.AfterFunc(owner.ctx, cancel)
		stopRequest := context.AfterFunc(ctx, owner.cancel)
		defer func() { stopRequest(); stopOwner(); cancel() }()
		if err := owner.begin(ctx); err != nil {
			return CreateResult{}, s.fail(ctx, session.ID, "intent", "unavailable", err, owner)
		}
		go func() {
			<-owner.ctx.Done()
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.registry.StopOnce(cleanupCtx, session.ID, "operation cancelled")
		}()
	}
	streamID, ssrc := randomPlaybackValue("pb-"), randomPlaybackSSRC()
	node, err := s.picker.Pick(ctx, PickRequest{OwnerID: request.OwnerID, DeviceID: request.DeviceID, ChannelID: request.ChannelID,
		SIPChannelID: request.SIPChannelID, RecordKey: request.RecordKey, StreamID: streamID,
		Destination: request.Destination, Transport: request.Transport, PreferredNodeID: request.PreferredNodeID, TCPMode: request.TCPMode})
	if err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "node", "unavailable", fmt.Errorf("%w: %w", ErrNodeUnavailable, err), nil)
	}
	if strings.TrimSpace(node.RecvIP) == "" {
		return CreateResult{}, s.fail(ctx, session.ID, "node", "invalid", fmt.Errorf("%w: receive IP missing", ErrNodeUnavailable), nil)
	}
	if err := ctx.Err(); err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "node", "cancelled", err, nil)
	}
	tcpMode := request.TCPMode || node.TCPMode
	rtpRequest := RTPRequest{NodeID: node.ID, StreamID: streamID, SSRC: ssrc, TCPMode: tcpMode}
	var allocation RTPAllocation
	if owner == nil {
		allocation, err = s.rtp.Open(ctx, rtpRequest)
	} else {
		owner.rtp, err = s.rtp.(IntentRTPFactory).PrepareIntent(ctx, owner.store, owner.id, owner.version, node, rtpRequest)
		if err == nil && owner.rtp == nil {
			err = ErrRTPUnavailable
		}
		if err == nil {
			allocation, err = owner.rtp.Open(ctx)
		}
	}
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
	if err := ctx.Err(); err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "rtp", "cancelled", err, resources)
	}
	if allocation.Bind != nil {
		if err := allocation.Bind(); err != nil {
			return CreateResult{}, s.fail(ctx, session.ID, "rtp", "bind_failed", err, resources)
		}
	}
	if err := ctx.Err(); err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "rtp", "cancelled", err, resources)
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
	var body string
	if session.Mode == ModeDownload {
		body, err = sdp.BuildDownloadSDP(sdp.DownloadParams{ServerID: serverID, ChannelID: sipChannelID, RecvIP: node.RecvIP,
			RecvPort: allocation.Port, SSRC: ssrc, TCPMode: tcpMode, Start: start, End: end,
			DownloadSpeed: session.DownloadSpeed, Extended: gbconfig.SDPExtensionEnabled()})
	} else {
		body, err = sdp.BuildPlaybackSDP(sdp.PlaybackParams{ServerID: serverID, ChannelID: sipChannelID, RecvIP: node.RecvIP,
			RecvPort: allocation.Port, SSRC: ssrc, TCPMode: tcpMode, Start: start, End: end, PlayFrom: playFrom,
			Extended: gbconfig.SDPExtensionEnabled()})
	}
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
	invite := UACInvite{DeviceID: request.DeviceID, ChannelID: sipChannelID, Destination: destination, Transport: transport, SSRC: ssrc, SDP: body}
	var dialog DialogInfo
	if owner == nil {
		dialog, err = s.inviter.Invite(ctx, invite)
	} else {
		var stored playauth.DeviceRTPResourceSteps
		stored, err = owner.store.LoadRTPResourceSteps(ctx, owner.id)
		var stepID string
		if err == nil {
			stepID, err = playauth.NewDeviceOperationIntentID()
		}
		if err == nil {
			owner.sip, err = s.inviter.(IntentSIPFactory).PrepareIntent(owner.ctx, owner.store, owner.barrier, owner.lease, owner.id, stored.Intent.RowVersion, stepID, invite)
		}
		if err == nil && owner.sip == nil {
			err = ErrRTPUnavailable
		}
		if err == nil {
			dialog, err = owner.sip.Invite(ctx)
		}
	}
	if dialog.CleanupRequired {
		resources.teardown = func(teardownCtx context.Context) error { return s.inviter.Teardown(teardownCtx, dialog.CallID) }
		if dialog.CallID == "" && dialog.CleanupRequired {
			resources.teardown = func(context.Context) error { return ErrPlaybackNotFound }
		}
	}
	if err != nil {
		if dialog.CleanupRequired && dialog.CallID != "" {
			updateErr := s.registry.Update(session.ID, func(value *Session) error {
				value.NodeID, value.StreamID, value.SSRC, value.CallID = node.ID, streamID, ssrc, dialog.CallID
				return nil
			})
			err = errors.Join(err, updateErr)
		}
		return CreateResult{}, s.fail(ctx, session.ID, "invite", "failed", err, resources)
	}
	resources.teardown = func(teardownCtx context.Context) error { return s.inviter.Teardown(teardownCtx, dialog.CallID) }
	var publishedResources CleanupResources = resources
	if owner != nil {
		publishedResources = owner
	}
	if err := s.registry.Update(session.ID, func(value *Session) error {
		value.NodeID, value.StreamID, value.SSRC, value.CallID = node.ID, streamID, ssrc, dialog.CallID
		value.PlayFrom, value.State, value.Resources = playFrom, StateBuffering, publishedResources
		return nil
	}); err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "invite", "session_update", err, resources)
	}
	if err := ctx.Err(); err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "invite", "cancelled", err, resources)
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
	if err := ctx.Err(); err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "media_wait", "cancelled", err, resources)
	}
	if err := s.registry.Update(session.ID, func(value *Session) error {
		selected := playurl.Select(playurl.FromMap(ready.URLs), request.DefaultProtocol, request.Secure)
		value.MediaURLs, value.HasAudio, value.State = ready.URLs, ready.HasAudio, StatePlaying
		value.DefaultProtocol, value.Protocol, value.URL, value.ZLMWebRTC = request.DefaultProtocol, selected.Protocol, selected.URL, selected.ZLMWebRTC
		return nil
	}); err != nil {
		return CreateResult{}, err
	}
	result, ok := s.registry.Get(session.ID)
	if !ok {
		return CreateResult{}, ErrPlaybackNotFound
	}
	if s.mediaReady != nil {
		s.mediaReady(*result)
	}
	if err := ctx.Err(); err != nil {
		return CreateResult{}, s.fail(ctx, session.ID, "media_wait", "cancelled", err, resources)
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
	s.producerMu.Lock()
	s.closing = true
	done := s.producersDone
	s.producerMu.Unlock()
	if s.lifecycleStop != nil {
		s.lifecycleStop()
	}
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
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
	s.producerMu.Lock()
	defer s.producerMu.Unlock()
	if !s.closing && !s.sweeperStarted {
		s.sweeperStarted = true
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithCancel(s.lifecycle)
		stopParent := context.AfterFunc(parent, cancel)
		s.addProducerLocked()
		go func() {
			defer s.finishProducer()
			defer cancel()
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
	}
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
	operationCtx, finish, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	ctx = operationCtx
	if err := ctx.Err(); err != nil {
		return nil, err
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
	position, scale := request.PositionSeconds, request.Scale
	if owner, managed := session.Resources.(*playbackIntentOwner); managed {
		command := playauth.DeviceSIPINFOCommand{Action: request.Action}
		if request.Action == "seek" {
			command.PositionNanos, command.SegmentDurationNanos = int64(time.Duration(position*float64(time.Second))), int64(duration)
		}
		if request.Action == "scale" {
			command.Scale = scale
		}
		err = owner.action(ctx, command)
	} else {
		actioner, ok := s.inviter.(PlaybackActioner)
		if !ok {
			return nil, ErrRTPUnavailable
		}
		position, scale, err = actioner.Action(ctx, session.CallID, request.Action, request.PositionSeconds, request.Scale, duration)
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
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
