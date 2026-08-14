package diagnosis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type serviceRepository struct {
	mu       sync.Mutex
	records  []Record
	writeErr error
	block    <-chan struct{}
	prunedAt time.Time
	pruneN   int
	started  chan struct{}
}

func (repository *serviceRepository) UpsertBatch(ctx context.Context, records []Record) error {
	if repository.started != nil {
		select {
		case repository.started <- struct{}{}:
		default:
		}
	}
	if repository.block != nil {
		select {
		case <-repository.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.writeErr != nil {
		return repository.writeErr
	}
	repository.records = append(repository.records, records...)
	return nil
}

func (*serviceRepository) Query(context.Context, DiagnosisFilter) ([]Record, error) {
	return nil, nil
}

func (*serviceRepository) FindByEvidence(context.Context, time.Time, time.Time, string, uint32) ([]Record, error) {
	return nil, nil
}

func (repository *serviceRepository) Prune(_ context.Context, cutoff time.Time, batch int) (int64, error) {
	repository.mu.Lock()
	repository.prunedAt = cutoff
	repository.pruneN = batch
	repository.mu.Unlock()
	return 1, nil
}

func TestDiagnosisServiceQueuesAndFlushes(t *testing.T) {
	repository := &serviceRepository{}
	service := newTestDiagnosisService(t, repository, ServiceConfig{QueueCapacity: 8, BatchSize: 2, FlushInterval: time.Hour})
	defer stopDiagnosisService(t, service)

	require.NoError(t, service.Emit(context.Background(), validEvent()))
	second := validEvent()
	second.CorrelationKey = "second"
	require.NoError(t, service.Emit(context.Background(), second))
	require.Eventually(t, func() bool {
		repository.mu.Lock()
		defer repository.mu.Unlock()
		return len(repository.records) == 2
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, HealthReady, service.Health().State)
}

func TestDiagnosisServiceQueueFullDoesNotBlock(t *testing.T) {
	block := make(chan struct{})
	repository := &serviceRepository{block: block, started: make(chan struct{}, 1)}
	service := newTestDiagnosisService(t, repository, ServiceConfig{QueueCapacity: 1, BatchSize: 1, FlushInterval: time.Hour})
	defer close(block)
	defer stopDiagnosisService(t, service)

	require.NoError(t, service.Emit(context.Background(), validEvent()))
	<-repository.started
	second := validEvent()
	second.CorrelationKey = "second"
	require.NoError(t, service.Emit(context.Background(), second))
	third := validEvent()
	third.CorrelationKey = "third"
	started := time.Now()
	err := service.Emit(context.Background(), third)
	require.ErrorIs(t, err, ErrQueueFull)
	require.Less(t, time.Since(started), 50*time.Millisecond)
	health := service.Health()
	require.Equal(t, HealthDegraded, health.State)
	require.Equal(t, uint64(1), health.Dropped)
}

func TestDiagnosisServiceFailureAndRecovery(t *testing.T) {
	repository := &serviceRepository{writeErr: errors.New("database unavailable")}
	service := newTestDiagnosisService(t, repository, ServiceConfig{QueueCapacity: 4, BatchSize: 1, FlushInterval: time.Millisecond})
	defer stopDiagnosisService(t, service)

	require.NoError(t, service.Emit(context.Background(), validEvent()))
	// 修复后契约:批写失败保留重试,每次失败尝试累计 Failed;短暂窗口内 >=1
	require.Eventually(t, func() bool { return service.Health().Failed >= 1 }, time.Second, 10*time.Millisecond)
	require.Equal(t, HealthDegraded, service.Health().State)

	repository.mu.Lock()
	repository.writeErr = nil
	repository.mu.Unlock()
	second := validEvent()
	second.CorrelationKey = "recovered"
	require.NoError(t, service.Emit(context.Background(), second))
	require.Eventually(t, func() bool { return service.Health().State == HealthReady }, time.Second, 10*time.Millisecond)
}

func TestDiagnosisServiceRejectsInvalidEvent(t *testing.T) {
	repository := &serviceRepository{}
	service := newTestDiagnosisService(t, repository, ServiceConfig{QueueCapacity: 1, BatchSize: 1})
	defer stopDiagnosisService(t, service)

	err := service.Emit(context.Background(), Event{})
	require.Error(t, err)
	require.Zero(t, service.Health().Failed)
}

func TestDiagnosisServiceStopDrainsAndIsBounded(t *testing.T) {
	repository := &serviceRepository{}
	service := newTestDiagnosisService(t, repository, ServiceConfig{QueueCapacity: 4, BatchSize: 4, FlushInterval: time.Hour})
	require.NoError(t, service.Emit(context.Background(), validEvent()))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, service.Stop(ctx))
	repository.mu.Lock()
	require.Len(t, repository.records, 1)
	repository.mu.Unlock()

	blockedRepository := &serviceRepository{block: make(chan struct{}), started: make(chan struct{}, 1)}
	blocked := newTestDiagnosisService(t, blockedRepository, ServiceConfig{QueueCapacity: 2, BatchSize: 1, FlushInterval: time.Hour})
	require.NoError(t, blocked.Emit(context.Background(), validEvent()))
	<-blockedRepository.started
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stopCancel()
	require.ErrorIs(t, blocked.Stop(stopCtx), context.DeadlineExceeded)
}

func TestDiagnosisServiceConcurrentEmitAndStop(t *testing.T) {
	repository := &serviceRepository{}
	service := newTestDiagnosisService(t, repository, ServiceConfig{QueueCapacity: 64, BatchSize: 8})
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			for n := 0; n < 100; n++ {
				event := validEvent()
				event.CorrelationKey = fmt.Sprintf("%d-%d", index, n)
				err := service.Emit(context.Background(), event)
				if err != nil && !errors.Is(err, ErrQueueFull) && !errors.Is(err, ErrServiceStopped) {
					t.Errorf("Emit: %v", err)
					return
				}
			}
		}(i)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, service.Stop(ctx))
	wait.Wait()
}

func TestDiagnosisServicePruneUsesRetention(t *testing.T) {
	now := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	repository := &serviceRepository{}
	service := newTestDiagnosisService(t, repository, ServiceConfig{
		QueueCapacity: 1, BatchSize: 1, Retention: 7 * 24 * time.Hour,
		PruneBatchSize: 25, Now: func() time.Time { return now },
	})
	defer stopDiagnosisService(t, service)

	deleted, err := service.PruneOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	repository.mu.Lock()
	require.Equal(t, now.Add(-7*24*time.Hour), repository.prunedAt)
	require.Equal(t, 25, repository.pruneN)
	repository.mu.Unlock()
}

func newTestDiagnosisService(t *testing.T, repository Repository, config ServiceConfig) *Service {
	t.Helper()
	service, err := NewService(repository, config)
	require.NoError(t, err)
	return service
}

func stopDiagnosisService(t *testing.T, service *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = service.Stop(ctx)
}
