package handler

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"

	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
	"uvplatform.cn/uvp-gb28181/app/gb28181/trace/diagnosis"
)

const (
	defaultRegisterObservationWindow = 60 * time.Second
	defaultMaxRegisterAttempts       = 4096
)

type registerTimer interface {
	Stop() bool
}

func (h *RegisterHandler) trackRegisterChallenge(req *sip.Request, deviceID, nonce string) {
	if nonce == "" {
		return
	}
	callID := registerCallID(req)
	cseq := registerCSeq(req)
	h.attempts.startChallenge(diagnosis.Event{
		CorrelationKey: diagnosis.RegisterCorrelationKey(deviceID, callID, cseq, nonce),
		State:          diagnosis.StateActive, Category: diagnosis.CategoryRegisterFailure,
		Code: diagnosis.CodeRegisterTimeout, Stage: diagnosis.StageRegister,
		Source: diagnosis.SourceRuntime, DeviceID: deviceID, CallID: callID,
		CSeq: cseq, Method: string(sip.REGISTER),
	}, nonce)
}

func (h *RegisterHandler) finishRegisterAttempt(req *sip.Request, deviceID, nonce string) {
	key := diagnosis.RegisterCorrelationKey(deviceID, registerCallID(req), registerCSeq(req), nonce)
	h.attempts.finish(key, h.now())
}

func (h *RegisterHandler) failAndEmitRegister(req *sip.Request, deviceID, nonce string, code diagnosis.Code, status int) {
	key := diagnosis.RegisterCorrelationKey(deviceID, registerCallID(req), registerCSeq(req), nonce)
	h.attempts.fail(key)
	h.emitRegisterFailure(req, deviceID, nonce, code, status)
}

func (h *RegisterHandler) emitRegisterFailure(req *sip.Request, deviceID, nonce string, code diagnosis.Code, status int) {
	callID := registerCallID(req)
	cseq := registerCSeq(req)
	event := diagnosis.Event{
		ObservedAt:     h.now().UTC(),
		CorrelationKey: diagnosis.RegisterCorrelationKey(deviceID, callID, cseq, nonce),
		State:          diagnosis.StateActive, Category: diagnosis.CategoryRegisterFailure,
		Code: code, Stage: diagnosis.StageRegister, Source: diagnosis.SourceRuntime,
		DeviceID: deviceID, CallID: callID, CSeq: cseq, Method: string(sip.REGISTER),
		StatusCode: uint16(status),
	}
	_ = h.diagnosticSink.Emit(context.Background(), event)
}

func registerCallID(req *sip.Request) string {
	if req == nil || req.CallID() == nil {
		return ""
	}
	return string(*req.CallID())
}

func registerCSeq(req *sip.Request) uint32 {
	if req == nil || req.CSeq() == nil {
		return 0
	}
	return req.CSeq().SeqNo
}

func diagnosisCodeForNonceError(err error) diagnosis.Code {
	switch {
	case errors.Is(err, gbsecurity.ErrNonceExpired):
		return diagnosis.CodeNonceExpired
	case errors.Is(err, gbsecurity.ErrNonceReplay):
		return diagnosis.CodeNonceReplay
	case errors.Is(err, gbsecurity.ErrNonceStale):
		return diagnosis.CodeNonceStale
	default:
		return diagnosis.CodeNonceInvalid
	}
}

type registerAttempt struct {
	event    diagnosis.Event
	started  time.Time
	deadline time.Time
	timedOut bool
	timer    registerTimer
	nonceSum [sha256.Size]byte
}

type registerAttemptTracker struct {
	mu       sync.Mutex
	attempts map[string]*registerAttempt
	max      int
	window   time.Duration
	now      func() time.Time
	after    func(time.Duration, func()) registerTimer
	sink     diagnosis.DiagnosticSink
}

func newRegisterAttemptTracker(sink diagnosis.DiagnosticSink, now func() time.Time) *registerAttemptTracker {
	if sink == nil {
		sink = diagnosis.NoopSink{}
	}
	if now == nil {
		now = time.Now
	}
	return &registerAttemptTracker{
		attempts: make(map[string]*registerAttempt), max: defaultMaxRegisterAttempts,
		window: defaultRegisterObservationWindow, now: now, sink: sink,
		after: func(duration time.Duration, callback func()) registerTimer {
			return time.AfterFunc(duration, callback)
		},
	}
}

func (tracker *registerAttemptTracker) setSink(sink diagnosis.DiagnosticSink) {
	if sink == nil {
		sink = diagnosis.NoopSink{}
	}
	tracker.mu.Lock()
	tracker.sink = sink
	tracker.mu.Unlock()
}

func (tracker *registerAttemptTracker) start(event diagnosis.Event) {
	tracker.startChallenge(event, "")
}

func (tracker *registerAttemptTracker) startChallenge(event diagnosis.Event, nonce string) {
	now := tracker.now().UTC()
	event.ObservedAt = now
	attempt := &registerAttempt{event: event, started: now, deadline: now.Add(tracker.window)}
	if nonce != "" {
		attempt.nonceSum = sha256.Sum256([]byte(nonce))
	}
	tracker.mu.Lock()
	if current := tracker.attempts[event.CorrelationKey]; current != nil && current.timer != nil {
		current.timer.Stop()
	}
	if len(tracker.attempts) >= tracker.max {
		tracker.evictOldestLocked()
	}
	tracker.attempts[event.CorrelationKey] = attempt
	attempt.timer = tracker.after(tracker.window, func() { tracker.expire(event.CorrelationKey) })
	tracker.mu.Unlock()
}

func (tracker *registerAttemptTracker) hasChallenge(deviceID, nonce string) bool {
	if deviceID == "" || nonce == "" {
		return false
	}
	sum := sha256.Sum256([]byte(nonce))
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	for _, attempt := range tracker.attempts {
		if attempt.event.DeviceID == deviceID && attempt.nonceSum == sum {
			return true
		}
	}
	return false
}

func (tracker *registerAttemptTracker) expire(key string) bool {
	tracker.mu.Lock()
	attempt := tracker.attempts[key]
	if attempt == nil || attempt.timedOut {
		tracker.mu.Unlock()
		return false
	}
	attempt.timedOut = true
	event := attempt.event
	event.ObservedAt = tracker.now().UTC()
	event.State = diagnosis.StateActive
	event.Code = diagnosis.CodeRegisterTimeout
	event.StatusCode = 0
	sink := tracker.sink
	tracker.mu.Unlock()
	_ = sink.Emit(context.Background(), event)
	return true
}

func (tracker *registerAttemptTracker) finish(key string, completedAt time.Time) {
	completedAt = completedAt.UTC()
	tracker.mu.Lock()
	attempt := tracker.attempts[key]
	if attempt == nil {
		tracker.mu.Unlock()
		return
	}
	if attempt.timer != nil {
		attempt.timer.Stop()
	}
	delete(tracker.attempts, key)
	shouldResolve := attempt.timedOut && !completedAt.After(attempt.deadline)
	event := attempt.event
	event.ObservedAt = completedAt
	event.State = diagnosis.StateResolved
	event.Code = diagnosis.CodeRegisterTimeout
	event.StatusCode = 200
	event.ResolvedAt = &completedAt
	sink := tracker.sink
	tracker.mu.Unlock()
	if shouldResolve {
		_ = sink.Emit(context.Background(), event)
	}
}

func (tracker *registerAttemptTracker) fail(key string) {
	tracker.mu.Lock()
	attempt := tracker.attempts[key]
	if attempt != nil {
		if attempt.timer != nil {
			attempt.timer.Stop()
		}
		delete(tracker.attempts, key)
	}
	tracker.mu.Unlock()
}

func (tracker *registerAttemptTracker) failByCallID(callID string) {
	tracker.mu.Lock()
	for key, attempt := range tracker.attempts {
		if attempt.event.CallID != callID {
			continue
		}
		if attempt.timer != nil {
			attempt.timer.Stop()
		}
		delete(tracker.attempts, key)
	}
	tracker.mu.Unlock()
}

func (tracker *registerAttemptTracker) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for key, attempt := range tracker.attempts {
		if oldestKey == "" || attempt.started.Before(oldest) {
			oldestKey = key
			oldest = attempt.started
		}
	}
	if oldestKey == "" {
		return
	}
	if attempt := tracker.attempts[oldestKey]; attempt.timer != nil {
		attempt.timer.Stop()
	}
	delete(tracker.attempts, oldestKey)
}
