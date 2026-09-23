package trace

import (
	"context"
	"errors"
	"fmt"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/trace/diagnosis"
)

type combinedPrunableStore struct {
	trace     PrunableStore
	diagnosis *diagnosis.Service
}

func (store combinedPrunableStore) Prune(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	traceDeleted, traceErr := store.trace.Prune(ctx, cutoff, batchSize)
	diagnosisDeleted, diagnosisErr := store.diagnosis.Prune(ctx, cutoff, batchSize)
	return traceDeleted + diagnosisDeleted, errors.Join(traceErr, diagnosisErr)
}

const (
	DefaultTracePruneBatchSize = 500
	DefaultTracePruneTimeout   = 30 * time.Second
)

type PrunableStore interface {
	Prune(context.Context, time.Time, int) (int64, error)
}

type TracePruner struct {
	store         PrunableStore
	retentionDays int
	batchSize     int
	now           func() time.Time
	timeout       time.Duration
}

func NewTracePruner(store PrunableStore, retentionDays, batchSize int, now func() time.Time) *TracePruner {
	if retentionDays <= 0 {
		retentionDays = 7
	}
	if batchSize <= 0 {
		batchSize = DefaultTracePruneBatchSize
	}
	if now == nil {
		now = time.Now
	}
	return &TracePruner{store: store, retentionDays: retentionDays, batchSize: batchSize, now: now, timeout: DefaultTracePruneTimeout}
}

func (p *TracePruner) PruneOnce(ctx context.Context) (int64, error) {
	if p == nil || p.store == nil {
		return 0, ErrTraceStoreUnavailable
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	pruneCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	cutoff := p.now().UTC().Add(-time.Duration(p.retentionDays) * 24 * time.Hour)
	deleted, err := p.store.Prune(pruneCtx, cutoff, p.batchSize)
	if err != nil {
		return deleted, fmt.Errorf("prune SIP trace messages: %w", err)
	}
	return deleted, nil
}

func (p *TracePruner) Run(ctx context.Context, interval time.Duration, onError func(error)) {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := p.PruneOnce(ctx); err != nil && onError != nil && ctx.Err() == nil {
				onError(err)
			}
		}
	}
}
