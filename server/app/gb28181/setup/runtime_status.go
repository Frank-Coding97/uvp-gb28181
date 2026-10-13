package setup

import (
	"regexp"
	"sync"
	"time"
)

type RuntimeState string

const (
	RuntimeDisabled        RuntimeState = "disabled"
	RuntimeUnconfigured    RuntimeState = "unconfigured"
	RuntimeStarting        RuntimeState = "starting"
	RuntimeRunning         RuntimeState = "running"
	RuntimeFailed          RuntimeState = "failed"
	RuntimeRestartRequired RuntimeState = "restart_required"
)

type RuntimeSnapshot struct {
	State        RuntimeState `json:"state"`
	ErrorSummary string       `json:"errorSummary,omitempty"`
	UpdatedAt    time.Time    `json:"updatedAt"`
	StartedAt    *time.Time   `json:"startedAt,omitempty"`
}

type RuntimeStatus struct {
	mu      sync.RWMutex
	state   RuntimeState
	errText string
	updated time.Time
	started time.Time
	now     func() time.Time
}

func NewRuntimeStatus() *RuntimeStatus {
	return &RuntimeStatus{state: RuntimeDisabled, now: time.Now}
}

func (s *RuntimeStatus) MarkDisabled()          { s.set(RuntimeDisabled, "") }
func (s *RuntimeStatus) MarkUnconfigured()      { s.set(RuntimeUnconfigured, "") }
func (s *RuntimeStatus) MarkStarting()          { s.set(RuntimeStarting, "") }
func (s *RuntimeStatus) MarkRunning()           { s.set(RuntimeRunning, "") }
func (s *RuntimeStatus) MarkConfigSaved()       { s.set(RuntimeRestartRequired, "") }
func (s *RuntimeStatus) MarkFailed(text string) { s.set(RuntimeFailed, safeRuntimeSummary(text)) }

func (s *RuntimeStatus) Snapshot() RuntimeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var startedAt *time.Time
	if !s.started.IsZero() {
		value := s.started
		startedAt = &value
	}
	return RuntimeSnapshot{State: s.state, ErrorSummary: s.errText, UpdatedAt: s.updated, StartedAt: startedAt}
}

func (s *RuntimeStatus) set(state RuntimeState, errText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if state == RuntimeRunning && s.state != RuntimeRunning && s.state != RuntimeRestartRequired {
		s.started = now
	}
	if state == RuntimeDisabled || state == RuntimeUnconfigured || state == RuntimeFailed {
		s.started = time.Time{}
	}
	s.state = state
	s.errText = errText
	s.updated = now
}

var passwordPattern = regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*\S+`)

func safeRuntimeSummary(text string) string {
	return passwordPattern.ReplaceAllString(text, "$1=[redacted]")
}
