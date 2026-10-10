package play

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeLifecycleWriter struct {
	mu      sync.Mutex
	events  []LifecycleEvent
	streams []string
	block   <-chan struct{}
	started chan struct{}
	err     error
}

func (writer *fakeLifecycleWriter) Append(_ context.Context, _ string, event LifecycleEvent) error {
	if writer.block != nil {
		if writer.started != nil {
			select {
			case <-writer.started:
			default:
				close(writer.started)
			}
		}
		<-writer.block
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.events = append(writer.events, event)
	return writer.err
}

func (writer *fakeLifecycleWriter) AppendByStream(_ context.Context, streamID string, _ int64, event LifecycleEvent) (bool, error) {
	if writer.block != nil {
		<-writer.block
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.streams = append(writer.streams, streamID)
	writer.events = append(writer.events, event)
	return streamID != "missing", writer.err
}

func TestAsyncLifecycleRecorderDoesNotBlockAndReportsDropped(t *testing.T) {
	block := make(chan struct{})
	writer := &fakeLifecycleWriter{block: block, started: make(chan struct{})}
	recorder := NewAsyncLifecycleRecorder(writer, 1)
	require.True(t, recorder.Record(context.Background(), "life", LifecycleEvent{EventID: "e1"}))
	require.Eventually(t, func() bool {
		select {
		case <-writer.started:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	recorder.Record(context.Background(), "life", LifecycleEvent{EventID: "e2"})
	require.False(t, recorder.Record(context.Background(), "life", LifecycleEvent{EventID: "e3"}))
	require.EqualValues(t, 1, recorder.Stats().Dropped)
	close(block)
	require.NoError(t, recorder.Shutdown(context.Background()))
}

func TestAsyncLifecycleRecorderTracksPersistenceFailure(t *testing.T) {
	recorder := NewAsyncLifecycleRecorder(&fakeLifecycleWriter{err: errors.New("db down")}, 2)
	require.True(t, recorder.Record(context.Background(), "life", LifecycleEvent{EventID: "e1"}))
	require.NoError(t, recorder.Shutdown(context.Background()))
	require.EqualValues(t, 1, recorder.Stats().PersistFailed)
	require.False(t, recorder.Record(context.Background(), "life", LifecycleEvent{EventID: "late"}))
	require.EqualValues(t, 1, recorder.Stats().RejectedClosed)
}

func TestAsyncLifecycleRecorderShutdownHonorsDeadline(t *testing.T) {
	recorder := NewAsyncLifecycleRecorder(&fakeLifecycleWriter{block: make(chan struct{})}, 1)
	require.True(t, recorder.Record(context.Background(), "life", LifecycleEvent{EventID: "e1"}))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, recorder.Shutdown(ctx), context.DeadlineExceeded)
}

func TestAsyncLifecycleRecorderRecordsByStreamWithoutBlocking(t *testing.T) {
	writer := &fakeLifecycleWriter{}
	recorder := NewAsyncLifecycleRecorder(writer, 2)
	require.True(t, recorder.RecordByStream(context.Background(), "stream-1", 9, LifecycleEvent{EventID: "stop-1"}))
	require.NoError(t, recorder.Shutdown(context.Background()))
	require.Equal(t, []string{"stream-1"}, writer.streams)
	require.EqualValues(t, 1, recorder.Stats().Persisted)
}
