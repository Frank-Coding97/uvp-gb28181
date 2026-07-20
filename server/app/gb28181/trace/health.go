package trace

import (
	"sync"
	"time"
)

type HealthState string

const (
	HealthDisabled HealthState = "disabled"
	HealthDegraded HealthState = "degraded"
	HealthReady    HealthState = "ready"
)

type HealthSnapshot struct {
	State         HealthState  `json:"state"`
	QueueDepth    int          `json:"queueDepth"`
	QueueCapacity int          `json:"queueCapacity"`
	Dropped       uint64       `json:"dropped"`
	LastError     string       `json:"lastError,omitempty"`
	LastSuccessAt *time.Time   `json:"lastSuccessAt,omitempty"`
	CurrentGap    *GapSnapshot `json:"currentGap,omitempty"`
	LastGap       *GapSnapshot `json:"lastGap,omitempty"`
}

// GapSnapshot describes a period in which trace events could not be persisted.
// It is intentionally part of health rather than inferred from queue depth: a
// healthy queue can still hide a failed storage batch.
type GapSnapshot struct {
	StartedAt  time.Time  `json:"startedAt"`
	EndedAt    *time.Time `json:"endedAt,omitempty"`
	Reason     string     `json:"reason"`
	EventCount uint64     `json:"eventCount"`
}

type healthTracker struct {
	mu            sync.RWMutex
	state         HealthState
	lastError     string
	lastSuccessAt *time.Time
	currentGap    *GapSnapshot
	lastGap       *GapSnapshot
}

func newHealthTracker(state HealthState, lastError string) *healthTracker {
	return &healthTracker{state: state, lastError: lastError}
}

func (h *healthTracker) degraded(err string) {
	h.mu.Lock()
	h.state = HealthDegraded
	h.lastError = err
	h.mu.Unlock()
}

func (h *healthTracker) ready(at time.Time) {
	h.mu.Lock()
	h.state = HealthReady
	h.lastError = ""
	at = at.UTC()
	h.lastSuccessAt = &at
	h.mu.Unlock()
}

func (h *healthTracker) snapshot() HealthSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return HealthSnapshot{
		State: h.state, LastError: h.lastError, LastSuccessAt: cloneTimePtr(h.lastSuccessAt),
		CurrentGap: cloneGap(h.currentGap), LastGap: cloneGap(h.lastGap),
	}
}

func (h *healthTracker) gapStart(at time.Time, reason string, count uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.currentGap == nil {
		h.currentGap = &GapSnapshot{StartedAt: at.UTC(), Reason: reason}
	}
	h.currentGap.EventCount += count
	h.state = HealthDegraded
	h.lastError = reason
}

func (h *healthTracker) gapAdd(count uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.currentGap != nil {
		h.currentGap.EventCount += count
	}
}

func (h *healthTracker) gapEnd(at time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.currentGap != nil {
		ended := at.UTC()
		h.currentGap.EndedAt = &ended
		h.lastGap = cloneGap(h.currentGap)
		h.currentGap = nil
	}
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func cloneGap(value *GapSnapshot) *GapSnapshot {
	if value == nil {
		return nil
	}
	copy := *value
	copy.StartedAt = copy.StartedAt.UTC()
	copy.EndedAt = cloneTimePtr(value.EndedAt)
	return &copy
}

func DisabledHealth() HealthSnapshot {
	return HealthSnapshot{State: HealthDisabled}
}
