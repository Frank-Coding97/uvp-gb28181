package upgrade

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type helperSchedulerEvents struct {
	mu     sync.Mutex
	events []UpgradeEvent
}

type failingHelperSchedulerEvents struct {
	failEvent string
}

func (s failingHelperSchedulerEvents) Append(_ context.Context, event UpgradeEvent) error {
	if event.Event == s.failEvent {
		return errors.New("injected event failure")
	}
	return nil
}

func (s *helperSchedulerEvents) Append(_ context.Context, event UpgradeEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *helperSchedulerEvents) all() []UpgradeEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]UpgradeEvent(nil), s.events...)
}

type helperSchedulerFixture struct {
	scheduler *HelperScheduler
	store     *TaskStore
	policies  *PolicyStore
	policy    AutoUpgradePolicy
	record    TaskRecord
	preflight *PreflightSnapshot
	events    *helperSchedulerEvents
}

func TestHelperSchedulerDefaultIntervalCoversShortestMaintenanceWindow(t *testing.T) {
	if defaultHelperSchedulerInterval > time.Minute {
		t.Fatalf("default helper scheduler interval = %s, want at most one minute", defaultHelperSchedulerInterval)
	}
}

func newHelperSchedulerFixture(t *testing.T) *helperSchedulerFixture {
	t.Helper()
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	policies, err := OpenPolicyStore(filepath.Join(root, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	policy := AutoUpgradePolicy{
		Enabled:       true,
		PolicyVersion: 1,
		Timezone:      "UTC",
		WindowStart:   "02:00",
		WindowEnd:     "04:00",
		Channel:       "stable",
		Deployment:    "standalone",
		OS:            "linux",
		Arch:          "amd64",
		PatchOnly:     true,
		AllowRestart:  true,
		AutoRollback:  true,
	}
	if err := policies.Save(policy); err != nil {
		t.Fatal(err)
	}
	record, err := store.Submit(TaskRequest{
		IdempotencyKey: "helper-scheduler",
		Actor:          "policy",
		TargetDigest:   strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []TaskPhase{PhaseVerifying, PhasePreparing, PhaseAwaitingConfirmation} {
		if err := store.MarkPhase(record.TaskID, phase); err != nil {
			t.Fatal(err)
		}
	}
	preflight := &PreflightSnapshot{
		CurrentVersion:  "1.1.0",
		CurrentBuildID:  "source-build",
		ConfigDigest:    "config-a",
		DatabaseDialect: "sqlite",
		MigrationDigest: strings.Repeat("b", 64),
	}
	compatibility := &CompatibilityDeclaration{
		SchemaVersion:    1,
		AutomaticAllowed: true,
		SourceBuilds:     []ReleaseIdentity{{Version: "1.1.0", BuildID: "source-build"}},
		Database: DatabaseCompatibility{
			Dialect:                       "sqlite",
			AllowedSourceMigrationDigests: []string{preflight.MigrationDigest},
			TargetMigrationDigest:         strings.Repeat("c", 64),
		},
	}
	if err := store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
		policySnapshot := policy
		preflightSnapshot := *preflight
		task.Version = "1.1.1"
		task.BuildID = "target-build"
		task.StagingPath = filepath.Join(root, "staging", "release.tar.gz")
		task.PolicyVersion = policy.PolicyVersion
		task.PolicySnapshot = &policySnapshot
		task.Preflight = &preflightSnapshot
		task.Compatibility = CloneCompatibility(compatibility)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	record, err = store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	events := &helperSchedulerEvents{}
	fixture := &helperSchedulerFixture{
		store:     store,
		policies:  policies,
		policy:    policy,
		record:    record,
		preflight: preflight,
		events:    events,
	}
	fixture.scheduler = &HelperScheduler{
		Store:       store,
		PolicyStore: policies,
		Target:      Target{Deployment: "standalone", OS: "linux", Arch: "amd64", UpdaterProtocol: UpdaterProtocolVersion},
		Preflight: func(context.Context) (PreflightSnapshot, error) {
			return *fixture.preflight, nil
		},
		Apply: func(context.Context, TaskRecord) error {
			t.Fatal("unexpected apply")
			return nil
		},
		Events: events,
		Now: func() time.Time {
			return time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)
		},
	}
	return fixture
}

func TestHelperSchedulerAppliesPreparedPolicyTaskInMaintenanceWindow(t *testing.T) {
	fixture := newHelperSchedulerFixture(t)
	applyCalls := 0
	fixture.scheduler.Apply = func(_ context.Context, record TaskRecord) error {
		applyCalls++
		if record.Phase != PhaseStopping {
			t.Fatalf("apply phase = %s", record.Phase)
		}
		persisted, err := fixture.store.Get(record.TaskID)
		if err != nil {
			return err
		}
		if persisted.Phase != PhaseStopping {
			t.Fatalf("persisted phase = %s", persisted.Phase)
		}
		return nil
	}

	if err := fixture.scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if applyCalls != 1 {
		t.Fatalf("apply calls = %d", applyCalls)
	}
	updated, err := fixture.store.Get(fixture.record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseStopping {
		t.Fatalf("task phase = %s", updated.Phase)
	}
	events := fixture.events.all()
	if len(events) != 2 || events[0].Event != "auto_approved_by_policy" || events[1].Event != "auto_started" {
		t.Fatalf("events = %#v", events)
	}
}

func TestHelperSchedulerKeepsPreparedTaskOutsideMaintenanceWindow(t *testing.T) {
	fixture := newHelperSchedulerFixture(t)
	fixture.scheduler.Now = func() time.Time {
		return time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC)
	}
	called := false
	fixture.scheduler.Apply = func(context.Context, TaskRecord) error {
		called = true
		return nil
	}

	if err := fixture.scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("apply called outside maintenance window")
	}
	updated, err := fixture.store.Get(fixture.record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseAwaitingConfirmation {
		t.Fatalf("task phase = %s", updated.Phase)
	}
	if events := fixture.events.all(); len(events) != 0 {
		t.Fatalf("events = %#v", events)
	}
}

func TestHelperSchedulerCancelsPreparedTaskWhenAuthorizationChanges(t *testing.T) {
	for _, scenario := range []string{"policy", "target", "preflight", "compatibility"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newHelperSchedulerFixture(t)
			fixture.scheduler.Apply = func(context.Context, TaskRecord) error {
				t.Fatal("apply called after authorization changed")
				return nil
			}
			switch scenario {
			case "policy":
				changed := fixture.policy
				changed.PolicyVersion++
				if err := fixture.policies.Save(changed); err != nil {
					t.Fatal(err)
				}
			case "target":
				fixture.scheduler.Target.Deployment = "compose"
			case "preflight":
				fixture.preflight.ConfigDigest = "config-b"
			case "compatibility":
				if err := fixture.store.UpdateMetadata(fixture.record.TaskID, func(task *TaskRecord) error {
					task.Compatibility.AutomaticAllowed = false
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}

			if err := fixture.scheduler.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			updated, err := fixture.store.Get(fixture.record.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			if updated.Phase != PhaseFailed || updated.Error == "" {
				t.Fatalf("task = %#v", updated)
			}
			events := fixture.events.all()
			if len(events) != 1 || events[0].Event != "blocked" || events[0].Reason == "" {
				t.Fatalf("events = %#v", events)
			}
		})
	}
}

func TestHelperSchedulerRevokesPreparedTasksWhenPolicyIsDisabled(t *testing.T) {
	fixture := newHelperSchedulerFixture(t)
	fixture.scheduler.Now = func() time.Time {
		return time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC)
	}
	policy := fixture.policy
	policy.Enabled = false
	if err := fixture.policies.Save(policy); err != nil {
		t.Fatal(err)
	}

	if err := fixture.scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	updated, err := fixture.store.Get(fixture.record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseFailed || !strings.Contains(updated.Error, "disabled") {
		t.Fatalf("task = %#v", updated)
	}
	events := fixture.events.all()
	if len(events) != 1 || events[0].Event != "blocked" {
		t.Fatalf("events = %#v", events)
	}
}

func TestHelperSchedulerLeavesPreparedTaskWhenRecoveryIsRequired(t *testing.T) {
	fixture := newHelperSchedulerFixture(t)
	recovery := fixture.record
	recovery.Phase = PhaseRecoveryRequired
	prepared := fixture.record
	prepared.TaskID = strings.Repeat("d", 32)
	prepared.IdempotencyKey = "helper-scheduler-prepared"
	prepared.Phase = PhaseAwaitingConfirmation
	if err := fixture.store.save([]TaskRecord{recovery, prepared}); err != nil {
		t.Fatal(err)
	}
	fixture.scheduler.Apply = func(context.Context, TaskRecord) error {
		t.Fatal("apply called while recovery is required")
		return nil
	}

	if err := fixture.scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	updated, err := fixture.store.Get(prepared.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseAwaitingConfirmation {
		t.Fatalf("prepared task phase = %s", updated.Phase)
	}
}

func TestHelperSchedulerCancellationDoesNotRevokePreparedTask(t *testing.T) {
	fixture := newHelperSchedulerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fixture.scheduler.Tick(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("tick error = %v", err)
	}
	updated, err := fixture.store.Get(fixture.record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseAwaitingConfirmation {
		t.Fatalf("task phase = %s", updated.Phase)
	}
}

func TestHelperSchedulerAppliesAfterStartedEventWriteFailure(t *testing.T) {
	fixture := newHelperSchedulerFixture(t)
	fixture.scheduler.Events = failingHelperSchedulerEvents{failEvent: "auto_started"}
	called := false
	fixture.scheduler.Apply = func(context.Context, TaskRecord) error {
		called = true
		return nil
	}
	if err := fixture.scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("apply did not run after crossing the stopping boundary")
	}
}

func TestHelperSchedulerRunContinuesAfterApplyError(t *testing.T) {
	fixture := newHelperSchedulerFixture(t)
	started := make(chan struct{}, 1)
	fixture.scheduler.Interval = time.Millisecond
	fixture.scheduler.Apply = func(context.Context, TaskRecord) error {
		started <- struct{}{}
		return errors.New("injected apply failure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- fixture.scheduler.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not attempt apply")
	}
	select {
	case err := <-done:
		t.Fatalf("scheduler exited after one apply error: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v", err)
	}
	events := fixture.events.all()
	foundBlocked := false
	for _, event := range events {
		if event.Event == "blocked" && strings.Contains(event.Reason, "injected apply failure") {
			foundBlocked = true
		}
	}
	if !foundBlocked {
		t.Fatalf("events = %#v", events)
	}
}
