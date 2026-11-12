package upgrade

import (
	"errors"
	"sync"
	"testing"
)

func TestTaskStoreIdempotencyAndActiveTaskConflict(t *testing.T) {
	store, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, err := store.Submit(TaskRequest{IdempotencyKey: "request-1", Actor: "admin-1", TargetDigest: "digest-a"})
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := store.Submit(TaskRequest{IdempotencyKey: "request-1", Actor: "admin-1", TargetDigest: "digest-a"})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.TaskID != first.TaskID {
		t.Fatalf("idempotent request created a second task: %q != %q", repeated.TaskID, first.TaskID)
	}
	if _, err := store.Submit(TaskRequest{IdempotencyKey: "request-1", Actor: "admin-2", TargetDigest: "digest-a"}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
	if _, err := store.Submit(TaskRequest{IdempotencyKey: "request-2", Actor: "admin-1", TargetDigest: "digest-b"}); !errors.Is(err, ErrTaskBusy) {
		t.Fatalf("expected active task conflict, got %v", err)
	}
	for _, phase := range []TaskPhase{PhaseVerifying, PhasePreparing, PhaseAwaitingConfirmation, PhaseStopping, PhaseBackingUp, PhaseMigrating, PhaseActivating, PhaseValidating, PhaseCommitting, PhaseSucceeded} {
		if err := store.MarkPhase(first.TaskID, phase); err != nil {
			t.Fatal(err)
		}
	}
	second, err := store.Submit(TaskRequest{IdempotencyKey: "request-2", Actor: "admin-1", TargetDigest: "digest-b"})
	if err != nil {
		t.Fatal(err)
	}
	if second.TaskID == first.TaskID {
		t.Fatal("new request reused a completed task")
	}
}

func TestTaskStoreSerializesIndependentProcesses(t *testing.T) {
	dir := t.TempDir()
	left, err := OpenTaskStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer left.Close()
	right, err := OpenTaskStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer right.Close()
	var wg sync.WaitGroup
	results := make(chan TaskRecord, 2)
	errs := make(chan error, 2)
	for _, store := range []*TaskStore{left, right} {
		wg.Add(1)
		go func(store *TaskStore) {
			defer wg.Done()
			record, err := store.Submit(TaskRequest{IdempotencyKey: "same-key", Actor: "admin", TargetDigest: "digest"})
			if err != nil {
				errs <- err
				return
			}
			results <- record
		}(store)
	}
	wg.Wait()
	close(results)
	close(errs)
	var records []TaskRecord
	for record := range results {
		records = append(records, record)
	}
	for err := range errs {
		t.Fatalf("concurrent submit failed: %v", err)
	}
	if len(records) != 2 || records[0].TaskID != records[1].TaskID {
		t.Fatalf("concurrent idempotent submits diverged: %#v", records)
	}
}

func TestTaskStoreBlocksNewTaskUntilRecoveryCompletes(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	if err := store.MarkPhase(record.TaskID, PhaseRecoveryRequired); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Submit(TaskRequest{IdempotencyKey: "after-failed-recovery", Actor: "admin", TargetDigest: "next"}); !errors.Is(err, ErrTaskBusy) {
		t.Fatalf("new task while recovery required: %v", err)
	}
	if err := store.MarkPhase(record.TaskID, PhaseRolledBack); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Submit(TaskRequest{IdempotencyKey: "after-failed-recovery", Actor: "admin", TargetDigest: "next"}); err != nil {
		t.Fatal(err)
	}
}
