package logging

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	defaultRealtimeRing  = 2000
	defaultRealtimeQueue = 256
	maxRealtimeMessage   = 2048
)

// RealtimeEvent is the redacted, bounded event exposed to the operator console.
type RealtimeEvent struct {
	SchemaVersion string    `json:"schemaVersion"`
	EventID       string    `json:"eventId"`
	InstanceID    string    `json:"instanceId"`
	Sequence      uint64    `json:"sequence"`
	OccurredAt    time.Time `json:"occurredAt"`
	ObservedAt    time.Time `json:"observedAt"`
	Level         string    `json:"level"`
	Module        string    `json:"module"`
	Event         string    `json:"event"`
	Stage         string    `json:"stage,omitempty"`
	Outcome       string    `json:"outcome,omitempty"`
	RequestID     string    `json:"requestId,omitempty"`
	OperationID   string    `json:"operationId,omitempty"`
	CorrelationID string    `json:"correlationId,omitempty"`
	DeviceID      string    `json:"deviceId,omitempty"`
	ChannelID     string    `json:"channelId,omitempty"`
	NodeID        string    `json:"nodeId,omitempty"`
	StreamID      string    `json:"streamId,omitempty"`
	CallID        string    `json:"callId,omitempty"`
	ReasonCode    string    `json:"reasonCode,omitempty"`
	DurationMs    float64   `json:"durationMs,omitempty"`
	Message       string    `json:"message"`
	Truncated     bool      `json:"truncated,omitempty"`
}

type RealtimeFilter struct {
	Level, Module, Event, DeviceID, ChannelID, NodeID, StreamID, CallID, RequestID, OperationID, CorrelationID string
}

func (f RealtimeFilter) Matches(e RealtimeEvent) bool {
	return (f.Level == "" || strings.EqualFold(f.Level, e.Level)) &&
		(f.Module == "" || e.Module == f.Module) &&
		(f.Event == "" || e.Event == f.Event) &&
		(f.DeviceID == "" || e.DeviceID == f.DeviceID) &&
		(f.ChannelID == "" || e.ChannelID == f.ChannelID) &&
		(f.NodeID == "" || e.NodeID == f.NodeID) &&
		(f.StreamID == "" || e.StreamID == f.StreamID) &&
		(f.CallID == "" || e.CallID == f.CallID) &&
		(f.RequestID == "" || e.RequestID == f.RequestID) &&
		(f.OperationID == "" || e.OperationID == f.OperationID) &&
		(f.CorrelationID == "" || e.CorrelationID == f.CorrelationID)
}

type realtimeSubscriber struct {
	id       string
	filter   RealtimeFilter
	ch       chan RealtimeEvent
	dropped  atomic.Uint64
	reported atomic.Uint64
}

type RealtimeSubscription struct {
	ID       string
	Events   <-chan RealtimeEvent
	dropped  *atomic.Uint64
	reported *atomic.Uint64
}

func (s RealtimeSubscription) Dropped() uint64 {
	if s.dropped == nil {
		return 0
	}
	return s.dropped.Load()
}

// TakeDropped returns the cumulative drop count only when it changed since the
// previous call. This prevents repeating the same control frame for every event.
func (s RealtimeSubscription) TakeDropped() uint64 {
	if s.dropped == nil {
		return 0
	}
	current := s.dropped.Load()
	if s.reported == nil {
		return current
	}
	previous := s.reported.Load()
	for current != previous {
		if s.reported.CompareAndSwap(previous, current) {
			return current
		}
		previous = s.reported.Load()
	}
	return 0
}

// EventHub is an instance-local bounded ring and fan-out hub.
type EventHub struct {
	mu         sync.RWMutex
	ring       []RealtimeEvent
	capacity   int
	subs       map[string]*realtimeSubscriber
	sequence   atomic.Uint64
	instanceID string
}

func NewEventHub() *EventHub {
	return NewEventHubWithInstance(uuid.NewString())
}

func NewEventHubWithInstance(instanceID string) *EventHub {
	if strings.TrimSpace(instanceID) == "" {
		instanceID = uuid.NewString()
	}
	return &EventHub{capacity: defaultRealtimeRing, subs: make(map[string]*realtimeSubscriber), instanceID: instanceID}
}

func (h *EventHub) Publish(e RealtimeEvent) {
	if h == nil || e.Event == "" {
		return
	}
	e.Sequence = h.sequence.Add(1)
	if e.EventID == "" {
		e.EventID = uuid.NewString()
	}
	if e.SchemaVersion == "" {
		e.SchemaVersion = "1"
	}
	if e.InstanceID == "" {
		e.InstanceID = h.instanceID
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	if e.ObservedAt.IsZero() {
		e.ObservedAt = time.Now()
	}
	for _, value := range []*string{&e.Level, &e.Module, &e.Event, &e.Stage, &e.Outcome, &e.RequestID, &e.OperationID, &e.CorrelationID, &e.DeviceID, &e.ChannelID, &e.NodeID, &e.StreamID, &e.CallID, &e.ReasonCode} {
		*value = clipRealtimeValue(*value)
	}
	e.Message = redactRealtimeMessage(e.Message)
	var cut bool
	e.Message, cut = clipJSON(e.Message, maxRealtimeMessage)
	e.Truncated = e.Truncated || cut
	h.mu.Lock()
	if len(h.ring) >= h.capacity {
		copy(h.ring, h.ring[1:])
		h.ring[len(h.ring)-1] = e
	} else {
		h.ring = append(h.ring, e)
	}
	for _, sub := range h.subs {
		if !sub.filter.Matches(e) {
			continue
		}
		select {
		case sub.ch <- e:
		default:
			select {
			case <-sub.ch:
			default:
			}
			select {
			case sub.ch <- e:
			default:
			}
			sub.dropped.Add(1)
		}
	}
	h.mu.Unlock()
}

func (h *EventHub) Subscribe(ctx context.Context, f RealtimeFilter, since uint64) (RealtimeSubscription, []RealtimeEvent, bool) {
	if h == nil {
		return RealtimeSubscription{}, nil, false
	}
	s := &realtimeSubscriber{id: uuid.NewString(), filter: f, ch: make(chan RealtimeEvent, defaultRealtimeQueue)}
	h.mu.Lock()
	var snapshot []RealtimeEvent
	gap := false
	effectiveSince := since
	if since > h.sequence.Load() {
		gap = true
		effectiveSince = 0
	}
	if since > 0 && len(h.ring) > 0 && since+1 < h.ring[0].Sequence {
		gap = true
	}
	for _, e := range h.ring {
		if e.Sequence > effectiveSince && f.Matches(e) {
			snapshot = append(snapshot, e)
		}
	}
	h.subs[s.id] = s
	h.mu.Unlock()
	if done := ctx.Done(); done != nil {
		go func() { <-done; h.Unsubscribe(s.id) }()
	}
	return RealtimeSubscription{ID: s.id, Events: s.ch, dropped: &s.dropped, reported: &s.reported}, snapshot, gap
}

func (h *EventHub) Unsubscribe(id string) {
	h.mu.Lock()
	s, ok := h.subs[id]
	if ok {
		delete(h.subs, id)
		close(s.ch)
	}
	h.mu.Unlock()
}
func (h *EventHub) SubscriberCount() int { h.mu.RLock(); defer h.mu.RUnlock(); return len(h.subs) }
func (h *EventHub) LatestSequence() uint64 {
	if h == nil {
		return 0
	}
	return h.sequence.Load()
}

func (h *EventHub) InstanceID() string {
	if h == nil {
		return ""
	}
	return h.instanceID
}

func isRealtimeEvent(name string) bool {
	if name == "" || name == "http.access" || strings.HasPrefix(name, "audit.") || strings.HasPrefix(name, "legacy.") || strings.HasPrefix(name, "casbin.") {
		return false
	}
	for _, p := range []string{"gb28181.register.", "gb28181.play.", "gb28181.hook.", "gb28181.device.scanner.", "gb28181.message.keepalive_", "play.attempt_", "play.recording_", "play.auto_start.", "zlm.media_online."} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func realtimeString(fields map[string]any, key string) string {
	if v, ok := fields[key]; ok {
		switch value := v.(type) {
		case string:
			return value
		case fmt.Stringer:
			return value.String()
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return fmt.Sprint(value)
		}
	}
	return ""
}

func realtimeStringAny(fields map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := realtimeString(fields, key); value != "" {
			return value
		}
	}
	return ""
}

func realtimeDuration(fields map[string]any) float64 {
	for _, key := range []string{"duration_ms", "durationMs"} {
		switch value := fields[key].(type) {
		case float64:
			return value
		case float32:
			return float64(value)
		case int64:
			return float64(value)
		case int:
			return float64(value)
		}
	}
	return 0
}

func clipRealtimeValue(value string) string {
	clipped, _ := clipJSON(value, 256)
	return clipped
}

var (
	realtimeSecretPattern = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*(?:bearer\s+)?|bearer\s+|(?:token|access_token|secret|password|cookie|hook[_-]?key|zlm[_-]?secret)\s*[:=]\s*)([^\s,;]+)`)
	realtimeURLPattern    = regexp.MustCompile(`https?://[^\s]+`)
)

func redactRealtimeMessage(message string) string {
	message = realtimeSecretPattern.ReplaceAllString(message, `$1=[REDACTED]`)
	return realtimeURLPattern.ReplaceAllStringFunc(message, func(raw string) string {
		trailing := ""
		for len(raw) > 0 && strings.ContainsRune(".,);]}", rune(raw[len(raw)-1])) {
			trailing = string(raw[len(raw)-1]) + trailing
			raw = raw[:len(raw)-1]
		}
		return redactRealtimeURL(raw) + trailing
	})
}

func buildRealtimeEvent(instance string, entry zapcore.Entry, fields []zap.Field) (RealtimeEvent, bool) {
	if entry.Level < zapcore.InfoLevel {
		return RealtimeEvent{}, false
	}
	m := zapcore.NewMapObjectEncoder()
	for _, f := range fields {
		f.AddTo(m)
	}
	if !isRealtimeEvent(realtimeString(m.Fields, "event")) {
		return RealtimeEvent{}, false
	}
	event := realtimeString(m.Fields, "event")
	module := entry.LoggerName
	if i := strings.LastIndex(module, "."); i > 0 {
		module = module[:i]
	}
	outcome := ""
	if strings.Contains(event, "failed") || strings.Contains(event, "denied") || strings.Contains(event, "timeout") {
		outcome = "failed"
	} else if strings.Contains(event, "succeeded") || strings.Contains(event, "completed") || strings.Contains(event, "accepted") || strings.Contains(event, "ready") {
		outcome = "succeeded"
	}
	stage := ""
	parts := strings.Split(event, ".")
	if len(parts) > 2 {
		stage = parts[len(parts)-1]
	}
	occurred := entry.Time
	if occurred.IsZero() {
		occurred = time.Now()
	}
	stageValue := realtimeStringAny(m.Fields, "stage")
	if stageValue == "" {
		stageValue = stage
	}
	outcomeValue := realtimeStringAny(m.Fields, "outcome")
	if outcomeValue == "" {
		outcomeValue = outcome
	}
	return RealtimeEvent{InstanceID: instance, OccurredAt: occurred, ObservedAt: time.Now(), Level: entry.Level.String(), Module: clipRealtimeValue(module), Event: clipRealtimeValue(event), Stage: clipRealtimeValue(stageValue), Outcome: clipRealtimeValue(outcomeValue), RequestID: clipRealtimeValue(realtimeStringAny(m.Fields, "request_id", "requestId")), OperationID: clipRealtimeValue(realtimeStringAny(m.Fields, "operation_id", "operationId")), CorrelationID: clipRealtimeValue(realtimeStringAny(m.Fields, "correlation_id", "correlationId")), DeviceID: clipRealtimeValue(realtimeStringAny(m.Fields, "device_id", "deviceId")), ChannelID: clipRealtimeValue(realtimeStringAny(m.Fields, "channel_id", "channelId")), NodeID: clipRealtimeValue(realtimeStringAny(m.Fields, "node_id", "nodeId")), StreamID: clipRealtimeValue(realtimeStringAny(m.Fields, "stream_id", "streamId", "stream")), CallID: clipRealtimeValue(realtimeStringAny(m.Fields, "call_id", "callId")), ReasonCode: clipRealtimeValue(realtimeStringAny(m.Fields, "reason_code", "reasonCode")), DurationMs: realtimeDuration(m.Fields), Message: redactRealtimeMessage(entry.Message)}, true
}

func redactRealtimeURL(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		for _, k := range []string{"token", "access_token", "secret", "password", "key"} {
			if q.Has(k) {
				q.Set(k, "[REDACTED]")
			}
		}
		u.RawQuery = q.Encode()
		return u.String()
	}
	return raw
}
