package metrics

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type metricBucketKey struct {
	BucketStart time.Time
	Method      string
	Direction   string
}

type metricDelta struct {
	Requests     uint64
	Transactions uint64
	Success      uint64
	Failure      uint64
}

type metricFlushBatch struct {
	ID             string
	Deltas         map[metricBucketKey]metricDelta
	FailureStarted time.Time
}

// PersistentRecorder keeps the low-latency in-memory recorder and batches the
// durable minute ledger behind it. SIP request paths never wait for a DB write.
type PersistentRecorder struct {
	db    *gorm.DB
	inner Recorder
	clock func() time.Time

	flushMu  sync.Mutex
	mu       sync.Mutex
	pairs    map[string]Transaction
	pending  map[metricBucketKey]metricDelta
	inflight *metricFlushBatch

	lastHeartbeatMinute time.Time
	failureStarted      time.Time
	restartChecked      bool
	seq                 atomic.Uint64
}

const restartGapTolerance = 2 * time.Minute

func NewPersistentRecorder(db *gorm.DB, inner Recorder) *PersistentRecorder {
	return &PersistentRecorder{db: db, inner: inner, clock: time.Now, pairs: map[string]Transaction{}, pending: map[metricBucketKey]metricDelta{}}
}

func (recorder *PersistentRecorder) SetClock(clock func() time.Time) { recorder.clock = clock }

func (recorder *PersistentRecorder) Begin(transaction Transaction) {
	if recorder.inner != nil {
		recorder.inner.Begin(transaction)
	}
	if transaction.CallID == "" || transaction.CSeq == "" || transaction.Kind == TxUnknown {
		return
	}
	if transaction.StartedAt.IsZero() {
		transaction.StartedAt = recorder.clock()
	}
	key := transaction.CallID + ":" + transaction.CSeq
	bucket := metricBucketKey{BucketStart: minuteStart(transaction.StartedAt), Method: transaction.Kind.String(), Direction: transaction.Direction.String()}
	recorder.mu.Lock()
	recorder.pairs[key] = transaction
	delta := recorder.pending[bucket]
	delta.Requests++
	recorder.pending[bucket] = delta
	recorder.mu.Unlock()
}

func (recorder *PersistentRecorder) End(callID, cseq string, statusCode int, success bool) {
	if recorder.inner != nil {
		recorder.inner.End(callID, cseq, statusCode, success)
	}
	key := callID + ":" + cseq
	recorder.mu.Lock()
	transaction, ok := recorder.pairs[key]
	if ok {
		delete(recorder.pairs, key)
		bucket := metricBucketKey{BucketStart: minuteStart(transaction.StartedAt), Method: transaction.Kind.String(), Direction: transaction.Direction.String()}
		delta := recorder.pending[bucket]
		delta.Transactions++
		if success {
			delta.Success++
		} else {
			delta.Failure++
		}
		recorder.pending[bucket] = delta
	}
	recorder.mu.Unlock()
}

func (recorder *PersistentRecorder) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	_ = recorder.recordRestartGap(ctx)
	_ = recorder.Flush(ctx)
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = recorder.Flush(flushCtx)
			cancel()
			return
		case <-ticker.C:
			_ = recorder.Flush(ctx)
		}
	}
}

func (recorder *PersistentRecorder) recordRestartGap(ctx context.Context) error {
	if recorder.db == nil {
		return errors.New("SIP metric database is unavailable")
	}
	recorder.mu.Lock()
	if recorder.restartChecked {
		recorder.mu.Unlock()
		return nil
	}
	recorder.restartChecked = true
	recorder.mu.Unlock()

	var latest gbmodels.GbSipMetricFlush
	result := recorder.db.WithContext(ctx).Order("created_at DESC").Take(&latest)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil
	}
	if result.Error != nil {
		recorder.mu.Lock()
		recorder.restartChecked = false
		recorder.mu.Unlock()
		return result.Error
	}

	now := recorder.clock()
	lastMinute := minuteStart(latest.CreatedAt)
	recorder.mu.Lock()
	recorder.lastHeartbeatMinute = lastMinute
	recorder.mu.Unlock()
	if now.Sub(latest.CreatedAt) <= restartGapTolerance {
		return nil
	}
	startedAt := lastMinute.Add(time.Minute)
	endedAt := minuteStart(now)
	if !endedAt.After(startedAt) {
		return nil
	}
	return recorder.db.WithContext(ctx).Create(&gbmodels.GbSipMetricGap{
		StartedAt: startedAt,
		EndedAt:   endedAt,
		Reason:    "restart",
	}).Error
}

func (recorder *PersistentRecorder) Flush(ctx context.Context) error {
	if recorder.db == nil {
		return errors.New("SIP metric database is unavailable")
	}
	recorder.flushMu.Lock()
	defer recorder.flushMu.Unlock()

	now := recorder.clock()
	heartbeatMinute := minuteStart(now)
	recorder.mu.Lock()
	batch := recorder.inflight
	if batch == nil {
		needsHeartbeat := recorder.lastHeartbeatMinute.IsZero() || heartbeatMinute.After(recorder.lastHeartbeatMinute)
		if len(recorder.pending) == 0 && !needsHeartbeat && recorder.failureStarted.IsZero() {
			recorder.mu.Unlock()
			return nil
		}
		batch = &metricFlushBatch{
			ID:             fmt.Sprintf("%d-%d", now.UnixNano(), recorder.seq.Add(1)),
			Deltas:         recorder.pending,
			FailureStarted: recorder.failureStarted,
		}
		recorder.pending = map[metricBucketKey]metricDelta{}
		recorder.inflight = batch
	}
	recorder.mu.Unlock()

	committedAt, err := recorder.persistBatch(ctx, batch, now)
	if err != nil {
		recorder.mu.Lock()
		if recorder.failureStarted.IsZero() {
			recorder.failureStarted = now
		}
		if batch.FailureStarted.IsZero() {
			batch.FailureStarted = recorder.failureStarted
		}
		recorder.mu.Unlock()
		return err
	}
	recorder.mu.Lock()
	if recorder.inflight == batch {
		recorder.inflight = nil
	}
	recorder.lastHeartbeatMinute = minuteStart(committedAt)
	recorder.failureStarted = time.Time{}
	recorder.mu.Unlock()
	return nil
}

// persistBatch applies a stable flush id and its metric deltas atomically. A
// retry after an ambiguous commit observes the existing id and does not apply
// the same counters twice.
func (recorder *PersistentRecorder) persistBatch(ctx context.Context, batch *metricFlushBatch, now time.Time) (time.Time, error) {
	if recorder == nil || recorder.db == nil || batch == nil {
		return time.Time{}, errors.New("SIP metric flush batch is unavailable")
	}
	committedAt := now
	err := recorder.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing gbmodels.GbSipMetricFlush
		result := tx.Select("created_at").Where("flush_id = ?", batch.ID).Limit(1).Find(&existing)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			committedAt = existing.CreatedAt
			return nil
		}
		if err := tx.Create(&gbmodels.GbSipMetricFlush{FlushID: batch.ID, CreatedAt: now}).Error; err != nil {
			return err
		}
		if !batch.FailureStarted.IsZero() && now.After(batch.FailureStarted) {
			if err := tx.Create(&gbmodels.GbSipMetricGap{StartedAt: batch.FailureStarted, EndedAt: now, Reason: "persist_failure"}).Error; err != nil {
				return err
			}
		}
		for key, delta := range batch.Deltas {
			var row gbmodels.GbSipMetricMinute
			result := tx.Where("bucket_start = ? AND method = ? AND direction = ?", key.BucketStart, key.Method, key.Direction).Take(&row)
			switch {
			case errors.Is(result.Error, gorm.ErrRecordNotFound) || (result.Error == nil && result.RowsAffected == 0):
				row = gbmodels.GbSipMetricMinute{BucketStart: key.BucketStart, Method: key.Method, Direction: key.Direction, RequestCount: delta.Requests, TransactionCount: delta.Transactions, TransactionSuccess: delta.Success, TransactionFailure: delta.Failure}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			case result.Error != nil:
				return result.Error
			default:
				if err := tx.Model(&gbmodels.GbSipMetricMinute{}).
					Where("bucket_start = ? AND method = ? AND direction = ?", key.BucketStart, key.Method, key.Direction).
					Updates(map[string]any{
						"request_count":       gorm.Expr("request_count + ?", delta.Requests),
						"transaction_count":   gorm.Expr("transaction_count + ?", delta.Transactions),
						"transaction_success": gorm.Expr("transaction_success + ?", delta.Success),
						"transaction_failure": gorm.Expr("transaction_failure + ?", delta.Failure),
					}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	return committedAt, err
}

func minuteStart(value time.Time) time.Time { return value.Truncate(time.Minute) }

var _ Recorder = (*PersistentRecorder)(nil)
