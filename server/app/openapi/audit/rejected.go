package audit

import (
	"net/netip"
	"sync"
	"time"
)

// RejectedEventCapacity is deliberately fixed so attacker-controlled traffic
// cannot grow this process-local diagnostic buffer without bound.
const RejectedEventCapacity = 256

type ReasonClass string

const (
	ReasonInvalidRequest       ReasonClass = "INVALID_REQUEST"
	ReasonAuthenticationFailed ReasonClass = "AUTHENTICATION_FAILED"
	ReasonRequestExpired       ReasonClass = "REQUEST_EXPIRED"
	ReasonRequestReplayed      ReasonClass = "REQUEST_REPLAYED"
	ReasonCapabilityDenied     ReasonClass = "CAPABILITY_DENIED"
	ReasonResourceNotFound     ReasonClass = "RESOURCE_NOT_FOUND"
	ReasonConflict             ReasonClass = "CONFLICT"
	ReasonRateLimited          ReasonClass = "RATE_LIMITED"
	ReasonQuotaExceeded        ReasonClass = "QUOTA_EXCEEDED"
	ReasonServiceUnavailable   ReasonClass = "SERVICE_UNAVAILABLE"
	ReasonUnknown              ReasonClass = "UNKNOWN"
)

const reasonClassCount = 11

var publishedRejectedReasonClasses = [...]ReasonClass{
	ReasonInvalidRequest,
	ReasonAuthenticationFailed,
	ReasonRequestExpired,
	ReasonRequestReplayed,
	ReasonCapabilityDenied,
	ReasonResourceNotFound,
	ReasonConflict,
	ReasonRateLimited,
	ReasonQuotaExceeded,
	ReasonServiceUnavailable,
	ReasonUnknown,
}

// PublishedRejectedReasonClasses returns a copy of the fixed reason set. The
// set is an API contract; arbitrary caller strings never create new classes.
func PublishedRejectedReasonClasses() []ReasonClass {
	classes := make([]ReasonClass, len(publishedRejectedReasonClasses))
	copy(classes, publishedRejectedReasonClasses[:])
	return classes
}

// RejectedInput contains only fields that may be retained after admission
// sanitization. Callers must provide RequestID and ClientID only after they
// have been generated or verified by the authentication path.
type RejectedInput struct {
	RequestID string
	ClientID  int64
	Scope     string
	Source    string
	Reason    string
}

// RejectedEvent is the complete set of data retained for a bounded rejected
// request. It intentionally has no access key, signature, query, body, token,
// or arbitrary error field.
type RejectedEvent struct {
	At        time.Time   `json:"at"`
	RequestID string      `json:"requestId,omitempty"`
	ClientID  int64       `json:"clientId,omitempty"`
	Scope     string      `json:"scope,omitempty"`
	Source    string      `json:"source,omitempty"`
	Reason    ReasonClass `json:"reason"`
}

type RejectedSnapshot struct {
	Counts map[ReasonClass]uint64 `json:"counts"`
	Events []RejectedEvent        `json:"events"`
}

// RejectedCollector is a process-local summary only. It is not the durable
// audit trail for authenticated admissions or started business requests.
type RejectedCollector struct {
	mu     sync.Mutex
	events [RejectedEventCapacity]RejectedEvent
	next   uint16
	size   uint16
	counts [reasonClassCount]uint64
}

func NewRejectedCollector() *RejectedCollector {
	return &RejectedCollector{}
}

// Record performs no database, logger, or network operation. Invalid optional
// fields are discarded while the normalized reason is still counted.
func (c *RejectedCollector) Record(input RejectedInput) {
	if c == nil {
		return
	}
	event := RejectedEvent{At: time.Now().UTC(), Reason: normalizeReason(input.Reason)}
	if validRejectedRequestID(input.RequestID) {
		event.RequestID = input.RequestID
	}
	if input.ClientID > 0 {
		event.ClientID = input.ClientID
	}
	event.Scope = normalizeRejectedScope(input.Scope)
	event.Source = normalizeRejectedSource(input.Source)

	c.mu.Lock()
	defer c.mu.Unlock()
	index := int(c.next)
	c.events[index] = event
	c.next = uint16((index + 1) % RejectedEventCapacity)
	if c.size < RejectedEventCapacity {
		c.size++
	}
	class := reasonIndex(event.Reason)
	if c.counts[class] != ^uint64(0) {
		c.counts[class]++
	}
}

// Snapshot copies both the fixed counters and the ring contents. Mutating the
// returned value cannot affect subsequent records.
func (c *RejectedCollector) Snapshot() RejectedSnapshot {
	snapshot := RejectedSnapshot{Counts: make(map[ReasonClass]uint64, len(publishedRejectedReasonClasses))}
	if c == nil {
		for _, class := range publishedRejectedReasonClasses {
			snapshot.Counts[class] = 0
		}
		snapshot.Events = make([]RejectedEvent, 0)
		return snapshot
	}

	c.mu.Lock()
	for index, class := range publishedRejectedReasonClasses {
		snapshot.Counts[class] = c.counts[index]
	}
	snapshot.Events = make([]RejectedEvent, c.size)
	start := (int(c.next) - int(c.size) + RejectedEventCapacity) % RejectedEventCapacity
	for i := range snapshot.Events {
		snapshot.Events[i] = c.events[(start+i)%RejectedEventCapacity]
	}
	c.mu.Unlock()
	return snapshot
}

func reasonIndex(reason ReasonClass) int {
	for index, class := range publishedRejectedReasonClasses {
		if class == reason {
			return index
		}
	}
	return len(publishedRejectedReasonClasses) - 1
}

func normalizeReason(reason string) ReasonClass {
	class := ReasonClass(reason)
	if reasonIndex(class) == len(publishedRejectedReasonClasses)-1 {
		return ReasonUnknown
	}
	return class
}

func validRejectedRequestID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if !((value[i] >= '0' && value[i] <= '9') || (value[i] >= 'a' && value[i] <= 'f')) {
			return false
		}
	}
	return true
}

func normalizeRejectedScope(scope string) string {
	switch scope {
	case "device:list", "device:detail", "device:status", "channel:list", "channel:detail", "channel:status", "play:live:apply":
		return scope
	default:
		return ""
	}
}

func normalizeRejectedSource(source string) string {
	address, err := netip.ParseAddr(source)
	if err != nil || !address.IsValid() || address.Zone() != "" {
		return ""
	}
	return address.Unmap().String()
}
