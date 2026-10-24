package upgrade

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// HelperScheduler owns only the maintenance-window handoff for a prepared
// automatic task. Downloading and staging remain outside the privileged
// helper; this lets a prepared task be activated even after the business
// process that downloaded it has stopped.
type HelperScheduler struct {
	Store       *TaskStore
	PolicyStore *PolicyStore
	Target      Target
	Preflight   func(context.Context) (PreflightSnapshot, error)
	Apply       func(context.Context, TaskRecord) error
	Events      UpgradeEventSink
	Now         func() time.Time
	Interval    time.Duration

	mu sync.Mutex
}

const defaultHelperSchedulerInterval = 30 * time.Second

// Tick applies one already prepared policy task when the persisted policy and
// current installation state still authorize it. It intentionally never
// checks releases, downloads artifacts, or accepts data from the business
// process over its control socket.
func (s *HelperScheduler) Tick(ctx context.Context) error {
	if s == nil || s.Store == nil || s.PolicyStore == nil || s.Apply == nil {
		return errors.New("helper scheduler is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	policy, err := s.PolicyStore.Load()
	if err != nil {
		return fmt.Errorf("load automatic upgrade policy: %w", err)
	}
	records, err := s.Store.List()
	if err != nil {
		return fmt.Errorf("load upgrade tasks: %w", err)
	}
	if !policy.Enabled {
		return s.cancelDisabledPolicyTasks(ctx, records, policy)
	}
	for _, record := range records {
		if record.Phase == PhaseRecoveryRequired {
			return nil
		}
	}

	prepared, err := preparedPolicyTask(records)
	if err != nil || prepared == nil {
		return err
	}
	if !policy.InWindow(s.now()) {
		return nil
	}
	if err := s.authorize(ctx, policy, *prepared); err != nil {
		if ctx.Err() != nil {
			return err
		}
		return s.blockPreparedTask(ctx, policy, *prepared, err)
	}
	return s.startPreparedTask(ctx, policy, *prepared)
}

func preparedPolicyTask(records []TaskRecord) (*TaskRecord, error) {
	var prepared *TaskRecord
	for index := range records {
		if records[index].Actor == "policy" && records[index].Phase == PhaseAwaitingConfirmation {
			if prepared != nil {
				return nil, errors.New("multiple prepared automatic upgrade tasks require operator recovery")
			}
			prepared = &records[index]
			continue
		}
		if !isTerminalPhase(records[index].Phase) && records[index].Actor != "policy" {
			return nil, fmt.Errorf("active non-policy upgrade task %q prevents automatic apply", records[index].TaskID)
		}
	}
	return prepared, nil
}

func (s *HelperScheduler) authorize(ctx context.Context, policy AutoUpgradePolicy, record TaskRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.PolicySnapshot == nil || record.PolicyVersion != policy.PolicyVersion || *record.PolicySnapshot != policy {
		return errors.New("automatic upgrade policy changed; new approval is required")
	}
	target := s.target()
	if target.Deployment == "" {
		return errors.New("registered deployment target is required")
	}
	if !policy.Enabled || !policy.InWindow(s.now()) || !policy.AllowsTarget(target) {
		return errors.New("maintenance window or automatic upgrade authorization expired")
	}
	if record.Preflight == nil {
		return errors.New("automatic task has no compatibility preflight snapshot")
	}
	if record.Version == "" || record.BuildID == "" || record.StagingPath == "" || !validDigest(record.TargetDigest) {
		return errors.New("prepared automatic task has incomplete verified release metadata")
	}
	if !policy.AllowsRelease(Manifest{Version: record.Version, Channel: record.PolicySnapshot.Channel}, record.Preflight.CurrentVersion) {
		return errors.New("prepared release is outside automatic upgrade policy")
	}
	if record.Compatibility == nil {
		return errors.New("prepared release has no automatic compatibility declaration")
	}
	if err := record.Compatibility.AllowsAutomatic(*record.Preflight); err != nil {
		return fmt.Errorf("persisted automatic compatibility no longer matches: %w", err)
	}
	if s.Preflight == nil {
		return errors.New("automatic upgrade requires a runtime compatibility preflight")
	}
	current, err := s.Preflight(ctx)
	if err != nil {
		return fmt.Errorf("recheck preflight: %w", err)
	}
	if current != *record.Preflight {
		return errors.New("current version, configuration, database dialect, or migration state changed; new preflight is required")
	}
	if err := record.Compatibility.AllowsAutomatic(current); err != nil {
		return fmt.Errorf("current automatic compatibility no longer matches: %w", err)
	}
	return nil
}

func (s *HelperScheduler) startPreparedTask(ctx context.Context, policy AutoUpgradePolicy, record TaskRecord) error {
	if err := s.emit(ctx, UpgradeEvent{
		Event:         "auto_approved_by_policy",
		TaskID:        record.TaskID,
		PolicyVersion: policy.PolicyVersion,
		BuildID:       record.BuildID,
		At:            s.now(),
	}); err != nil {
		return err
	}
	if err := s.Store.MarkPhase(record.TaskID, PhaseStopping); err != nil {
		// A manual socket request can win the same transition. Its daemon owns
		// the worker in that case, so do not invoke Apply a second time here.
		latest, getErr := s.Store.Get(record.TaskID)
		if getErr == nil && isResumableApplyPhase(latest.Phase) {
			return nil
		}
		return err
	}
	started, err := s.Store.Get(record.TaskID)
	if err != nil {
		return err
	}
	_ = s.emit(ctx, UpgradeEvent{
		Event:         "auto_started",
		TaskID:        started.TaskID,
		PolicyVersion: policy.PolicyVersion,
		BuildID:       started.BuildID,
		At:            s.now(),
	})
	return s.Apply(ctx, started)
}

func (s *HelperScheduler) blockPreparedTask(ctx context.Context, policy AutoUpgradePolicy, record TaskRecord, cause error) error {
	if err := s.Store.Cancel(record.TaskID); err != nil {
		return err
	}
	if err := s.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
		task.Error = cause.Error()
		return nil
	}); err != nil {
		return err
	}
	return s.emit(ctx, UpgradeEvent{
		Event:         "blocked",
		TaskID:        record.TaskID,
		PolicyVersion: policy.PolicyVersion,
		BuildID:       record.BuildID,
		Reason:        cause.Error(),
		At:            s.now(),
	})
}

func (s *HelperScheduler) cancelDisabledPolicyTasks(ctx context.Context, records []TaskRecord, policy AutoUpgradePolicy) error {
	var failures []error
	for _, record := range records {
		if record.Actor != "policy" || !helperCancellablePolicyPhase(record.Phase) {
			continue
		}
		const reason = "automatic upgrade policy disabled before maintenance"
		if err := s.Store.Cancel(record.TaskID); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := s.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
			task.Error = reason
			return nil
		}); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := s.emit(ctx, UpgradeEvent{
			Event:         "blocked",
			TaskID:        record.TaskID,
			PolicyVersion: policy.PolicyVersion,
			BuildID:       record.BuildID,
			Reason:        reason,
			At:            s.now(),
		}); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func helperCancellablePolicyPhase(phase TaskPhase) bool {
	switch phase {
	case PhaseFetching, PhaseVerifying, PhasePreparing, PhaseAwaitingConfirmation:
		return true
	default:
		return false
	}
}

// Run retries polling after transient failures. A blocked event is durable
// audit evidence, while the loop remains available for the next window.
func (s *HelperScheduler) Run(ctx context.Context) error {
	if s == nil {
		return errors.New("helper scheduler is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	interval := s.Interval
	if interval <= 0 {
		interval = defaultHelperSchedulerInterval
	}
	run := func() {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			_ = s.emit(ctx, UpgradeEvent{Event: "blocked", Reason: err.Error(), At: s.now()})
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			run()
		}
	}
}

func (s *HelperScheduler) target() Target {
	target := s.Target
	if target.OS == "" {
		target.OS = runtime.GOOS
	}
	if target.Arch == "" {
		target.Arch = runtime.GOARCH
	}
	if target.UpdaterProtocol == 0 {
		target.UpdaterProtocol = UpdaterProtocolVersion
	}
	return target
}

func (s *HelperScheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *HelperScheduler) emit(ctx context.Context, event UpgradeEvent) error {
	if s.Events == nil {
		return nil
	}
	return s.Events.Append(ctx, event)
}
