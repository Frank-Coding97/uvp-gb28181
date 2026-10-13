package diagnosis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrQueueFull      = errors.New("diagnosis queue is full")
	ErrServiceStopped = errors.New("diagnosis service is stopped")
)

type ServiceConfig struct {
	QueueCapacity  int
	BatchSize      int
	FlushInterval  time.Duration
	Retention      time.Duration
	PruneBatchSize int
	Now            func() time.Time
}

type Service struct {
	repository    Repository
	queue         chan Event
	batchSize     int
	flushInterval time.Duration
	retention     time.Duration
	pruneBatch    int
	now           func() time.Time
	health        *healthTracker
	ctx           context.Context
	cancel        context.CancelFunc
	done          chan struct{}
	closed        atomic.Bool
	enqueueMu     sync.RWMutex
	stopOnce      sync.Once
}

func NewService(repository Repository, config ServiceConfig) (*Service, error) {
	if repository == nil {
		return nil, ErrRepositoryUnavailable
	}
	if config.QueueCapacity <= 0 {
		config.QueueCapacity = 256
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 50
	}
	if config.BatchSize > config.QueueCapacity {
		config.BatchSize = config.QueueCapacity
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 500 * time.Millisecond
	}
	if config.Retention <= 0 {
		config.Retention = 7 * 24 * time.Hour
	}
	if config.PruneBatchSize <= 0 {
		config.PruneBatchSize = 500
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		repository: repository, queue: make(chan Event, config.QueueCapacity),
		batchSize: config.BatchSize, flushInterval: config.FlushInterval,
		retention: config.Retention, pruneBatch: config.PruneBatchSize, now: config.Now,
		health: newHealthTracker(), ctx: ctx, cancel: cancel, done: make(chan struct{}),
	}
	go service.runWorker()
	return service, nil
}

func (service *Service) Emit(_ context.Context, event Event) error {
	if service == nil {
		return ErrServiceStopped
	}
	if err := event.Validate(); err != nil {
		return err
	}
	service.enqueueMu.RLock()
	defer service.enqueueMu.RUnlock()
	if service.closed.Load() {
		return ErrServiceStopped
	}
	select {
	case service.queue <- event:
		return nil
	default:
		service.health.droppedEvent(ErrQueueFull.Error(), 1)
		return ErrQueueFull
	}
}

func (service *Service) Health() HealthSnapshot {
	if service == nil || service.health == nil {
		return DisabledHealth()
	}
	return service.health.snapshot(len(service.queue), cap(service.queue))
}

func (service *Service) PruneOnce(ctx context.Context) (int64, error) {
	if service == nil || service.repository == nil {
		return 0, ErrRepositoryUnavailable
	}
	cutoff := service.now().UTC().Add(-service.retention)
	deleted, err := service.repository.Prune(ctx, cutoff, service.pruneBatch)
	if err != nil {
		service.health.failedBatch(err, 0)
		return deleted, fmt.Errorf("prune diagnosis: %w", err)
	}
	return deleted, nil
}

func (service *Service) Prune(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if service == nil || service.repository == nil {
		return 0, ErrRepositoryUnavailable
	}
	deleted, err := service.repository.Prune(ctx, cutoff.UTC(), batchSize)
	if err != nil {
		service.health.failedBatch(err, 0)
		return deleted, fmt.Errorf("prune diagnosis: %w", err)
	}
	return deleted, nil
}

func (service *Service) Stop(ctx context.Context) error {
	if service == nil {
		return nil
	}
	service.stopOnce.Do(func() {
		service.enqueueMu.Lock()
		defer service.enqueueMu.Unlock()
		service.closed.Store(true)
		close(service.queue)
	})
	select {
	case <-service.done:
		service.cancel()
		return nil
	case <-ctx.Done():
		service.cancel()
		remaining := len(service.queue)
		if remaining > 0 {
			service.health.droppedEvent("diagnosis shutdown deadline exceeded", uint64(remaining))
		}
		return ctx.Err()
	}
}

func (service *Service) MarkDegraded(err error) {
	if service == nil || err == nil {
		return
	}
	service.health.failedBatch(err, 0)
}
