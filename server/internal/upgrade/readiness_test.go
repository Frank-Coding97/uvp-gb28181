package upgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func unsetGatePathEnv(t *testing.T) {
	t.Helper()
	previous, wasSet := os.LookupEnv(UpdaterGatePathEnv)
	if err := os.Unsetenv(UpdaterGatePathEnv); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(UpdaterGatePathEnv, previous)
			return
		}
		_ = os.Unsetenv(UpdaterGatePathEnv)
	})
}

func TestCandidateValidatorRequiresAllChecksForThreeAttempts(t *testing.T) {
	count := 0
	validator := CandidateValidator{Attempts: 3, Checks: []CandidateCheck{
		func(context.Context, TaskRecord) error { count++; return nil },
		func(context.Context, TaskRecord) error { return nil },
	}}
	if err := validator.Validate(context.Background(), TaskRecord{}); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("check count = %d", count)
	}
	failing := CandidateValidator{Attempts: 3, Checks: []CandidateCheck{func(context.Context, TaskRecord) error { return errors.New("not ready") }}}
	if err := failing.Validate(context.Background(), TaskRecord{}); err == nil {
		t.Fatal("failing candidate accepted")
	}
}

func TestGateStorePersistsRecoveryState(t *testing.T) {
	store, err := OpenGateStore(filepath.Join(t.TempDir(), "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(GateState{Mode: GateRecovery, TaskID: "task", Reason: "rollback failed"}); err != nil {
		t.Fatal(err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != GateRecovery || state.TaskID != "task" || state.UpdatedAt == "" {
		t.Fatalf("state = %#v", state)
	}
}

func TestValidateStartupGateFailsClosedForRecoveryAndUnknownModes(t *testing.T) {
	if err := ValidateStartupGate(GateState{Mode: GateNormal}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateStartupGate(GateState{Mode: GateRecovery, Reason: "rollback failed"}); err == nil {
		t.Fatal("recovery gate was accepted")
	}
	if err := ValidateStartupGate(GateState{Mode: GateMode("unexpected")}); err == nil {
		t.Fatal("unknown gate mode was accepted")
	}
}

func TestGateStateFromEnvReadsCandidateAndDefaultsToNormal(t *testing.T) {
	unsetGatePathEnv(t)
	t.Setenv("UVP_UPDATER_STATE", "")
	state, err := GateStateFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != GateNormal {
		t.Fatalf("default gate = %#v", state)
	}

	root := t.TempDir()
	t.Setenv("UVP_UPDATER_STATE", root)
	store, err := OpenGateStore(filepath.Join(root, "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(GateState{Mode: GateCandidate, TaskID: "task-1"}); err != nil {
		t.Fatal(err)
	}
	state, err = GateStateFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != GateCandidate || state.TaskID != "task-1" {
		t.Fatalf("candidate gate = %#v", state)
	}
}

func TestGateStateFromEnvUsesExplicitSharedGateAndFailsClosedWhenMissing(t *testing.T) {
	legacyRoot := t.TempDir()
	legacy, err := OpenGateStore(filepath.Join(legacyRoot, "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Save(GateState{Mode: GateNormal}); err != nil {
		t.Fatal(err)
	}

	sharedPath := filepath.Join(t.TempDir(), "updater-gate", "gate.json")
	if err := os.MkdirAll(filepath.Dir(sharedPath), 0750); err != nil {
		t.Fatal(err)
	}
	shared, err := OpenSharedGateStore(sharedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := shared.Save(GateState{Mode: GateCandidate, TaskID: "shared-task"}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("UVP_UPDATER_STATE", legacyRoot)
	t.Setenv(UpdaterGatePathEnv, sharedPath)
	state, err := GateStateFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != GateCandidate || state.TaskID != "shared-task" {
		t.Fatalf("shared gate = %#v", state)
	}

	// Windows 没有 POSIX 权限位：闸门的信任边界靠安装目录 ACL 表达，chmod 也
	// 改不动它，所以这两段只在 Unix 上成立。
	if runtime.GOOS != "windows" {
		info, err := os.Stat(sharedPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0640 {
			t.Fatalf("shared gate mode = %o, want 0640", info.Mode().Perm())
		}
		if err := os.Chmod(sharedPath, 0660); err != nil {
			t.Fatal(err)
		}
		if _, err := GateStateFromEnv(); err == nil || !strings.Contains(err.Error(), "mode must be 0640") {
			t.Fatalf("writable shared gate error = %v", err)
		}
	}
	if err := shared.Save(GateState{Mode: GateCandidate, TaskID: "shared-task"}); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(sharedPath); err != nil {
		t.Fatal(err)
	}
	if _, err := GateStateFromEnv(); err == nil || !strings.Contains(err.Error(), "configured updater gate is missing") {
		t.Fatalf("missing explicit gate error = %v", err)
	}
}

func TestGateStateFromEnvRejectsEmptyExplicitPath(t *testing.T) {
	t.Setenv(UpdaterGatePathEnv, "")
	if _, err := GateStateFromEnv(); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("empty explicit gate error = %v", err)
	}
}

func TestSharedGateStoreRequiresDaemonProvisionedParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unprovisioned", "gate.json")
	if _, err := OpenSharedGateStore(path); err == nil || !strings.Contains(err.Error(), "not provisioned") {
		t.Fatalf("unprovisioned shared gate error = %v", err)
	}
}

func TestWaitForNormalGateDelaysBusinessRuntimeUntilAdmission(t *testing.T) {
	unsetGatePathEnv(t)
	stateRoot := t.TempDir()
	t.Setenv("UVP_UPDATER_STATE", stateRoot)
	gate, err := OpenGateStore(filepath.Join(stateRoot, "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Save(GateState{Mode: GateCandidate, TaskID: "candidate-task"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	started := make(chan struct{}, 1)
	finished := make(chan bool, 1)
	go func() {
		finished <- WaitForNormalGate(ctx, 5*time.Millisecond, func() { started <- struct{}{} })
	}()

	select {
	case <-started:
		t.Fatal("candidate gate started the business runtime")
	case <-time.After(50 * time.Millisecond):
	}

	if err := gate.Save(GateState{Mode: GateNormal}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("normal gate did not start the business runtime")
	}
	select {
	case ok := <-finished:
		if !ok {
			t.Fatal("normal gate wait was canceled instead of admitting the runtime")
		}
	case <-time.After(time.Second):
		t.Fatal("gate waiter did not finish")
	}
}

func TestBusinessRuntimeStarterRunsEffectsOnlyOnce(t *testing.T) {
	ctx := context.WithValue(context.Background(), "runtime", "candidate-admitted")
	var mu sync.Mutex
	events := make([]string, 0, 5)
	mark := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, name)
	}
	maintenance := make(chan struct{}, 1)
	start := NewBusinessRuntimeStarter(
		ctx,
		func() error { mark("business-jobs"); return nil },
		func() { mark("job-scheduler") },
		func(got context.Context) {
			if got != ctx {
				t.Error("maintenance received a different runtime context")
			}
			mark("maintenance")
			maintenance <- struct{}{}
		},
		func() { mark("gb28181") },
	)

	start()
	select {
	case <-maintenance:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not start")
	}
	start()
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 4 {
		t.Fatalf("events = %#v", events)
	}
	if events[0] != "business-jobs" || events[1] != "job-scheduler" {
		t.Fatalf("startup order = %#v", events)
	}
}
