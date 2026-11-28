package recordingplan

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/play"
	"uvplatform.com/uvp-gb28181/app/gb28181/stream"
)

func TestOrchestratorStartsLiveThenRecordingAndReusesGeneration(t *testing.T) {
	order := []string{}
	live := &fakePlanLive{result: &play.Result{StreamID: "stream-1", SSRC: "ssrc-1", Generation: 4}, order: &order}
	recording := &fakePlanRecording{order: &order}
	leases := play.NewSourceLeaseRegistry()
	orchestrator := NewOrchestrator(live, recording, leases, nil)

	first, err := orchestrator.Start(context.Background(), ChannelTarget{ID: 7, DeviceCode: "D", ChannelCode: "C"})
	require.NoError(t, err)
	require.Equal(t, []string{"enable", "live", "record"}, order)
	require.False(t, first.Reused)
	require.True(t, leases.HasLease("stream-1"))
	order = order[:0]
	second, err := orchestrator.Start(context.Background(), ChannelTarget{ID: 7, DeviceCode: "D", ChannelCode: "C"})
	require.NoError(t, err)
	require.True(t, second.Reused)
	require.Equal(t, 1, leases.LeaseCount("stream-1"))
}

func TestOrchestratorClassifiesMediaTimeoutAndDoesNotMarkRecording(t *testing.T) {
	live := &fakePlanLive{err: play.ErrStreamNotReady}
	recording := &fakePlanRecording{}
	orchestrator := NewOrchestrator(live, recording, play.NewSourceLeaseRegistry(), nil)
	_, err := orchestrator.Start(context.Background(), ChannelTarget{ID: 7, DeviceCode: "D", ChannelCode: "C"})
	var stageErr *OrchestrationError
	require.ErrorAs(t, err, &stageErr)
	require.Equal(t, FailureMediaWait, stageErr.Stage)
	require.Zero(t, recording.recordCalls)
}

func TestOrchestratorStopKeepsLeaseOnRecordStopFailure(t *testing.T) {
	live := &fakePlanLive{result: &play.Result{StreamID: "stream-1", SSRC: "ssrc-1", Generation: 4}}
	recording := &fakePlanRecording{disableErr: errors.New("stop record failed")}
	leases := play.NewSourceLeaseRegistry()
	orchestrator := NewOrchestrator(live, recording, leases, nil)
	_, err := orchestrator.Start(context.Background(), ChannelTarget{ID: 7, DeviceCode: "D", ChannelCode: "C"})
	require.NoError(t, err)
	err = orchestrator.Stop(context.Background(), 7)
	require.Error(t, err)
	require.True(t, leases.HasLease("stream-1"))
}

func TestOrchestratorStopReleasesOnlyPlanLeaseAndRequestsConditionalStop(t *testing.T) {
	live := &fakePlanLive{result: &play.Result{StreamID: "stream-1", SSRC: "ssrc-1", Generation: 4}}
	recording := &fakePlanRecording{}
	leases := play.NewSourceLeaseRegistry()
	stopper := &fakePlanStopper{}
	orchestrator := NewOrchestrator(live, recording, leases, stopper)
	_, err := orchestrator.Start(context.Background(), ChannelTarget{ID: 7, DeviceCode: "D", ChannelCode: "C"})
	require.NoError(t, err)
	require.NoError(t, orchestrator.Stop(context.Background(), 7))
	require.False(t, leases.HasLease("stream-1"))
	require.Equal(t, 1, stopper.calls)
}

func TestOrchestratorRetainsLeaseAndRetriesConditionalStopAfterFailure(t *testing.T) {
	live := &fakePlanLive{result: &play.Result{StreamID: "stream-1", SSRC: "ssrc-1", Generation: 4}}
	leases := play.NewSourceLeaseRegistry()
	stopper := &fakePlanStopper{errors: []error{errors.New("reader count unavailable")}}
	orchestrator := NewOrchestrator(live, &fakePlanRecording{}, leases, stopper)
	_, err := orchestrator.Start(context.Background(), ChannelTarget{ID: 7, DeviceCode: "D", ChannelCode: "C"})
	require.NoError(t, err)

	require.Error(t, orchestrator.Stop(context.Background(), 7))
	require.True(t, leases.HasLease("stream-1"), "keep the source protected while the stop is retried")
	require.NoError(t, orchestrator.Stop(context.Background(), 7))
	require.False(t, leases.HasLease("stream-1"))
	require.Equal(t, 2, stopper.calls)
}

func TestOrchestratorStopsRecoveredStreamWithoutInMemoryPlanState(t *testing.T) {
	ref := stream.LiveRef{StreamID: "recovered", SSRC: "ssrc-1", Generation: 8, NodeID: 2}
	for _, test := range []struct {
		name      string
		ssrc      string
		wantCalls int
	}{
		{"matching source", ref.SSRC, 1},
		{"replaced source", "old-ssrc", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			stopper := &recoveredPlanStopper{ref: ref}
			recording := &fakePlanRecording{channel: &models.GbChannel{StreamID: ref.StreamID, CurrentSSRC: test.ssrc}}
			orchestrator := NewOrchestrator(&fakePlanLive{}, recording, play.NewSourceLeaseRegistry(), stopper)
			require.NoError(t, orchestrator.Stop(context.Background(), 7))
			require.Equal(t, test.wantCalls, stopper.calls)
			if test.wantCalls > 0 {
				require.Equal(t, ref, stopper.lastRef)
			}
			require.Empty(t, orchestrator.active)
		})
	}
}

func TestOrchestratorRetriesRecoveredStreamStopFailure(t *testing.T) {
	ref := stream.LiveRef{StreamID: "recovered", SSRC: "ssrc-1", Generation: 8, NodeID: 2}
	stopper := &recoveredPlanStopper{ref: ref, fakePlanStopper: fakePlanStopper{errors: []error{errors.New("zlm unavailable")}}}
	leases := play.NewSourceLeaseRegistry()
	recording := &fakePlanRecording{channel: &models.GbChannel{StreamID: ref.StreamID, CurrentSSRC: ref.SSRC}}
	orchestrator := NewOrchestrator(&fakePlanLive{}, recording, leases, stopper)
	require.Error(t, orchestrator.Stop(context.Background(), 7))
	require.True(t, leases.HasLease(ref.StreamID))
	require.NoError(t, orchestrator.Stop(context.Background(), 7))
	require.False(t, leases.HasLease(ref.StreamID))
	require.Equal(t, 2, stopper.calls)
}

func TestOrchestratorStopKeepsLiveWhenCascadeLeaseRemains(t *testing.T) {
	live := &fakePlanLive{result: &play.Result{StreamID: "stream-1", SSRC: "ssrc-1", Generation: 4}}
	leases := play.NewSourceLeaseRegistry()
	stopper := &fakePlanStopper{}
	orchestrator := NewOrchestrator(live, &fakePlanRecording{}, leases, stopper)
	orchestrator.SetCombinedLeaseChecker(alwaysPlanLease(true))
	_, err := orchestrator.Start(context.Background(), ChannelTarget{ID: 7, DeviceCode: "D", ChannelCode: "C"})
	require.NoError(t, err)
	require.NoError(t, orchestrator.Stop(context.Background(), 7))
	require.False(t, leases.HasLease("stream-1"))
	require.Zero(t, stopper.calls)
}

func TestOrchestratorRecordingFailureReleasesLeaseAndAllowsRetry(t *testing.T) {
	live := &fakePlanLive{result: &play.Result{StreamID: "stream-1", SSRC: "ssrc-1", Generation: 4}}
	recording := &fakePlanRecording{beginErr: errors.New("zlm unavailable")}
	leases := play.NewSourceLeaseRegistry()
	orchestrator := NewOrchestrator(live, recording, leases, nil)
	_, err := orchestrator.Start(context.Background(), ChannelTarget{ID: 7, DeviceCode: "D", ChannelCode: "C"})
	require.Error(t, err)
	require.False(t, leases.HasLease("stream-1"))
	recording.beginErr = nil
	_, err = orchestrator.Start(context.Background(), ChannelTarget{ID: 7, DeviceCode: "D", ChannelCode: "C"})
	require.NoError(t, err)
	require.Equal(t, 2, recording.recordCalls)
}

func TestOrchestratorReleasesPerChannelLocksAfterHighCardinalityTraffic(t *testing.T) {
	orchestrator := NewOrchestrator(&fakePlanLive{}, &fakePlanRecording{}, play.NewSourceLeaseRegistry(), nil)
	for channelID := uint(1); channelID <= 10000; channelID++ {
		unlock := orchestrator.lockChannel(channelID)
		unlock()
	}
	require.Empty(t, orchestrator.channelMux)
}

type fakePlanLive struct {
	result *play.Result
	err    error
	order  *[]string
}

func (f *fakePlanLive) EnsureLive(context.Context, play.Request) (*play.Result, error) {
	if f.order != nil {
		*f.order = append(*f.order, "live")
	}
	return f.result, f.err
}

type fakePlanRecording struct {
	order       *[]string
	recordCalls int
	disableErr  error
	beginErr    error
	channel     *models.GbChannel
}

func (f *fakePlanRecording) Enable(context.Context, uint) (*models.GbChannel, error) {
	if f.order != nil {
		*f.order = append(*f.order, "enable")
	}
	return &models.GbChannel{}, nil
}
func (f *fakePlanRecording) BeginPlayback(context.Context, string) error {
	f.recordCalls++
	if f.order != nil {
		*f.order = append(*f.order, "record")
	}
	return f.beginErr
}
func (f *fakePlanRecording) Disable(context.Context, uint) (*models.GbChannel, error) {
	if f.channel != nil {
		return f.channel, f.disableErr
	}
	return &models.GbChannel{}, f.disableErr
}

type recoveredPlanStopper struct {
	fakePlanStopper
	ref stream.LiveRef
}

func (f *recoveredPlanStopper) CurrentLiveRef(streamID string) (stream.LiveRef, bool) {
	return f.ref, f.ref.StreamID == streamID
}

type fakePlanStopper struct {
	calls   int
	errors  []error
	lastRef stream.LiveRef
}

func (f *fakePlanStopper) StopOnNoneReader(_ context.Context, ref stream.LiveRef) (bool, error) {
	f.calls++
	f.lastRef = ref
	if len(f.errors) == 0 {
		return true, nil
	}
	err := f.errors[0]
	f.errors = f.errors[1:]
	return false, err
}

type alwaysPlanLease bool

func (answer alwaysPlanLease) HasLease(string) bool { return bool(answer) }
