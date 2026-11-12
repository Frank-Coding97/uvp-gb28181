package upgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetentionPreservesCurrentRollbackAndRecoveryArtifacts(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	layout := RegisteredLayout{Root: root}
	if err := layout.validate(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenTaskStore(layout.State)
	if err != nil {
		t.Fatal(err)
	}
	var records []TaskRecord
	for i := 0; i < 5; i++ {
		record, err := store.Submit(TaskRequest{IdempotencyKey: string(rune('a' + i)), Actor: "admin", TargetDigest: strings.Repeat("a", 64)})
		if err != nil {
			t.Fatal(err)
		}
		record.Phase = PhaseSucceeded
		record.UpdatedAt = time.Date(2026, 10, 16, i, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
		record.StagingPath = filepath.Join(layout.State, "verified", record.TaskID+".tar")
		for _, dir := range []string{
			filepath.Join(layout.Releases, record.TaskID),
			filepath.Join(layout.State, "backups", record.TaskID),
			filepath.Join(layout.State, "prepared-backups", record.TaskID),
		} {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "proof"), []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(filepath.Dir(record.StagingPath), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(record.StagingPath, []byte("artifact"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := store.UpdateMetadata(record.TaskID, func(r *TaskRecord) error { *r = record; return nil }); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	records[4].PreviousTarget = filepath.Join(layout.Releases, records[3].TaskID)
	records[1].Phase = PhaseRecoveryRequired
	for _, record := range []TaskRecord{records[4], records[1]} {
		if err := store.UpdateMetadata(record.TaskID, func(r *TaskRecord) error { *r = record; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(layout.Releases, records[4].TaskID), layout.Current); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(layout.Releases, "user-release")
	if err := os.MkdirAll(unknown, 0700); err != nil {
		t.Fatal(err)
	}
	if err := PruneCompletedArtifacts(context.Background(), store, layout, 1); err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{1, 3, 4} {
		if _, err := os.Stat(filepath.Join(layout.Releases, records[i].TaskID)); err != nil {
			t.Fatalf("removed protected release %d: %v", i, err)
		}
	}
	for _, i := range []int{0, 2} {
		if _, err := os.Stat(filepath.Join(layout.Releases, records[i].TaskID)); !os.IsNotExist(err) {
			t.Fatalf("retained expired release %d: %v", i, err)
		}
		if _, err := os.Stat(filepath.Join(layout.State, "prepared-backups", records[i].TaskID)); !os.IsNotExist(err) {
			t.Fatalf("retained expired prepared backup %d: %v", i, err)
		}
	}
	for _, i := range []int{1, 3, 4} {
		if _, err := os.Stat(filepath.Join(layout.State, "prepared-backups", records[i].TaskID)); err != nil {
			t.Fatalf("removed protected prepared backup %d: %v", i, err)
		}
	}
	if _, err := os.Stat(unknown); err != nil {
		t.Fatalf("removed unknown release: %v", err)
	}
	if err := PruneCompletedArtifacts(context.Background(), store, layout, 1); err != nil {
		t.Fatalf("cleanup not idempotent: %v", err)
	}
}

func TestCleanupFailureCannotRollbackCommittedUpgrade(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	adapter := &fakeWorkflowAdapter{}
	w := WorkflowExecutor{Store: store, Adapter: adapter, Cleanup: func(context.Context, TaskRecord) error { return errors.New("cleanup filesystem unavailable") }}
	if err := w.Apply(context.Background(), record); err == nil {
		t.Fatal("cleanup diagnostic was lost")
	}
	r, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Phase != PhaseSucceeded || !r.Committed {
		t.Fatalf("cleanup changed committed outcome: %#v", r)
	}
	for _, step := range adapter.steps {
		if step == "rollback" {
			t.Fatal("cleanup rolled back committed data")
		}
	}
}

func TestRetentionRejectsSymlinkCleanupRoot(t *testing.T) {
	outside := t.TempDir()
	proof := filepath.Join(outside, "task")
	if err := os.Mkdir(proof, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "backups")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := removeOwnedArtifact(link, "task"); err == nil {
		t.Fatal("followed cleanup root symlink")
	}
	if _, err := os.Stat(proof); err != nil {
		t.Fatalf("removed outside data: %v", err)
	}
}

func TestRetentionZeroStillKeepsActiveRollbackSource(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	layout := RegisteredLayout{Root: root}
	if err := layout.validate(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenTaskStore(layout.State)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Submit(TaskRequest{IdempotencyKey: "current", Actor: "admin", TargetDigest: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	previous := "task-" + strings.Repeat("b", 32)
	record.Phase = PhaseSucceeded
	record.PreviousTarget = filepath.Join(layout.Releases, previous)
	if err := store.UpdateMetadata(record.TaskID, func(r *TaskRecord) error { *r = record; return nil }); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{record.TaskID, previous} {
		if err := os.MkdirAll(filepath.Join(layout.Releases, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(layout.Releases, record.TaskID), layout.Current); err != nil {
		t.Fatal(err)
	}
	if err := PruneCompletedArtifacts(context.Background(), store, layout, 0); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{record.TaskID, previous} {
		if _, err := os.Stat(filepath.Join(layout.Releases, name)); err != nil {
			t.Fatalf("removed active source %s: %v", name, err)
		}
	}
}

func TestRetentionProtectsNestedCurrentAndRecoveryTarget(t *testing.T) {
	releases := filepath.Join(t.TempDir(), "releases")
	target := filepath.Join(releases, "task-current", "backend")
	if got := protectedReleaseRoot(releases, target); got != filepath.Join(releases, "task-current") {
		t.Fatalf("nested target protection %s", got)
	}
}

func TestRetentionRejectsAncestorSymlink(t *testing.T) {
	actual := t.TempDir()
	link := filepath.Join(t.TempDir(), "state")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(actual, "backups"), 0700); err != nil {
		t.Fatal(err)
	}
	if dir, err := openCleanupRoot(filepath.Join(link, "backups")); err == nil {
		dir.Close()
		t.Fatal("followed ancestor symlink")
	}
}
