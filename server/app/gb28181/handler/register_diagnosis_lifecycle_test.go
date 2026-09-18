package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/trace/diagnosis"
)

type blockingDiagnosisSink struct {
	started chan struct{}
	release chan struct{}
}

func (sink *blockingDiagnosisSink) Emit(context.Context, diagnosis.Event) error {
	select {
	case <-sink.started:
	default:
		close(sink.started)
	}
	<-sink.release
	return nil
}

func TestLoggingRegisterDiagnosisCloseStopsTimersAndJoinsCallback(t *testing.T) {
	sink := &blockingDiagnosisSink{started: make(chan struct{}), release: make(chan struct{})}
	tracker := newRegisterAttemptTracker(sink, nil)
	key := "register-close-callback"
	tracker.start(diagnosis.Event{
		CorrelationKey: key,
		State:          diagnosis.StateActive,
		Category:       diagnosis.CategoryRegisterFailure,
		Code:           diagnosis.CodeRegisterTimeout,
		Stage:          diagnosis.StageRegister,
		Source:         diagnosis.SourceRuntime,
	})

	expired := make(chan bool, 1)
	go func() { expired <- tracker.expire(key) }()
	<-sink.started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, tracker.Close(ctx), context.Canceled)

	close(sink.release)
	require.True(t, <-expired)
	require.NoError(t, tracker.Close(context.Background()))
	require.False(t, tracker.expire(key))

	tracker.start(diagnosis.Event{CorrelationKey: "after-close"})
	tracker.mu.Lock()
	require.Empty(t, tracker.attempts)
	tracker.mu.Unlock()
}
