package play

import (
	"context"
	"sync"
	"sync/atomic"
)

type LifecycleEventWriter interface {
	Append(context.Context, string, LifecycleEvent) error
}

type StreamLifecycleEventWriter interface {
	AppendByStream(context.Context, string, int64, LifecycleEvent) (bool, error)
}

type LifecycleRecorder interface {
	Record(context.Context, string, LifecycleEvent) bool
}

type StreamLifecycleRecorder interface {
	RecordByStream(context.Context, string, int64, LifecycleEvent) bool
}

type LifecycleRecorderStats struct {
	Accepted       uint64
	Dropped        uint64
	Persisted      uint64
	PersistFailed  uint64
	RejectedClosed uint64
}

type lifecycleRecord struct {
	ctx         context.Context
	lifecycleID string
	streamID    string
	nodeID      int64
	event       LifecycleEvent
}

type AsyncLifecycleRecorder struct {
	writer                                                      LifecycleEventWriter
	queue                                                       chan lifecycleRecord
	done                                                        chan struct{}
	mu                                                          sync.RWMutex
	closed                                                      bool
	accepted, dropped, persisted, persistFailed, rejectedClosed atomic.Uint64
}

func NewAsyncLifecycleRecorder(writer LifecycleEventWriter, capacity int) *AsyncLifecycleRecorder {
	if capacity <= 0 {
		capacity = 256
	}
	recorder := &AsyncLifecycleRecorder{writer: writer, queue: make(chan lifecycleRecord, capacity), done: make(chan struct{})}
	go recorder.run()
	return recorder
}

func (recorder *AsyncLifecycleRecorder) Record(ctx context.Context, lifecycleID string, event LifecycleEvent) bool {
	if recorder == nil || recorder.writer == nil || lifecycleID == "" {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}
	return recorder.enqueue(lifecycleRecord{ctx: ctx, lifecycleID: lifecycleID, event: event})
}

func (recorder *AsyncLifecycleRecorder) RecordByStream(ctx context.Context, streamID string, nodeID int64, event LifecycleEvent) bool {
	if recorder == nil || recorder.writer == nil || streamID == "" {
		return false
	}
	if _, ok := recorder.writer.(StreamLifecycleEventWriter); !ok {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}
	return recorder.enqueue(lifecycleRecord{ctx: ctx, streamID: streamID, nodeID: nodeID, event: event})
}

func (recorder *AsyncLifecycleRecorder) enqueue(record lifecycleRecord) bool {
	recorder.mu.RLock()
	defer recorder.mu.RUnlock()
	if recorder.closed {
		recorder.rejectedClosed.Add(1)
		return false
	}
	select {
	case recorder.queue <- record:
		recorder.accepted.Add(1)
		return true
	default:
		recorder.dropped.Add(1)
		return false
	}
}

func (recorder *AsyncLifecycleRecorder) Shutdown(ctx context.Context) error {
	if recorder == nil {
		return nil
	}
	recorder.mu.Lock()
	if !recorder.closed {
		recorder.closed = true
		close(recorder.queue)
	}
	recorder.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-recorder.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (recorder *AsyncLifecycleRecorder) Stats() LifecycleRecorderStats {
	if recorder == nil {
		return LifecycleRecorderStats{}
	}
	return LifecycleRecorderStats{
		Accepted: recorder.accepted.Load(), Dropped: recorder.dropped.Load(), Persisted: recorder.persisted.Load(),
		PersistFailed: recorder.persistFailed.Load(), RejectedClosed: recorder.rejectedClosed.Load(),
	}
}

func (recorder *AsyncLifecycleRecorder) run() {
	defer close(recorder.done)
	for record := range recorder.queue {
		var err error
		if record.streamID != "" {
			_, err = recorder.writer.(StreamLifecycleEventWriter).AppendByStream(record.ctx, record.streamID, record.nodeID, record.event)
		} else {
			err = recorder.writer.Append(record.ctx, record.lifecycleID, record.event)
		}
		if err != nil {
			recorder.persistFailed.Add(1)
			continue
		}
		recorder.persisted.Add(1)
	}
}
