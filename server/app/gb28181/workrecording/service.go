package workrecording

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const (
	DesiredActionStart = "start"
	DesiredActionStop  = "stop"
	workOperationLimit = 60 * time.Second
)

// PreparedRecording is the server-owned media generation and its long-lived
// source lease. The lease remains held while a start result is uncertain and
// is released only after Recorder.ReleaseStopped succeeds.
type PreparedRecording struct {
	Target  MediaTarget
	Release func()
}

// PrepareFunc resolves a live source and a node-side absolute recording root.
// The browser supplies neither the media tuple nor the filesystem path.
type PrepareFunc func(context.Context, uint, string) (PreparedRecording, error)

type reserveAfterLegacyChecker interface {
	ReserveAfterLegacyCheck(context.Context, uint, Owner, MediaTarget) (RecorderHandle, error)
}

// Service coordinates durable work jobs with Recorder. Database operations
// and external ZLM calls are deliberately separate; in particular, a DB
// transaction is never held while Recorder calls the media server.
type Service struct {
	db       *gorm.DB
	recorder *Recorder
	prepare  PrepareFunc
	now      func() time.Time

	locks [64]sync.Mutex

	leaseMu sync.Mutex
	leases  map[string]func()

	// stateObserverMu guards stateObserver, which is installed once at startup
	// but read from every recorder goroutine.
	stateObserverMu sync.RWMutex
	stateObserver   func(context.Context, string)
}

func NewService(db *gorm.DB, recorder *Recorder, prepare PrepareFunc) *Service {
	return &Service{
		db:       db,
		recorder: recorder,
		prepare:  prepare,
		now:      time.Now,
		leases:   make(map[string]func()),
	}
}

// SetStateObserver registers a best-effort listener invoked with the owning
// ledger id whenever a child recording changes state. It exists so an aggregate
// ledger can refresh itself without the recorder engine having to know anything
// about ledgers. The observer must not fail the caller.
func (s *Service) SetStateObserver(observer func(context.Context, string)) {
	s.stateObserverMu.Lock()
	defer s.stateObserverMu.Unlock()
	s.stateObserver = observer
}

func (s *Service) notifyStateObserver(ctx context.Context, batchID string) {
	if batchID == "" {
		return
	}
	s.stateObserverMu.RLock()
	observer := s.stateObserver
	s.stateObserverMu.RUnlock()
	if observer != nil {
		observer(ctx, batchID)
	}
}

func (s *Service) lock(channelID uint) func() {
	mutex := &s.locks[channelID%uint(len(s.locks))]
	mutex.Lock()
	return mutex.Unlock
}

// Start creates the durable job identity before claiming a recorder resource.
// Once a request identity exists, a repeat returns its recorded outcome and
// never calls Recorder.Start a second time.
func (s *Service) Start(ctx context.Context, actor uint, request StartRequest, maxSecond int) (Snapshot, error) {
	var empty Snapshot
	if s == nil || s.db == nil || s.recorder == nil || s.prepare == nil || maxSecond < 0 {
		return empty, ErrInvalidRequest
	}
	if err := request.Validate(); err != nil {
		return empty, err
	}

	unlock := s.lock(request.ChannelID)
	defer unlock()

	job, found, err := s.findRequest(ctx, actor, request.RequestID)
	if err != nil {
		return empty, err
	}
	if found {
		if job.ChannelID != request.ChannelID {
			return jobSnapshot(job), ErrRequestConflict
		}
		return jobSnapshot(job), existingStartError(job.State)
	}

	job = &models.GbWorkRecording{
		ID:            uuid.NewString(),
		ChannelID:     request.ChannelID,
		CreatedBy:     actor,
		RequestID:     request.RequestID,
		State:         StateStarting,
		DesiredAction: DesiredActionStart,
		Version:       1,
		FileState:     FilePending,
		FormState:     FormDraft,
		SchemaVersion: 1,
		FormJSON:      "{}",
	}
	if err := s.db.WithContext(ctx).Create(job).Error; err != nil {
		// A concurrent process may have won the unique request key. Resolve it
		// before returning the raw unique-index error so retries stay idempotent.
		if existing, lookupErr := s.findRequestByID(ctx, actor, request.RequestID); lookupErr == nil {
			if existing.ChannelID != request.ChannelID {
				return jobSnapshot(existing), ErrRequestConflict
			}
			return jobSnapshot(existing), existingStartError(existing.State)
		}
		return empty, err
	}
	operationCtx, operationCancel := workOperationContext(ctx)
	defer operationCancel()

	owner := Owner{Kind: OwnerWork, ID: job.ID}
	jobID := job.ID
	handle, err := s.recorder.Reserve(operationCtx, request.ChannelID, owner)
	var prepared PreparedRecording
	if err != nil {
		if !errors.Is(err, ErrOwnerConflict) {
			return s.failStart(job, StateFailed, 0, err)
		}
		legacy, claimErr := NewClaims(s.db).Get(operationCtx, ChannelResource(request.ChannelID))
		if claimErr != nil || legacy.OwnerKind != OwnerLegacy {
			return s.failStart(job, StateFailed, 0, err)
		}
		// A legacy claim is the only case where media preparation precedes the
		// recorder reservation. ReserveAfterLegacyCheck performs the guarded,
		// atomic legacy handoff; no other owner conflict is allowed to prepare.
		prepared, claimErr = s.prepare(operationCtx, request.ChannelID, jobID)
		if claimErr != nil {
			if prepared.Release != nil {
				s.holdLease(jobID, prepared.Release)
			}
			return s.unknownStart(jobID, 0, claimErr)
		}
		if prepared.Release == nil || !validPreparedTarget(prepared.Target, jobID) {
			return s.unknownStart(jobID, 0, ErrInvalidRequest)
		}
		s.holdLease(jobID, prepared.Release)
		legacyReserver, ok := any(s.recorder).(reserveAfterLegacyChecker)
		if !ok {
			return s.unknownStart(jobID, 0, ErrInvalidRequest)
		}
		handle, claimErr = legacyReserver.ReserveAfterLegacyCheck(operationCtx, request.ChannelID, owner, prepared.Target)
		if claimErr != nil {
			return s.legacyHandoffFailure(job, request.ChannelID, handle, claimErr)
		}
	} else {
		job, err = s.updateJob(operationCtx, job, map[string]any{
			"recorder_claim_version": handle.Version,
		})
		if err != nil {
			return s.unknownStart(jobID, handle.Version, err)
		}

		prepared, err = s.prepare(operationCtx, request.ChannelID, jobID)
		if err != nil {
			if prepared.Release != nil {
				s.holdLease(jobID, prepared.Release)
			}
			if prepared.Release == nil && prepared.Target == (MediaTarget{}) {
				if abortErr := s.recorder.AbortStarting(operationCtx, handle); abortErr == nil {
					return s.failStart(job, StateFailed, handle.Version+1, err)
				} else {
					return s.unknownStart(jobID, handle.Version, errors.Join(err, abortErr))
				}
			}
			return s.unknownStart(jobID, handle.Version, err)
		}
	}
	if prepared.Release == nil {
		if prepared.Target == (MediaTarget{}) {
			if abortErr := s.recorder.AbortStarting(operationCtx, handle); abortErr == nil {
				return s.failStart(job, StateFailed, handle.Version+1, ErrInvalidRequest)
			} else {
				return s.unknownStart(jobID, handle.Version, errors.Join(ErrInvalidRequest, abortErr))
			}
		}
		return s.unknownStart(jobID, handle.Version, ErrInvalidRequest)
	}
	// From this point a source has been acquired. Keep it across every
	// uncertain result, including a failure before the external Start call.
	s.holdLease(jobID, prepared.Release)

	if !validPreparedTarget(prepared.Target, jobID) {
		return s.unknownStart(jobID, handle.Version, ErrInvalidRequest)
	}
	job, err = s.updateJob(operationCtx, job, map[string]any{
		"node_id":                prepared.Target.NodeID,
		"v_host":                 prepared.Target.VHost,
		"app":                    prepared.Target.App,
		"stream":                 prepared.Target.Stream,
		"recording_root":         prepared.Target.RecordingRoot,
		"generation":             prepared.Target.Generation,
		"recorder_claim_version": handle.Version,
	})
	if err != nil {
		return s.unknownStart(jobID, handle.Version, err)
	}

	started, err := s.recorder.Start(operationCtx, handle, prepared.Target, maxSecond)
	if err != nil {
		if abortErr := s.recorder.AbortStarting(operationCtx, started); abortErr == nil {
			s.releaseLease(jobID)
			return s.failStart(job, StateFailed, started.Version+1, err)
		}
		return s.unknownStart(jobID, started.Version, err)
	}

	startedAt := s.now()
	job, err = s.updateJob(operationCtx, job, map[string]any{
		"state":                  StateRecording,
		"desired_action":         DesiredActionStart,
		"recorder_claim_version": started.Version,
		"started_at":             startedAt,
		"last_error":             "",
	})
	if err != nil {
		return s.unknownStart(jobID, started.Version, err)
	}
	return jobSnapshot(job), nil
}

// Stop records the stop intent before touching ZLM. A confirmed stop is
// persisted as stopped/finalizing, then both claims are yielded atomically;
// only then is the source lease released.
func (s *Service) Stop(ctx context.Context, jobID string) (Snapshot, error) {
	var empty Snapshot
	if s == nil || s.db == nil || s.recorder == nil || !validJobID(jobID) {
		return empty, ErrInvalidRequest
	}
	job, err := s.findJob(ctx, jobID)
	if err != nil {
		return empty, err
	}
	unlock := s.lock(job.ChannelID)
	defer unlock()
	job, err = s.findJob(ctx, jobID)
	if err != nil {
		return empty, err
	}

	claim, err := NewClaims(s.db).Get(ctx, ChannelResource(job.ChannelID))
	if err != nil {
		return jobSnapshot(job), err
	}
	if job.State == StateStopped {
		// A completed old job is idempotent even after this channel has been
		// reused. Never interpret the current owner's claim as this job's
		// resource and never issue a stop against it.
		if claim.State == StateIdle || claim.OwnerKind != OwnerWork || claim.OwnerID != job.ID {
			s.releaseLease(job.ID)
			return jobSnapshot(job), nil
		}
		if claim.State != StateStopped || !jobMatchesClaim(job, claim) {
			return jobSnapshot(job), nil
		}
		handle := RecorderHandle{ChannelID: job.ChannelID, Owner: Owner{Kind: OwnerWork, ID: job.ID}, Version: claim.Version}
		operationCtx, operationCancel := workOperationContext(ctx)
		defer operationCancel()
		return s.persistStoppedAndRelease(operationCtx, job, handle)
	}
	if claim.State == StateIdle {
		return jobSnapshot(job), ErrAttributionUnknown
	}
	if claim.OwnerKind != OwnerWork || claim.OwnerID != job.ID {
		return jobSnapshot(job), ErrOwnerConflict
	}
	if !jobMatchesClaim(job, claim) {
		return jobSnapshot(job), ErrOwnerConflict
	}
	handle := RecorderHandle{ChannelID: job.ChannelID, Owner: Owner{Kind: OwnerWork, ID: job.ID}, Version: claim.Version}

	if claim.State == StateStopped {
		operationCtx, operationCancel := workOperationContext(ctx)
		defer operationCancel()
		return s.persistStoppedAndRelease(operationCtx, job, handle)
	}

	job, err = s.updateJob(ctx, job, map[string]any{
		"state":          StateStopping,
		"desired_action": DesiredActionStop,
		"last_error":     "",
	})
	if err != nil {
		return jobSnapshot(job), err
	}
	operationCtx, operationCancel := workOperationContext(ctx)
	defer operationCancel()
	if claim.State == StateStarting {
		if err := s.recorder.AbortStarting(operationCtx, handle); err != nil {
			return s.unknownStop(job.ID, handle.Version, err)
		}
		return s.persistAborted(operationCtx, job, handle)
	}

	stopped, err := s.recorder.Stop(operationCtx, handle)
	if err != nil {
		return s.unknownStop(job.ID, stopped.Version, err)
	}
	return s.persistStoppedAndRelease(operationCtx, job, stopped)
}

func (s *Service) persistAborted(ctx context.Context, job *models.GbWorkRecording, handle RecorderHandle) (Snapshot, error) {
	updated, err := s.updateJob(ctx, job, map[string]any{
		"state":                  StateStopped,
		"desired_action":         DesiredActionStop,
		"file_state":             FileFinalizing,
		"stopped_at":             s.now(),
		"recorder_claim_version": handle.Version + 1,
		"last_error":             "",
	})
	if err != nil {
		// AbortStarting already yielded the claims. Retain the lease until the
		// durable job fact is written, so a DB failure cannot silently detach
		// source ownership from the job record.
		return jobSnapshot(job), err
	}
	s.releaseLease(job.ID)
	return jobSnapshot(updated), nil
}

func (s *Service) Get(ctx context.Context, jobID string) (Snapshot, error) {
	if s == nil || s.db == nil || !validJobID(jobID) {
		return Snapshot{}, ErrInvalidRequest
	}
	job, err := s.findJob(ctx, jobID)
	if err != nil {
		return Snapshot{}, err
	}
	unlock := s.lock(job.ChannelID)
	defer unlock()
	job, err = s.findJob(ctx, jobID)
	if err != nil {
		return Snapshot{}, err
	}
	if job.State == StateFailed {
		return jobSnapshot(job), nil
	}
	claim, err := NewClaims(s.db).Get(ctx, ChannelResource(job.ChannelID))
	if err != nil {
		return jobSnapshot(job), err
	}
	if claim.State == StateIdle {
		if job.State == StateStopped {
			return jobSnapshot(job), nil
		}
		return jobSnapshot(job), ErrAttributionUnknown
	}
	if claim.OwnerKind != OwnerWork || claim.OwnerID != job.ID {
		if job.State == StateStopped {
			return jobSnapshot(job), nil
		}
		return jobSnapshot(job), ErrOwnerConflict
	}
	// A stopped job is terminal. It may retry releasing its exact stopped
	// claim, but an active claim with the same old owner must never resurrect
	// the job or be observed as a new run.
	if job.State == StateStopped && (claim.State != StateStopped || !jobMatchesClaim(job, claim)) {
		return jobSnapshot(job), nil
	}
	if !jobMatchesClaim(job, claim) {
		return jobSnapshot(job), ErrOwnerConflict
	}
	if claim.State != StateStopped && job.RecorderClaimVersion != claim.Version {
		return jobSnapshot(job), ErrVersionConflict
	}
	handle := RecorderHandle{ChannelID: job.ChannelID, Owner: Owner{Kind: OwnerWork, ID: job.ID}, Version: claim.Version}
	observed, observedState, observeErr := s.recorder.Observe(ctx, handle)
	checkedAt := s.now()
	if observeErr != nil {
		claimVersion := observed.Version
		if claimVersion == 0 {
			claimVersion = claim.Version
		}
		updated, persistErr := s.updateJob(ctx, job, map[string]any{
			"state":                  StateUnknown,
			"last_checked_at":        checkedAt,
			"recorder_claim_version": claimVersion,
			"last_error":             errorText(observeErr),
		})
		if persistErr != nil {
			combinedErr := errors.Join(observeErr, persistErr)
			return observationUnknownSnapshot(job, checkedAt, combinedErr), combinedErr
		}
		return jobSnapshot(updated), observeErr
	}
	if observedState == StateStopped {
		updated, persistErr := s.persistStoppedAndReleaseAt(ctx, job, observed, &checkedAt)
		if persistErr != nil && updated.State != StateStopped {
			return observationUnknownSnapshot(job, checkedAt, persistErr), persistErr
		}
		return updated, persistErr
	}
	state := observedState
	// Unknown and stopping are protective states. A stale starting/recording
	// claim must not turn either state into a falsely successful recording.
	if job.State == StateUnknown {
		state = StateUnknown
	}
	if job.State == StateStopping {
		state = StateStopping
	}
	values := map[string]any{
		"state":                  state,
		"last_checked_at":        checkedAt,
		"recorder_claim_version": observed.Version,
	}
	if state != StateUnknown && state != StateStopping {
		values["last_error"] = ""
	}
	updated, persistErr := s.updateJob(ctx, job, values)
	if persistErr != nil {
		return observationUnknownSnapshot(job, checkedAt, persistErr), persistErr
	}
	return jobSnapshot(updated), nil
}

func (s *Service) findRequest(ctx context.Context, actor uint, requestID string) (*models.GbWorkRecording, bool, error) {
	job, err := s.findRequestByID(ctx, actor, requestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return job, err == nil, err
}

func (s *Service) findRequestByID(ctx context.Context, actor uint, requestID string) (*models.GbWorkRecording, error) {
	var job models.GbWorkRecording
	result := s.db.WithContext(ctx).Where("created_by = ? AND request_id = ?", actor, requestID).First(&job)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &job, nil
}

func (s *Service) findJob(ctx context.Context, jobID string) (*models.GbWorkRecording, error) {
	var job models.GbWorkRecording
	result := s.db.WithContext(ctx).First(&job, "id = ?", jobID)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &job, nil
}

func (s *Service) updateJob(ctx context.Context, job *models.GbWorkRecording, values map[string]any) (*models.GbWorkRecording, error) {
	if job == nil || job.ID == "" {
		return nil, ErrInvalidRequest
	}
	updates := make(map[string]any, len(values)+1)
	for key, value := range values {
		updates[key] = value
	}
	updates["version"] = job.Version + 1
	result := s.db.WithContext(ctx).Model(&models.GbWorkRecording{}).
		Where("id = ? AND version = ?", job.ID, job.Version).
		Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrVersionConflict
	}
	updated, err := s.findJob(ctx, job.ID)
	if err != nil {
		return nil, err
	}
	// Every child state transition funnels through here, so this is the one
	// place an aggregate ledger needs to hear about.
	if _, stateChanged := values["state"]; stateChanged {
		s.notifyStateObserver(ctx, updated.BatchID)
	}
	return updated, nil
}

func (s *Service) failStart(job *models.GbWorkRecording, state string, claimVersion uint64, cause error) (Snapshot, error) {
	if job == nil {
		return Snapshot{}, errors.Join(cause, ErrInvalidRequest)
	}
	ctx := context.WithoutCancel(context.Background())
	current, err := s.findJob(ctx, job.ID)
	if err == nil {
		values := map[string]any{"state": state, "last_error": errorText(cause)}
		if claimVersion > 0 {
			values["recorder_claim_version"] = claimVersion
		}
		if updated, updateErr := s.updateJob(ctx, current, values); updateErr == nil {
			return jobSnapshot(updated), cause
		} else {
			err = updateErr
		}
	}
	return snapshotOrEmpty(s.findJob(ctx, job.ID)), errors.Join(cause, err)
}

func (s *Service) unknownStart(jobID string, claimVersion uint64, cause error) (Snapshot, error) {
	snapshot, persistErr := s.markUnknown(jobID, claimVersion, cause)
	return snapshot, errors.Join(cause, persistErr)
}

func (s *Service) unknownStop(jobID string, claimVersion uint64, cause error) (Snapshot, error) {
	snapshot, persistErr := s.markUnknown(jobID, claimVersion, cause)
	return snapshot, errors.Join(cause, persistErr)
}

func (s *Service) legacyHandoffFailure(job *models.GbWorkRecording, channelID uint, handle RecorderHandle, cause error) (Snapshot, error) {
	if job == nil {
		return Snapshot{}, errors.Join(cause, ErrInvalidRequest)
	}
	inspectCtx, inspectCancel := workOperationContext(context.Background())
	defer inspectCancel()
	claim, err := NewClaims(s.db).Get(inspectCtx, ChannelResource(channelID))
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		// We cannot prove that the handoff failed before acquiring our work
		// claim. Preserve the lease and the unknown state until it can be
		// reconciled safely.
		return s.unknownStart(job.ID, handle.Version, errors.Join(cause, err))
	}
	if err == nil && claim.State != StateIdle && claim.OwnerKind == OwnerWork && claim.OwnerID == job.ID {
		// ReserveAfterLegacyCheck may have acquired the work claim before an
		// error was returned. Keep the lease and let the unknown path resolve it.
		return s.unknownStart(job.ID, handle.Version, cause)
	}
	// The handoff did not acquire this job's work claim. Release only the
	// source lease acquired by Prepare; the legacy claim remains untouched.
	s.releaseLease(job.ID)
	return s.failStart(job, StateFailed, 0, cause)
}

func (s *Service) markUnknown(jobID string, claimVersion uint64, cause error) (Snapshot, error) {
	ctx := context.WithoutCancel(context.Background())
	job, err := s.findJob(ctx, jobID)
	if err != nil {
		return Snapshot{}, err
	}
	if job.State == StateUnknown && (claimVersion == 0 || job.RecorderClaimVersion == claimVersion) {
		return jobSnapshot(job), nil
	}
	values := map[string]any{"state": StateUnknown, "last_error": errorText(cause)}
	if claimVersion > 0 {
		values["recorder_claim_version"] = claimVersion
	}
	updated, err := s.updateJob(ctx, job, values)
	if err != nil {
		return jobSnapshot(job), err
	}
	return jobSnapshot(updated), nil
}

func (s *Service) persistStoppedAndRelease(ctx context.Context, job *models.GbWorkRecording, handle RecorderHandle) (Snapshot, error) {
	return s.persistStoppedAndReleaseAt(ctx, job, handle, nil)
}

func (s *Service) persistStoppedAndReleaseAt(ctx context.Context, job *models.GbWorkRecording, handle RecorderHandle, checkedAt *time.Time) (Snapshot, error) {
	if job == nil {
		return Snapshot{}, ErrInvalidRequest
	}
	needsStateUpdate := job.State != StateStopped || (job.FileState != FileFinalizing && job.FileState != FileReady)
	needsUpdate := needsStateUpdate || checkedAt != nil
	if needsUpdate {
		values := map[string]any{
			"recorder_claim_version": handle.Version,
			"last_error":             "",
		}
		if needsStateUpdate {
			values["state"] = StateStopped
			values["desired_action"] = DesiredActionStop
			if job.FileState == FileReady && job.State == StateStopped {
				values["file_state"] = FileReady
			} else {
				values["file_state"] = FileFinalizing
			}
			if job.StoppedAt == nil {
				values["stopped_at"] = s.now()
			}
		}
		if checkedAt != nil {
			values["last_checked_at"] = *checkedAt
		}
		updated, err := s.updateJob(ctx, job, values)
		if err != nil {
			// Stop was already confirmed, so retain the claim and retry this
			// durable fact later instead of yielding an untracked resource.
			return jobSnapshot(job), err
		}
		job = updated
	}
	if err := s.recorder.ReleaseStopped(ctx, handle); err != nil {
		return jobSnapshot(job), err
	}
	s.releaseLease(job.ID)
	return jobSnapshot(job), nil
}

func (s *Service) holdLease(jobID string, release func()) {
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	if _, exists := s.leases[jobID]; !exists {
		s.leases[jobID] = release
	}
}

func (s *Service) releaseLease(jobID string) {
	s.leaseMu.Lock()
	release := s.leases[jobID]
	delete(s.leases, jobID)
	s.leaseMu.Unlock()
	if release != nil {
		release()
	}
}

func jobMatchesClaim(job *models.GbWorkRecording, claim *models.GbRecorderClaim) bool {
	if job == nil || claim == nil || job.ChannelID != claim.ChannelID {
		return false
	}
	// A crash can leave the channel claim reserved before Recorder.bind has
	// copied the prepared media target. Exact owner plus an unbound starting
	// claim is sufficient for AbortStarting; it is not sufficient for Stop.
	if claim.State == StateStarting && claim.NodeID == 0 && claim.VHost == "" && claim.App == "" && claim.Stream == "" && claim.Generation == 0 && claim.RecordingRoot == "" {
		return true
	}
	return job.NodeID == claim.NodeID && job.VHost == claim.VHost && job.App == claim.App && job.Stream == claim.Stream &&
		job.RecordingRoot == claim.RecordingRoot && job.Generation == claim.Generation
}

func validPreparedTarget(target MediaTarget, jobID string) bool {
	return target.valid() && absoluteNodePath(cleanNodePath(target.RecordingRoot)) && ownsWorkDirectory(target.RecordingRoot, jobID)
}

func jobSnapshot(job *models.GbWorkRecording) Snapshot {
	if job == nil {
		return Snapshot{}
	}
	return Snapshot{
		ID:            job.ID,
		ChannelID:     job.ChannelID,
		State:         job.State,
		Version:       job.Version,
		LastCheckedAt: job.LastCheckedAt,
		StartedAt:     job.StartedAt,
		StoppedAt:     job.StoppedAt,
		FileState:     job.FileState,
		FormState:     job.FormState,
		LastError:     job.LastError,
	}
}

func observationUnknownSnapshot(job *models.GbWorkRecording, checkedAt time.Time, cause error) Snapshot {
	snapshot := jobSnapshot(job)
	snapshot.State = StateUnknown
	snapshot.LastCheckedAt = &checkedAt
	snapshot.LastError = errorText(cause)
	return snapshot
}

func validJobID(value string) bool {
	if strings.TrimSpace(value) != value || value == "" {
		return false
	}
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func existingStartError(state string) error {
	switch state {
	case StateRecording, StateStopped:
		return nil
	case StateUnknown, StateStarting, StateStopping:
		return ErrAttributionUnknown
	default:
		return ErrVersionConflict
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	value := []rune(err.Error())
	if len(value) > 500 {
		value = value[:500]
	}
	return string(value)
}

func snapshotOrEmpty(job *models.GbWorkRecording, err error) Snapshot {
	if err != nil {
		return Snapshot{}
	}
	return jobSnapshot(job)
}

func workOperationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), workOperationLimit)
}
