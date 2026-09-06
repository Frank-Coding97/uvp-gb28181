package logging

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const repeatLimit = 4096

// RepeatKey identifies an explicitly selected background state. Callers use
// static component/event names and business IDs, never request IDs or raw errors.
type RepeatKey struct {
	Component, Event string
	NodeID           int64
	JobID            string
}

// Fixed storage prevents a short slice/string from retaining an unbounded input.
// Together the map key and state occupy less than 512 bytes, excluding map overhead.
type repeatKey struct {
	component [48]byte
	event     [96]byte
	job       [64]byte
	class     [32]byte
	node      int64
}
type repeatState struct {
	first, last, window time.Time
	pending, sequence   uint64
}
type Repeater struct {
	mu       sync.Mutex
	logger   *zap.Logger
	now      func() time.Time
	states   map[repeatKey]repeatState
	sequence uint64
	closed   bool
}

func newRepeater(logger *zap.Logger, now func() time.Time) *Repeater {
	return &Repeater{logger: logger, now: now, states: make(map[repeatKey]repeatState)}
}
func boundedRepeatLabel(dst []byte, value string) {
	if len(value) <= len(dst) {
		copy(dst, value)
		return
	}
	// Oversized identifiers keep a bounded digest rather than collide by prefix.
	sum := sha256.Sum256([]byte(value))
	copy(dst, "sha256:")
	hex.Encode(dst[7:7+24], sum[:12])
}
func repeatStorageKey(key RepeatKey, class string) (out repeatKey) {
	boundedRepeatLabel(out.component[:], key.Component)
	boundedRepeatLabel(out.event[:], key.Event)
	boundedRepeatLabel(out.job[:], key.JobID)
	boundedRepeatLabel(out.class[:], class)
	out.node = key.NodeID
	return
}
func repeatLabel(value []byte) string { return strings.TrimRight(string(value), "\x00") }

func (r *Repeater) Fail(key RepeatKey, err error) {
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	now := r.now()
	storage := repeatStorageKey(key, ErrorClass(err))
	r.sequence++
	if state, ok := r.states[storage]; ok && now.Sub(state.last) >= 10*time.Minute {
		r.emit(storage, state, "idle")
		delete(r.states, storage)
	}
	if state, ok := r.states[storage]; ok {
		state.last = now
		state.pending++
		state.sequence = r.sequence
		if now.Sub(state.window) >= time.Minute {
			r.emit(storage, state, "window")
			state.pending = 0
			state.window = now
		}
		r.states[storage] = state
		return
	}
	if len(r.states) >= repeatLimit {
		var oldest repeatKey
		var sequence uint64 = ^uint64(0)
		for k, s := range r.states {
			if s.sequence < sequence {
				oldest = k
				sequence = s.sequence
			}
		}
		r.emit(oldest, r.states[oldest], "capacity")
		delete(r.states, oldest)
	}
	state := repeatState{first: now, last: now, window: now, sequence: r.sequence}
	r.states[storage] = state
	r.emit(storage, state, "first")
}

// Recovered settles every error class for this exact operation and object.
// It reports recovery only when a preceding failure was actually observed.
func (r *Repeater) Recovered(key RepeatKey) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	base := repeatStorageKey(key, "")
	for k, state := range r.states {
		candidate := k
		candidate.class = [32]byte{}
		if candidate == base {
			r.emit(k, state, "recovered")
			delete(r.states, k)
		}
	}
}
func (r *Repeater) maintain() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	now := r.now()
	for key, state := range r.states {
		if now.Sub(state.last) >= 10*time.Minute {
			r.emit(key, state, "idle")
			delete(r.states, key)
			continue
		}
		if state.pending > 0 && now.Sub(state.window) >= time.Minute {
			r.emit(key, state, "window")
			state.pending = 0
			state.window = now
			r.states[key] = state
		}
	}
}
func (r *Repeater) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	for key, state := range r.states {
		r.emit(key, state, "close")
		delete(r.states, key)
	}
}
func (r *Repeater) Len() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states)
}
func (r *Repeater) emit(key repeatKey, state repeatState, reason string) {
	event := "logging.repeat_summary"
	source := repeatLabel(key.event[:])
	if reason == "first" {
		event = source
	}
	if reason == "recovered" {
		event = "logging.repeat_recovered"
	}
	fields := []zap.Field{zap.String("event", event), zap.String("source_event", source), zap.String("error_class", repeatLabel(key.class[:])), zap.String("settlement", reason), zap.Uint64("suppressed_count", state.pending), zap.Time("first_seen", state.first), zap.Time("last_seen", state.last)}
	if key.node != 0 {
		fields = append(fields, zap.Int64("node_id", key.node))
	}
	if job := repeatLabel(key.job[:]); job != "" {
		fields = append(fields, zap.String("job_id", job))
	}
	logger := r.logger.Named(repeatLabel(key.component[:]))
	if reason == "recovered" {
		logger.Info("Background operation recovered", fields...)
	} else if reason == "first" {
		logger.Warn("Background operation failed", fields...)
	} else {
		logger.Warn("Repeated background failure summarized", fields...)
	}
}
