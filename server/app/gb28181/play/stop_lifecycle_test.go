package play

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeStreamLifecycleRecorder struct {
	accepted bool
	events   []LifecycleEvent
}

func (recorder *fakeStreamLifecycleRecorder) RecordByStream(_ context.Context, _ string, _ int64, event LifecycleEvent) bool {
	recorder.events = append(recorder.events, event)
	return recorder.accepted
}

func TestStopRecordsRequestedAndCompletedLifecycleEvents(t *testing.T) {
	z := &mockZLM{}
	inv := &mockInviter{}
	recorder := &fakeStreamLifecycleRecorder{accepted: true}
	s, _, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
	WithStreamLifecycleRecorder(recorder)(s)

	require.NoError(t, s.Stop(context.Background(), "fake-stream", "device-1", "channel-1"))
	require.Len(t, recorder.events, 2)
	require.Equal(t, EventStopRequested, recorder.events[0].EventName)
	require.Equal(t, FactConfirmed, recorder.events[0].FactState)
	require.Equal(t, EventCleanupCompleted, recorder.events[1].EventName)
	require.Equal(t, FactConfirmed, recorder.events[1].FactState)
}

func TestStopRecordsPartialCleanupFailureWithoutChangingResult(t *testing.T) {
	stopErr := context.DeadlineExceeded
	z := &mockZLM{closeErr: stopErr}
	inv := &mockInviter{}
	recorder := &fakeStreamLifecycleRecorder{accepted: false}
	s, _, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
	WithStreamLifecycleRecorder(recorder)(s)

	err := s.Stop(context.Background(), "fake-stream", "device-1", "channel-1")
	require.ErrorIs(t, err, stopErr)
	require.Len(t, recorder.events, 2)
	require.Equal(t, EventCleanupPartialFailure, recorder.events[1].EventName)
	require.Equal(t, FactFailed, recorder.events[1].FactState)
	require.Equal(t, ReasonCleanupPartialFailed, recorder.events[1].ReasonCode)
}
