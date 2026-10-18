package upgrade

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// DeploymentAdapter is the only place where a deployment-specific operation
// may happen. The updater protocol exposes no command or path fields; these
// operations are constructed from the locally registered adapter.
type DeploymentAdapter interface {
	Stop(context.Context, TaskRecord) error
	Backup(context.Context, TaskRecord) error
	Migrate(context.Context, TaskRecord) error
	Activate(context.Context, TaskRecord) error
	Validate(context.Context, TaskRecord) error
	Commit(context.Context, TaskRecord) error
	Rollback(context.Context, TaskRecord) error
}

type currentTargetReader interface {
	CurrentTarget() (string, error)
}

type packagePreparer interface {
	PreparePackage(context.Context, TaskRecord) error
}

// WorkflowExecutor persists every boundary before moving to the next one.
// It is safe to restart the helper and inspect the last durable phase.
type WorkflowExecutor struct {
	Store       *TaskStore
	Adapter     DeploymentAdapter
	Ready       func(context.Context, TaskRecord) error
	Gate        *GateStore
	PolicyStore *PolicyStore
	Events      UpgradeEventSink
	Now         func() time.Time
	Verify      func(context.Context, TaskRecord) (TaskRecord, error)
	Preflight   func(context.Context) (PreflightSnapshot, error)
	Prepare     func(context.Context, TaskRecord) error
	Cleanup     func(context.Context, TaskRecord) error
	mu          sync.Mutex
}

func (w *WorkflowExecutor) Apply(ctx context.Context, record TaskRecord) error {
	if w == nil || w.Store == nil || w.Adapter == nil {
		return errors.New("upgrade workflow is not configured")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	lock, err := acquireUpgradeLock(filepath.Join(w.Store.root, "workflow.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	if record.TaskID == "" {
		return errors.New("task ID is required")
	}
	if record.Phase == PhaseSucceeded || record.Phase == PhaseRolledBack {
		return nil
	}
	if !isResumableApplyPhase(record.Phase) {
		return fmt.Errorf("task %q is not in an apply phase: %s", record.TaskID, record.Phase)
	}
	// Reload the durable record before resuming. Callers may have retained a
	// stale copy while the helper was restarted or another request persisted a
	// boundary. The task store remains the source of truth for recovery.
	if persisted, err := w.Store.Get(record.TaskID); err == nil {
		record = persisted
	} else {
		return fmt.Errorf("load task %q: %w", record.TaskID, err)
	}
	if record.Phase == PhaseSucceeded || record.Phase == PhaseRolledBack {
		return nil
	}
	if !isResumableApplyPhase(record.Phase) {
		return fmt.Errorf("task %q is no longer in an apply phase: %s", record.TaskID, record.Phase)
	}
	if w.Verify != nil {
		verified, err := w.Verify(ctx, record)
		if err != nil {
			if record.Phase == PhaseStopping {
				if w.Gate == nil {
					return w.fail(ctx, record, false, err)
				}
				state, gateErr := w.Gate.Load()
				if gateErr == nil && state.Mode == GateNormal {
					return w.fail(ctx, record, false, err)
				}
			}
			return w.requireRecovery(context.WithoutCancel(ctx), record, err)
		}
		record = verified
		if err := w.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
			task.StagingPath, task.Version, task.BuildID, task.Images = record.StagingPath, record.Version, record.BuildID, record.Images
			task.Compatibility = CloneCompatibility(record.Compatibility)
			return nil
		}); err != nil {
			return w.requireRecovery(context.WithoutCancel(ctx), record, err)
		}
	}
	if reader, ok := w.Adapter.(currentTargetReader); ok && !record.PreviousTargetCaptured {
		previous, err := reader.CurrentTarget()
		if err != nil {
			return w.fail(ctx, record, false, fmt.Errorf("read current release: %w", err))
		}
		if err := w.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
			task.PreviousTarget = previous
			task.PreviousTargetCaptured = true
			return nil
		}); err != nil {
			return w.fail(ctx, record, false, fmt.Errorf("persist current release: %w", err))
		}
		record.PreviousTarget = previous
		record.PreviousTargetCaptured = true
	}
	steps := []struct {
		next TaskPhase
		run  func(context.Context, TaskRecord) error
	}{
		{PhaseBackingUp, w.Adapter.Stop},
		{PhaseMigrating, w.Adapter.Backup},
		{PhaseActivating, w.Adapter.Migrate},
		{PhaseValidating, w.Adapter.Activate},
		{PhaseCommitting, w.Adapter.Validate},
		{PhaseSucceeded, w.Adapter.Commit},
	}
	start := applyStepIndex(record.Phase)
	if start < 0 {
		return fmt.Errorf("task %q has no resumable step for phase %s", record.TaskID, record.Phase)
	}
	// A task past the stopping boundary has already disrupted the service (or
	// may have done so before a crash), so any later failure must attempt
	// rollback. The stopping step itself only becomes disruptive after it
	// succeeds, preserving the original stop-failure behavior.
	disrupted := record.Phase != PhaseStopping
	if w.Gate != nil && record.Phase == PhaseStopping {
		state, err := w.Gate.Load()
		if err != nil {
			return w.requireRecovery(context.WithoutCancel(ctx), record, fmt.Errorf("read maintenance gate: %w", err))
		}
		disrupted = state.TaskID == record.TaskID && state.Mode != GateNormal
	}
	for index := start; index < len(steps); index++ {
		step := steps[index]
		if err := ctx.Err(); err != nil {
			return w.fail(ctx, record, disrupted, err)
		}
		if index == 0 && w.Prepare != nil {
			if err := w.Prepare(ctx, record); err != nil {
				return w.fail(ctx, record, disrupted, fmt.Errorf("package and capacity preflight: %w", err))
			}
		}
		if index == 0 {
			if preparer, ok := w.Adapter.(packagePreparer); ok {
				if err := preparer.PreparePackage(ctx, record); err != nil {
					return w.fail(ctx, record, disrupted, fmt.Errorf("prepare target runtime before stop: %w", err))
				}
			}
		}
		if index == 0 && record.Actor == "policy" {
			if err := w.authorizeAutomatic(ctx, record); err != nil {
				return w.fail(ctx, record, disrupted, err)
			}
		}
		if index == 0 && record.Actor != "policy" && (w.Verify != nil || w.Preflight != nil || record.Preflight != nil) {
			if err := w.recheckInstallation(ctx, record); err != nil {
				return w.fail(ctx, record, disrupted, err)
			}
		}
		if index == 0 {
			if err := w.saveGate(GateState{Mode: GateBlocked, TaskID: record.TaskID, BuildID: record.BuildID}); err != nil {
				return w.fail(ctx, record, disrupted, fmt.Errorf("persist maintenance gate: %w", err))
			}
			if record.Actor == "policy" {
				if err := w.authorizePolicy(record); err != nil {
					if !disrupted {
						if gateErr := w.saveGate(GateState{Mode: GateNormal}); gateErr != nil {
							return w.requireRecovery(context.WithoutCancel(ctx), record, errors.Join(err, gateErr))
						}
					}
					return w.fail(ctx, record, disrupted, err)
				}
			}
			// Stop can partially succeed before returning an error or crashing.
			disrupted = true
		}
		if step.next == PhaseValidating {
			if err := w.saveGate(GateState{Mode: GateCandidate, TaskID: record.TaskID, BuildID: record.BuildID}); err != nil {
				return w.fail(ctx, record, disrupted, fmt.Errorf("persist candidate gate: %w", err))
			}
		}
		if err := step.run(ctx, record); err != nil {
			return w.fail(ctx, record, disrupted, fmt.Errorf("%s: %w", step.next, err))
		}
		if step.next == PhaseCommitting && w.Ready != nil {
			if err := w.Ready(ctx, record); err != nil {
				return w.fail(ctx, record, disrupted, fmt.Errorf("candidate readiness: %w", err))
			}
		}
		if index == 0 {
			disrupted = true
		}
		if step.next == PhaseSucceeded {
			if err := w.saveGate(GateState{Mode: GateNormal}); err != nil {
				return w.fail(ctx, record, true, fmt.Errorf("clear candidate gate: %w", err))
			}
		}
		if err := w.Store.MarkPhase(record.TaskID, step.next); err != nil {
			return w.fail(ctx, record, disrupted, err)
		}
		record.Phase = step.next
		if step.next == PhaseCommitting || step.next == PhaseSucceeded {
			record.Committed = true
		}
		if step.next == PhaseSucceeded {
			return errors.Join(w.emitOutcome(ctx, record), w.cleanupCompleted(ctx, record))
		}
	}
	return nil
}

func isResumableApplyPhase(phase TaskPhase) bool {
	switch phase {
	case PhaseStopping, PhaseBackingUp, PhaseMigrating, PhaseActivating, PhaseValidating, PhaseCommitting:
		return true
	default:
		return false
	}
}

func applyStepIndex(phase TaskPhase) int {
	switch phase {
	case PhaseStopping:
		return 0
	case PhaseBackingUp:
		return 1
	case PhaseMigrating:
		return 2
	case PhaseActivating:
		return 3
	case PhaseValidating:
		return 4
	case PhaseCommitting:
		return 5
	default:
		return -1
	}
}

// Recover resumes the one active task, if any, after the updater process has
// restarted. Awaiting confirmation remains untouched; only a previously
// confirmed task that entered service switching is eligible for recovery.
func (w *WorkflowExecutor) Recover(ctx context.Context) error {
	if w == nil || w.Store == nil || w.Adapter == nil {
		return errors.New("upgrade workflow is not configured")
	}
	records, err := w.Store.List()
	if err != nil {
		return err
	}
	for _, record := range records {
		if !isResumableApplyPhase(record.Phase) {
			if err := w.emitOutcome(ctx, record); err != nil {
				return err
			}
			if err := w.cleanupRecovered(ctx, record); err != nil {
				return err
			}
			continue
		}
		if err := w.Apply(ctx, record); err != nil {
			return err
		}
		// TaskStore enforces a single active task, so there can be no second
		// resumable record. Continue defensively if a legacy store contains one.
	}
	return nil
}

func (w *WorkflowExecutor) cleanupRecovered(ctx context.Context, record TaskRecord) error {
	if w.Cleanup == nil || record.Phase != PhaseSucceeded {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	lock, err := acquireUpgradeLock(filepath.Join(w.Store.root, "workflow.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	current, err := w.Store.Get(record.TaskID)
	if err != nil {
		return err
	}
	return w.cleanupCompleted(ctx, current)
}

func (w *WorkflowExecutor) cleanupCompleted(ctx context.Context, record TaskRecord) error {
	if w.Cleanup == nil || record.Phase != PhaseSucceeded {
		return nil
	}
	if err := w.Cleanup(ctx, record); err != nil {
		// Cleanup is after commit: report it without invoking data rollback or
		// changing the successful upgrade outcome.
		if w.Events != nil {
			now := time.Now()
			if w.Now != nil {
				now = w.Now()
			}
			return errors.Join(err, w.Events.Append(ctx, UpgradeEvent{Event: "cleanup_failed", TaskID: record.TaskID, BuildID: record.BuildID, Reason: err.Error(), At: now.UTC()}))
		}
		return err
	}
	return nil
}

func (w *WorkflowExecutor) fail(ctx context.Context, record TaskRecord, disrupted bool, cause error) error {
	// Persist recovery even when the operation's context was canceled.
	ctx = context.WithoutCancel(ctx)
	if err := w.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
		task.Error = cause.Error()
		rememberFailurePhase(task, record)
		return nil
	}); err != nil {
		return errors.Join(cause, err)
	}
	if !disrupted {
		return errors.Join(cause, w.finish(ctx, record, PhaseFailed))
	}
	if record.Committed || (record.Actor == "policy" && (record.PolicySnapshot == nil || !record.PolicySnapshot.AutoRollback)) {
		return w.requireRecovery(ctx, record, cause)
	}
	if record.Actor == "policy" && !record.Compatibility.AllowsAutomaticRollback() {
		return w.requireRecovery(ctx, record, cause)
	}
	if err := w.prepareRollbackGate(record); err != nil {
		return w.requireRecovery(ctx, record, errors.Join(cause, err))
	}
	if rollbackErr := w.Adapter.Rollback(ctx, record); rollbackErr != nil {
		return w.requireRecovery(ctx, record, errors.Join(cause, fmt.Errorf("rollback: %w", rollbackErr)))
	}
	if gateErr := w.saveGate(GateState{Mode: GateNormal}); gateErr != nil {
		return w.requireRecovery(ctx, record, errors.Join(cause, fmt.Errorf("clear recovery gate: %w", gateErr)))
	}
	return errors.Join(fmt.Errorf("%w; automatically rolled back", cause), w.finish(ctx, record, PhaseRolledBack))
}

func (w *WorkflowExecutor) requireRecovery(ctx context.Context, record TaskRecord, cause error) error {
	metadataErr := w.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
		task.Error = cause.Error()
		rememberFailurePhase(task, record)
		return nil
	})
	gateErr := w.saveGate(GateState{Mode: GateRecovery, TaskID: record.TaskID, BuildID: record.BuildID, Reason: cause.Error()})
	return errors.Join(cause, metadataErr, gateErr, w.finish(ctx, record, PhaseRecoveryRequired))
}

// A terminal recovery status must not erase the last execution boundary.
// Repeated operator recovery attempts keep the original boundary so an early
// backup failure cannot be mistaken for an already-migrated database.
func rememberFailurePhase(task *TaskRecord, record TaskRecord) {
	if isResumableApplyPhase(record.Phase) {
		task.FailurePhase = record.Phase
	}
}

func (w *WorkflowExecutor) finish(ctx context.Context, record TaskRecord, phase TaskPhase) error {
	current, err := w.Store.Get(record.TaskID)
	if err != nil {
		return err
	}
	if current.Phase != phase {
		if err := w.Store.MarkPhase(record.TaskID, phase); err != nil {
			return err
		}
	}
	updated, err := w.Store.Get(record.TaskID)
	if err != nil {
		return err
	}
	record = updated
	return w.emitOutcome(ctx, record)
}

func (w *WorkflowExecutor) authorizeAutomatic(ctx context.Context, record TaskRecord) error {
	if w.PolicyStore == nil || record.PolicySnapshot == nil {
		return errors.New("automatic approval is missing its persisted policy")
	}
	if err := w.recheckInstallation(ctx, record); err != nil {
		return err
	}
	return w.authorizePolicy(record)
}

func (w *WorkflowExecutor) authorizePolicy(record TaskRecord) error {
	policy, err := w.PolicyStore.Load()
	if err != nil {
		return fmt.Errorf("recheck automatic policy: %w", err)
	}
	now := time.Now()
	if w.Now != nil {
		now = w.Now()
	}
	if policy != *record.PolicySnapshot || record.PolicyVersion != policy.PolicyVersion || !policy.Enabled || !policy.AllowRestart || !policy.InWindow(now) {
		return errors.New("automatic policy or maintenance authorization expired before stop")
	}
	if record.Preflight == nil || !policy.AllowsRelease(Manifest{Version: record.Version, Channel: policy.Channel}, record.Preflight.CurrentVersion) {
		return errors.New("automatic task has no valid release preflight")
	}
	if err := record.Compatibility.AllowsAutomatic(*record.Preflight); err != nil {
		return err
	}
	return nil
}

func (w *WorkflowExecutor) recheckInstallation(ctx context.Context, record TaskRecord) error {
	if w.Preflight == nil || record.Preflight == nil {
		return errors.New("stop requires independent installation preflight and approval snapshot")
	}
	current, err := w.Preflight(ctx)
	if err != nil {
		return fmt.Errorf("final independent installation preflight: %w", err)
	}
	if current != *record.Preflight {
		return errors.New("installation state changed immediately before stop; new approval is required")
	}
	if record.Actor == "policy" {
		return record.Compatibility.AllowsAutomatic(current)
	}
	return nil
}

func (w *WorkflowExecutor) emitOutcome(ctx context.Context, record TaskRecord) error {
	if w.Events == nil || !isTerminalPhase(record.Phase) || record.OutcomeReported == record.Phase {
		return nil
	}
	now := time.Now()
	if w.Now != nil {
		now = w.Now()
	}
	if err := w.Events.Append(ctx, UpgradeEvent{Event: string(record.Phase), TaskID: record.TaskID, PolicyVersion: record.PolicyVersion, BuildID: record.BuildID, Reason: record.Error, At: now.UTC()}); err != nil {
		return err
	}
	return w.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error { task.OutcomeReported = record.Phase; return nil })
}

func (w *WorkflowExecutor) Rollback(ctx context.Context, record TaskRecord) error {
	if w == nil || w.Store == nil || w.Adapter == nil {
		return errors.New("upgrade workflow is not configured")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	lock, err := acquireUpgradeLock(filepath.Join(w.Store.root, "workflow.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	persisted, err := w.Store.Get(record.TaskID)
	if err != nil {
		return err
	}
	record = persisted
	if record.Committed {
		return errors.New("committed task requires a new compatibility review before rollback")
	}
	if record.Phase != PhaseFailed && record.Phase != PhaseRecoveryRequired {
		return fmt.Errorf("task %q is not eligible for rollback", record.TaskID)
	}
	if !rollbackExecutionBoundary(record) {
		return errors.New("task has no persisted service-switch boundary; preparation failures cannot be rolled back")
	}
	if err := w.prepareRollbackGate(record); err != nil {
		return w.requireRecovery(ctx, record, err)
	}
	if err := w.Adapter.Rollback(ctx, record); err != nil {
		return w.requireRecovery(context.WithoutCancel(ctx), record, err)
	}
	if err := w.saveGate(GateState{Mode: GateNormal}); err != nil {
		return w.requireRecovery(ctx, record, err)
	}
	return w.finish(ctx, record, PhaseRolledBack)
}

func (w *WorkflowExecutor) prepareRollbackGate(record TaskRecord) error {
	// The previous release must be allowed to initialize for validation, while
	// business ingress stays closed until rollback has actually completed.
	buildID := ""
	if record.Preflight != nil {
		buildID = record.Preflight.CurrentBuildID
	}
	return w.saveGate(GateState{Mode: GateCandidate, TaskID: record.TaskID, BuildID: buildID, Reason: "validating previous release"})
}

func (w *WorkflowExecutor) saveGate(state GateState) error {
	if w.Gate == nil {
		return nil
	}
	return w.Gate.Save(state)
}
