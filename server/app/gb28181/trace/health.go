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
	State         HealthState `json:"state"`
	QueueDepth    int         `json:"queueDepth"`
	QueueCapacity int         `json:"queueCapacity"`
	Dropped       uint64      `json:"dropped"`
	LastError     string      `json:"lastError,omitempty"`
	LastSuccessAt *time.Time  `json:"lastSuccessAt,omitempty"`
}

type healthTracker struct {
	mu            sync.RWMutex
	state         HealthState
	lastError     string
	lastSuccessAt *time.Time
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
	return HealthSnapshot{State: h.state, LastError: h.lastError, LastSuccessAt: h.lastSuccessAt}
}

func DisabledHealth() HealthSnapshot {
	return HealthSnapshot{State: HealthDisabled}
}
