package upgrade

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildMigrationPlanBlocksUnknownAndDestructiveChanges(t *testing.T) {
	plan := BuildMigrationPlan("sqlite", []string{"a", "legacy"}, []string{"a", "b"}, []string{"b"})
	if plan.CanApply || plan.BlockReason == "" || len(plan.Unknown) != 1 || len(plan.Destructive) != 1 {
		t.Fatalf("unexpected blocked plan: %#v", plan)
	}
	plan = BuildMigrationPlan("sqlite", []string{"a"}, []string{"a", "b"}, nil)
	if !plan.CanApply || len(plan.Pending) != 1 {
		t.Fatalf("unexpected applicable plan: %#v", plan)
	}
}

func TestSQLiteBackupUsesConsistentSnapshotAndIntegrityCheck(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT); INSERT INTO items(value) VALUES ('wal-data')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "backup.db")
	if err := SQLiteBackup(context.Background(), source, destination); err != nil {
		t.Fatal(err)
	}
	backup, err := sql.Open("sqlite", destination)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var value string
	if err := backup.QueryRow("SELECT value FROM items WHERE id = 1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "wal-data" {
		t.Fatalf("backup value = %q", value)
	}
}

func TestSQLiteRestoreReplacesTargetAndRemovesStaleWALFiles(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	target := filepath.Join(dir, "target.db")

	for _, fixture := range []struct {
		path  string
		value string
	}{
		{path: source, value: "source"},
		{path: target, value: "target"},
	} {
		db, err := sql.Open("sqlite", fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT); INSERT INTO items(value) VALUES (?)", fixture.value); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}

	snapshot := filepath.Join(dir, "snapshot.db")
	if err := SQLiteBackup(context.Background(), source, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+"-wal", []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+"-shm", []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+"-journal", []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := SQLiteRestore(context.Background(), snapshot, target); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(target + suffix); !os.IsNotExist(err) {
			t.Fatalf("stale SQLite sidecar %s remains: %v", suffix, err)
		}
	}
	restored, err := sql.Open("sqlite", target)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var value string
	if err := restored.QueryRow("SELECT value FROM items WHERE id = 1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "source" {
		t.Fatalf("restored value = %q, want source", value)
	}
}
