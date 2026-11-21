package upgrade

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskStateAllowsOnlyOrderedTransitions(t *testing.T) {
	state := NewTaskState("task-1")
	for _, next := range []TaskPhase{PhaseVerifying, PhasePreparing, PhaseAwaitingConfirmation, PhaseStopping, PhaseBackingUp, PhaseMigrating, PhaseActivating, PhaseValidating, PhaseCommitting, PhaseSucceeded} {
		if err := state.Advance(next); err != nil {
			t.Fatalf("advance to %s: %v", next, err)
		}
	}
	if err := state.Advance(PhaseStopping); err == nil {
		t.Fatal("terminal task was allowed to transition")
	}
	if state.Phase != PhaseSucceeded {
		t.Fatalf("unexpected final phase %s", state.Phase)
	}
}

func TestTaskStatePersistenceIsAtomicAndIgnoresTrailingCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.json")
	state := NewTaskState("task-2")
	state.Phase = PhasePreparing
	if err := SaveTaskState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTaskState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TaskID != state.TaskID || loaded.Phase != state.Phase {
		t.Fatalf("loaded state mismatch: %#v", loaded)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary state file was left behind: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"task_id":"task-2","phase":"preparing"} trailing`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTaskState(path); err == nil {
		t.Fatal("corrupt trailing data was accepted")
	}
}

func TestTaskStateRecoveryKeepsCommittedTasksOutOfRollback(t *testing.T) {
	state := NewTaskState("task-3")
	state.Phase = PhaseCommitting
	if err := state.Advance(PhaseSucceeded); err != nil {
		t.Fatal(err)
	}
	if state.IsCommitted() == false {
		t.Fatal("succeeded task was not marked committed")
	}
}
