package trace

import (
	"context"
	"fmt"
	"time"
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

		backoff := m.retryMin
		for {
			err := insertBatchSafely(ctx, m.store, batch)
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

func insertBatchSafely(ctx context.Context, store Store, batch []Event) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("trace store panic: %v", recovered)
		}
	}()
	return store.InsertBatch(ctx, batch)
}

type unavailableStore struct{}

func (unavailableStore) InsertBatch(context.Context, []Event) error {
	return fmt.Errorf("trace store is not configured")
}
