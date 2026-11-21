package upgrade

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingEvents struct {
	events []UpgradeEvent
}

func (r *recordingEvents) Append(_ context.Context, event UpgradeEvent) error {
	r.events = append(r.events, event)
	return nil
}

func pendingBackupFixture(t *testing.T) (RegisteredLayout, *TaskStore, string) {
	t.Helper()
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
	dir := filepath.Join(layout.State, "backups", strings.Repeat("a", 32))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return layout, store, dir
}

// The stop window must not read the snapshot back: a pending marker is accepted
// even for bytes that no longer decode, because the scan happens later.
func TestValidateBackupCompleteAcceptsPendingSnapshotWithoutScan(t *testing.T) {
	_, _, dir := pendingBackupFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "database.sqlite"), []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeBackupCompleteState(dir, backupIntegrityPending); err != nil {
		t.Fatal(err)
	}
	complete, err := validateBackupComplete(dir, "/var/lib/uvp/data/uvp.db")
	if err != nil || !complete {
		t.Fatalf("pending backup validation = complete:%v err:%v", complete, err)
	}
}

func TestValidateBackupCompleteRejectsFailedSnapshot(t *testing.T) {
	_, _, dir := pendingBackupFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "database.sqlite"), []byte("bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeBackupCompleteState(dir, backupIntegrityFailed); err != nil {
		t.Fatal(err)
	}
	if complete, err := validateBackupComplete(dir, "/var/lib/uvp/data/uvp.db"); err == nil || complete {
		t.Fatalf("failed backup accepted: complete:%v err:%v", complete, err)
	}
}

func TestVerifyPendingBackupSnapshotsReportsCorruption(t *testing.T) {
	layout, store, dir := pendingBackupFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "database.sqlite"), []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeBackupCompleteState(dir, backupIntegrityPending); err != nil {
		t.Fatal(err)
	}
	events := &recordingEvents{}
	if err := VerifyPendingBackupSnapshots(context.Background(), store, layout, events); err != nil {
		t.Fatal(err)
	}
	completed, present, err := readCompletedBackup(dir)
	if err != nil || !present {
		t.Fatalf("marker after verification = present:%v err:%v", present, err)
	}
	if completed.Integrity != backupIntegrityFailed {
		t.Fatalf("integrity state = %q", completed.Integrity)
	}
	if len(events.events) != 1 || events.events[0].Event != "backup_verification_failed" {
		t.Fatalf("reported events = %#v", events.events)
	}
	if _, err := os.Stat(filepath.Join(dir, "database.sqlite")); err != nil {
		t.Fatalf("failed snapshot bytes were removed: %v", err)
	}
}

func TestVerifyPendingBackupSnapshotsConfirmsValidSnapshotOnce(t *testing.T) {
	layout, store, dir := pendingBackupFixture(t)
	source := filepath.Join(t.TempDir(), "live.db")
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT); INSERT INTO items(value) VALUES ('kept')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := SQLiteSnapshot(context.Background(), source, filepath.Join(dir, "database.sqlite")); err != nil {
		t.Fatal(err)
	}
	if err := writeBackupCompleteState(dir, backupIntegrityPending); err != nil {
		t.Fatal(err)
	}
	events := &recordingEvents{}
	for i := 0; i < 2; i++ {
		if err := VerifyPendingBackupSnapshots(context.Background(), store, layout, events); err != nil {
			t.Fatal(err)
		}
	}
	completed, _, err := readCompletedBackup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Integrity != backupIntegrityOK {
		t.Fatalf("integrity state = %q", completed.Integrity)
	}
	if len(events.events) != 0 {
		t.Fatalf("valid snapshot reported events = %#v", events.events)
	}
}
