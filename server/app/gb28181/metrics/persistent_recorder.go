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

// PersistentRecorder keeps the low-latency in-memory recorder and batches the
// durable minute ledger behind it. SIP request paths never wait for a DB write.
type PersistentRecorder struct {
	db    *gorm.DB
	inner Recorder
	clock func() time.Time

	mu      sync.Mutex
	pairs   map[string]Transaction
	pending map[metricBucketKey]metricDelta
	seq     atomic.Uint64
}

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

func (recorder *PersistentRecorder) Flush(ctx context.Context) error {
	if recorder.db == nil {
		return errors.New("SIP metric database is unavailable")
	}
	recorder.mu.Lock()
	if len(recorder.pending) == 0 {
		recorder.mu.Unlock()
		return nil
	}
	batch := recorder.pending
	recorder.pending = map[metricBucketKey]metricDelta{}
	recorder.mu.Unlock()

	flushID := fmt.Sprintf("%d-%d", recorder.clock().UnixNano(), recorder.seq.Add(1))
	err := recorder.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&gbmodels.GbSipMetricFlush{FlushID: flushID, CreatedAt: recorder.clock()}).Error; err != nil {
			return err
		}
		for key, delta := range batch {
			var row gbmodels.GbSipMetricMinute
			err := tx.Where("bucket_start = ? AND method = ? AND direction = ?", key.BucketStart, key.Method, key.Direction).Take(&row).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				row = gbmodels.GbSipMetricMinute{BucketStart: key.BucketStart, Method: key.Method, Direction: key.Direction, RequestCount: delta.Requests, TransactionCount: delta.Transactions, TransactionSuccess: delta.Success, TransactionFailure: delta.Failure}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			case err != nil:
				return err
			default:
				if err := tx.Model(&row).Updates(map[string]any{
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
	if err != nil {
		recorder.mu.Lock()
		for key, delta := range batch {
			current := recorder.pending[key]
			current.Requests += delta.Requests
			current.Transactions += delta.Transactions
			current.Success += delta.Success
			current.Failure += delta.Failure
			recorder.pending[key] = current
		}
		recorder.mu.Unlock()
	}
	return err
}

func minuteStart(value time.Time) time.Time { return value.Truncate(time.Minute) }

var _ Recorder = (*PersistentRecorder)(nil)
