package management

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

// RecordingState is the management-side state of a manual recorder lease.
// The state is intentionally process-local: it is not a replacement for the
// durable GB recording session or ZLM's actual recorder state.
type RecordingState string

const (
	RecordingStateUnknown   RecordingState = "unknown"
	RecordingStateStarting  RecordingState = "starting"
	RecordingStateRecording RecordingState = "recording"
	RecordingStateStopping  RecordingState = "stopping"
	RecordingStateStopped   RecordingState = "stopped"
)

// RecordingExternalState describes the last safe conclusion about ZLM after
// a command. It is kept separate from the process-local lease state so a
// failed confirmation can explicitly report an uncertain rollback.
type RecordingExternalState string

const (
	RecordingExternalStateUnknown   RecordingExternalState = "unknown"
	RecordingExternalStateRecording RecordingExternalState = "recording"
	RecordingExternalStateStopped   RecordingExternalState = "stopped"
)

// RecordingType aliases the ZLM recorder enum. HLS is 0 and MP4 is 1.
type RecordingType = zlm.RecorderType

const (
	RecordingTypeHLS = zlm.RecorderHLS
	RecordingTypeMP4 = zlm.RecorderMP4
	RecorderTypeHLS  = zlm.RecorderHLS
	RecorderTypeMP4  = zlm.RecorderMP4
)

// RecordingStartRequest is the typed request accepted by Start. NodeID and
// Media are convenience fields for callers that do not wrap an
// OwnershipTarget; Target wins when both forms are supplied.
type RecordingStartRequest struct {
	Target       OwnershipTarget  `json:"target"`
	NodeID       int64            `json:"nodeId,omitempty"`
	Media        MediaIdentity    `json:"media,omitempty"`
	Type         zlm.RecorderType `json:"type"`
	RecorderType zlm.RecorderType `json:"recorderType,omitempty"`
	MaxSecond    int              `json:"maxSecond,omitempty"`

	// UserID is deliberately excluded from JSON and is ignored by the service.
	// The authenticated caller must be supplied as the Start argument.
	UserID uint `json:"-"`
}

// Aliases keep the service usable by HTTP adapters whose existing naming uses
// ManualRecord rather than Recording.
type ManualRecordRequest = RecordingStartRequest
type ManualRecordingRequest = RecordingStartRequest
type StartRecordingRequest = RecordingStartRequest

// RecordingStopRequest has no trusted user identity. UserID, when populated
// by a decoded request body, is ignored. Force is rejected instead of being
// interpreted as an alternate force-stop path.
type RecordingStopRequest struct {
	Target       OwnershipTarget     `json:"target"`
	NodeID       int64               `json:"nodeId,omitempty"`
	Media        MediaIdentity       `json:"media,omitempty"`
	Type         zlm.RecorderType    `json:"type"`
	RecorderType zlm.RecorderType    `json:"recorderType,omitempty"`
	Fingerprint  string              `json:"fingerprint,omitempty"`
	Preflight    *RecordingPreflight `json:"-"`
	Force        bool                `json:"force,omitempty"`
	UserID       uint                `json:"-"`
}

type ManualRecordStopRequest = RecordingStopRequest
type StopRecordingRequest = RecordingStopRequest

// RecordingForceStopRequest is separate from RecordingStopRequest by design.
// A force operation cannot be smuggled into an ordinary request with a bool.
type RecordingForceStopRequest struct {
	Target       OwnershipTarget     `json:"target"`
	NodeID       int64               `json:"nodeId,omitempty"`
	Media        MediaIdentity       `json:"media,omitempty"`
	Type         zlm.RecorderType    `json:"type"`
	RecorderType zlm.RecorderType    `json:"recorderType,omitempty"`
	Reason       string              `json:"reason"`
	Fingerprint  string              `json:"fingerprint,omitempty"`
	Preflight    *RecordingPreflight `json:"-"`
}

type ForceStopRecordingRequest = RecordingForceStopRequest

// RecordingResult is the safe result returned by start/stop operations. It
// never contains a node Secret or a raw upstream response.
type RecordingResult struct {
	Target            OwnershipTarget        `json:"target"`
	Type              zlm.RecorderType       `json:"type"`
	State             RecordingState         `json:"state"`
	ExternalState     RecordingExternalState `json:"externalState"`
	RollbackUncertain bool                   `json:"rollbackUncertain,omitempty"`
	Recording         bool                   `json:"recording"`
	Retryable         bool                   `json:"retryable"`
	LeaseID           string                 `json:"leaseId,omitempty"`
	Ownership         OwnershipSnapshot      `json:"ownership"`
	Reason            string                 `json:"reason,omitempty"`
}

type ManualRecordResult = RecordingResult
type RecordingStatus = RecordingResult

// RecordingPreflight is the immutable confirmation token used by stop paths.
// The fingerprint includes the target, runtime presence, all T6 ownership
// evidence and the in-process manual lease evidence.
type RecordingPreflight struct {
	Target      OwnershipTarget   `json:"target"`
	Type        zlm.RecorderType  `json:"type"`
	UserID      uint              `json:"-"`
	LeaseID     string            `json:"leaseId,omitempty"`
	Snapshot    OwnershipSnapshot `json:"snapshot"`
	Ownership   OwnershipSnapshot `json:"-"`
	Fingerprint string            `json:"fingerprint"`
}

// TypedRecorderClient is the only ZLM recorder surface used by this service.
// It intentionally excludes arbitrary API names and query maps.
type TypedRecorderClient interface {
	StartRecordWithType(context.Context, string, string, string, zlm.RecorderType, int) error
	StopRecordWithType(context.Context, string, string, string, zlm.RecorderType) error
	IsRecordingWithType(context.Context, string, string, string, zlm.RecorderType) (bool, error)
}

type TypedRecorderFactory func(OwnershipTarget) TypedRecorderClient

// GBChannelRef is the minimum attribution needed to enter the existing GB
// recording lifecycle. A caller cannot invent an attribution without a
// GBChannelResolver proof.
type GBChannelRef struct {
	ChannelID uint   `json:"channelId"`
	DeviceID  string `json:"deviceId,omitempty"`
	StreamID  string `json:"streamId,omitempty"`
	NodeID    int64  `json:"nodeId,omitempty"`
}

// GBChannelResolver proves that a media target belongs to an existing GB
// channel. A false result is not an error; it means that an external/orphan
// MP4 must be rejected.
type GBChannelResolver interface {
	ResolveGBChannel(context.Context, OwnershipTarget) (GBChannelRef, bool, error)
}

type GBChannelResolverFunc func(context.Context, OwnershipTarget) (GBChannelRef, bool, error)

func (f GBChannelResolverFunc) ResolveGBChannel(ctx context.Context, target OwnershipTarget) (GBChannelRef, bool, error) {
	if f == nil {
		return GBChannelRef{}, false, nil
	}
	return f(ctx, target)
}

// ExistingRecordingService matches the public lifecycle of recording.Service
// without coupling this package to its repository or controller internals.
type ExistingRecordingService interface {
	Enable(context.Context, uint) (*gbmodels.GbChannel, error)
	BeginPlayback(context.Context, string) error
	Disable(context.Context, uint) (*gbmodels.GbChannel, error)
}

// GBRecordingService is the specialized adapter used for manual MP4. HLS
// deliberately never calls this interface.
type GBRecordingService interface {
	StartManual(context.Context, GBChannelRef, MediaIdentity) error
	StopManual(context.Context, GBChannelRef, MediaIdentity) error
}

type ManualRecordingService = GBRecordingService

// RecordingSessionLookup is optional and only enriches the in-process lease
// with the existing session id. It is never used to manufacture ZLM state.
type RecordingSessionLookup interface {
	FindLatestSessionByChannel(context.Context, uint) (*gbmodels.GbRecordingSession, error)
}

// RecordingAudit receives a redacted, typed operation result. An audit
// failure is surfaced after the recorder state is retained; it never changes
// a successful state back to stopped/failed.
type RecordingAudit interface {
	RecordRecordingOperation(context.Context, RecordingAuditEvent) error
}

type RecordingAuditFunc func(context.Context, RecordingAuditEvent) error

func (f RecordingAuditFunc) RecordRecordingOperation(ctx context.Context, event RecordingAuditEvent) error {
	if f == nil {
		return nil
	}
	return f(ctx, event)
}

type RecordingAuditEvent struct {
	Action            string                 `json:"action"`
	Target            OwnershipTarget        `json:"target"`
	Type              zlm.RecorderType       `json:"type"`
	UserID            uint                   `json:"userId"`
	LeaseID           string                 `json:"leaseId,omitempty"`
	Reason            string                 `json:"reason,omitempty"`
	State             RecordingState         `json:"state"`
	ExternalState     RecordingExternalState `json:"externalState"`
	RollbackUncertain bool                   `json:"rollbackUncertain,omitempty"`
	Recording         bool                   `json:"recording"`
	Retryable         bool                   `json:"retryable"`
	Success           bool                   `json:"success"`
}

type RecordingOpsConfig struct {
	// Executor is preferred in production: it supplies node state,
	// authorization and a per-operation deadline for typed ZLM calls.
	Executor  *NodeExecutor
	Resolver  *OwnershipResolver
	Ownership OwnershipDependencies

	RecorderFactory  TypedRecorderFactory
	ChannelResolver  GBChannelResolver
	RecordingService GBRecordingService
	// ExistingRecordingService is adapted to GBRecordingService when the
	// caller has a concrete recording.Service but no custom adapter.
	ExistingRecordingService ExistingRecordingService
	SessionLookup            RecordingSessionLookup
	Audit                    RecordingAudit
	MP4MutationGuard         MP4MutationGuard
}

type RecordingOps struct {
	resolver         *OwnershipResolver
	recorderFactory  TypedRecorderFactory
	channelResolver  GBChannelResolver
	recordingService GBRecordingService
	sessionLookup    RecordingSessionLookup
	auditSink        RecordingAudit
	mp4MutationGuard MP4MutationGuard

	mu          sync.Mutex
	leases      map[string]*manualRecordingLease
	locks       map[string]*sync.Mutex
	nextLeaseID uint64
}

type RecordingOperationService = RecordingOps

type manualRecordingLease struct {
	ID        string
	Target    OwnershipTarget
	Type      zlm.RecorderType
	UserID    uint
	State     RecordingState
	SessionID uint64
}

// NewRecordingOps constructs the manual control facade. No desired-state
// goroutine is started; leases are intentionally in-process and disappear on
// restart, where ordinary stop then fails closed as unknown.
func NewRecordingOps(config RecordingOpsConfig) *RecordingOps {
	resolver := config.Resolver
	if resolver == nil && (config.Ownership.Presence != nil || len(config.Ownership.Sources) > 0) {
		resolver = NewOwnershipResolver(config.Ownership)
	}
	service := config.RecordingService
	if service == nil && config.ExistingRecordingService != nil {
		service = NewExistingRecordingServiceAdapter(config.ExistingRecordingService)
	}
	factory := config.RecorderFactory
	if factory == nil && config.Executor != nil {
		executor := config.Executor
		factory = func(target OwnershipTarget) TypedRecorderClient {
			recorder := NewExecutorTypedRecorder(executor, target)
			recorder.mp4MutationGuard = config.MP4MutationGuard
			return recorder
		}
	}
	return &RecordingOps{
		resolver: resolver, recorderFactory: factory, channelResolver: config.ChannelResolver,
		recordingService: service, sessionLookup: config.SessionLookup, auditSink: config.Audit,
		mp4MutationGuard: config.MP4MutationGuard,
		leases:           make(map[string]*manualRecordingLease), locks: make(map[string]*sync.Mutex),
	}
}

func NewRecordingOperations(config RecordingOpsConfig) *RecordingOps { return NewRecordingOps(config) }

func NewRecordingOpsWithExecutor(executor *NodeExecutor, resolver *OwnershipResolver, channels GBChannelResolver, service GBRecordingService) *RecordingOps {
	return NewRecordingOps(RecordingOpsConfig{Executor: executor, Resolver: resolver, ChannelResolver: channels, RecordingService: service})
}

// targetLock serializes one target/type while all of the associated resolver,
// recording-service and ZLM calls are in flight. The global mutex protects
// only the lock and lease maps, so a slow target cannot block unrelated
// recordings and no external callback runs under that global mutex.
func (s *RecordingOps) targetLock(key string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks == nil {
		s.locks = make(map[string]*sync.Mutex)
	}
	lock := s.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.locks[key] = lock
	}
	return lock
}

func (s *RecordingOps) leaseForKey(key string) *manualRecordingLease {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leases[key]
}

func (s *RecordingOps) setLease(key string, lease *manualRecordingLease) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leases == nil {
		s.leases = make(map[string]*manualRecordingLease)
	}
	s.leases[key] = lease
}

func (s *RecordingOps) deleteLease(key string, lease *manualRecordingLease) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.leases[key]; ok && (lease == nil || current == lease) {
		delete(s.leases, key)
	}
}

// Preflight resolves one target without reserving a lease. Stop callers should
// use PreflightStop so the current manual creator and lease are bound too.
func (s *RecordingOps) Preflight(ctx context.Context, request RecordingStartRequest) (RecordingPreflight, error) {
	target, recorderType, err := validateStartRequest(request)
	if err != nil {
		return RecordingPreflight{}, err
	}
	if s == nil {
		return RecordingPreflight{}, NewInternalError(nodeIDString(target.NodeID), "recording operations are not configured")
	}
	lock := s.targetLock(leaseKey(target, recorderType))
	lock.Lock()
	defer lock.Unlock()
	snapshot, err := s.snapshot(ctx, target, nil)
	if err != nil {
		return RecordingPreflight{}, err
	}
	return newRecordingPreflight(target, recorderType, 0, nil, snapshot), nil
}

func (s *RecordingOps) PreflightTarget(ctx context.Context, target OwnershipTarget, recorderType zlm.RecorderType) (RecordingPreflight, error) {
	return s.Preflight(ctx, RecordingStartRequest{Target: target, Type: recorderType})
}

func (s *RecordingOps) PreflightStop(ctx context.Context, userID uint, request RecordingStopRequest) (RecordingPreflight, error) {
	if userID == 0 {
		return RecordingPreflight{}, NewValidationError(map[string]string{"userId": "must be provided by authentication"})
	}
	target, recorderType, err := validateStopRequest(request)
	if err != nil {
		return RecordingPreflight{}, err
	}
	if s == nil {
		return RecordingPreflight{}, NewInternalError(nodeIDString(target.NodeID), "recording operations are not configured")
	}
	lock := s.targetLock(leaseKey(target, recorderType))
	lock.Lock()
	defer lock.Unlock()
	lease := s.leaseForKey(leaseKey(target, recorderType))
	if lease == nil || lease.State == RecordingStateStopped {
		return RecordingPreflight{}, recordingOwnershipConflict(target, "manual recording lease is unavailable")
	}
	if lease.UserID != userID {
		return RecordingPreflight{}, recordingOwnershipConflict(target, "manual recording lease belongs to another user")
	}
	if recorderType == zlm.RecorderMP4 {
		if _, ok, err := s.resolveGBChannel(ctx, target); err != nil {
			return RecordingPreflight{}, err
		} else if !ok {
			return RecordingPreflight{}, orphanMP4Error(target)
		}
	}
	snapshot, err := s.snapshot(ctx, target, lease)
	if err != nil {
		return RecordingPreflight{}, err
	}
	return newRecordingPreflight(target, recorderType, userID, lease, snapshot), nil
}

func (s *RecordingOps) PreflightForceStop(ctx context.Context, request RecordingForceStopRequest) (RecordingPreflight, error) {
	target, recorderType, err := validateForceStopRequest(request)
	if err != nil {
		return RecordingPreflight{}, err
	}
	if s == nil {
		return RecordingPreflight{}, NewInternalError(nodeIDString(target.NodeID), "recording operations are not configured")
	}
	lock := s.targetLock(leaseKey(target, recorderType))
	lock.Lock()
	defer lock.Unlock()
	lease := s.leaseForKey(leaseKey(target, recorderType))
	if recorderType == zlm.RecorderMP4 {
		if _, ok, err := s.resolveGBChannel(ctx, target); err != nil {
			return RecordingPreflight{}, err
		} else if !ok {
			return RecordingPreflight{}, orphanMP4Error(target)
		}
	}
	snapshot, err := s.snapshot(ctx, target, lease)
	if err != nil {
		return RecordingPreflight{}, err
	}
	return newRecordingPreflight(target, recorderType, 0, lease, snapshot), nil
}

// Start creates one process-local manual lease and only reports recording
// after a typed isRecording readback confirms the state.
func (s *RecordingOps) Start(ctx context.Context, userID uint, request RecordingStartRequest) (RecordingResult, error) {
	ctx = nonNilContext(ctx)
	if userID == 0 {
		return RecordingResult{}, NewValidationError(map[string]string{"userId": "must be provided by authentication"})
	}
	target, recorderType, err := validateStartRequest(request)
	if err != nil {
		return RecordingResult{}, err
	}
	if s == nil {
		return RecordingResult{}, NewInternalError(nodeIDString(target.NodeID), "recording operations are not configured")
	}
	lock := s.targetLock(leaseKey(target, recorderType))
	lock.Lock()
	defer lock.Unlock()

	key := leaseKey(target, recorderType)
	if current := s.leaseForKey(key); current != nil && current.State != RecordingStateStopped {
		return s.resultForLease(current, nil), recordingOwnershipConflict(target, "manual recording is already active")
	}

	gbChannel, gbOwned, err := s.resolveGBChannel(ctx, target)
	if err != nil {
		return RecordingResult{}, err
	}
	if recorderType == zlm.RecorderMP4 && !gbOwned {
		return RecordingResult{}, orphanMP4Error(target)
	}
	if s.recorderFactory == nil {
		return RecordingResult{}, NewInternalError(nodeIDString(target.NodeID), "typed recorder is not configured")
	}
	if recorderType == zlm.RecorderMP4 && s.recordingService == nil {
		return RecordingResult{}, NewInternalError(nodeIDString(target.NodeID), "GB recording lifecycle is not configured")
	}

	// Probe ownership before reserving the lease. For HLS, a target must be
	// explicitly managed or proven to be a GB stream. MP4 attribution was
	// already proven above and is intentionally allowed to use the existing
	// recording service even when the target has other live consumers.
	base, err := s.snapshot(ctx, target, nil)
	if err != nil {
		return RecordingResult{}, err
	}
	if err := s.validateStartProvenance(target, recorderType, gbOwned, base); err != nil {
		return RecordingResult{}, err
	}

	lease := s.newLease(target, recorderType, userID)
	s.setLease(key, lease)
	preflightSnapshot, err := s.snapshot(ctx, target, lease)
	if err != nil {
		s.deleteLease(key, lease)
		return RecordingResult{}, err
	}
	preflight := newRecordingPreflight(target, recorderType, userID, lease, preflightSnapshot)
	current, err := s.recheck(ctx, preflight, lease, false)
	if err != nil {
		s.deleteLease(key, lease)
		return s.resultForLease(lease, &preflightSnapshot), err
	}
	if recorderType == zlm.RecorderMP4 {
		latestChannel, channelErr := s.recheckGBChannel(ctx, target, gbChannel)
		if channelErr != nil {
			return s.startFailure(lease, &current, channelErr)
		}
		gbChannel = latestChannel
	}

	recorder := s.recorderFactory(target)
	if recorder == nil {
		lease.State = RecordingStateUnknown
		return s.startFailure(lease, &current, errors.New("typed recorder is not configured"))
	}
	startedByRequest := false
	if recorderType == zlm.RecorderMP4 {
		alreadyRecording, readErr := recorder.IsRecordingWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType)
		if readErr != nil {
			return s.startFailure(lease, &current, NormalizeError(readErr, nodeIDString(target.NodeID)))
		}
		if alreadyRecording {
			return s.rejectExternalRecording(lease, target, recorderType, &current)
		}
		if err := runMutationGuard(ctx, s.mp4MutationGuard, target, func(operationCtx context.Context) error {
			return s.recordingService.StartManual(operationCtx, gbChannel, target.Media)
		}); err != nil {
			return s.startFailure(lease, &current, NormalizeError(err, nodeIDString(target.NodeID)))
		}
		startedByRequest = true
		if s.sessionLookup != nil {
			if session, lookupErr := s.sessionLookup.FindLatestSessionByChannel(ctx, gbChannel.ChannelID); lookupErr == nil && session != nil {
				lease.SessionID = session.ID
			}
		}
	} else {
		alreadyRecording, readErr := recorder.IsRecordingWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType)
		if readErr != nil {
			return s.startFailure(lease, &current, NormalizeError(readErr, nodeIDString(target.NodeID)))
		}
		if alreadyRecording {
			return s.rejectExternalRecording(lease, target, recorderType, &current)
		}
		if startErr := recorder.StartRecordWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType, request.MaxSecond); startErr != nil {
			return s.startFailure(lease, &current, NormalizeError(startErr, nodeIDString(target.NodeID)))
		}
		startedByRequest = true
	}

	confirmed, readErr := recorder.IsRecordingWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType)
	if readErr != nil {
		cause := recordingReadbackError(target, readErr)
		if startedByRequest {
			return s.startConfirmationFailure(ctx, lease, &current, recorder, gbChannel, target, recorderType, cause)
		}
		return s.startFailure(lease, &current, cause)
	}
	if !confirmed {
		cause := recordingReadbackError(target, errors.New("recording state is false after start"))
		if startedByRequest {
			return s.startConfirmationFailure(ctx, lease, &current, recorder, gbChannel, target, recorderType, cause)
		}
		return s.startFailure(lease, &current, cause)
	}
	lease.State = RecordingStateRecording
	result := s.resultForLease(lease, &current)
	if auditErr := s.audit(ctx, "start", result, userID, ""); auditErr != nil {
		return result, auditErr
	}
	return result, nil
}

func (s *RecordingOps) StartRecording(ctx context.Context, userID uint, request RecordingStartRequest) (RecordingResult, error) {
	return s.Start(ctx, userID, request)
}

func (s *RecordingOps) StartManualRecord(ctx context.Context, userID uint, request RecordingStartRequest) (RecordingResult, error) {
	return s.Start(ctx, userID, request)
}

// Stop releases only the current user's manual lease. A stop error preserves
// stopping/retryable state so a later call can retry the same lease.
func (s *RecordingOps) Stop(ctx context.Context, userID uint, request RecordingStopRequest) (RecordingResult, error) {
	ctx = nonNilContext(ctx)
	if userID == 0 {
		return RecordingResult{}, NewValidationError(map[string]string{"userId": "must be provided by authentication"})
	}
	target, recorderType, err := validateStopRequest(request)
	if err != nil {
		return RecordingResult{}, err
	}
	if s == nil {
		return RecordingResult{}, NewInternalError(nodeIDString(target.NodeID), "recording operations are not configured")
	}
	if err := validateRecordingPreflightBinding(request.Preflight, target, recorderType); err != nil {
		return RecordingResult{Target: target, Type: recorderType, State: RecordingStateUnknown}, err
	}
	lock := s.targetLock(leaseKey(target, recorderType))
	lock.Lock()
	defer lock.Unlock()

	lease := s.leaseForKey(leaseKey(target, recorderType))
	if lease == nil {
		return RecordingResult{Target: target, Type: recorderType, State: RecordingStateUnknown}, recordingOwnershipConflict(target, "manual recording ownership is unknown")
	}
	result := s.resultForLease(lease, nil)
	if lease.UserID != userID {
		return result, recordingOwnershipConflict(target, "manual recording lease belongs to another user")
	}
	if lease.State == RecordingStateStopped {
		return result, nil
	}
	if recorderType == zlm.RecorderMP4 {
		gbChannel, ok, resolveErr := s.resolveGBChannel(ctx, target)
		if resolveErr != nil {
			return result, resolveErr
		}
		if !ok {
			return result, orphanMP4Error(target)
		}
		return s.stopLocked(ctx, userID, request.Preflight, request.Fingerprint, lease, target, recorderType, gbChannel, false)
	}
	return s.stopLocked(ctx, userID, request.Preflight, request.Fingerprint, lease, target, recorderType, GBChannelRef{}, false)
}

func (s *RecordingOps) StopRecording(ctx context.Context, userID uint, request RecordingStopRequest) (RecordingResult, error) {
	return s.Stop(ctx, userID, request)
}

func (s *RecordingOps) StopManualRecord(ctx context.Context, userID uint, request RecordingStopRequest) (RecordingResult, error) {
	return s.Stop(ctx, userID, request)
}

// ForceStop is intentionally separate from Stop. It still rechecks the
// ownership fingerprint, but can operate on another owner's target when the
// caller supplies an explicit reason through this method.
func (s *RecordingOps) ForceStop(ctx context.Context, userID uint, request RecordingForceStopRequest) (RecordingResult, error) {
	ctx = nonNilContext(ctx)
	if userID == 0 {
		return RecordingResult{}, NewValidationError(map[string]string{"userId": "must be provided by authentication"})
	}
	target, recorderType, err := validateForceStopRequest(request)
	if err != nil {
		return RecordingResult{}, err
	}
	if s == nil {
		return RecordingResult{}, NewInternalError(nodeIDString(target.NodeID), "recording operations are not configured")
	}
	if err := validateRecordingPreflightBinding(request.Preflight, target, recorderType); err != nil {
		return RecordingResult{Target: target, Type: recorderType, State: RecordingStateUnknown}, err
	}
	lock := s.targetLock(leaseKey(target, recorderType))
	lock.Lock()
	defer lock.Unlock()

	gbChannel := GBChannelRef{}
	if recorderType == zlm.RecorderMP4 {
		var ok bool
		gbChannel, ok, err = s.resolveGBChannel(ctx, target)
		if err != nil {
			return RecordingResult{}, err
		}
		if !ok {
			return RecordingResult{}, orphanMP4Error(target)
		}
		if s.recordingService == nil {
			return RecordingResult{}, NewInternalError(nodeIDString(target.NodeID), "GB recording lifecycle is not configured")
		}
	}
	if s.recorderFactory == nil {
		return RecordingResult{}, NewInternalError(nodeIDString(target.NodeID), "typed recorder is not configured")
	}
	lease := s.leaseForKey(leaseKey(target, recorderType))
	preflight := request.Preflight
	if preflight == nil {
		snapshot, snapshotErr := s.snapshot(ctx, target, lease)
		if snapshotErr != nil {
			return RecordingResult{}, snapshotErr
		}
		value := newRecordingPreflight(target, recorderType, 0, lease, snapshot)
		if strings.TrimSpace(request.Fingerprint) != "" {
			value.Fingerprint = strings.TrimSpace(request.Fingerprint)
		}
		preflight = &value
	} else if strings.TrimSpace(request.Fingerprint) != "" {
		copy := *preflight
		copy.Fingerprint = strings.TrimSpace(request.Fingerprint)
		preflight = &copy
	}
	current, err := s.recheck(ctx, *preflight, lease, true)
	if err != nil {
		return s.resultForLeaseOrTarget(lease, target, recorderType, &current), err
	}
	if recorderType == zlm.RecorderMP4 {
		latestChannel, channelErr := s.recheckGBChannel(ctx, target, gbChannel)
		if channelErr != nil {
			return s.resultForLeaseOrTarget(lease, target, recorderType, &current), channelErr
		}
		gbChannel = latestChannel
	}
	recorder := s.recorderFactory(target)
	if recorder == nil {
		return s.resultForLeaseOrTarget(lease, target, recorderType, &current), NewInternalError(nodeIDString(target.NodeID), "typed recorder is not configured")
	}
	if recorderType == zlm.RecorderMP4 {
		if stopErr := runMutationGuard(ctx, s.mp4MutationGuard, target, func(operationCtx context.Context) error {
			return s.recordingService.StopManual(operationCtx, gbChannel, target.Media)
		}); stopErr != nil {
			if lease != nil {
				lease.State = RecordingStateStopping
			}
			result := s.resultForLeaseOrTarget(lease, target, recorderType, &current)
			result.State, result.Retryable, result.Reason = RecordingStateStopping, true, "recording stop is retryable"
			return result, retryableRecordingError(target, stopErr)
		}
		confirmed, readErr := recorder.IsRecordingWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType)
		if readErr != nil || confirmed {
			if lease != nil {
				lease.State = RecordingStateStopping
			}
			cause := readErr
			if cause == nil {
				cause = errors.New("recording state is still true after stop")
			}
			result := s.resultForLeaseOrTarget(lease, target, recorderType, &current)
			result.State, result.Retryable, result.Reason = RecordingStateStopping, true, "recording stop is retryable"
			return result, recordingReadbackError(target, cause)
		}
	} else {
		if stopErr := recorder.StopRecordWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType); stopErr != nil {
			if lease != nil {
				lease.State = RecordingStateStopping
			}
			result := s.resultForLeaseOrTarget(lease, target, recorderType, &current)
			result.State, result.Retryable, result.Reason = RecordingStateStopping, true, "recording stop is retryable"
			return result, retryableRecordingError(target, stopErr)
		}
		confirmed, readErr := recorder.IsRecordingWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType)
		if readErr != nil || confirmed {
			if lease != nil {
				lease.State = RecordingStateStopping
			}
			cause := readErr
			if cause == nil {
				cause = errors.New("recording state is still true after stop")
			}
			result := s.resultForLeaseOrTarget(lease, target, recorderType, &current)
			result.State, result.Retryable, result.Reason = RecordingStateStopping, true, "recording stop is retryable"
			return result, recordingReadbackError(target, cause)
		}
	}
	if lease != nil {
		lease.State = RecordingStateStopped
	}
	result := s.resultForLeaseOrTarget(lease, target, recorderType, &current)
	result.State, result.ExternalState, result.Recording, result.Retryable = RecordingStateStopped, RecordingExternalStateStopped, false, false
	if auditErr := s.audit(ctx, "force-stop", result, userID, request.Reason); auditErr != nil {
		return result, auditErr
	}
	return result, nil
}

func (s *RecordingOps) ForceStopRecording(ctx context.Context, userID uint, request RecordingForceStopRequest) (RecordingResult, error) {
	return s.ForceStop(ctx, userID, request)
}

// Status is a read-only convenience. It never creates a lease and reports
// unknown after a process restart unless the caller has a live manual lease.
func (s *RecordingOps) Status(ctx context.Context, request RecordingStartRequest) (RecordingResult, error) {
	ctx = nonNilContext(ctx)
	target, recorderType, err := validateStartRequest(request)
	if err != nil {
		return RecordingResult{}, err
	}
	if s == nil {
		return RecordingResult{}, NewInternalError(nodeIDString(target.NodeID), "recording operations are not configured")
	}
	lock := s.targetLock(leaseKey(target, recorderType))
	lock.Lock()
	defer lock.Unlock()
	lease := s.leaseForKey(leaseKey(target, recorderType))
	snapshot, snapshotErr := s.snapshot(ctx, target, lease)
	if snapshotErr != nil {
		return RecordingResult{}, snapshotErr
	}
	result := s.resultForLeaseOrTarget(lease, target, recorderType, &snapshot)
	if s.recorderFactory == nil {
		return result, NewInternalError(nodeIDString(target.NodeID), "typed recorder is not configured")
	}
	recorder := s.recorderFactory(target)
	if recorder == nil {
		return result, NewInternalError(nodeIDString(target.NodeID), "typed recorder is not configured")
	}
	actual, readErr := recorder.IsRecordingWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType)
	if readErr != nil {
		result.State, result.Recording, result.Retryable = RecordingStateUnknown, false, true
		return result, recordingReadbackError(target, readErr)
	}
	result.Recording = actual
	if actual {
		result.ExternalState = RecordingExternalStateRecording
		if lease != nil && lease.State == RecordingStateRecording {
			result.State = RecordingStateRecording
		} else {
			// The ZLM read is authoritative for runtime state, but after a
			// restart (or before this process commits a lease) manual ownership
			// is unknown and must not be reconstructed from that read alone.
			result.State = RecordingStateUnknown
		}
	} else if lease != nil && lease.State == RecordingStateStopped {
		result.State = RecordingStateStopped
		result.ExternalState = RecordingExternalStateStopped
	} else {
		result.State = RecordingStateUnknown
		result.ExternalState = RecordingExternalStateUnknown
	}
	return result, nil
}

func (s *RecordingOps) GetStatus(ctx context.Context, request RecordingStartRequest) (RecordingResult, error) {
	return s.Status(ctx, request)
}

func (s *RecordingOps) stopLocked(ctx context.Context, userID uint, provided *RecordingPreflight, requestFingerprint string, lease *manualRecordingLease, target OwnershipTarget, recorderType zlm.RecorderType, gbChannel GBChannelRef, force bool) (RecordingResult, error) {
	preflight := provided
	if preflight == nil {
		snapshot, err := s.snapshot(ctx, target, lease)
		if err != nil {
			return s.resultForLease(lease, nil), err
		}
		value := newRecordingPreflight(target, recorderType, userID, lease, snapshot)
		if strings.TrimSpace(requestFingerprint) != "" {
			value.Fingerprint = strings.TrimSpace(requestFingerprint)
		}
		preflight = &value
	} else if strings.TrimSpace(requestFingerprint) != "" {
		copy := *preflight
		copy.Fingerprint = strings.TrimSpace(requestFingerprint)
		preflight = &copy
	}
	current, err := s.recheck(ctx, *preflight, lease, force)
	if err != nil {
		return s.resultForLease(lease, &current), err
	}
	if recorderType == zlm.RecorderMP4 {
		latestChannel, channelErr := s.recheckGBChannel(ctx, target, gbChannel)
		if channelErr != nil {
			return s.resultForLease(lease, &current), channelErr
		}
		gbChannel = latestChannel
	}
	recorder := s.recorderFactory(target)
	if recorder == nil {
		return s.resultForLease(lease, &current), NewInternalError(nodeIDString(target.NodeID), "typed recorder is not configured")
	}
	if recorderType == zlm.RecorderMP4 {
		if s.recordingService == nil {
			return s.resultForLease(lease, &current), NewInternalError(nodeIDString(target.NodeID), "GB recording lifecycle is not configured")
		}
		if stopErr := runMutationGuard(ctx, s.mp4MutationGuard, target, func(operationCtx context.Context) error {
			return s.recordingService.StopManual(operationCtx, gbChannel, target.Media)
		}); stopErr != nil {
			lease.State = RecordingStateStopping
			result := s.resultForLease(lease, &current)
			result.State, result.Retryable, result.Reason = RecordingStateStopping, true, "recording stop is retryable"
			return result, retryableRecordingError(target, stopErr)
		}
		confirmed, readErr := recorder.IsRecordingWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType)
		if readErr != nil || confirmed {
			lease.State = RecordingStateStopping
			cause := readErr
			if cause == nil {
				cause = errors.New("recording state is still true after stop")
			}
			result := s.resultForLease(lease, &current)
			result.State, result.Retryable, result.Reason = RecordingStateStopping, true, "recording stop is retryable"
			return result, recordingReadbackError(target, cause)
		}
	} else {
		if stopErr := recorder.StopRecordWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType); stopErr != nil {
			lease.State = RecordingStateStopping
			result := s.resultForLease(lease, &current)
			result.State, result.Retryable, result.Reason = RecordingStateStopping, true, "recording stop is retryable"
			return result, retryableRecordingError(target, stopErr)
		}
		confirmed, readErr := recorder.IsRecordingWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType)
		if readErr != nil || confirmed {
			lease.State = RecordingStateStopping
			cause := readErr
			if cause == nil {
				cause = errors.New("recording state is still true after stop")
			}
			result := s.resultForLease(lease, &current)
			result.State, result.Retryable, result.Reason = RecordingStateStopping, true, "recording stop is retryable"
			return result, recordingReadbackError(target, cause)
		}
	}
	lease.State = RecordingStateStopped
	result := s.resultForLease(lease, &current)
	result.State, result.ExternalState, result.Recording, result.Retryable = RecordingStateStopped, RecordingExternalStateStopped, false, false
	if auditErr := s.audit(ctx, "stop", result, userID, ""); auditErr != nil {
		return result, auditErr
	}
	return result, nil
}

func (s *RecordingOps) recheck(ctx context.Context, preflight RecordingPreflight, lease *manualRecordingLease, force bool) (OwnershipSnapshot, error) {
	if strings.TrimSpace(preflight.Fingerprint) == "" {
		return OwnershipSnapshot{}, NewValidationError(map[string]string{"fingerprint": "is required"})
	}
	if lease != nil {
		if preflight.LeaseID != lease.ID {
			return OwnershipSnapshot{}, recordingOwnershipConflict(preflight.Target, "manual recording ownership changed")
		}
	} else if preflight.LeaseID != "" {
		return OwnershipSnapshot{}, recordingOwnershipConflict(preflight.Target, "manual recording ownership changed")
	}
	current, err := s.snapshot(ctx, preflight.Target, lease)
	if err != nil {
		return OwnershipSnapshot{}, err
	}
	if current.Fingerprint != preflight.Fingerprint {
		return current, ownershipConflict(preflight.Target, current, "ownership fingerprint changed; reconfirm required")
	}
	if s.resolver == nil || !current.PresenceKnown {
		return current, ownershipConflict(preflight.Target, current, "ownership state is unknown")
	}
	// Start uses the same fingerprint recheck while its lease is still in the
	// starting state, but business ownership is a stop-time safety decision.
	// A plan/continuous/other owner may coexist with a manual recording; that
	// owner must block only ordinary stop, never be released by it.
	if !force && lease != nil && lease.State != RecordingStateStarting && !normalStopAllowed(current, lease, preflight.UserID) {
		return current, ownershipConflict(preflight.Target, current, "resource is owned or ownership is uncertain")
	}
	return current, nil
}

func validateRecordingPreflightBinding(preflight *RecordingPreflight, target OwnershipTarget, recorderType zlm.RecorderType) error {
	if preflight == nil {
		return nil
	}
	if preflight.Target != target || preflight.Type != recorderType {
		return recordingOwnershipConflict(target, "recording preflight does not match the requested target")
	}
	return nil
}

func normalStopAllowed(snapshot OwnershipSnapshot, lease *manualRecordingLease, userID uint) bool {
	if lease == nil || lease.State == RecordingStateStopped || lease.UserID == 0 || lease.UserID != userID {
		return false
	}
	if snapshot.Status == OwnershipStatusUnknown || snapshot.Status == OwnershipStatusConflicted {
		return false
	}
	for _, owner := range snapshot.Owners {
		if owner.Conflict {
			return false
		}
		if owner.Type == OwnershipTypeManaged && owner.Confidence == OwnershipConfidenceProven {
			continue
		}
		if owner.Type == OwnershipTypeRecordingSession && lease.SessionID != 0 && owner.Key == strconv.FormatUint(lease.SessionID, 10) && owner.Confidence == OwnershipConfidenceProven {
			continue
		}
		return false
	}
	return true
}

func (s *RecordingOps) validateStartProvenance(target OwnershipTarget, recorderType zlm.RecorderType, gbOwned bool, snapshot OwnershipSnapshot) error {
	if recorderType == zlm.RecorderMP4 {
		if !gbOwned {
			return orphanMP4Error(target)
		}
		if s.resolver != nil && (!snapshot.PresenceKnown || !snapshot.Present) {
			return ownershipConflict(target, snapshot, "GB media presence is not confirmed")
		}
		return nil
	}
	if gbOwned {
		if s.resolver != nil && (!snapshot.PresenceKnown || !snapshot.Present) {
			return ownershipConflict(target, snapshot, "GB media presence is not confirmed")
		}
		return nil
	}
	if !hasProvenManaged(snapshot) {
		return ownershipConflict(target, snapshot, "HLS recording requires a managed or GB media source")
	}
	if !snapshot.PresenceKnown || !snapshot.Present {
		return ownershipConflict(target, snapshot, "managed media presence is not confirmed")
	}
	return nil
}

func hasProvenManaged(snapshot OwnershipSnapshot) bool {
	for _, owner := range snapshot.Owners {
		if owner.Type == OwnershipTypeManaged && owner.Confidence == OwnershipConfidenceProven {
			return true
		}
	}
	return false
}

func (s *RecordingOps) snapshot(ctx context.Context, target OwnershipTarget, lease *manualRecordingLease) (OwnershipSnapshot, error) {
	ctx = nonNilContext(ctx)
	var snapshot OwnershipSnapshot
	if s != nil && s.resolver != nil {
		resolved, err := s.resolver.Resolve(ctx, target)
		if err != nil {
			return OwnershipSnapshot{}, err
		}
		snapshot = resolved
	} else {
		snapshot = OwnershipSnapshot{Target: target, Owners: make([]OwnershipEvidence, 0), Status: OwnershipStatusUnknown}
	}
	snapshot.Target = target
	owners := append([]OwnershipEvidence(nil), snapshot.Owners...)
	if lease != nil && lease.State != RecordingStateStopped {
		// A matching own session is the one durable recording fact created by the
		// existing GB service. Do not let the generic recording-session adapter
		// mistake it for a different business owner.
		if lease.SessionID != 0 {
			filtered := owners[:0]
			for _, owner := range owners {
				if owner.Type == OwnershipTypeRecordingSession && owner.Key == strconv.FormatUint(lease.SessionID, 10) {
					continue
				}
				filtered = append(filtered, owner)
			}
			owners = filtered
		}
		owners = append(owners, OwnershipEvidence{
			Type: OwnershipTypeManaged, ResourceType: "manual_recording", Key: lease.ID,
			Owner: "user:" + strconv.FormatUint(uint64(lease.UserID), 10), Confidence: OwnershipConfidenceProven,
			Reason:      "in-process manual recording lease",
			Fingerprint: fingerprintParts(lease.ID, strconv.FormatUint(uint64(lease.UserID), 10), strconv.Itoa(int(lease.Type))),
		})
	}
	snapshot.Owners = owners
	sortOwnershipEvidence(snapshot.Owners)
	snapshot.Status = deriveOwnershipStatus(snapshot)
	snapshot.Impacts = impactsForOwnership(target, snapshot.Owners)
	snapshot.Fingerprint = FingerprintOwnershipSnapshot(snapshot)
	return snapshot, nil
}

func (s *RecordingOps) resolveGBChannel(ctx context.Context, target OwnershipTarget) (GBChannelRef, bool, error) {
	if s == nil || s.channelResolver == nil {
		return GBChannelRef{}, false, nil
	}
	channel, ok, err := s.channelResolver.ResolveGBChannel(nonNilContext(ctx), target)
	if err != nil {
		return GBChannelRef{}, false, NormalizeError(err, nodeIDString(target.NodeID))
	}
	if !ok {
		return GBChannelRef{}, false, nil
	}
	if channel.ChannelID == 0 || (channel.NodeID != 0 && channel.NodeID != target.NodeID) || (channel.StreamID != "" && channel.StreamID != target.Media.Stream) {
		return GBChannelRef{}, false, recordingOwnershipConflict(target, "GB channel attribution does not match media target")
	}
	return channel, true, nil
}

func (s *RecordingOps) recheckGBChannel(ctx context.Context, target OwnershipTarget, expected GBChannelRef) (GBChannelRef, error) {
	current, ok, err := s.resolveGBChannel(ctx, target)
	if err != nil {
		return GBChannelRef{}, err
	}
	if !ok || !sameGBChannel(current, expected) {
		return GBChannelRef{}, recordingOwnershipConflict(target, "GB channel attribution changed; reconfirm required")
	}
	return current, nil
}

func sameGBChannel(left, right GBChannelRef) bool {
	return left.ChannelID == right.ChannelID && left.DeviceID == right.DeviceID && left.StreamID == right.StreamID && left.NodeID == right.NodeID
}

func (s *RecordingOps) newLease(target OwnershipTarget, recorderType zlm.RecorderType, userID uint) *manualRecordingLease {
	s.mu.Lock()
	s.nextLeaseID++
	id := s.nextLeaseID
	s.mu.Unlock()
	return &manualRecordingLease{ID: "manual-recording-" + strconv.FormatUint(id, 10), Target: target, Type: recorderType, UserID: userID, State: RecordingStateStarting}
}

func (s *RecordingOps) resultForLease(lease *manualRecordingLease, snapshot *OwnershipSnapshot) RecordingResult {
	if lease == nil {
		return RecordingResult{}
	}
	return s.resultForLeaseOrTarget(lease, lease.Target, lease.Type, snapshot)
}

func (s *RecordingOps) resultForLeaseOrTarget(lease *manualRecordingLease, target OwnershipTarget, recorderType zlm.RecorderType, snapshot *OwnershipSnapshot) RecordingResult {
	result := RecordingResult{Target: target, Type: recorderType, State: RecordingStateUnknown, ExternalState: RecordingExternalStateUnknown}
	if lease != nil {
		result.Target, result.Type, result.State, result.LeaseID = lease.Target, lease.Type, lease.State, lease.ID
		result.Recording = lease.State == RecordingStateRecording
		switch lease.State {
		case RecordingStateRecording:
			result.ExternalState = RecordingExternalStateRecording
		case RecordingStateStopped:
			result.ExternalState = RecordingExternalStateStopped
		}
	}
	if snapshot != nil {
		result.Ownership = *snapshot
	}
	return result
}

func (s *RecordingOps) rejectExternalRecording(lease *manualRecordingLease, target OwnershipTarget, recorderType zlm.RecorderType, snapshot *OwnershipSnapshot) (RecordingResult, error) {
	if s != nil && lease != nil {
		s.deleteLease(leaseKey(target, recorderType), lease)
	}
	if snapshot != nil && lease != nil {
		clean := snapshotWithoutManualLease(*snapshot, lease.ID)
		snapshot = &clean
	}
	const reason = "recording is already active and is not owned by this request"
	result := s.resultForLeaseOrTarget(nil, target, recorderType, snapshot)
	result.ExternalState, result.Recording, result.Reason = RecordingExternalStateRecording, true, reason
	return result, recordingOwnershipConflict(target, reason)
}

func (s *RecordingOps) startConfirmationFailure(ctx context.Context, lease *manualRecordingLease, snapshot *OwnershipSnapshot, recorder TypedRecorderClient, gbChannel GBChannelRef, target OwnershipTarget, recorderType zlm.RecorderType, cause error) (RecordingResult, error) {
	if !startRollbackAllowed(snapshot) {
		uncertain := &recordingRollbackError{cause: errors.New("another business owner is active")}
		failure := NewManagementError(CodeInternal, nodeIDString(target.NodeID), "recording start rollback is uncertain", true, uncertain)
		result, _ := s.startFailure(lease, snapshot, failure)
		result.RollbackUncertain = true
		return result, failure
	}
	rollbackErr := s.rollbackStart(ctx, recorder, gbChannel, target, recorderType)
	if rollbackErr != nil {
		uncertain := &recordingRollbackError{cause: errors.Join(cause, rollbackErr)}
		failure := NewManagementError(CodeInternal, nodeIDString(target.NodeID), "recording start rollback is uncertain", true, uncertain)
		result, _ := s.startFailure(lease, snapshot, failure)
		result.RollbackUncertain = true
		return result, failure
	}
	result, err := s.startFailure(lease, snapshot, cause)
	result.ExternalState = RecordingExternalStateStopped
	result.Reason = "recording start was not confirmed; rollback completed"
	return result, err
}

func (s *RecordingOps) rollbackStart(ctx context.Context, recorder TypedRecorderClient, gbChannel GBChannelRef, target OwnershipTarget, recorderType zlm.RecorderType) error {
	if recorderType == zlm.RecorderMP4 {
		if s == nil || s.recordingService == nil {
			return errors.New("GB recording lifecycle is not configured")
		}
		if err := runMutationGuard(ctx, s.mp4MutationGuard, target, func(operationCtx context.Context) error {
			return s.recordingService.StopManual(operationCtx, gbChannel, target.Media)
		}); err != nil {
			return err
		}
	} else {
		if recorder == nil {
			return errors.New("typed recorder is not configured")
		}
		if err := recorder.StopRecordWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType); err != nil {
			return err
		}
	}
	if recorder == nil {
		return errors.New("typed recorder is not configured")
	}
	confirmed, err := recorder.IsRecordingWithType(ctx, target.Media.Vhost, target.Media.App, target.Media.Stream, recorderType)
	if err != nil {
		return err
	}
	if confirmed {
		return errors.New("recording remained active after rollback")
	}
	return nil
}

func startRollbackAllowed(snapshot *OwnershipSnapshot) bool {
	if snapshot == nil {
		return true
	}
	for _, owner := range snapshot.Owners {
		if owner.Type != OwnershipTypeManaged {
			return false
		}
	}
	return true
}

func (s *RecordingOps) startFailure(lease *manualRecordingLease, snapshot *OwnershipSnapshot, err error) (RecordingResult, error) {
	// A failed start must not reserve the target indefinitely. Once command or
	// readback confirmation fails, the lease is no longer trustworthy; discard
	// it so a later authenticated caller can retry, while ordinary stop remains
	// fail-closed because there is no ownership record to release.
	if s != nil && lease != nil {
		s.deleteLease(leaseKey(lease.Target, lease.Type), lease)
	}
	lease.State = RecordingStateUnknown
	if snapshot != nil && lease != nil {
		clean := snapshotWithoutManualLease(*snapshot, lease.ID)
		snapshot = &clean
	}
	result := s.resultForLease(lease, snapshot)
	result.State, result.ExternalState, result.Recording, result.Retryable, result.Reason = RecordingStateUnknown, RecordingExternalStateUnknown, false, true, "recording start state could not be confirmed"
	if errors.Is(err, errRecordingRollbackUncertain) {
		result.RollbackUncertain = true
		result.Reason = "recording start rollback is uncertain"
	}
	return result, err
}

func snapshotWithoutManualLease(snapshot OwnershipSnapshot, leaseID string) OwnershipSnapshot {
	if leaseID == "" {
		return snapshot
	}
	owners := make([]OwnershipEvidence, 0, len(snapshot.Owners))
	for _, owner := range snapshot.Owners {
		if owner.Type == OwnershipTypeManaged && owner.ResourceType == "manual_recording" && owner.Key == leaseID {
			continue
		}
		owners = append(owners, owner)
	}
	snapshot.Owners = owners
	sortOwnershipEvidence(snapshot.Owners)
	snapshot.Status = deriveOwnershipStatus(snapshot)
	snapshot.Impacts = impactsForOwnership(snapshot.Target, snapshot.Owners)
	snapshot.Fingerprint = FingerprintOwnershipSnapshot(snapshot)
	return snapshot
}

func (s *RecordingOps) audit(ctx context.Context, action string, result RecordingResult, userID uint, reason string) error {
	if s == nil || s.auditSink == nil {
		return nil
	}
	event := RecordingAuditEvent{Action: action, Target: result.Target, Type: result.Type, UserID: userID, LeaseID: result.LeaseID, Reason: reason, State: result.State, ExternalState: result.ExternalState, RollbackUncertain: result.RollbackUncertain, Recording: result.Recording, Retryable: result.Retryable, Success: !result.Retryable && result.State != RecordingStateUnknown}
	return NormalizeError(s.auditSink.RecordRecordingOperation(nonNilContext(ctx), event), nodeIDString(result.Target.NodeID))
}

func newRecordingPreflight(target OwnershipTarget, recorderType zlm.RecorderType, userID uint, lease *manualRecordingLease, snapshot OwnershipSnapshot) RecordingPreflight {
	preflight := RecordingPreflight{Target: target, Type: recorderType, UserID: userID, Snapshot: snapshot, Ownership: snapshot, Fingerprint: snapshot.Fingerprint}
	if lease != nil {
		preflight.LeaseID = lease.ID
	}
	return preflight
}

func validateStartRequest(request RecordingStartRequest) (OwnershipTarget, zlm.RecorderType, error) {
	target := request.Target
	if target == (OwnershipTarget{}) {
		target = OwnershipTarget{NodeID: request.NodeID, Media: request.Media}
	}
	if err := target.Validate(); err != nil {
		return OwnershipTarget{}, 0, err
	}
	recorderType := request.Type
	if request.RecorderType != zlm.RecorderHLS {
		recorderType = request.RecorderType
	}
	if recorderType != zlm.RecorderHLS && recorderType != zlm.RecorderMP4 {
		return OwnershipTarget{}, 0, NewValidationError(map[string]string{"type": "must be HLS(0) or MP4(1)"})
	}
	if request.MaxSecond < 0 {
		return OwnershipTarget{}, 0, NewValidationError(map[string]string{"maxSecond": "must not be negative"})
	}
	return target, recorderType, nil
}

func validateStopRequest(request RecordingStopRequest) (OwnershipTarget, zlm.RecorderType, error) {
	if request.Force {
		return OwnershipTarget{}, 0, NewValidationError(map[string]string{"force": "use the separate force-stop method"})
	}
	start := RecordingStartRequest{Target: request.Target, NodeID: request.NodeID, Media: request.Media, Type: request.Type, RecorderType: request.RecorderType}
	return validateStartRequest(start)
}

func validateForceStopRequest(request RecordingForceStopRequest) (OwnershipTarget, zlm.RecorderType, error) {
	if strings.TrimSpace(request.Reason) == "" {
		return OwnershipTarget{}, 0, NewValidationError(map[string]string{"reason": "is required"})
	}
	start := RecordingStartRequest{Target: request.Target, NodeID: request.NodeID, Media: request.Media, Type: request.Type, RecorderType: request.RecorderType}
	return validateStartRequest(start)
}

func leaseKey(target OwnershipTarget, recorderType zlm.RecorderType) string {
	return canonicalOwnershipTarget(target) + "|" + strconv.Itoa(int(recorderType))
}

func recordingOwnershipConflict(target OwnershipTarget, reason string) error {
	return NewOwnershipConflictError(nodeIDString(target.NodeID), reason, ErrOwnershipConflict)
}

func orphanMP4Error(target OwnershipTarget) error {
	return NewOwnershipConflictError(nodeIDString(target.NodeID), "MP4 recording requires a proven GB channel to avoid orphan files", ErrOwnershipConflict)
}

func recordingReadbackError(target OwnershipTarget, cause error) error {
	return NewManagementError(CodeInternal, nodeIDString(target.NodeID), "recording state readback could not confirm the requested state", true, cause)
}

func retryableRecordingError(target OwnershipTarget, cause error) error {
	normalized := NormalizeError(cause, nodeIDString(target.NodeID))
	if managementErr, ok := AsManagementError(normalized); ok {
		copy := *managementErr
		copy.Retryable = true
		return &copy
	}
	return normalized
}

var errRecordingRollbackUncertain = errors.New("recording start rollback uncertain")

type recordingRollbackError struct{ cause error }

func (e *recordingRollbackError) Error() string {
	return errRecordingRollbackUncertain.Error()
}

func (e *recordingRollbackError) Unwrap() error {
	if e == nil {
		return errRecordingRollbackUncertain
	}
	return errors.Join(errRecordingRollbackUncertain, e.cause)
}

// ExistingRecordingServiceAdapter is the narrow bridge from recording.Service
// to the manual MP4 boundary. It is deliberately not used for HLS.
type ExistingRecordingServiceAdapter struct{ service ExistingRecordingService }

func NewExistingRecordingServiceAdapter(service ExistingRecordingService) *ExistingRecordingServiceAdapter {
	return &ExistingRecordingServiceAdapter{service: service}
}

func NewGBRecordingServiceAdapter(service ExistingRecordingService) *ExistingRecordingServiceAdapter {
	return NewExistingRecordingServiceAdapter(service)
}

func NewRecordingServiceAdapter(service ExistingRecordingService) *ExistingRecordingServiceAdapter {
	return NewExistingRecordingServiceAdapter(service)
}

func (a *ExistingRecordingServiceAdapter) StartManual(ctx context.Context, channel GBChannelRef, target MediaIdentity) error {
	if a == nil || a.service == nil || channel.ChannelID == 0 {
		return errors.New("GB recording lifecycle is not configured")
	}
	if target.Vhost != gbmodels.DefaultRecordingVHost || target.App != gbmodels.DefaultRecordingApp {
		return errors.New("GB recording media identity is unsupported")
	}
	if channel.StreamID != "" && channel.StreamID != target.Stream {
		return errors.New("GB recording channel does not match media target")
	}
	if _, err := a.service.Enable(nonNilContext(ctx), channel.ChannelID); err != nil {
		return err
	}
	if err := a.service.BeginPlayback(nonNilContext(ctx), target.Stream); err != nil {
		if _, rollbackErr := a.service.Disable(nonNilContext(ctx), channel.ChannelID); rollbackErr != nil {
			return &recordingRollbackError{cause: rollbackErr}
		}
		return err
	}
	return nil
}

func (a *ExistingRecordingServiceAdapter) StopManual(ctx context.Context, channel GBChannelRef, target MediaIdentity) error {
	if a == nil || a.service == nil || channel.ChannelID == 0 {
		return errors.New("GB recording lifecycle is not configured")
	}
	if target.Vhost != gbmodels.DefaultRecordingVHost || target.App != gbmodels.DefaultRecordingApp {
		return errors.New("GB recording media identity is unsupported")
	}
	if channel.StreamID != "" && channel.StreamID != target.Stream {
		return errors.New("GB recording channel does not match media target")
	}
	_, err := a.service.Disable(nonNilContext(ctx), channel.ChannelID)
	return err
}

// ExecutorTypedRecorder adapts T6 NodeExecutor to the typed recorder surface.
// It resolves/authorizes the node independently for every read or write.
type ExecutorTypedRecorder struct {
	executor         *NodeExecutor
	target           OwnershipTarget
	mp4MutationGuard MP4MutationGuard
}

func NewExecutorTypedRecorder(executor *NodeExecutor, target OwnershipTarget) *ExecutorTypedRecorder {
	return &ExecutorTypedRecorder{executor: executor, target: target}
}

// SetMP4MutationGuard configures the shared external gate for direct adapter
// callers. RecordingOps also wires the same guard when it constructs this
// adapter from a NodeExecutor.
func (r *ExecutorTypedRecorder) SetMP4MutationGuard(guard MP4MutationGuard) *ExecutorTypedRecorder {
	if r != nil {
		r.mp4MutationGuard = guard
	}
	return r
}

func (r *ExecutorTypedRecorder) StartRecordWithType(ctx context.Context, vhost, appName, stream string, recorderType zlm.RecorderType, maxSecond int) error {
	if r == nil || r.executor == nil {
		return errors.New("typed recorder executor is not configured")
	}
	operation := func(operationCtx context.Context) error {
		return r.executor.ExecuteWrite(operationCtx, r.target.NodeID, func(clientCtx context.Context, client *zlm.Client) error {
			return client.StartRecordWithType(clientCtx, vhost, appName, stream, recorderType, maxSecond)
		})
	}
	if recorderType == zlm.RecorderMP4 {
		actualTarget := r.target
		actualTarget.Media.Vhost, actualTarget.Media.App, actualTarget.Media.Stream = vhost, appName, stream
		return runMutationGuard(ctx, r.mp4MutationGuard, actualTarget, operation)
	}
	return operation(nonNilContext(ctx))
}

func (r *ExecutorTypedRecorder) StopRecordWithType(ctx context.Context, vhost, appName, stream string, recorderType zlm.RecorderType) error {
	if r == nil || r.executor == nil {
		return errors.New("typed recorder executor is not configured")
	}
	operation := func(operationCtx context.Context) error {
		return r.executor.ExecuteWrite(operationCtx, r.target.NodeID, func(clientCtx context.Context, client *zlm.Client) error {
			return client.StopRecordWithType(clientCtx, vhost, appName, stream, recorderType)
		})
	}
	if recorderType == zlm.RecorderMP4 {
		actualTarget := r.target
		actualTarget.Media.Vhost, actualTarget.Media.App, actualTarget.Media.Stream = vhost, appName, stream
		return runMutationGuard(ctx, r.mp4MutationGuard, actualTarget, operation)
	}
	return operation(nonNilContext(ctx))
}

func (r *ExecutorTypedRecorder) IsRecordingWithType(ctx context.Context, vhost, appName, stream string, recorderType zlm.RecorderType) (bool, error) {
	if r == nil || r.executor == nil {
		return false, errors.New("typed recorder executor is not configured")
	}
	var recording bool
	err := r.executor.ExecuteRead(nonNilContext(ctx), r.target.NodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		recording, err = client.IsRecordingWithType(operationCtx, vhost, appName, stream, recorderType)
		return err
	})
	return recording, err
}

// GBChannelReader and GBStreamLocation let T14/bootstrap construct the
// resolver from the existing recording repository and live LocationMap.
type GBChannelReader interface {
	FindChannelByStream(context.Context, string) (*gbmodels.GbChannel, error)
}

type GBStreamLocation interface {
	Lookup(string) (int64, bool)
}

type repositoryGBChannelResolver struct {
	channels GBChannelReader
	location GBStreamLocation
}

func NewGBChannelResolver(channels GBChannelReader, location GBStreamLocation) GBChannelResolver {
	return repositoryGBChannelResolver{channels: channels, location: location}
}

func (r repositoryGBChannelResolver) ResolveGBChannel(ctx context.Context, target OwnershipTarget) (GBChannelRef, bool, error) {
	if r.channels == nil || r.location == nil {
		return GBChannelRef{}, false, nil
	}
	channel, err := r.channels.FindChannelByStream(nonNilContext(ctx), target.Media.Stream)
	if err != nil {
		return GBChannelRef{}, false, err
	}
	if channel == nil || channel.ID == 0 || channel.StreamID != target.Media.Stream {
		return GBChannelRef{}, false, nil
	}
	nodeID, ok := r.location.Lookup(target.Media.Stream)
	if !ok || nodeID != target.NodeID {
		return GBChannelRef{}, false, nil
	}
	return GBChannelRef{ChannelID: channel.ID, DeviceID: channel.DeviceID, StreamID: channel.StreamID, NodeID: nodeID}, true, nil
}
