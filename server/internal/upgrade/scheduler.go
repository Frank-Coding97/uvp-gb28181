package upgrade

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
)

type UpgradeEvent struct {
	Event         string    `json:"event"`
	TaskID        string    `json:"task_id,omitempty"`
	PolicyVersion uint64    `json:"policy_version,omitempty"`
	ReleaseID     string    `json:"release_id,omitempty"`
	BuildID       string    `json:"build_id,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	At            time.Time `json:"at"`
}

type UpgradeEventSink interface {
	Append(context.Context, UpgradeEvent) error
}

type AutoScheduler struct {
	Store          *TaskStore
	PolicyStore    *PolicyStore
	Target         Target
	Check          func(context.Context) (Manifest, error)
	CurrentVersion func() string
	// CurrentVersionWithContext lets a registered helper obtain the current
	// release from its independent preflight command without relying on the
	// business process or losing the preflight error behind a string callback.
	CurrentVersionWithContext func(context.Context) (string, error)
	Prepare                   func(context.Context, Manifest, Artifact) (StagedArtifact, error)
	Preflight                 func(context.Context) (PreflightSnapshot, error)
	Events                    UpgradeEventSink
	Now                       func() time.Time
	Interval                  time.Duration
	mu                        sync.Mutex
}

func (s *AutoScheduler) Tick(ctx context.Context) error {
	if s == nil || s.Store == nil || s.PolicyStore == nil {
		return errors.New("auto scheduler is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Check == nil || s.Prepare == nil {
		return errors.New("auto scheduler callbacks are incomplete")
	}
	policy, err := s.PolicyStore.Load()
	if err != nil {
		return s.blocked(ctx, UpgradeEvent{Event: "blocked", Reason: "policy: " + err.Error(), At: s.now()})
	}
	if !policy.Enabled {
		return s.cancelPendingAuto(ctx, policy, s.now())
	}
	if changed, err := s.invalidateStaleAuto(ctx, policy, s.now()); err != nil || changed {
		return err
	}
	manifest, err := s.Check(ctx)
	if err != nil {
		return s.blocked(ctx, UpgradeEvent{Event: "blocked", PolicyVersion: policy.PolicyVersion, Reason: "check: " + err.Error(), At: s.now()})
	}
	artifact, err := manifest.ArtifactFor(s.target())
	if err != nil {
		return s.blocked(ctx, UpgradeEvent{Event: "blocked", PolicyVersion: policy.PolicyVersion, ReleaseID: manifest.ReleaseID, BuildID: manifest.BuildID, Reason: err.Error(), At: s.now()})
	}
	current := ""
	if s.CurrentVersionWithContext != nil {
		current, err = s.CurrentVersionWithContext(ctx)
		if err != nil {
			return s.blocked(ctx, UpgradeEvent{Event: "blocked", PolicyVersion: policy.PolicyVersion, ReleaseID: manifest.ReleaseID, BuildID: manifest.BuildID, Reason: "current preflight: " + err.Error(), At: s.now()})
		}
	} else if s.CurrentVersion != nil {
		current = s.CurrentVersion()
	}
	if !policy.AllowsRelease(manifest, current) || !policy.AllowsTarget(s.target()) {
		return s.blocked(ctx, UpgradeEvent{Event: "blocked", PolicyVersion: policy.PolicyVersion, ReleaseID: manifest.ReleaseID, BuildID: manifest.BuildID, Reason: "release or deployment is outside automatic upgrade policy", At: s.now()})
	}
	records, err := s.Store.List()
	if err != nil {
		return err
	}
	for _, record := range records {
		// Recovery-required tasks need repair before a new automatic task.
		if record.Phase == PhaseRecoveryRequired {
			return nil
		}
		if isTerminalPhase(record.Phase) {
			continue
		}
		if record.Actor == "policy" && record.Phase == PhaseAwaitingConfirmation {
			return s.validatePreparedAutoTask(ctx, policy, manifest, artifact, record)
		}
		return nil
	}
	key := fmt.Sprintf("auto:%d:%s:%d:%s", policy.PolicyVersion, manifest.Channel, manifest.Sequence, artifact.SHA256)
	for _, record := range records {
		if record.IdempotencyKey == key {
			return nil
		}
	}
	preflight, err := s.automaticPreflight(ctx, manifest)
	if err != nil {
		return s.blocked(ctx, UpgradeEvent{Event: "blocked", PolicyVersion: policy.PolicyVersion, ReleaseID: manifest.ReleaseID, BuildID: manifest.BuildID, Reason: "compatibility: " + err.Error(), At: s.now()})
	}
	staged, err := s.Prepare(ctx, manifest, artifact)
	if err != nil {
		return s.blocked(ctx, UpgradeEvent{Event: "blocked", PolicyVersion: policy.PolicyVersion, ReleaseID: manifest.ReleaseID, BuildID: manifest.BuildID, Reason: "prepare: " + err.Error(), At: s.now()})
	}
	record, err := s.Store.Submit(TaskRequest{IdempotencyKey: key, Actor: "policy", TargetDigest: artifact.SHA256})
	if errors.Is(err, ErrTaskBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.Phase != PhaseFetching {
		return nil
	}
	if err := s.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
		task.Source, task.StagingPath, task.Version, task.BuildID = "online", staged.Path, manifest.Version, manifest.BuildID
		task.ReleaseNotes, task.PublishedAt = manifest.ReleaseNotes, manifest.PublishedAt
		task.Images = append([]ImageArtifact(nil), staged.Images...)
		task.PolicyVersion, task.Preflight = policy.PolicyVersion, &preflight
		task.Compatibility = CloneCompatibility(manifest.Compatibility)
		policyCopy := policy
		task.PolicySnapshot = &policyCopy
		return nil
	}); err != nil {
		return err
	}
	for _, phase := range []TaskPhase{PhaseVerifying, PhasePreparing, PhaseAwaitingConfirmation} {
		if err := s.Store.MarkPhase(record.TaskID, phase); err != nil {
			return err
		}
	}
	if err := s.emit(ctx, UpgradeEvent{Event: "downloaded", TaskID: record.TaskID, PolicyVersion: policy.PolicyVersion, ReleaseID: manifest.ReleaseID, BuildID: manifest.BuildID, At: s.now()}); err != nil {
		return err
	}
	return nil
}

// validatePreparedAutoTask keeps a staged task aligned with the signed release
// and policy that created it. The independent helper is the only component
// allowed to cross the stopping boundary; it repeats the runtime preflight
// after the business process is no longer trusted to stay alive.
func (s *AutoScheduler) validatePreparedAutoTask(ctx context.Context, policy AutoUpgradePolicy, manifest Manifest, artifact Artifact, record TaskRecord) error {
	block := func(reason string) error {
		if err := s.Store.Cancel(record.TaskID); err != nil {
			return err
		}
		if err := s.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error { task.Error = reason; return nil }); err != nil {
			return err
		}
		return s.emit(ctx, UpgradeEvent{Event: "blocked", TaskID: record.TaskID, PolicyVersion: policy.PolicyVersion, ReleaseID: manifest.ReleaseID, BuildID: manifest.BuildID, Reason: reason, At: s.now()})
	}
	if record.Version != manifest.Version || record.BuildID != manifest.BuildID || record.TargetDigest != artifact.SHA256 {
		return block("trusted release changed before automatic apply; new preflight is required")
	}
	if !CompatibilityEqual(record.Compatibility, manifest.Compatibility) {
		return block("signed compatibility declaration changed before automatic apply; new preflight is required")
	}
	if record.PolicySnapshot == nil || record.PolicyVersion != policy.PolicyVersion || *record.PolicySnapshot != policy {
		return block("automatic upgrade policy changed; new approval is required")
	}
	if err := manifest.AllowsAutomaticUpgrade(dereferencePreflight(record.Preflight)); err != nil {
		return block("signed compatibility declaration no longer authorizes this installation: " + err.Error())
	}
	return nil
}

func (s *AutoScheduler) automaticPreflight(ctx context.Context, manifest Manifest) (PreflightSnapshot, error) {
	if s.Preflight == nil {
		return PreflightSnapshot{}, errors.New("automatic upgrade requires a compatibility preflight")
	}
	preflight, err := s.Preflight(ctx)
	if err != nil {
		return PreflightSnapshot{}, err
	}
	if err := manifest.AllowsAutomaticUpgrade(preflight); err != nil {
		return PreflightSnapshot{}, err
	}
	return preflight, nil
}

func dereferencePreflight(preflight *PreflightSnapshot) PreflightSnapshot {
	if preflight == nil {
		return PreflightSnapshot{}
	}
	return *preflight
}

func (s *AutoScheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *AutoScheduler) cancelPendingAuto(ctx context.Context, policy AutoUpgradePolicy, at time.Time) error {
	records, err := s.Store.List()
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Actor != "policy" || !isPendingAutoPhase(record.Phase) {
			continue
		}
		if err := s.Store.Cancel(record.TaskID); err != nil {
			continue
		}
		const reason = "policy disabled before maintenance"
		if err := s.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
			task.Error = reason
			return nil
		}); err != nil {
			return err
		}
		if err := s.emit(ctx, UpgradeEvent{Event: "blocked", TaskID: record.TaskID, PolicyVersion: policy.PolicyVersion, Reason: reason, At: at}); err != nil {
			return err
		}
	}
	return nil
}

func (s *AutoScheduler) invalidateStaleAuto(ctx context.Context, policy AutoUpgradePolicy, at time.Time) (bool, error) {
	records, err := s.Store.List()
	if err != nil {
		return false, err
	}
	changed := false
	for _, record := range records {
		if record.Actor != "policy" || !isPendingAutoPhase(record.Phase) || record.PolicyVersion == 0 || record.PolicyVersion == policy.PolicyVersion {
			continue
		}
		if err := s.Store.Cancel(record.TaskID); err != nil {
			continue
		}
		changed = true
		reason := fmt.Sprintf("policy version changed from %d to %d; new approval is required", record.PolicyVersion, policy.PolicyVersion)
		if err := s.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
			task.Error = reason
			return nil
		}); err != nil {
			return changed, err
		}
		if err := s.emit(ctx, UpgradeEvent{Event: "blocked", TaskID: record.TaskID, PolicyVersion: policy.PolicyVersion, Reason: reason, At: at}); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func isPendingAutoPhase(phase TaskPhase) bool {
	switch phase {
	case PhaseFetching, PhaseVerifying, PhasePreparing, PhaseAwaitingConfirmation:
		return true
	default:
		return false
	}
}

func (s *AutoScheduler) Run(ctx context.Context) error {
	interval := s.Interval
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
		// A single failed poll must not terminate unattended maintenance. The
		// durable blocked event carries the reason; later polls can recover.
		_ = s.blocked(ctx, UpgradeEvent{Event: "blocked", Reason: err.Error(), At: time.Now()})
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
				_ = s.blocked(ctx, UpgradeEvent{Event: "blocked", Reason: err.Error(), At: time.Now()})
			}
		}
	}
}

func (s *AutoScheduler) target() Target {
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

func (s *AutoScheduler) emit(ctx context.Context, event UpgradeEvent) error {
	if s.Events == nil {
		return nil
	}
	return s.Events.Append(ctx, event)
}

func (s *AutoScheduler) blocked(ctx context.Context, event UpgradeEvent) error {
	if err := s.emit(ctx, event); err != nil {
		return err
	}
	return nil
}
