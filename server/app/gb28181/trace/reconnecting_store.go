package trace

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrTraceStoreUnavailable = errors.New("SIP trace store is unavailable")

// StoreFactory opens and fully initializes one store connection, including
// schema checks. It is deliberately injectable so cold-start and recovery can
// be tested without a database process.
type StoreFactory func(context.Context) (Store, error)

type ReconnectingStore struct {
	mu         sync.RWMutex
	factory    StoreFactory
	current    Store
	nextTry    time.Time
	backoff    time.Duration
	minBackoff time.Duration
	maxBackoff time.Duration
}

func NewReconnectingStore(factory StoreFactory, minBackoff, maxBackoff time.Duration) *ReconnectingStore {
	if minBackoff <= 0 {
		minBackoff = 250 * time.Millisecond
	}
	if maxBackoff < minBackoff {
		maxBackoff = 30 * time.Second
	}
	return &ReconnectingStore{factory: factory, minBackoff: minBackoff, maxBackoff: maxBackoff, backoff: minBackoff}
}

func (s *ReconnectingStore) ensure(ctx context.Context) (Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		return s.current, nil
	}
	if s.factory == nil || (!s.nextTry.IsZero() && time.Now().Before(s.nextTry)) {
		return nil, ErrTraceStoreUnavailable
	}
	store, err := s.factory(ctx)
	if err != nil {
		s.nextTry = time.Now().Add(s.backoff)
		s.backoff *= 2
		if s.backoff > s.maxBackoff {
			s.backoff = s.maxBackoff
		}
		return nil, err
	}
	s.current = store
	s.nextTry = time.Time{}
	s.backoff = s.minBackoff
	return store, nil
}

func (s *ReconnectingStore) markBroken(store Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != store {
		return
	}
	s.current = nil
	s.nextTry = time.Now().Add(s.backoff)
	if closer, ok := store.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

func (s *ReconnectingStore) InsertBatch(ctx context.Context, events []StoredEvent) error {
	store, err := s.ensure(ctx)
	if err != nil {
		return err
	}
	if err := store.InsertBatch(ctx, events); err != nil {
		s.markBroken(store)
		return err
	}
	return nil
}

// repository 触发 lazy dial 后返回 QueryRepository。
// 之前只在 InsertBatch 里 ensure,导致 SIP 未采集到任何报文前 UI 查询永远 503。
func (s *ReconnectingStore) repository(ctx context.Context) (QueryRepository, error) {
	store, err := s.ensure(ctx)
	if err != nil {
		return nil, err
	}
	repository, ok := store.(QueryRepository)
	if !ok {
		return nil, ErrTraceStoreUnavailable
	}
	return repository, nil
}

func (s *ReconnectingStore) callRepo(ctx context.Context, fn func(QueryRepository) error) error {
	repository, err := s.repository(ctx)
	if err != nil {
		return err
	}
	if err := fn(repository); err != nil {
		s.markBroken(s.current)
		return err
	}
	return nil
}

func (s *ReconnectingStore) ListMessages(ctx context.Context, filter MessageFilter) (MessagePage, error) {
	var page MessagePage
	err := s.callRepo(ctx, func(r QueryRepository) error {
		var e error
		page, e = r.ListMessages(ctx, filter)
		return e
	})
	return page, err
}

func (s *ReconnectingStore) GetMessage(ctx context.Context, eventID string) (StoredMessage, error) {
	var msg StoredMessage
	err := s.callRepo(ctx, func(r QueryRepository) error {
		var e error
		msg, e = r.GetMessage(ctx, eventID)
		return e
	})
	return msg, err
}

func (s *ReconnectingStore) ListSessions(ctx context.Context, filter SessionFilter) ([]SessionSummary, error) {
	var list []SessionSummary
	err := s.callRepo(ctx, func(r QueryRepository) error {
		var e error
		list, e = r.ListSessions(ctx, filter)
		return e
	})
	return list, err
}

func (s *ReconnectingStore) GetSessionStats(ctx context.Context, filter SessionFilter) (SessionStats, error) {
	var stats SessionStats
	err := s.callRepo(ctx, func(r QueryRepository) error {
		var e error
		stats, e = r.GetSessionStats(ctx, filter)
		return e
	})
	return stats, err
}

func (s *ReconnectingStore) Prune(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	store, err := s.ensure(ctx)
	if err != nil {
		return 0, err
	}
	prunable, ok := store.(PrunableStore)
	if !ok {
		return 0, ErrTraceStoreUnavailable
	}
	deleted, err := prunable.Prune(ctx, cutoff, batchSize)
	if err != nil {
		s.markBroken(store)
	}
	return deleted, err
}

func (s *ReconnectingStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil
	}
	if closer, ok := s.current.(interface{ Close() error }); ok {
		s.current = nil
		return closer.Close()
	}
	s.current = nil
	return nil
}
