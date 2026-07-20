package trace

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func (m *Module) runWriter(ctx context.Context) {
	defer close(m.done)
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
			if !queueOpen {
				return
			}
			continue
		}

		backoff := m.retryMin
		for {
			err := insertBatchSafely(ctx, m.store, storedBatch)
			if err == nil {
				m.health.ready(m.now())
				break
			}
			m.health.degraded(err.Error())
			select {
			case <-time.After(backoff):
				backoff *= 2
				if backoff > m.retryMax {
					backoff = m.retryMax
				}
			case <-ctx.Done():
				return
			}
		}
		if !queueOpen {
			return
		}
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
