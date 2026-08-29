package management

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

func TestRecordingOpsGBMP4UsesExistingLifecycleAndReadback(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: true}
	gb := &recordingOpsGBService{typed: typed}
	ops := newRecordingOpsForTest(t, typed, gb, true, nil)

	result, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.NoError(t, err)
	require.Equal(t, RecordingStateRecording, result.State)
	require.True(t, result.Recording)
	require.Equal(t, 1, gb.startCalls)
	require.Equal(t, 1, typed.readCalls, "start must read back actual recorder state")
	require.Zero(t, typed.startCalls, "GB MP4 must use the existing recording lifecycle")
}

func TestRecordingOpsExternalMP4RejectsBeforeAnyRecorderCall(t *testing.T) {
	typed := &recordingOpsTypedRecorder{}
	gb := &recordingOpsGBService{}
	ops := newRecordingOpsForTest(t, typed, gb, false, nil)

	_, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.Error(t, err)
	managementErr, ok := AsManagementError(err)
	require.True(t, ok)
	require.Equal(t, CodeOwnershipConflict, managementErr.Code)
	require.Contains(t, managementErr.Message, "orphan")
	require.Zero(t, typed.totalCalls())
	require.Zero(t, gb.startCalls)
}

func TestRecordingOpsHLSUsesTypedRecorderWithoutGBLifecycle(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: false}
	gb := &recordingOpsGBService{}
	managed := staticOwnershipSource{evidence: OwnershipEvidence{
		Type: OwnershipTypeManaged, ResourceType: "managed", Key: "resource-1", Confidence: OwnershipConfidenceProven,
	}}
	ops := newRecordingOpsForTest(t, typed, gb, false, []OwnershipSource{managed})

	result, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderHLS))
	require.NoError(t, err)
	require.Equal(t, RecordingStateRecording, result.State)
	require.True(t, result.Recording)
	require.Equal(t, 1, typed.startCalls)
	require.Equal(t, 2, typed.readCalls)
	require.Zero(t, gb.startCalls, "HLS must not write GB recording session state")
}

func TestRecordingOpsStartDoesNotReportRecordingWhenReadbackIsFalse(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: false, keepFalse: true}
	gb := &recordingOpsGBService{typed: typed}
	ops := newRecordingOpsForTest(t, typed, gb, true, nil)

	result, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.Error(t, err)
	require.Equal(t, RecordingStateUnknown, result.State)
	require.False(t, result.Recording)
	require.True(t, result.Retryable)
	require.NotEqual(t, RecordingStateRecording, result.State)
}

func TestRecordingOpsStatusWithoutManualLeaseFailsClosedAfterRestart(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: true}
	ops := NewRecordingOps(RecordingOpsConfig{
		Resolver:        NewOwnershipResolver(OwnershipDependencies{Presence: staticPresence(true)}),
		RecorderFactory: func(OwnershipTarget) TypedRecorderClient { return typed },
	})

	result, err := ops.Status(context.Background(), recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderHLS))
	require.NoError(t, err)
	require.Equal(t, RecordingStateUnknown, result.State)
	require.Equal(t, RecordingExternalStateRecording, result.ExternalState)
	require.True(t, result.Recording, "runtime state may be observed, but it cannot recreate manual ownership")
	require.Empty(t, result.LeaseID)
}

func TestRecordingOpsStopBlocksBusinessOwnerWithoutCallingRecorder(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: true}
	gb := &recordingOpsGBService{typed: typed}
	plan := staticOwnershipSource{evidence: OwnershipEvidence{
		Type: OwnershipTypeRecordingPlan, Key: "plan-1", Confidence: OwnershipConfidenceProven,
	}}
	ops := newRecordingOpsForTest(t, typed, gb, true, []OwnershipSource{plan})
	_, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.NoError(t, err)

	result, err := ops.Stop(context.Background(), 7, recordingStopRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.Error(t, err)
	require.Equal(t, RecordingStateRecording, result.State)
	managementErr, ok := AsManagementError(err)
	require.True(t, ok)
	require.Equal(t, CodeOwnershipConflict, managementErr.Code)
	require.Zero(t, gb.stopCalls)
	require.Zero(t, typed.stopCalls)
}

func TestRecordingOpsOrdinaryStopBlocksPlanContinuousUnknownAndConflicted(t *testing.T) {
	tests := []struct {
		name     string
		evidence OwnershipEvidence
	}{
		{name: "recording-plan", evidence: OwnershipEvidence{
			Type: OwnershipTypeRecordingPlan, Key: "plan-1", Confidence: OwnershipConfidenceProven,
		}},
		{name: "continuous-recording", evidence: OwnershipEvidence{
			Type: OwnershipTypeContinuousRecording, Key: "continuous-1", Confidence: OwnershipConfidenceProven,
		}},
		{name: "other-business-owner", evidence: OwnershipEvidence{
			Type: OwnershipTypeRealtimePlayback, Key: "live-1", Confidence: OwnershipConfidenceProven,
		}},
		{name: "unknown", evidence: OwnershipEvidence{
			Type: OwnershipTypeUnknown, Key: "unknown-1", Confidence: OwnershipConfidenceUncertain,
		}},
		{name: "conflicted", evidence: OwnershipEvidence{
			Type: OwnershipTypeRealtimePlayback, Key: "live-1", Confidence: OwnershipConfidenceProven, Conflict: true,
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			typed := &recordingOpsTypedRecorder{state: true}
			gb := &recordingOpsGBService{typed: typed}
			ops := newRecordingOpsForTest(t, typed, gb, true, []OwnershipSource{
				staticOwnershipSource{evidence: test.evidence},
			})
			_, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
			require.NoError(t, err)

			result, err := ops.Stop(context.Background(), 7, recordingStopRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
			require.Error(t, err)
			managementErr, ok := AsManagementError(err)
			require.True(t, ok)
			require.Equal(t, CodeOwnershipConflict, managementErr.Code)
			require.Equal(t, RecordingStateRecording, result.State)
			require.Zero(t, gb.stopCalls)
			require.Zero(t, typed.stopCalls)
		})
	}
}

func TestRecordingOpsStartFailureDoesNotReserveManualLease(t *testing.T) {
	tests := []struct {
		name      string
		firstUser uint
		retryUser uint
	}{
		{name: "same-user-retry", firstUser: 7, retryUser: 7},
		{name: "other-authorized-user-retry", firstUser: 7, retryUser: 8},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			typed := &recordingOpsTypedRecorder{state: false}
			gb := &recordingOpsGBService{typed: typed, startErr: errors.New("start failed")}
			ops := newRecordingOpsForTest(t, typed, gb, true, nil)

			failed, err := ops.Start(context.Background(), test.firstUser, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
			require.Error(t, err)
			require.Equal(t, RecordingStateUnknown, failed.State)
			require.True(t, failed.Retryable)
			require.Equal(t, 1, gb.startCalls)

			gb.startErr = nil
			retried, err := ops.Start(context.Background(), test.retryUser, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
			require.NoError(t, err)
			require.Equal(t, RecordingStateRecording, retried.State)
			require.Equal(t, 2, gb.startCalls)
		})
	}
}

func TestRecordingOpsConcurrentStartHasOneCreator(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: false}
	gb := &recordingOpsGBService{typed: typed}
	ops := newRecordingOpsForTest(t, typed, gb, true, nil)
	request := recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderMP4)

	results := make(chan struct {
		result RecordingResult
		err    error
	}, 2)
	var wait sync.WaitGroup
	for _, userID := range []uint{7, 8} {
		wait.Add(1)
		go func(userID uint) {
			defer wait.Done()
			result, err := ops.Start(context.Background(), userID, request)
			results <- struct {
				result RecordingResult
				err    error
			}{result: result, err: err}
		}(userID)
	}
	wait.Wait()
	close(results)

	var successes, conflicts int
	for result := range results {
		if result.err == nil {
			successes++
			require.Equal(t, RecordingStateRecording, result.result.State)
			continue
		}
		managementErr, ok := AsManagementError(result.err)
		require.True(t, ok)
		require.Equal(t, CodeOwnershipConflict, managementErr.Code)
		conflicts++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	require.Equal(t, 1, gb.startCalls)
}

func TestRecordingOpsBlockedTargetDoesNotBlockAnotherTarget(t *testing.T) {
	targetA := testRecordingOpsTarget()
	targetB := targetA
	targetB.Media.Stream = "34020000001320000002"
	blocked := &recordingOpsBlockingRecorder{
		recordingOpsTypedRecorder: &recordingOpsTypedRecorder{state: false},
		entered:                   make(chan struct{}),
		release:                   make(chan struct{}),
	}
	recorderB := &recordingOpsTypedRecorder{state: false}
	managed := staticOwnershipSource{evidence: OwnershipEvidence{
		Type: OwnershipTypeManaged, ResourceType: "managed", Key: "resource-1", Confidence: OwnershipConfidenceProven,
	}}
	ops := NewRecordingOps(RecordingOpsConfig{
		Resolver: NewOwnershipResolver(OwnershipDependencies{Presence: staticPresence(true), Sources: []OwnershipSource{managed}}),
		RecorderFactory: func(target OwnershipTarget) TypedRecorderClient {
			if target.Media.Stream == targetA.Media.Stream {
				return blocked
			}
			return recorderB
		},
	})

	aDone := make(chan error, 1)
	go func() {
		_, err := ops.Start(context.Background(), 7, recordingStartRequest(targetA, zlm.RecorderHLS))
		aDone <- err
	}()
	select {
	case <-blocked.entered:
	case <-time.After(time.Second):
		t.Fatal("target A did not reach its blocked recorder call")
	}

	bDone := make(chan error, 1)
	go func() {
		_, err := ops.Start(context.Background(), 8, recordingStartRequest(targetB, zlm.RecorderHLS))
		bDone <- err
	}()
	select {
	case err := <-bDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		close(blocked.release)
		t.Fatal("a slow target blocked an unrelated target")
	}
	close(blocked.release)
	require.NoError(t, <-aDone)
}

func TestRecordingOpsStartRollbackFailureIsExplicitlyUncertain(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: false, keepFalse: true}
	gb := &recordingOpsGBService{typed: typed, stopErr: errors.New("rollback failed")}
	ops := newRecordingOpsForTest(t, typed, gb, true, nil)

	result, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.Error(t, err)
	require.Equal(t, RecordingStateUnknown, result.State)
	require.Equal(t, RecordingExternalStateUnknown, result.ExternalState)
	require.True(t, result.RollbackUncertain)
	require.True(t, result.Retryable)
	managementErr, ok := AsManagementError(err)
	require.True(t, ok)
	require.True(t, managementErr.Retryable)
	require.Equal(t, 1, gb.stopCalls, "a successful start command must receive compensating cleanup")
}

func TestExistingRecordingServiceAdapterRollsBackAfterBeginPlaybackFailure(t *testing.T) {
	service := &recordingOpsExistingService{beginErr: errors.New("begin failed")}
	adapter := NewExistingRecordingServiceAdapter(service)
	err := adapter.StartManual(context.Background(), GBChannelRef{ChannelID: 1}, testRecordingOpsTarget().Media)
	require.Error(t, err)
	require.Equal(t, 1, service.enableCalls)
	require.Equal(t, 1, service.beginCalls)
	require.Equal(t, 1, service.disableCalls)

	service.disableErr = errors.New("disable failed")
	err = adapter.StartManual(context.Background(), GBChannelRef{ChannelID: 1}, testRecordingOpsTarget().Media)
	require.Error(t, err)
	require.ErrorIs(t, err, errRecordingRollbackUncertain)
	require.Equal(t, 2, service.enableCalls)
	require.Equal(t, 2, service.beginCalls)
	require.Equal(t, 2, service.disableCalls)
}

func TestRecordingOpsAuditRetainsAuthenticatedActor(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: true}
	gb := &recordingOpsGBService{typed: typed}
	events := make([]RecordingAuditEvent, 0, 3)
	ops := NewRecordingOps(RecordingOpsConfig{
		Resolver:         NewOwnershipResolver(OwnershipDependencies{Presence: staticPresence(true)}),
		RecorderFactory:  func(OwnershipTarget) TypedRecorderClient { return typed },
		ChannelResolver:  recordingOpsChannelResolver{available: true},
		RecordingService: gb,
		Audit: RecordingAuditFunc(func(_ context.Context, event RecordingAuditEvent) error {
			events = append(events, event)
			return nil
		}),
	})
	target := testRecordingOpsTarget()
	_, err := ops.Start(context.Background(), 7, recordingStartRequest(target, zlm.RecorderMP4))
	require.NoError(t, err)
	_, err = ops.Stop(context.Background(), 7, recordingStopRequest(target, zlm.RecorderMP4))
	require.NoError(t, err)
	_, err = ops.ForceStop(context.Background(), 99, RecordingForceStopRequest{Target: target, Type: zlm.RecorderMP4, Reason: "incident-42"})
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, uint(7), events[0].UserID)
	require.Equal(t, uint(7), events[1].UserID)
	require.Equal(t, uint(99), events[2].UserID)
}

func TestRecordingOpsOnlyCreatorCanStopAndStopFailureRemainsRetryable(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: true}
	gb := &recordingOpsGBService{typed: typed, stopErr: errors.New("upstream stop failed")}
	ops := newRecordingOpsForTest(t, typed, gb, true, nil)
	_, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.NoError(t, err)

	request := recordingStopRequest(testRecordingOpsTarget(), zlm.RecorderMP4)
	request.UserID = 7
	_, err = ops.Stop(context.Background(), 8, request)
	require.Error(t, err, "a different user cannot release the manual lease")
	require.Zero(t, gb.stopCalls)

	failed, err := ops.Stop(context.Background(), 7, recordingStopRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.Error(t, err)
	require.Equal(t, RecordingStateStopping, failed.State)
	require.True(t, failed.Retryable)
	require.NotEqual(t, RecordingStateStopped, failed.State)
	managementErr, ok := AsManagementError(err)
	require.True(t, ok)
	require.True(t, managementErr.Retryable)
	require.Equal(t, 1, gb.stopCalls)

	gb.stopErr = nil
	retried, err := ops.Stop(context.Background(), 7, recordingStopRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.NoError(t, err)
	require.Equal(t, RecordingStateStopped, retried.State)
	require.Equal(t, 2, gb.stopCalls)

	idempotent, err := ops.Stop(context.Background(), 7, recordingStopRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.NoError(t, err)
	require.Equal(t, RecordingStateStopped, idempotent.State)
	require.Equal(t, 2, gb.stopCalls, "stopped manual recording is idempotent")
}

func TestRecordingOpsFingerprintChangePreventsStopExecution(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: true}
	gb := &recordingOpsGBService{typed: typed}
	mutable := &recordingOpsMutableOwnership{}
	ops := newRecordingOpsForTest(t, typed, gb, true, []OwnershipSource{mutable})
	_, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.NoError(t, err)

	preflight, err := ops.PreflightStop(context.Background(), 7, recordingStopRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.NoError(t, err)
	mutable.mu.Lock()
	mutable.evidence = OwnershipEvidence{Type: OwnershipTypeContinuousRecording, Key: "continuous-1", Confidence: OwnershipConfidenceProven}
	mutable.mu.Unlock()
	result, err := ops.Stop(context.Background(), 7, RecordingStopRequest{Target: testRecordingOpsTarget(), Type: zlm.RecorderMP4, Preflight: &preflight})
	require.Error(t, err)
	require.Equal(t, RecordingStateRecording, result.State)
	require.Zero(t, gb.stopCalls)
	managementErr, ok := AsManagementError(err)
	require.True(t, ok)
	require.Equal(t, CodeOwnershipConflict, managementErr.Code)
}

func TestRecordingOpsForceStopIsSeparateAndRequiresReason(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: true}
	managed := staticOwnershipSource{evidence: OwnershipEvidence{
		Type: OwnershipTypeManaged, ResourceType: "managed", Key: "resource-1", Confidence: OwnershipConfidenceProven,
	}}
	ops := newRecordingOpsForTest(t, typed, nil, false, []OwnershipSource{managed})
	_, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderHLS))
	require.NoError(t, err)

	_, err = ops.ForceStop(context.Background(), 7, RecordingForceStopRequest{Target: testRecordingOpsTarget(), Type: zlm.RecorderHLS})
	require.Error(t, err)
	validation, ok := err.(*ValidationError)
	require.True(t, ok)
	require.Contains(t, validation.Fields, "reason")
	require.Zero(t, typed.stopCalls)

	result, err := ops.ForceStop(context.Background(), 7, RecordingForceStopRequest{
		Target: testRecordingOpsTarget(), Type: zlm.RecorderHLS, Reason: "incident-42",
	})
	require.NoError(t, err)
	require.Equal(t, RecordingStateStopped, result.State)
	require.Equal(t, 1, typed.stopCalls)
}

func TestRecordingOpsForceStopFingerprintChangePreventsExecution(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: true}
	mutable := &recordingOpsMutableOwnership{evidence: OwnershipEvidence{
		Type: OwnershipTypeManaged, ResourceType: "managed", Key: "resource-1", Confidence: OwnershipConfidenceProven,
	}}
	ops := newRecordingOpsForTest(t, typed, nil, false, []OwnershipSource{mutable})
	_, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderHLS))
	require.NoError(t, err)

	preflight, err := ops.PreflightForceStop(context.Background(), RecordingForceStopRequest{
		Target: testRecordingOpsTarget(), Type: zlm.RecorderHLS, Reason: "incident-42",
	})
	require.NoError(t, err)
	mutable.mu.Lock()
	mutable.evidence = OwnershipEvidence{
		Type: OwnershipTypeContinuousRecording, Key: "continuous-1", Confidence: OwnershipConfidenceProven,
	}
	mutable.mu.Unlock()

	result, err := ops.ForceStop(context.Background(), 99, RecordingForceStopRequest{
		Target: testRecordingOpsTarget(), Type: zlm.RecorderHLS, Reason: "incident-42", Preflight: &preflight,
	})
	require.Error(t, err)
	managementErr, ok := AsManagementError(err)
	require.True(t, ok)
	require.Equal(t, CodeOwnershipConflict, managementErr.Code)
	require.Equal(t, RecordingStateRecording, result.State)
	require.Zero(t, typed.stopCalls, "force may bypass owner policy but not a changed fingerprint")
}

func newRecordingOpsForTest(t *testing.T, typed *recordingOpsTypedRecorder, gb *recordingOpsGBService, gbChannel bool, sources []OwnershipSource) *RecordingOps {
	t.Helper()
	resolver := NewOwnershipResolver(OwnershipDependencies{Presence: staticPresence(true), Sources: sources})
	return NewRecordingOps(RecordingOpsConfig{
		Resolver:         resolver,
		RecorderFactory:  func(OwnershipTarget) TypedRecorderClient { return typed },
		ChannelResolver:  recordingOpsChannelResolver{available: gbChannel},
		RecordingService: gb,
	})
}

func testRecordingOpsTarget() OwnershipTarget {
	return OwnershipTarget{NodeID: 7, Media: MediaIdentity{
		Schema: "rtsp", Vhost: gbmodels.DefaultRecordingVHost, App: gbmodels.DefaultRecordingApp, Stream: "34020000001320000001",
	}}
}

func recordingStartRequest(target OwnershipTarget, recorderType zlm.RecorderType) RecordingStartRequest {
	return RecordingStartRequest{Target: target, Type: recorderType}
}

func recordingStopRequest(target OwnershipTarget, recorderType zlm.RecorderType) RecordingStopRequest {
	return RecordingStopRequest{Target: target, Type: recorderType}
}

type recordingOpsChannelResolver struct{ available bool }

func (r recordingOpsChannelResolver) ResolveGBChannel(_ context.Context, target OwnershipTarget) (GBChannelRef, bool, error) {
	if !r.available {
		return GBChannelRef{}, false, nil
	}
	return GBChannelRef{ChannelID: 1, DeviceID: "device-1", StreamID: target.Media.Stream, NodeID: target.NodeID}, true, nil
}

type recordingOpsTypedRecorder struct {
	mu         sync.Mutex
	state      bool
	keepFalse  bool
	startErr   error
	stopErr    error
	readErr    error
	startCalls int
	stopCalls  int
	readCalls  int
}

type recordingOpsBlockingRecorder struct {
	*recordingOpsTypedRecorder
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *recordingOpsBlockingRecorder) IsRecordingWithType(ctx context.Context, vhost, app, stream string, recorderType zlm.RecorderType) (bool, error) {
	r.once.Do(func() { close(r.entered) })
	select {
	case <-r.release:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return r.recordingOpsTypedRecorder.IsRecordingWithType(ctx, vhost, app, stream, recorderType)
}

func (r *recordingOpsTypedRecorder) StartRecordWithType(context.Context, string, string, string, zlm.RecorderType, int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startCalls++
	if r.startErr == nil && !r.keepFalse {
		r.state = true
	}
	return r.startErr
}

func (r *recordingOpsTypedRecorder) StopRecordWithType(context.Context, string, string, string, zlm.RecorderType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopCalls++
	if r.stopErr == nil {
		r.state = false
	}
	return r.stopErr
}

func (r *recordingOpsTypedRecorder) IsRecordingWithType(context.Context, string, string, string, zlm.RecorderType) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readCalls++
	return r.state, r.readErr
}

func (r *recordingOpsTypedRecorder) totalCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startCalls + r.stopCalls + r.readCalls
}

type recordingOpsGBService struct {
	typed      *recordingOpsTypedRecorder
	startErr   error
	stopErr    error
	startCalls int
	stopCalls  int
}

type recordingOpsExistingService struct {
	beginErr   error
	disableErr error
	enableCalls,
	beginCalls,
	disableCalls int
}

func (s *recordingOpsExistingService) Enable(context.Context, uint) (*gbmodels.GbChannel, error) {
	s.enableCalls++
	return &gbmodels.GbChannel{ID: 1}, nil
}

func (s *recordingOpsExistingService) BeginPlayback(context.Context, string) error {
	s.beginCalls++
	return s.beginErr
}

func (s *recordingOpsExistingService) Disable(context.Context, uint) (*gbmodels.GbChannel, error) {
	s.disableCalls++
	if s.disableErr != nil {
		return nil, s.disableErr
	}
	return &gbmodels.GbChannel{ID: 1}, nil
}

func (s *recordingOpsGBService) StartManual(context.Context, GBChannelRef, MediaIdentity) error {
	s.startCalls++
	if s.startErr == nil && s.typed != nil && !s.typed.keepFalse {
		s.typed.mu.Lock()
		s.typed.state = true
		s.typed.mu.Unlock()
	}
	return s.startErr
}

func (s *recordingOpsGBService) StopManual(context.Context, GBChannelRef, MediaIdentity) error {
	s.stopCalls++
	if s.stopErr == nil && s.typed != nil {
		s.typed.mu.Lock()
		s.typed.state = false
		s.typed.mu.Unlock()
	}
	return s.stopErr
}

type recordingOpsMutableOwnership struct {
	mu       sync.Mutex
	evidence OwnershipEvidence
}

func (s *recordingOpsMutableOwnership) Resolve(context.Context, OwnershipTarget) ([]OwnershipEvidence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.evidence.Type == "" {
		return nil, nil
	}
	return []OwnershipEvidence{s.evidence}, nil
}
