package trace

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type retryBatch struct {
	events     []StoredEvent
	nextTry    time.Time
	backoff    time.Duration
	lastReason string
}

func (m *Module) runWriter(ctx context.Context) {
	defer func() {
		close(m.done)
		// Stop retry work once the live queue has drained during shutdown.
		m.cancel()
	}()
	queue := m.collector.queue
	for {
		first, ok := receiveEvent(ctx, queue)
		if !ok {
			return
		}
		batch := []Event{first}
		queueOpen := true
		timer := time.NewTimer(m.flushInterval)
	collect:
		for len(batch) < m.batchSize && queueOpen {
			select {
			case event, open := <-queue:
				if !open {
					queueOpen = false
					break collect
				}
				batch = append(batch, event)
			case <-timer.C:
				break collect
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}

		storedBatch, err := m.encryptBatch(batch)
		if err != nil {
			m.collector.Drop(uint64(len(batch)))
			m.health.degraded(err.Error())
			m.health.gapStart(m.nowUTC(), err.Error(), uint64(len(batch)))
			if !queueOpen {
				return
			}
			continue
		}

		if err := insertBatchSafely(ctx, m.store, storedBatch); err != nil {
			m.health.degraded(err.Error())
			m.health.gapStart(m.nowUTC(), err.Error(), uint64(len(storedBatch)))
			select {
			case m.retryQueue <- retryBatch{events: storedBatch, nextTry: m.nowUTC().Add(m.retryMin), backoff: m.retryMin, lastReason: err.Error()}:
				m.pendingRetries.Add(1)
			default:
				m.collector.Drop(uint64(len(storedBatch)))
				m.health.gapAdd(uint64(len(storedBatch)))
			}
		} else {
			m.markStoreReady()
		}
		if !queueOpen {
			return
		}
	}
}

// runRetryWriter retries failed batches independently of the live collector.
// A storage outage therefore creates an explicit bounded gap instead of
// stopping ingestion of later SIP frames.
func (m *Module) runRetryWriter(ctx context.Context) {
	defer close(m.retryDone)
	pending := make([]retryBatch, 0, cap(m.retryQueue))
	for {
		var timer *time.Timer
		var timerC <-chan time.Time
		if len(pending) > 0 {
			delay := time.Until(pending[0].nextTry)
			if delay < 0 {
				delay = 0
			}
			timer = time.NewTimer(delay)
			timerC = timer.C
		}
		select {
		case batch, ok := <-m.retryQueue:
			if timer != nil {
				timer.Stop()
			}
			if !ok {
				return
			}
			pending = append(pending, batch)
		case <-timerC:
			batch := pending[0]
			pending = pending[1:]
			err := insertBatchSafely(ctx, m.store, batch.events)
			if err != nil {
				batch.lastReason = err.Error()
				batch.backoff *= 2
				if batch.backoff > m.retryMax {
					batch.backoff = m.retryMax
				}
				batch.nextTry = m.nowUTC().Add(batch.backoff)
				m.health.degraded(err.Error())
				m.health.gapAdd(uint64(len(batch.events)))
				pending = append(pending, batch)
			} else {
				m.pendingRetries.Add(-1)
				m.markStoreReady()
			}
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}

func (m *Module) markStoreReady() {
	m.health.ready(m.nowUTC())
	if m.pendingRetries.Load() == 0 {
		m.health.gapEnd(m.nowUTC())
	}
}

func receiveEvent(ctx context.Context, queue <-chan Event) (Event, bool) {
	select {
	case event, ok := <-queue:
		return event, ok
	case <-ctx.Done():
		return Event{}, false
	}
}

func (m *Module) encryptBatch(events []Event) ([]StoredEvent, error) {
	stored := make([]StoredEvent, 0, len(events))
	for _, event := range events {
		metadata := extractSIPMetadata(event.Raw, event.Direction)
		if event.ParseError != "" {
			metadata.ParseError = event.ParseError
		}
		payload, err := m.cipher.Encrypt(event.Raw)
		if err != nil {
			return nil, fmt.Errorf("encrypt SIP trace payload: %w", err)
		}
		stored = append(stored, StoredEvent{
			EventID:    uuid.NewString(),
			OccurredAt: event.OccurredAt,
			Direction:  event.Direction,
			Transport:  event.Transport,
			LocalAddr:  event.LocalAddr,
			RemoteAddr: event.RemoteAddr,
			DeviceID:   metadata.DeviceID,
			Method:     metadata.Method,
			StatusCode: metadata.StatusCode,
			CallID:     metadata.CallID,
			CSeq:       metadata.CSeq,
			CSeqMethod: metadata.CSeqMethod,
			Malformed:  event.Malformed,
			ParseError: metadata.ParseError,
			Payload:    payload,
		})
	}
	return stored, nil
}

func insertBatchSafely(ctx context.Context, store Store, batch []StoredEvent) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("trace store panic: %v", recovered)
		}
	}()
	return store.InsertBatch(ctx, batch)
}

type unavailableStore struct{}

func (unavailableStore) InsertBatch(context.Context, []StoredEvent) error {
	return fmt.Errorf("trace store is not configured")
}

type errorStore struct {
	err error
}

func (s errorStore) InsertBatch(context.Context, []StoredEvent) error {
	return s.err
}
