package management

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

var errMutationGuardRejected = errors.New("mutation guard rejected")

func TestRecordingOpsMP4MutationGuardRejectsStartStopAndRollback(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: false}
	gb := &recordingOpsGBService{typed: typed}
	var calls atomic.Int32
	reject := atomic.Bool{}
	ops := NewRecordingOps(RecordingOpsConfig{
		Resolver:         NewOwnershipResolver(OwnershipDependencies{Presence: staticPresence(true)}),
		RecorderFactory:  func(OwnershipTarget) TypedRecorderClient { return typed },
		ChannelResolver:  recordingOpsChannelResolver{available: true},
		RecordingService: gb,
		MP4MutationGuard: func(ctx context.Context, target OwnershipTarget, fn func(context.Context) error) error {
			calls.Add(1)
			if !reject.Load() {
				return fn(ctx)
			}
			return errMutationGuardRejected
		},
	})
	target := testRecordingOpsTarget()

	result, err := ops.Start(context.Background(), 7, recordingStartRequest(target, zlm.RecorderMP4))
	require.NoError(t, err)
	require.Equal(t, RecordingStateRecording, result.State)
	require.Equal(t, 1, gb.startCalls)

	// A stop request cannot reach the old GB lifecycle while the gate rejects.
	reject.Store(true)
	_, err = ops.Stop(context.Background(), 7, recordingStopRequest(target, zlm.RecorderMP4))
	require.ErrorIs(t, err, errMutationGuardRejected)
	require.Zero(t, gb.stopCalls)

	// Force stop shares the same guarded MP4 path.
	_, err = ops.ForceStop(context.Background(), 7, RecordingForceStopRequest{Target: target, Type: zlm.RecorderMP4, Reason: "test"})
	require.ErrorIs(t, err, errMutationGuardRejected)
	require.Zero(t, gb.stopCalls)
	require.GreaterOrEqual(t, calls.Load(), int32(1))

	// A failed readback must guard its rollback stop as well.
	typed = &recordingOpsTypedRecorder{state: false, keepFalse: true}
	gb = &recordingOpsGBService{typed: typed}
	reject.Store(false)
	rollbackCalls := atomic.Int32{}
	rollbackOps := NewRecordingOps(RecordingOpsConfig{
		Resolver:         NewOwnershipResolver(OwnershipDependencies{Presence: staticPresence(true)}),
		RecorderFactory:  func(OwnershipTarget) TypedRecorderClient { return typed },
		ChannelResolver:  recordingOpsChannelResolver{available: true},
		RecordingService: gb,
		MP4MutationGuard: func(ctx context.Context, target OwnershipTarget, fn func(context.Context) error) error {
			if rollbackCalls.Add(1) > 1 {
				return errMutationGuardRejected
			}
			return fn(ctx)
		},
	})
	_, err = rollbackOps.Start(context.Background(), 7, recordingStartRequest(target, zlm.RecorderMP4))
	require.ErrorIs(t, err, errMutationGuardRejected)
	require.Zero(t, gb.stopCalls)
	require.GreaterOrEqual(t, rollbackCalls.Load(), int32(2))
}

func TestRecordingOpsMP4MutationGuardPassesCallbackContext(t *testing.T) {
	type contextKey struct{}
	typed := &recordingOpsTypedRecorder{state: false}
	gb := &recordingOpsContextGBService{typed: typed, key: contextKey{}}
	ops := NewRecordingOps(RecordingOpsConfig{
		Resolver:         NewOwnershipResolver(OwnershipDependencies{Presence: staticPresence(true)}),
		RecorderFactory:  func(OwnershipTarget) TypedRecorderClient { return typed },
		ChannelResolver:  recordingOpsChannelResolver{available: true},
		RecordingService: gb,
		MP4MutationGuard: func(ctx context.Context, _ OwnershipTarget, fn func(context.Context) error) error {
			return fn(context.WithValue(ctx, contextKey{}, "guarded"))
		},
	})

	result, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderMP4))
	require.NoError(t, err)
	require.Equal(t, "guarded", gb.startValue)
	require.Equal(t, RecordingStateRecording, result.State)
}

func TestRecordingOpsHLSDoesNotInvokeMP4MutationGuard(t *testing.T) {
	typed := &recordingOpsTypedRecorder{state: false}
	var guardCalls atomic.Int32
	managed := staticOwnershipSource{evidence: OwnershipEvidence{
		Type: OwnershipTypeManaged, ResourceType: "managed", Key: "resource-1", Confidence: OwnershipConfidenceProven,
	}}
	ops := NewRecordingOps(RecordingOpsConfig{
		Resolver:        NewOwnershipResolver(OwnershipDependencies{Presence: staticPresence(true), Sources: []OwnershipSource{managed}}),
		RecorderFactory: func(OwnershipTarget) TypedRecorderClient { return typed },
		MP4MutationGuard: func(context.Context, OwnershipTarget, func(context.Context) error) error {
			guardCalls.Add(1)
			return errMutationGuardRejected
		},
	})

	result, err := ops.Start(context.Background(), 7, recordingStartRequest(testRecordingOpsTarget(), zlm.RecorderHLS))
	require.NoError(t, err)
	require.Equal(t, RecordingStateRecording, result.State)
	require.Zero(t, guardCalls.Load())
	require.Equal(t, 1, typed.startCalls)
}

func TestExecutorTypedRecorderMP4MutationGuardRejectsBeforeExecutorWrite(t *testing.T) {
	var calls atomic.Int32
	recorder := NewExecutorTypedRecorder(&NodeExecutor{}, testRecordingOpsTarget()).SetMP4MutationGuard(
		func(context.Context, OwnershipTarget, func(context.Context) error) error {
			calls.Add(1)
			return errMutationGuardRejected
		},
	)

	err := recorder.StartRecordWithType(context.Background(), "vhost", "app", "stream", zlm.RecorderMP4, 0)
	require.ErrorIs(t, err, errMutationGuardRejected)
	err = recorder.StopRecordWithType(context.Background(), "vhost", "app", "stream", zlm.RecorderMP4)
	require.ErrorIs(t, err, errMutationGuardRejected)
	require.Equal(t, int32(2), calls.Load())
}

func TestDirectSourceCloseAdaptersRejectBeforeExecutorWrite(t *testing.T) {
	guard := func(context.Context, OwnershipTarget, func(context.Context) error) error {
		return errMutationGuardRejected
	}

	streamExecutor := NewNodeMediaExecutor(&NodeExecutor{}).SetSourceCloseGuard(guard)
	_, err := streamExecutor.CloseStream(context.Background(), 1, zlm.StreamTarget{Schema: "rtsp", VHost: "v", App: "a", Stream: "s"}, false)
	require.ErrorIs(t, err, errMutationGuardRejected)

	rtpAdapter := NewNodeRTPClientAdapter(&NodeExecutor{}).SetSourceCloseGuard(guard)
	_, err = rtpAdapter.CloseRtpServer(context.Background(), 1, "v", "a", "s")
	require.ErrorIs(t, err, errMutationGuardRejected)
}

func TestStreamServiceSourceCloseGuardRejectsBeforeExternalWrite(t *testing.T) {
	identity := t9Identity("guarded-close")
	target := OwnershipTarget{NodeID: 1, Media: identity}
	fingerprint := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fresh := &t9FreshReader{infos: map[string][]*zlm.MediaInfo{t9MediaKey(1, identity): {t9MediaInfo(identity), t9MediaInfo(identity), nil}}}
	ownership := &t9Ownership{preflight: OwnershipPreflight{
		Target:      target,
		Snapshot:    OwnershipSnapshot{Target: target, Present: true, PresenceKnown: true, Status: OwnershipStatusManaged, Fingerprint: fingerprint},
		Fingerprint: fingerprint,
	}}
	action := &t9MediaAction{closeResult: zlm.CloseStreamResult{Closed: true}}
	service := NewStreamService(StreamServiceDependencies{
		Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}},
		Fresh:    fresh, Executor: action, Ownership: ownership,
		SourceCloseGuard: func(context.Context, OwnershipTarget, func(context.Context) error) error {
			return errMutationGuardRejected
		},
	})

	_, err := service.CloseStream(context.Background(), CloseStreamRequest{Target: target, Fingerprint: fingerprint})
	require.ErrorIs(t, err, errMutationGuardRejected)
	require.Zero(t, action.closeCalls)
}

func TestStreamServiceBatchSourceCloseGuardRejectsBeforeExternalWrite(t *testing.T) {
	identity := t9Identity("guarded-batch-close")
	target := OwnershipTarget{NodeID: 1, Media: identity}
	fingerprint := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ownership := &t9Ownership{}
	action := &t9MediaAction{closeResult: zlm.CloseStreamResult{Closed: true}}
	service := NewStreamService(StreamServiceDependencies{
		Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}},
		Fresh: &t9FreshReader{infos: map[string][]*zlm.MediaInfo{
			t9MediaKey(1, identity): {t9MediaInfo(identity), t9MediaInfo(identity)},
		}},
		Executor: action, Ownership: ownership,
		SourceCloseGuard: func(context.Context, OwnershipTarget, func(context.Context) error) error {
			return errMutationGuardRejected
		},
	})
	preflight := OwnershipBatchPreflight{
		Targets: []OwnershipTarget{target},
		Snapshots: []OwnershipSnapshot{{
			Target: target, Present: true, PresenceKnown: true, Status: OwnershipStatusManaged,
			Owners: []OwnershipEvidence{{Type: OwnershipTypeManaged, Confidence: OwnershipConfidenceProven}},
		}},
		Fingerprint: fingerprint,
	}
	result, err := service.CloseStreams(context.Background(), preflight)
	require.ErrorIs(t, err, errMutationGuardRejected)
	require.Zero(t, action.closeCalls)
	require.False(t, result.Uncertain, "a gate rejection did not invoke an external close")
}

func TestRTPServiceSourceCloseGuardRejectsBeforeExternalWriteAndLedgerCleanup(t *testing.T) {
	key := rtpLedgerKey(7, "guarded-rtp")
	ledger := &t11RTPLedger{rows: map[string]*gbmodels.GbZLMManagedResource{
		key: {NodeID: 7, ResourceType: ResourceTypeRTPServer, ResourceKey: "guarded-rtp", Schema: "rtp", Vhost: "__defaultVhost__", App: "rtp", Stream: "guarded-rtp"},
	}}
	client := &t11RTPClient{list: []zlm.RtpServerInfo{{Key: "guarded-rtp", VHost: "__defaultVhost__", App: "rtp", StreamID: "guarded-rtp", Port: 41000}}}
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: &t11MutablePresence{present: true},
		Sources:  []OwnershipSource{t11ManagedSource{resourceType: ResourceTypeRTPServer, key: "guarded-rtp"}},
	})
	service := NewRTPService(RTPDependencies{
		Client: client, Ledger: ledger, Ownership: resolver,
		SourceCloseGuard: func(context.Context, OwnershipTarget, func(context.Context) error) error {
			return errMutationGuardRejected
		},
	})

	_, err := service.Close(context.Background(), RTPServerCloseRequest{NodeID: 7, VHost: "__defaultVhost__", App: "rtp", Stream: "guarded-rtp"})
	require.ErrorIs(t, err, errMutationGuardRejected)
	require.Zero(t, client.closeCalls)
	require.Nil(t, ledger.rows[key].TombstonedAt)
}

type recordingOpsContextGBService struct {
	typed      *recordingOpsTypedRecorder
	key        any
	startValue any
}

func (s *recordingOpsContextGBService) StartManual(ctx context.Context, _ GBChannelRef, _ MediaIdentity) error {
	s.startValue = ctx.Value(s.key)
	s.typed.mu.Lock()
	s.typed.state = true
	s.typed.mu.Unlock()
	return nil
}

func (s *recordingOpsContextGBService) StopManual(context.Context, GBChannelRef, MediaIdentity) error {
	return nil
}

func TestExecutorTypedRecorderGuardsActualWriteTuple(t *testing.T) {
	recorder := NewExecutorTypedRecorder(&NodeExecutor{}, OwnershipTarget{NodeID: 1, Media: MediaIdentity{Vhost: "original", App: "old", Stream: "old"}})
	recorder.SetMP4MutationGuard(func(_ context.Context, target OwnershipTarget, _ func(context.Context) error) error {
		require.Equal(t, "actual-vhost", target.Media.Vhost)
		require.Equal(t, "actual-app", target.Media.App)
		require.Equal(t, "actual-stream", target.Media.Stream)
		return errMutationGuardRejected
	})
	require.ErrorIs(t, recorder.StartRecordWithType(context.Background(), "actual-vhost", "actual-app", "actual-stream", zlm.RecorderMP4, 0), errMutationGuardRejected)
	require.ErrorIs(t, recorder.StopRecordWithType(context.Background(), "actual-vhost", "actual-app", "actual-stream", zlm.RecorderMP4), errMutationGuardRejected)
}
