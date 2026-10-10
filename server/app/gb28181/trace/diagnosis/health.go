package diagnosis

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
	Failed        uint64      `json:"failed"`
	LastError     string      `json:"lastError,omitempty"`
	LastSuccessAt *time.Time  `json:"lastSuccessAt,omitempty"`
}

type healthTracker struct {
	mu            sync.RWMutex
	state         HealthState
	dropped       uint64
	failed        uint64
	lastError     string
	lastSuccessAt *time.Time
}

func newHealthTracker() *healthTracker {
	return &healthTracker{state: HealthReady}
}

func (health *healthTracker) droppedEvent(reason string, count uint64) {
	health.mu.Lock()
	health.state = HealthDegraded
	health.dropped += count
	health.lastError = reason
	health.mu.Unlock()
}

func (health *healthTracker) failedBatch(err error, count uint64) {
	health.mu.Lock()
	health.state = HealthDegraded
	health.failed += count
	health.lastError = err.Error()
	health.mu.Unlock()
}

func (health *healthTracker) succeeded(at time.Time) {
	health.mu.Lock()
	health.state = HealthReady
	health.lastError = ""
	at = at.UTC()
	health.lastSuccessAt = &at
	health.mu.Unlock()
}

func (health *healthTracker) snapshot(depth, capacity int) HealthSnapshot {
	health.mu.RLock()
	defer health.mu.RUnlock()
	return HealthSnapshot{
		State: health.state, QueueDepth: depth, QueueCapacity: capacity,
		Dropped: health.dropped, Failed: health.failed, LastError: health.lastError,
		LastSuccessAt: cloneUTC(health.lastSuccessAt),
	}
}

func DisabledHealth() HealthSnapshot {
	return HealthSnapshot{State: HealthDisabled}
}
