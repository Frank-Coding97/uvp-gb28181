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
// be tested without a ClickHouse process.
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

func (s *ReconnectingStore) repository() (QueryRepository, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil {
		return nil, ErrTraceStoreUnavailable
	}
	repository, ok := s.current.(QueryRepository)
	if !ok {
		return nil, ErrTraceStoreUnavailable
	}
	return repository, nil
}

func (s *ReconnectingStore) ListMessages(ctx context.Context, filter MessageFilter) (MessagePage, error) {
	repository, err := s.repository()
	if err != nil {
		return MessagePage{}, err
	}
	return repository.ListMessages(ctx, filter)
}

func (s *ReconnectingStore) GetMessage(ctx context.Context, eventID string) (StoredMessage, error) {
	repository, err := s.repository()
	if err != nil {
		return StoredMessage{}, err
	}
	return repository.GetMessage(ctx, eventID)
}

func (s *ReconnectingStore) ListSessions(ctx context.Context, filter SessionFilter) ([]SessionSummary, error) {
	repository, err := s.repository()
	if err != nil {
		return nil, err
	}
	return repository.ListSessions(ctx, filter)
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
