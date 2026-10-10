package play

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLifecycleAppliesSuccessfulMediaAndClientFactsSeparately(t *testing.T) {
	snapshot := NewLifecycleSnapshot("life-1", 7, "D1", "C1")
	for _, event := range []LifecycleEvent{
		{EventID: "e1", Stage: StageRequest, EventName: EventRequestReceived, FactState: FactConfirmed, Source: SourceHTTP},
		{EventID: "e2", Stage: StageValidation, EventName: EventValidationSucceeded, FactState: FactConfirmed, Source: SourcePlayService},
		{EventID: "e3", Stage: StageNode, EventName: EventNodeSelected, FactState: FactConfirmed, Source: SourcePlayService, NodeID: 3},
		{EventID: "e4", Stage: StageRTP, EventName: EventRTPAllocated, FactState: FactConfirmed, Source: SourcePlayService},
		{EventID: "e5", Stage: StageInvite, EventName: EventInviteAccepted, FactState: FactConfirmed, Source: SourceSIP, CallID: "call-1", CSeq: "2"},
		{EventID: "e6", Stage: StageMedia, EventName: EventMediaReady, FactState: FactConfirmed, Source: SourcePlayService, StreamID: "s1"},
		{EventID: "e7", Stage: StageAuthorization, EventName: EventPlayURLIssued, FactState: FactConfirmed, Source: SourcePlayService},
		{EventID: "e8", Stage: StageClient, EventName: EventFirstFrame, FactState: FactConfirmed, Source: SourceClient},
	} {
		require.NoError(t, snapshot.Apply(event))
	}
	require.Equal(t, LifecycleStateInProgress, snapshot.LifecycleState)
	require.Equal(t, MediaStateReady, snapshot.MediaState)
	require.Equal(t, ClientStateFirstFrame, snapshot.ClientState)
	require.Equal(t, "call-1", snapshot.CallID)
	require.Equal(t, "s1", snapshot.StreamID)
}

func TestLifecycleMediaTimeoutDoesNotBecomeClientFailure(t *testing.T) {
	snapshot := NewLifecycleSnapshot("life-2", 7, "D1", "C1")
	require.NoError(t, snapshot.Apply(LifecycleEvent{EventID: "e1", Stage: StageInvite, EventName: EventInviteAccepted, FactState: FactConfirmed, Source: SourceSIP}))
	require.NoError(t, snapshot.Apply(LifecycleEvent{EventID: "e2", Stage: StageMedia, EventName: EventMediaTimeout, FactState: FactFailed, Source: SourcePlayService, ReasonCode: ReasonMediaTimeout}))
	require.Equal(t, MediaStateFailed, snapshot.MediaState)
	require.Equal(t, ClientStateUnknown, snapshot.ClientState)
	require.Equal(t, LifecycleStateFailed, snapshot.LifecycleState)
	require.Equal(t, StageMedia, snapshot.FailureStage)
}

func TestLifecycleClientErrorDoesNotOverwriteMediaReady(t *testing.T) {
	snapshot := NewLifecycleSnapshot("life-client-error", 7, "D1", "C1")
	require.NoError(t, snapshot.Apply(LifecycleEvent{EventID: "e1", Stage: StageMedia, EventName: EventMediaReady, FactState: FactConfirmed, Source: SourcePlayService}))
	require.NoError(t, snapshot.Apply(LifecycleEvent{EventID: "e2", Stage: StageClient, EventName: EventPlayerError, FactState: FactFailed, Source: SourceClient, ReasonCode: ReasonPlayerError}))
	require.Equal(t, MediaStateReady, snapshot.MediaState)
	require.Equal(t, ClientStateFailed, snapshot.ClientState)
}

func TestLifecycleReuseMarksRtpAndInviteNotApplicable(t *testing.T) {
	snapshot := NewLifecycleSnapshot("life-3", 7, "D1", "C1")
	snapshot.Reused = true
	require.NoError(t, snapshot.Apply(LifecycleEvent{EventID: "e1", Stage: StageRTP, EventName: EventRTPNotApplicable, FactState: FactNotApplicable, Source: SourcePlayService, Reused: true}))
	require.NoError(t, snapshot.Apply(LifecycleEvent{EventID: "e2", Stage: StageInvite, EventName: EventInviteNotApplicable, FactState: FactNotApplicable, Source: SourcePlayService, Reused: true}))
	require.Equal(t, FactNotApplicable, snapshot.StageFacts[StageRTP])
	require.Equal(t, FactNotApplicable, snapshot.StageFacts[StageInvite])
	require.True(t, snapshot.Reused)
}

func TestLifecycleTerminalStateIsIdempotentAndCannotBeReversed(t *testing.T) {
	snapshot := NewLifecycleSnapshot("life-4", 7, "D1", "C1")
	require.NoError(t, snapshot.Apply(LifecycleEvent{EventID: "e1", Stage: StageMedia, EventName: EventMediaTimeout, FactState: FactFailed, Source: SourcePlayService, ReasonCode: ReasonMediaTimeout}))
	require.NoError(t, snapshot.Apply(LifecycleEvent{EventID: "e1", Stage: StageMedia, EventName: EventMediaTimeout, FactState: FactFailed, Source: SourcePlayService, ReasonCode: ReasonMediaTimeout}))
	require.Error(t, snapshot.Apply(LifecycleEvent{EventID: "e2", Stage: StageMedia, EventName: EventMediaReady, FactState: FactConfirmed, Source: SourcePlayService}))
	require.Equal(t, LifecycleStateFailed, snapshot.LifecycleState)
}

func TestLifecycleTerminalStateStillAcceptsCleanupFacts(t *testing.T) {
	snapshot := NewLifecycleSnapshot("life-cleanup", 7, "D1", "C1")
	require.NoError(t, snapshot.Apply(LifecycleEvent{EventID: "e1", Stage: StageMedia, EventName: EventMediaTimeout, FactState: FactFailed, Source: SourcePlayService, ReasonCode: ReasonMediaTimeout}))
	require.NoError(t, snapshot.Apply(LifecycleEvent{EventID: "e2", Stage: StageStop, EventName: EventStopRequested, FactState: FactConfirmed, Source: SourcePlayService}))
	require.NoError(t, snapshot.Apply(LifecycleEvent{EventID: "e3", Stage: StageCleanup, EventName: EventCleanupCompleted, FactState: FactConfirmed, Source: SourcePlayService}))

	require.Equal(t, LifecycleStateFailed, snapshot.LifecycleState)
	require.Equal(t, StageMedia, snapshot.FailureStage)
	require.Equal(t, ReasonMediaTimeout, snapshot.ReasonCode)
	require.Equal(t, MediaStateStopped, snapshot.MediaState)
	require.Len(t, snapshot.Events, 3)
}

func TestLifecycleSanitizesSensitiveReasonMessage(t *testing.T) {
	snapshot := NewLifecycleSnapshot("life-5", 7, "D1", "C1")
	event := LifecycleEvent{
		EventID: "e1", Stage: StageAuthorization, EventName: EventAuthorizationFailed,
		FactState: FactFailed, Source: SourcePlayService, ReasonCode: ReasonAuthorizationFailed,
		ReasonMessage: "Authorization: Bearer secret https://zlm.example/live?token=abc " + strings.Repeat("x", 400),
	}
	require.NoError(t, snapshot.Apply(event))
	require.NotContains(t, snapshot.ReasonMessage, "Bearer")
	require.NotContains(t, snapshot.ReasonMessage, "token=abc")
	require.LessOrEqual(t, len(snapshot.ReasonMessage), MaxLifecycleReasonMessage)
}

func TestLifecycleElapsedUsesEventTime(t *testing.T) {
	snapshot := NewLifecycleSnapshot("life-6", 7, "D1", "C1")
	snapshot.StartedAt = time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	event := LifecycleEvent{EventID: "e1", EventAt: snapshot.StartedAt.Add(1500 * time.Millisecond), Stage: StageRequest, EventName: EventRequestReceived, FactState: FactConfirmed, Source: SourceHTTP}
	require.NoError(t, snapshot.Apply(event))
	require.EqualValues(t, 1500, snapshot.Events[0].ElapsedMS)
}
