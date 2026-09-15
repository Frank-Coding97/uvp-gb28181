package play

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/trace/diagnosis"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type playDiagnosisSink struct {
	mu     sync.Mutex
	events []diagnosis.Event
	err    error
}

func (sink *playDiagnosisSink) Emit(_ context.Context, event diagnosis.Event) error {
	sink.mu.Lock()
	sink.events = append(sink.events, event)
	sink.mu.Unlock()
	return sink.err
}

func (sink *playDiagnosisSink) snapshot() []diagnosis.Event {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]diagnosis.Event(nil), sink.events...)
}

func TestClassifyPlayStuck(t *testing.T) {
	tests := []struct {
		name         string
		outcome      uac.InviteOutcome
		err          error
		mediaReady   bool
		totalExpired bool
		code         diagnosis.Code
		ok           bool
	}{
		{
			name:    "signaling timeout",
			outcome: uac.InviteOutcome{RequestSent: true}, err: context.DeadlineExceeded,
			code: diagnosis.CodeSignalingTimeout, ok: true,
		},
		{
			name:    "media timeout",
			outcome: uac.InviteOutcome{RequestSent: true, FinalStatus: 200, AckSucceeded: true},
			err:     context.DeadlineExceeded, totalExpired: true,
			code: diagnosis.CodeMediaTimeout, ok: true,
		},
		{name: "explicit rejection", outcome: uac.InviteOutcome{RequestSent: true, FinalStatus: 486}, err: errors.New("busy")},
		{name: "ack failure", outcome: uac.InviteOutcome{RequestSent: true, FinalStatus: 200}, err: errors.New("ack failed")},
		{name: "request unsent", err: context.DeadlineExceeded},
		{name: "platform resource error", err: errors.New("open RTP failed")},
		{
			name: "media ready", outcome: uac.InviteOutcome{RequestSent: true, FinalStatus: 200, AckSucceeded: true},
			mediaReady: true, totalExpired: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := classifyPlayStuck(tt.outcome, tt.err, tt.mediaReady, tt.totalExpired)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.code, code)
		})
	}
}

func TestStartEmitsSignalingTimeoutDiagnosis(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = playbackSource(1000)
	sink := &playDiagnosisSink{err: diagnosis.ErrQueueFull}
	service, _, _ := newSvc(t, &mockZLM{port: 40000}, &delayedInviter{delay: 2 * time.Second}, onlineDevice(), aChannel())
	service.diagnosticSink = sink
	service.readyWait = 0

	_, err := service.Start(context.Background(), "34020000001320000002", "12345678911116666661")

	require.ErrorIs(t, err, ErrPlayTimeout)
	events := sink.snapshot()
	require.Len(t, events, 1)
	require.Equal(t, diagnosis.CodeSignalingTimeout, events[0].Code)
	require.NotEmpty(t, events[0].CorrelationKey)
	require.Equal(t, "delayed-call", events[0].CallID)
	require.Equal(t, uint32(1), events[0].CSeq)
}

func TestStartEmitsMediaTimeoutDiagnosis(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = playbackSource(1000)
	sink := &playDiagnosisSink{}
	service, _, _ := newSvc(t, &mockZLM{port: 40000}, &delayedInviter{}, onlineDevice(), aChannel())
	service.diagnosticSink = sink
	service.readyWait = 0

	_, err := service.Start(context.Background(), "34020000001320000002", "12345678911116666661")

	require.ErrorIs(t, err, ErrPlayTimeout)
	events := sink.snapshot()
	require.Len(t, events, 1)
	require.Equal(t, diagnosis.CodeMediaTimeout, events[0].Code)
	require.Equal(t, diagnosis.StageMedia, events[0].Stage)
	require.Equal(t, uint16(200), events[0].StatusCode)
}
