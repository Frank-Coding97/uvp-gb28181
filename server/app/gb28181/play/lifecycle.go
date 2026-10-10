package play

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

type LifecycleStage string
type FactState string
type LifecycleSource string
type LifecycleState string
type MediaState string
type ClientState string

const (
	StageRequest       LifecycleStage = "request"
	StageValidation    LifecycleStage = "validation"
	StageNode          LifecycleStage = "node"
	StageRTP           LifecycleStage = "rtp"
	StageInvite        LifecycleStage = "invite"
	StageMedia         LifecycleStage = "media"
	StageAuthorization LifecycleStage = "authorization"
	StageClient        LifecycleStage = "client"
	StageStop          LifecycleStage = "stop"
	StageCleanup       LifecycleStage = "cleanup"
)

const (
	FactUnknown       FactState = "unknown"
	FactInProgress    FactState = "in_progress"
	FactConfirmed     FactState = "confirmed"
	FactFailed        FactState = "failed"
	FactNotApplicable FactState = "not_applicable"
)

const (
	SourceHTTP        LifecycleSource = "http"
	SourcePlayService LifecycleSource = "play_service"
	SourceSIP         LifecycleSource = "sip"
	SourceZLMHook     LifecycleSource = "zlm_hook"
	SourceClient      LifecycleSource = "client"
	SourceRetention   LifecycleSource = "retention"
)

const (
	LifecycleStateInProgress LifecycleState = "in_progress"
	LifecycleStateCompleted  LifecycleState = "completed"
	LifecycleStateFailed     LifecycleState = "failed"
	LifecycleStateStale      LifecycleState = "stale_in_progress"

	MediaStateUnknown MediaState = "unknown"
	MediaStateReady   MediaState = "ready"
	MediaStateFailed  MediaState = "failed"
	MediaStateStopped MediaState = "stopped"

	ClientStateUnknown    ClientState = "unknown"
	ClientStateFirstFrame ClientState = "first_frame"
	ClientStateFailed     ClientState = "failed"
)

const (
	EventRequestReceived        = "request_received"
	EventValidationSucceeded    = "validation_succeeded"
	EventValidationFailed       = "validation_failed"
	EventNodeSelected           = "node_selected"
	EventNodeSelectionFailed    = "node_selection_failed"
	EventRTPAllocated           = "rtp_allocated"
	EventRTPAllocationFailed    = "rtp_allocation_failed"
	EventRTPNotApplicable       = "rtp_not_applicable"
	EventInviteSent             = "invite_sent"
	EventInviteAccepted         = "invite_accepted"
	EventInviteRejected         = "invite_rejected"
	EventInviteTimeout          = "invite_timeout"
	EventInviteNotApplicable    = "invite_not_applicable"
	EventMediaReady             = "media_ready"
	EventReuseMediaReady        = "reuse_media_ready"
	EventMediaTimeout           = "media_timeout"
	EventPlayURLIssued          = "play_url_issued"
	EventAuthorizationFailed    = "authorization_failed"
	EventFirstFrame             = "first_frame"
	EventPlayerError            = "player_error"
	EventStopRequested          = "stop_requested"
	EventCleanupCompleted       = "cleanup_completed"
	EventCleanupPartialFailure  = "cleanup_partial_failure"
	EventStaleInProgress        = "stale_in_progress"
	EventHookStreamRegistered   = "hook_stream_registered"
	EventHookStreamUnregistered = "hook_stream_unregistered"
	EventHookFlowReported       = "hook_flow_reported"
	EventHookNoneReader         = "hook_none_reader"
	EventHookRTPTimeout         = "hook_rtp_timeout"
)

const (
	ReasonDeviceNotFound       = "device_not_found"
	ReasonDeviceOffline        = "device_offline"
	ReasonChannelNotFound      = "channel_not_found"
	ReasonNodeUnavailable      = "node_unavailable"
	ReasonRTPAllocationFailed  = "rtp_allocation_failed"
	ReasonInviteRejected       = "invite_rejected"
	ReasonInviteTimeout        = "invite_timeout"
	ReasonMediaTimeout         = "media_timeout"
	ReasonAuthorizationFailed  = "authorization_failed"
	ReasonPlayerError          = "player_error"
	ReasonPlayerTimeout        = "player_timeout"
	ReasonCleanupPartialFailed = "cleanup_partial_failure"
	ReasonStaleInProgress      = "stale_in_progress"
)

const MaxLifecycleReasonMessage = 256

var (
	errLifecycleTerminal = errors.New("lifecycle is already terminal")
	authorizationPattern = regexp.MustCompile(`(?i)authorization\s*:\s*(?:bearer\s+)?[^\s]+`)
	urlPattern           = regexp.MustCompile(`(?i)https?://[^\s]+`)
)

type LifecycleEvent struct {
	EventID       string
	EventAt       time.Time
	ElapsedMS     int64
	Stage         LifecycleStage
	EventName     string
	FactState     FactState
	Source        LifecycleSource
	DeviceCode    string
	ChannelCode   string
	StreamID      string
	NodeID        int64
	SSRC          string
	Reused        bool
	CallID        string
	CSeq          string
	ReasonCode    string
	ReasonMessage string
	MetadataJSON  []byte
}

type LifecycleSnapshot struct {
	LifecycleID    string
	UserID         uint
	DeviceCode     string
	ChannelCode    string
	StreamID       string
	NodeID         int64
	SSRC           string
	Reused         bool
	CallID         string
	CSeq           string
	CurrentStage   LifecycleStage
	FailureStage   LifecycleStage
	ReasonCode     string
	ReasonMessage  string
	LifecycleState LifecycleState
	MediaState     MediaState
	ClientState    ClientState
	StartedAt      time.Time
	LastEventAt    time.Time
	StageFacts     map[LifecycleStage]FactState
	Events         []LifecycleEvent
	seenEvents     map[string]struct{}
}

func NewLifecycleSnapshot(lifecycleID string, userID uint, deviceCode, channelCode string) *LifecycleSnapshot {
	now := time.Now()
	return &LifecycleSnapshot{
		LifecycleID: lifecycleID, UserID: userID, DeviceCode: deviceCode, ChannelCode: channelCode,
		LifecycleState: LifecycleStateInProgress, MediaState: MediaStateUnknown, ClientState: ClientStateUnknown,
		StartedAt: now, StageFacts: make(map[LifecycleStage]FactState), seenEvents: make(map[string]struct{}),
	}
}

func (snapshot *LifecycleSnapshot) Apply(event LifecycleEvent) error {
	if _, exists := snapshot.seenEvents[event.EventID]; event.EventID != "" && exists {
		return nil
	}
	terminalState := snapshot.LifecycleState
	terminalFailureStage := snapshot.FailureStage
	terminalReasonCode := snapshot.ReasonCode
	terminalReasonMessage := snapshot.ReasonMessage
	wasTerminal := isLifecycleTerminal(terminalState)
	if wasTerminal && !isPostTerminalCleanupEvent(event.EventName) {
		return errLifecycleTerminal
	}
	if event.EventAt.IsZero() {
		event.EventAt = time.Now()
	}
	if event.EventAt.Before(snapshot.StartedAt) {
		event.ElapsedMS = 0
	} else {
		event.ElapsedMS = event.EventAt.Sub(snapshot.StartedAt).Milliseconds()
	}
	event.ReasonMessage = sanitizeLifecycleReason(event.ReasonMessage)

	snapshot.CurrentStage = event.Stage
	snapshot.StageFacts[event.Stage] = event.FactState
	snapshot.LastEventAt = event.EventAt
	copyLifecycleCorrelation(snapshot, event)

	switch event.EventName {
	case EventMediaReady, EventReuseMediaReady:
		snapshot.MediaState = MediaStateReady
	case EventMediaTimeout:
		snapshot.MediaState = MediaStateFailed
	case EventFirstFrame:
		snapshot.ClientState = ClientStateFirstFrame
	case EventPlayerError:
		snapshot.ClientState = ClientStateFailed
	case EventCleanupCompleted:
		snapshot.MediaState = MediaStateStopped
		snapshot.LifecycleState = LifecycleStateCompleted
	case EventStaleInProgress:
		snapshot.LifecycleState = LifecycleStateStale
	}

	if event.FactState == FactFailed && event.Stage != StageClient {
		snapshot.FailureStage = event.Stage
		snapshot.ReasonCode = event.ReasonCode
		snapshot.ReasonMessage = event.ReasonMessage
		snapshot.LifecycleState = LifecycleStateFailed
	}
	if wasTerminal {
		snapshot.LifecycleState = terminalState
		snapshot.FailureStage = terminalFailureStage
		snapshot.ReasonCode = terminalReasonCode
		snapshot.ReasonMessage = terminalReasonMessage
	}
	snapshot.Events = append(snapshot.Events, event)
	if event.EventID != "" {
		snapshot.seenEvents[event.EventID] = struct{}{}
	}
	return nil
}

func isPostTerminalCleanupEvent(eventName string) bool {
	switch eventName {
	case EventStopRequested, EventCleanupCompleted, EventCleanupPartialFailure:
		return true
	default:
		return false
	}
}

func isLifecycleTerminal(state LifecycleState) bool {
	return state == LifecycleStateCompleted || state == LifecycleStateFailed || state == LifecycleStateStale
}

func copyLifecycleCorrelation(snapshot *LifecycleSnapshot, event LifecycleEvent) {
	if event.StreamID != "" {
		snapshot.StreamID = event.StreamID
	}
	if event.NodeID != 0 {
		snapshot.NodeID = event.NodeID
	}
	if event.SSRC != "" {
		snapshot.SSRC = event.SSRC
	}
	if event.CallID != "" {
		snapshot.CallID = event.CallID
	}
	if event.CSeq != "" {
		snapshot.CSeq = event.CSeq
	}
	if event.Reused {
		snapshot.Reused = true
	}
}

func sanitizeLifecycleReason(message string) string {
	message = authorizationPattern.ReplaceAllString(message, "Authorization: [redacted]")
	message = urlPattern.ReplaceAllString(message, "[redacted-url]")
	message = strings.TrimSpace(message)
	if len(message) > MaxLifecycleReasonMessage {
		message = message[:MaxLifecycleReasonMessage]
	}
	return message
}
