package upgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type schedulerEvents struct{ events []UpgradeEvent }

func (s *schedulerEvents) Append(_ context.Context, event UpgradeEvent) error {
	s.events = append(s.events, event)
	return nil
}

type fakeWorkflowAdapter struct {
	steps       []string
	failAt      string
	rollbackErr error
	previous    string
}

func (f *fakeWorkflowAdapter) run(name string) error {
	f.steps = append(f.steps, name)
	if f.failAt == name {
		return errors.New("injected " + name + " failure")
	}
	return nil
}
func (f *fakeWorkflowAdapter) CurrentTarget() (string, error)             { return f.previous, nil }
func (f *fakeWorkflowAdapter) Stop(context.Context, TaskRecord) error     { return f.run("stop") }
func (f *fakeWorkflowAdapter) Backup(context.Context, TaskRecord) error   { return f.run("backup") }
func (f *fakeWorkflowAdapter) Migrate(context.Context, TaskRecord) error  { return f.run("migrate") }
func (f *fakeWorkflowAdapter) Activate(context.Context, TaskRecord) error { return f.run("activate") }
func (f *fakeWorkflowAdapter) Validate(context.Context, TaskRecord) error { return f.run("validate") }
func (f *fakeWorkflowAdapter) Commit(context.Context, TaskRecord) error   { return f.run("commit") }
func (f *fakeWorkflowAdapter) Rollback(context.Context, TaskRecord) error {
	f.steps = append(f.steps, "rollback")
	return f.rollbackErr
}

func prepareWorkflowTask(t *testing.T) (*TaskStore, TaskRecord) {
	t.Helper()
	store, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Submit(TaskRequest{IdempotencyKey: "workflow", Actor: "admin", TargetDigest: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []TaskPhase{PhaseVerifying, PhasePreparing, PhaseAwaitingConfirmation, PhaseStopping} {
		if err := store.MarkPhase(record.TaskID, phase); err != nil {
			t.Fatal(err)
		}
	}
	record.Phase = PhaseStopping
	return store, record
}

func TestWorkflowPersistsPreviousTargetAndRunsOrderedPhases(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	adapter := &fakeWorkflowAdapter{previous: "/opt/uvp/releases/a"}
	gate, err := OpenGateStore(filepath.Join(t.TempDir(), "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := (&WorkflowExecutor{Store: store, Adapter: adapter, Gate: gate}).Apply(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	want := []string{"stop", "backup", "migrate", "activate", "validate", "commit"}
	if strings.Join(adapter.steps, ",") != strings.Join(want, ",") {
		t.Fatalf("steps = %v, want %v", adapter.steps, want)
	}
	updated, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseSucceeded || updated.PreviousTarget != adapter.previous || !updated.PreviousTargetCaptured || !updated.Committed {
		t.Fatalf("unexpected task: %#v", updated)
	}
	state, err := gate.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != GateNormal {
		t.Fatalf("gate = %#v", state)
	}
}

func TestWorkflowCapturesEmptyPreviousTargetOnceForFlatInstallation(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	adapter := &fakeWorkflowAdapter{}
	if err := (&WorkflowExecutor{Store: store, Adapter: adapter}).Apply(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PreviousTarget != "" || !updated.PreviousTargetCaptured {
		t.Fatalf("flat previous target capture = %#v", updated)
	}
}

func TestWorkflowFailureRollsBackAndMarksRecoveryWhenRollbackFails(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	adapter := &fakeWorkflowAdapter{failAt: "activate", rollbackErr: errors.New("rollback unavailable")}
	gate, err := OpenGateStore(filepath.Join(t.TempDir(), "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := (&WorkflowExecutor{Store: store, Adapter: adapter, Gate: gate}).Apply(context.Background(), record); err == nil {
		t.Fatal("expected apply failure")
	}
	updated, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseRecoveryRequired {
		t.Fatalf("phase = %s", updated.Phase)
	}
	if !strings.Contains(updated.Error, "rollback unavailable") {
		t.Fatalf("error = %q", updated.Error)
	}
	state, err := gate.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != GateRecovery || state.TaskID != record.TaskID {
		t.Fatalf("gate = %#v", state)
	}
}

func TestWorkflowResumesFromPersistedBoundaryWithoutRepeatingEarlierSteps(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	if err := store.MarkPhase(record.TaskID, PhaseBackingUp); err != nil {
		t.Fatal(err)
	}
	record.Phase = PhaseBackingUp
	adapter := &fakeWorkflowAdapter{previous: "/opt/uvp/releases/a"}
	if err := (&WorkflowExecutor{Store: store, Adapter: adapter}).Apply(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	want := []string{"backup", "migrate", "activate", "validate", "commit"}
	if strings.Join(adapter.steps, ",") != strings.Join(want, ",") {
		t.Fatalf("resumed steps = %v, want %v", adapter.steps, want)
	}
}

func TestWorkflowRecoverResumesOnlyConfirmedInProgressTasks(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	for _, phase := range []TaskPhase{PhaseBackingUp, PhaseMigrating} {
		if err := store.MarkPhase(record.TaskID, phase); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &fakeWorkflowAdapter{previous: "/opt/uvp/releases/a"}
	w := &WorkflowExecutor{Store: store, Adapter: adapter}
	if err := w.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"migrate", "activate", "validate", "commit"}
	if strings.Join(adapter.steps, ",") != strings.Join(want, ",") {
		t.Fatalf("recovery steps = %v, want %v", adapter.steps, want)
	}
	updated, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseSucceeded {
		t.Fatalf("recovered phase = %s", updated.Phase)
	}
}

type blockingWorkflowAdapter struct {
	stopCalls atomic.Int32
	entered   chan struct{}
	release   chan struct{}
}

func (a *blockingWorkflowAdapter) CurrentTarget() (string, error) {
	return "/opt/uvp/releases/old", nil
}
func (a *blockingWorkflowAdapter) Stop(context.Context, TaskRecord) error {
	a.stopCalls.Add(1)
	select {
	case a.entered <- struct{}{}:
	default:
	}
	<-a.release
	return nil
}
func (a *blockingWorkflowAdapter) Backup(context.Context, TaskRecord) error   { return nil }
func (a *blockingWorkflowAdapter) Migrate(context.Context, TaskRecord) error  { return nil }
func (a *blockingWorkflowAdapter) Activate(context.Context, TaskRecord) error { return nil }
func (a *blockingWorkflowAdapter) Validate(context.Context, TaskRecord) error { return nil }
func (a *blockingWorkflowAdapter) Commit(context.Context, TaskRecord) error   { return nil }
func (a *blockingWorkflowAdapter) Rollback(context.Context, TaskRecord) error { return nil }

func TestWorkflowSerializesConcurrentApplyCalls(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	adapter := &blockingWorkflowAdapter{entered: make(chan struct{}, 1), release: make(chan struct{})}
	w := &WorkflowExecutor{Store: store, Adapter: adapter}
	firstDone := make(chan error, 1)
	go func() { firstDone <- w.Apply(context.Background(), record) }()
	select {
	case <-adapter.entered:
	case <-time.After(time.Second):
		t.Fatal("first apply did not reach stop")
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- w.Apply(context.Background(), record) }()
	time.Sleep(20 * time.Millisecond)
	if got := adapter.stopCalls.Load(); got != 1 {
		t.Fatalf("concurrent apply entered stop %d times", got)
	}
	close(adapter.release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowRecoveryWritesDurableTerminalEvent(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	events := &schedulerEvents{}
	workflow := &WorkflowExecutor{Store: store, Adapter: &fakeWorkflowAdapter{}, Events: events}
	if err := workflow.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(events.events) != 1 || events.events[0].Event != "succeeded" || events.events[0].TaskID != record.TaskID {
		t.Fatalf("recovery events = %#v", events.events)
	}
	if err := workflow.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(events.events) != 1 {
		t.Fatalf("duplicate recovery outcome: %#v", events.events)
	}
}

func TestWorkflowCommitBoundaryRequiresOperatorRecovery(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	for _, phase := range []TaskPhase{PhaseBackingUp, PhaseMigrating, PhaseActivating, PhaseValidating, PhaseCommitting} {
		if err := store.MarkPhase(record.TaskID, phase); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &fakeWorkflowAdapter{failAt: "commit"}
	workflow := &WorkflowExecutor{Store: store, Adapter: adapter}
	if err := workflow.Apply(context.Background(), record); err == nil {
		t.Fatal("failure missing")
	}
	updated, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseRecoveryRequired {
		t.Fatalf("phase: %s", updated.Phase)
	}
	for _, step := range adapter.steps {
		if step == "rollback" {
			t.Fatalf("committed task was rolled back: %v", adapter.steps)
		}
	}
}

func TestWorkflowManualRollbackRejectsCommittedTask(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	if err := store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error { task.Committed, task.Phase = true, PhaseRecoveryRequired; return nil }); err != nil {
		t.Fatal(err)
	}
	adapter := &fakeWorkflowAdapter{}
	workflow := &WorkflowExecutor{Store: store, Adapter: adapter}
	if err := workflow.Rollback(context.Background(), record); err == nil {
		t.Fatal("committed data restored without compatibility review")
	}
	if len(adapter.steps) != 0 {
		t.Fatalf("steps: %v", adapter.steps)
	}
}

func TestWorkflowManualRollbackRejectsPreparationFailureWithoutExecutionBoundary(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	// A task that failed while it was still being fetched/prepared is marked
	// failed without a resumable service-switch boundary. Rolling it back must
	// not call an adapter that would stop an otherwise healthy service.
	if err := store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
		task.Phase = PhaseFailed
		task.FailurePhase = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	adapter := &fakeWorkflowAdapter{}
	workflow := &WorkflowExecutor{Store: store, Adapter: adapter}
	if err := workflow.Rollback(context.Background(), record); err == nil {
		t.Fatal("accepted rollback without an execution boundary")
	}
	if len(adapter.steps) != 0 {
		t.Fatalf("rollback disturbed service: %v", adapter.steps)
	}
}

func TestWorkflowRechecksIndependentPreflightImmediatelyBeforeStop(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	approved := PreflightSnapshot{
		CurrentVersion: "1.1.0", CurrentBuildID: "build-1", ConfigDigest: strings.Repeat("a", 64),
		DatabaseDialect: "sqlite", MigrationDigest: strings.Repeat("b", 64),
	}
	record.Preflight = &approved
	if err := store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
		task.Preflight = &approved
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	adapter := &fakeWorkflowAdapter{}
	workflow := &WorkflowExecutor{Store: store, Adapter: adapter}
	// With no independent evidence, even a valid stored approval cannot stop
	// the business service. The helper must fail closed after a restart.
	if err := workflow.Apply(context.Background(), record); err == nil {
		t.Fatal("accepted stop without independent preflight")
	}
	if len(adapter.steps) != 0 {
		t.Fatalf("service disturbed: %v", adapter.steps)
	}
}

func TestWorkflowRejectsChangedIndependentPreflightBeforeManualStop(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	approved := PreflightSnapshot{
		CurrentVersion: "1.1.0", CurrentBuildID: "build-1", ConfigDigest: strings.Repeat("a", 64),
		DatabaseDialect: "sqlite", MigrationDigest: strings.Repeat("b", 64),
	}
	record.Preflight = &approved
	if err := store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
		task.Preflight = &approved
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	adapter := &fakeWorkflowAdapter{}
	workflow := &WorkflowExecutor{Store: store, Adapter: adapter, Preflight: func(context.Context) (PreflightSnapshot, error) {
		changed := approved
		changed.ConfigDigest = "changed"
		return changed, nil
	}}
	if err := workflow.Apply(context.Background(), record); err == nil {
		t.Fatal("manual stop accepted stale approval")
	}
	if len(adapter.steps) != 0 {
		t.Fatalf("service disturbed: %v", adapter.steps)
	}
}

type gateInspectingAdapter struct {
	fakeWorkflowAdapter
	gate *GateStore
	t    *testing.T
}

func (a *gateInspectingAdapter) Stop(ctx context.Context, record TaskRecord) error {
	state, err := a.gate.Load()
	if err != nil {
		a.t.Fatal(err)
	}
	if state.Mode != GateBlocked || state.TaskID != record.TaskID {
		a.t.Fatalf("service stopped without maintenance gate: %#v", state)
	}
	return a.fakeWorkflowAdapter.Stop(ctx, record)
}

func TestWorkflowBlocksBusinessStartupBeforeStopping(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	gate, err := OpenGateStore(filepath.Join(t.TempDir(), "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	adapter := &gateInspectingAdapter{gate: gate, t: t}
	workflow := &WorkflowExecutor{Store: store, Adapter: adapter, Gate: gate}
	if err := workflow.Apply(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	state, err := gate.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != GateNormal {
		t.Fatalf("gate: %#v", state)
	}
}

type gateFailureRollbackAdapter struct {
	fakeWorkflowAdapter
	gatePath string
}

func (a *gateFailureRollbackAdapter) Rollback(ctx context.Context, record TaskRecord) error {
	if err := os.Remove(a.gatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Mkdir(a.gatePath, 0700); err != nil {
		return err
	}
	return a.fakeWorkflowAdapter.Rollback(ctx, record)
}

func TestWorkflowDoesNotClaimRollbackWhenGateCannotBeCleared(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	gatePath := filepath.Join(t.TempDir(), "gate.json")
	gate, err := OpenGateStore(gatePath)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &gateFailureRollbackAdapter{fakeWorkflowAdapter: fakeWorkflowAdapter{failAt: "validate"}, gatePath: gatePath}
	events := &schedulerEvents{}
	workflow := &WorkflowExecutor{Store: store, Adapter: adapter, Gate: gate, Events: events}
	if err := workflow.Apply(context.Background(), record); err == nil {
		t.Fatal("gate persistence failure ignored")
	}
	updated, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseRecoveryRequired {
		t.Fatalf("phase: %s", updated.Phase)
	}
	for _, event := range events.events {
		if event.Event == "rolled_back" {
			t.Fatal("reported rollback while maintenance gate cannot be cleared")
		}
	}
}

func TestWorkflowRejectsUntrustedPackageWithoutDisruptingNormalService(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	gate, err := OpenGateStore(filepath.Join(t.TempDir(), "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	adapter := &fakeWorkflowAdapter{}
	workflow := &WorkflowExecutor{Store: store, Adapter: adapter, Gate: gate, Verify: func(context.Context, TaskRecord) (TaskRecord, error) {
		return TaskRecord{}, errors.New("untrusted signature")
	}}
	if err := workflow.Apply(context.Background(), record); err == nil {
		t.Fatal("untrusted package accepted")
	}
	if len(adapter.steps) != 0 {
		t.Fatalf("service disrupted: %v", adapter.steps)
	}
	state, err := gate.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != GateNormal {
		t.Fatalf("gate: %#v", state)
	}
	updated, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseFailed {
		t.Fatalf("phase: %s", updated.Phase)
	}
}

func TestWorkflowValidationFailureRollsBack(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	for _, phase := range []TaskPhase{PhaseBackingUp, PhaseMigrating, PhaseActivating, PhaseValidating} {
		if err := store.MarkPhase(record.TaskID, phase); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &fakeWorkflowAdapter{failAt: "validate"}
	workflow := &WorkflowExecutor{Store: store, Adapter: adapter}
	if err := workflow.Apply(context.Background(), record); err == nil {
		t.Fatal("validation failure ignored")
	}
	updated, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseRolledBack {
		t.Fatalf("phase: %s, want %s", updated.Phase, PhaseRolledBack)
	}
	rolledBack := false
	for _, step := range adapter.steps {
		if step == "rollback" {
			rolledBack = true
		}
	}
	if !rolledBack {
		t.Fatalf("failed activation was not rolled back: %v", adapter.steps)
	}
}
