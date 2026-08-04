package playback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	defaultIdleTimeout = time.Minute
	defaultMaxSession  = 30 * time.Minute
)

type sessionRecord struct {
	mu          sync.Mutex
	session     Session
	stopStarted bool
	stopDone    chan struct{}
	stopErr     error
}

type Registry struct {
	mu              sync.RWMutex
	closed          bool
	now             func() time.Time
	idleTimeout     time.Duration
	maxSession      time.Duration
	sessions        map[string]*sessionRecord
	activeByScope   map[string]string
	idempotentByKey map[string]string
	cleanup         CleanupRunner
	closeDone       chan struct{}
	closeErr        error
}

func NewRegistry(config RegistryConfig) *Registry {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	idle := config.IdleTimeout
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	maxSession := config.MaxSession
	if maxSession <= 0 {
		maxSession = defaultMaxSession
	}
	return &Registry{
		now: now, idleTimeout: idle, maxSession: maxSession,
		sessions:      make(map[string]*sessionRecord),
		activeByScope: make(map[string]string), idempotentByKey: make(map[string]string),
	}
}

func scopeKey(owner, channel string) string { return owner + "\x00" + channel }
func idempotencyKey(owner, channel, key string) string {
	return scopeKey(owner, channel) + "\x00" + key
}

func newSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func (r *Registry) Create(ctx context.Context, request CreateRequest) (CreateResult, error) {
	if err := ctx.Err(); err != nil {
		return CreateResult{}, err
	}
	if strings.TrimSpace(request.OwnerID) == "" || strings.TrimSpace(request.ChannelID) == "" || strings.TrimSpace(request.RecordKey) == "" ||
		request.SegmentEnd.IsZero() || !request.SegmentEnd.After(request.SegmentStart) {
		return CreateResult{}, ErrInvalidSession
	}
	now := request.Now
	if now.IsZero() {
		now = r.now()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return CreateResult{}, ErrRegistryClosed
	}
	if request.IdempotencyKey != "" {
		if id := r.idempotentByKey[idempotencyKey(request.OwnerID, request.ChannelID, request.IdempotencyKey)]; id != "" {
			if record := r.sessions[id]; record != nil {
				record.mu.Lock()
				session := record.session.clone()
				record.mu.Unlock()
				return CreateResult{Session: session, Existing: true}, nil
			}
		}
	}
	scope := scopeKey(request.OwnerID, request.ChannelID)
	if r.activeByScope[scope] != "" {
		return CreateResult{}, ErrPlaybackBusy
	}
	id, err := newSessionID()
	if err != nil {
		return CreateResult{}, fmt.Errorf("generate playback session id: %w", err)
	}
	session := Session{ID: id, OwnerID: request.OwnerID, DeviceID: request.DeviceID, ChannelID: request.ChannelID, RecordKey: request.RecordKey,
		IdempotencyKey: request.IdempotencyKey, SegmentStart: request.SegmentStart, SegmentEnd: request.SegmentEnd,
		PlayFrom: request.PlayFrom,
		State:    StateCreating, Scale: 1, CreatedAt: now, LastActivityAt: now,
		IdleDeadline: now.Add(r.idleTimeout), Deadline: now.Add(r.maxSession), Resources: request.Resources}
	record := &sessionRecord{session: session}
	r.sessions[id] = record
	r.activeByScope[scope] = id
	if request.IdempotencyKey != "" {
		r.idempotentByKey[idempotencyKey(request.OwnerID, request.ChannelID, request.IdempotencyKey)] = id
	}
	return CreateResult{Session: session.clone()}, nil
}

func (r *Registry) record(id string) (*sessionRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.sessions[id]
	return record, ok
}

func (r *Registry) Get(id string) (*Session, bool) {
	record, ok := r.record(id)
	if !ok {
		return nil, false
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	return record.session.clone(), true
}

func (r *Registry) GetForOwner(id, ownerID string) (*Session, bool) {
	session, ok := r.Get(id)
	if !ok || session.OwnerID != ownerID {
		return nil, false
	}
	return session, true
}

func (r *Registry) GetByCallID(callID string) (*Session, bool) {
	if strings.TrimSpace(callID) == "" {
		return nil, false
	}
	r.mu.RLock()
	records := make([]*sessionRecord, 0, len(r.sessions))
	for _, record := range r.sessions {
		records = append(records, record)
	}
	r.mu.RUnlock()
	for _, record := range records {
		record.mu.Lock()
		match := record.session.CallID == callID
		session := record.session.clone()
		record.mu.Unlock()
		if match {
			return session, true
		}
	}
	return nil, false
}

func (r *Registry) GetByStreamID(streamID string) (*Session, bool) {
	if strings.TrimSpace(streamID) == "" {
		return nil, false
	}
	r.mu.RLock()
	records := make([]*sessionRecord, 0, len(r.sessions))
	for _, record := range r.sessions {
		records = append(records, record)
	}
	r.mu.RUnlock()
	for _, record := range records {
		record.mu.Lock()
		match := record.session.StreamID == streamID
		session := record.session.clone()
		record.mu.Unlock()
		if match {
			return session, true
		}
	}
	return nil, false
}

func (r *Registry) MustGet(id string) *Session {
	session, ok := r.Get(id)
	if !ok {
		panic("playback session missing: " + id)
	}
	return session
}

func (r *Registry) Touch(id string, at time.Time) error {
	record, ok := r.record(id)
	if !ok {
		return ErrPlaybackNotFound
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	if record.session.State.IsTerminal() {
		return nil
	}
	record.session.LastActivityAt = at
	record.session.IdleDeadline = at.Add(r.idleTimeout)
	return nil
}

func (r *Registry) Update(id string, update func(*Session) error) error {
	if update == nil {
		return nil
	}
	record, ok := r.record(id)
	if !ok {
		return ErrPlaybackNotFound
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	if record.session.State.IsTerminal() {
		return ErrPlaybackNotFound
	}
	return update(&record.session)
}

func validTransition(from, to State) bool {
	switch from {
	case StateCreating:
		return to == StateBuffering || to == StateFailed || to == StateStopping
	case StateBuffering:
		return to == StatePlaying || to == StateFailed || to == StateStopping
	case StatePlaying:
		return to == StatePaused || to == StateEnded || to == StateFailed || to == StateStopping
	case StatePaused:
		return to == StatePlaying || to == StateEnded || to == StateFailed || to == StateStopping
	case StateStopping:
		return to == StateStopped || to == StateFailed
	default:
		return false
	}
}

func (r *Registry) Transition(id string, to State) error {
	record, ok := r.record(id)
	if !ok {
		return ErrPlaybackNotFound
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	if !validTransition(record.session.State, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, record.session.State, to)
	}
	record.session.State = to
	record.session.LastActivityAt = r.now()
	return nil
}

func (r *Registry) Finalize(id string, terminal State, reason string) error {
	return r.FinalizeContext(context.Background(), id, terminal, reason)
}

func (r *Registry) FinalizeContext(ctx context.Context, id string, terminal State, reason string) error {
	_, err := r.FinalizeContextOnce(ctx, id, terminal, reason)
	return err
}

func (r *Registry) FinalizeContextOnce(ctx context.Context, id string, terminal State, reason string) (bool, error) {
	if !terminal.IsTerminal() {
		return false, fmt.Errorf("%w: %s is not terminal", ErrInvalidTransition, terminal)
	}
	record, ok := r.record(id)
	if !ok {
		return false, ErrPlaybackNotFound
	}
	return r.stopRecord(ctx, record, reason, terminal)
}

func (r *Registry) removeActive(session Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activeByScope[scopeKey(session.OwnerID, session.ChannelID)] == session.ID {
		delete(r.activeByScope, scopeKey(session.OwnerID, session.ChannelID))
	}
}

func (r *Registry) stopRecord(ctx context.Context, record *sessionRecord, reason string, terminal State) (bool, error) {
	record.mu.Lock()
	if record.session.State.IsTerminal() {
		err := record.stopErr
		record.mu.Unlock()
		return false, err
	}
	if record.stopStarted {
		done := record.stopDone
		record.mu.Unlock()
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-done:
			record.mu.Lock()
			err := record.stopErr
			record.mu.Unlock()
			return false, err
		}
	}
	record.stopStarted, record.stopDone = true, make(chan struct{})
	record.session.State = StateStopping
	resources := record.session.Resources
	session := record.session
	done := record.stopDone
	record.mu.Unlock()

	err := r.cleanup.Run(ctx, resources)
	record.mu.Lock()
	record.session.State = terminal
	record.session.EndReason = reason
	if err != nil {
		record.session.Error, record.session.EndReason = err, reason+": "+err.Error()
	}
	record.stopErr = err
	close(done)
	record.mu.Unlock()
	r.removeActive(session)
	return true, err
}

func (r *Registry) Stop(ctx context.Context, id, reason string) error {
	_, err := r.StopOnce(ctx, id, reason)
	return err
}

func (r *Registry) StopOnce(ctx context.Context, id, reason string) (bool, error) {
	record, ok := r.record(id)
	if !ok {
		return false, ErrPlaybackNotFound
	}
	return r.stopRecord(ctx, record, reason, StateStopped)
}

func (r *Registry) StopForOwner(ctx context.Context, id, ownerID, reason string) error {
	_, err := r.StopForOwnerOnce(ctx, id, ownerID, reason)
	return err
}

func (r *Registry) StopForOwnerOnce(ctx context.Context, id, ownerID, reason string) (bool, error) {
	record, ok := r.record(id)
	if !ok {
		return false, ErrPlaybackNotFound
	}
	record.mu.Lock()
	ownerMatches := record.session.OwnerID == ownerID
	record.mu.Unlock()
	if !ownerMatches {
		return false, ErrPlaybackNotFound
	}
	return r.stopRecord(ctx, record, reason, StateStopped)
}

func (r *Registry) Sweep(ctx context.Context, at time.Time) error {
	_, err := r.SweepOnce(ctx, at)
	return err
}

func (r *Registry) SweepOnce(ctx context.Context, at time.Time) (int, error) {
	if at.IsZero() {
		at = r.now()
	}
	r.mu.RLock()
	allRecords := make([]*sessionRecord, 0, len(r.sessions))
	for _, record := range r.sessions {
		allRecords = append(allRecords, record)
	}
	r.mu.RUnlock()
	records := make([]*sessionRecord, 0, len(allRecords))
	for _, record := range allRecords {
		record.mu.Lock()
		expired := !record.session.State.IsTerminal() && (at.After(record.session.IdleDeadline) || !at.Before(record.session.Deadline))
		record.mu.Unlock()
		if expired {
			records = append(records, record)
		}
	}
	var result error
	cleaned := 0
	for _, record := range records {
		started, err := r.stopRecord(ctx, record, "playback session expired", StateStopped)
		if started {
			cleaned++
		}
		result = errors.Join(result, err)
	}
	return cleaned, result
}

func (r *Registry) Close(ctx context.Context) error {
	_, err := r.CloseOnce(ctx)
	return err
}

func (r *Registry) CloseOnce(ctx context.Context) (int, error) {
	r.mu.Lock()
	if r.closed {
		done := r.closeDone
		r.mu.Unlock()
		if done != nil {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-done:
			}
		}
		r.mu.RLock()
		err := r.closeErr
		r.mu.RUnlock()
		return 0, err
	}
	r.closed = true
	r.closeDone = make(chan struct{})
	records := make([]*sessionRecord, 0, len(r.sessions))
	for _, record := range r.sessions {
		records = append(records, record)
	}
	r.mu.Unlock()
	var result error
	cleaned := 0
	for _, record := range records {
		started, err := r.stopRecord(ctx, record, "playback registry closed", StateStopped)
		if started {
			cleaned++
		}
		result = errors.Join(result, err)
	}
	r.mu.Lock()
	r.sessions = make(map[string]*sessionRecord)
	r.activeByScope = make(map[string]string)
	r.idempotentByKey = make(map[string]string)
	r.closeErr = result
	close(r.closeDone)
	r.mu.Unlock()
	return cleaned, result
}

func (r *Registry) ActiveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.activeByScope)
}

func (r *Registry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}
