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
	// defaultTerminalTTL 终态会话默认保留 30 分钟,足够前端查询结果,
	// 又不让 sessions 表随回放次数无界增长
	defaultTerminalTTL = 30 * time.Minute
)

type sessionRecord struct {
	mu              sync.Mutex
	session         Session
	stopCall        *cleanupCall
	pendingTerminal State
	pendingReason   string
}

// A call owns its immutable result after done closes. Later retries must not
// overwrite the result observed by waiters of a previous attempt.
type cleanupCall struct {
	done chan struct{}
	err  error
}

type Registry struct {
	mu              sync.RWMutex
	closed          bool
	now             func() time.Time
	idleTimeout     time.Duration
	maxSession      time.Duration
	terminalTTL     time.Duration
	sessions        map[string]*sessionRecord
	activeByScope   map[string]string
	idempotentByKey map[string]string
	cleanup         CleanupRunner
	closeCall       *cleanupCall
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
	terminalTTL := config.TerminalTTL
	if terminalTTL <= 0 {
		terminalTTL = defaultTerminalTTL
	}
	return &Registry{
		now: now, idleTimeout: idle, maxSession: maxSession, terminalTTL: terminalTTL,
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
	if request.Mode == "" {
		request.Mode = ModePlayback
	}
	if request.Mode != ModePlayback && request.Mode != ModeDownload {
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
	if activeID := r.activeByScope[scope]; activeID != "" {
		if record := r.sessions[activeID]; record != nil {
			record.mu.Lock()
			active := record.session.clone()
			record.mu.Unlock()
			if !active.State.IsTerminal() && active.DeviceID == request.DeviceID && active.Mode == request.Mode &&
				active.SegmentStart.Equal(request.SegmentStart) && active.SegmentEnd.Equal(request.SegmentEnd) {
				if request.IdempotencyKey != "" {
					r.idempotentByKey[idempotencyKey(request.OwnerID, request.ChannelID, request.IdempotencyKey)] = activeID
				}
				return CreateResult{Session: active, Existing: true}, nil
			}
		}
		return CreateResult{}, ErrPlaybackBusy
	}
	id, err := newSessionID()
	if err != nil {
		return CreateResult{}, fmt.Errorf("generate playback session id: %w", err)
	}
	idleDeadline := now.Add(r.idleTimeout)
	if request.Mode == ModeDownload {
		// 下载没有播放器轮询或控制动作可用于续租；以任务总时限作为
		// deadline，避免长录像在传输途中被按“无人观看”回收。
		idleDeadline = now.Add(r.maxSession)
	}
	session := Session{ID: id, OwnerID: request.OwnerID, DeviceID: request.DeviceID, ChannelID: request.ChannelID, RecordKey: request.RecordKey,
		Mode: request.Mode, DownloadSpeed: request.DownloadSpeed,
		IdempotencyKey: request.IdempotencyKey, SegmentStart: request.SegmentStart, SegmentEnd: request.SegmentEnd,
		PlayFrom: request.PlayFrom,
		State:    StateCreating, Scale: 1, CreatedAt: now, LastActivityAt: now,
		IdleDeadline: idleDeadline, Deadline: now.Add(r.maxSession), Resources: request.Resources}
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
	for key, id := range r.idempotentByKey {
		if id == session.ID {
			delete(r.idempotentByKey, key)
		}
	}
}

func (r *Registry) stopRecord(ctx context.Context, record *sessionRecord, reason string, terminal State) (bool, error) {
	record.mu.Lock()
	if record.session.State.IsTerminal() {
		record.mu.Unlock()
		return false, nil
	}
	if call := record.stopCall; call != nil {
		record.mu.Unlock()
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-call.done:
			return false, call.err
		}
	}
	if record.pendingTerminal == "" {
		record.pendingTerminal, record.pendingReason = terminal, reason
	}
	call := &cleanupCall{done: make(chan struct{})}
	record.stopCall = call
	record.session.State = StateStopping
	resources := record.session.Resources
	session := record.session
	record.mu.Unlock()

	err := r.cleanup.Run(ctx, resources)
	record.mu.Lock()
	record.session.LastActivityAt = r.now()
	record.session.EndReason = record.pendingReason
	if err != nil {
		record.session.EndReason += ": " + err.Error()
	} else {
		record.session.State = record.pendingTerminal
		record.pendingTerminal, record.pendingReason = "", ""
	}
	call.err = err
	record.stopCall = nil
	close(call.done)
	record.mu.Unlock()
	if err != nil {
		return false, err
	}
	r.removeActive(session)
	r.pruneTerminal(r.now())
	return true, err
}

// pruneTerminal 删除超龄的终态记录,防止 sessions 表随回放次数无界增长.
// 机会式执行:每次有会话终止时顺带清理一次.
func (r *Registry) pruneTerminal(now time.Time) {
	cutoff := now.Add(-r.terminalTTL)
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, record := range r.sessions {
		record.mu.Lock()
		terminal := record.session.State.IsTerminal()
		lastActive := record.session.LastActivityAt
		record.mu.Unlock()
		if terminal && !lastActive.IsZero() && lastActive.Before(cutoff) {
			delete(r.sessions, id)
		}
	}
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
	if call := r.closeCall; call != nil {
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-call.done:
			return 0, call.err
		}
	}
	r.closed = true
	call := &cleanupCall{done: make(chan struct{})}
	r.closeCall = call
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
	if result == nil {
		r.sessions = make(map[string]*sessionRecord)
		r.activeByScope = make(map[string]string)
		r.idempotentByKey = make(map[string]string)
	}
	call.err = result
	r.closeCall = nil
	close(call.done)
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
