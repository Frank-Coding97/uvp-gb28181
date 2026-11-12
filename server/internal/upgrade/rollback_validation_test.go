package upgrade

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRollbackRequiresOriginalRuntimeIdentity(t *testing.T) {
	record := TaskRecord{TaskID: strings.Repeat("a", 32), Version: "1.2.0", BuildID: "target", Preflight: &PreflightSnapshot{CurrentVersion: "1.1.0", CurrentBuildID: "source", DatabaseDialect: "sqlite", MigrationDigest: strings.Repeat("b", 64)}}
	expected, err := expectedRollbackIdentity(record)
	if err != nil {
		t.Fatal(err)
	}
	if expected.Version != "1.1.0" || expected.BuildID != "source" || expected.TaskID != record.TaskID || expected.MigrationDigest != record.Preflight.MigrationDigest {
		t.Fatalf("rollback identity: %#v", expected)
	}
	actual := expected
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(actual) }))
	defer server.Close()
	layout := RegisteredLayout{Root: t.TempDir(), ReadinessURL: server.URL + RuntimeIdentityPath}
	probe := RuntimeIdentityProbe{Attempts: 1, Timeout: time.Second}
	if err := validateRollbackRuntime(context.Background(), layout, record, probe); err != nil {
		t.Fatal(err)
	}
	actual.BuildID = "target"
	if err := validateRollbackRuntime(context.Background(), layout, record, probe); err == nil {
		t.Fatal("accepted target binary as rollback")
	}
}

func TestRollbackRejectsIncompleteSourceEvidence(t *testing.T) {
	for _, record := range []TaskRecord{{}, {TaskID: strings.Repeat("a", 32), Preflight: &PreflightSnapshot{CurrentVersion: "1.1.0"}}} {
		if _, err := expectedRollbackIdentity(record); err == nil {
			t.Fatalf("accepted missing source identity: %#v", record)
		}
	}
}

func TestExternalRollbackValidatesMigratedDatabaseWithoutRestoring(t *testing.T) {
	record := TaskRecord{TaskID: strings.Repeat("a", 32), Phase: PhaseValidating, Preflight: &PreflightSnapshot{CurrentVersion: "1.1.0", CurrentBuildID: "source", DatabaseDialect: "mysql", MigrationDigest: strings.Repeat("b", 64)}}
	if _, err := expectedRollbackIdentity(record); err == nil {
		t.Fatal("accepted external rollback without compatibility")
	}
	record.Compatibility = &CompatibilityDeclaration{AutomaticAllowed: true, Database: DatabaseCompatibility{TargetMigrationDigest: strings.Repeat("c", 64)}, Rollback: &RollbackCompatibility{AutomaticAllowed: true, DatabaseCompatible: true, DependenciesCompatible: true}}
	expected, err := expectedRollbackIdentity(record)
	if err != nil {
		t.Fatal(err)
	}
	if expected.MigrationDigest != strings.Repeat("c", 64) {
		t.Fatal("expected a restored external schema")
	}
}

func TestSQLiteRollbackRestoresVerifiedSnapshot(t *testing.T) {
	root := t.TempDir()
	layout := RegisteredLayout{Root: root}
	if err := layout.validate(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	config := []byte("gormv2:\n  usedbtype: sqlite\n  sqlite:\n    write:\n      database: data/uvp.db\n")
	if err := os.WriteFile(filepath.Join(root, "config", "config.yml"), config, 0600); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(root, "data", "uvp.db")
	layout.SQLiteDatabase = database
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE proof(value TEXT); INSERT INTO proof VALUES ('original')"); err != nil {
		t.Fatal(err)
	}
	record := TaskRecord{TaskID: strings.Repeat("a", 32), Phase: PhaseMigrating, Preflight: &PreflightSnapshot{DatabaseDialect: "sqlite"}}
	backup := filepath.Join(layout.State, "backups", record.TaskID)
	if err := os.MkdirAll(backup, 0700); err != nil {
		t.Fatal(err)
	}
	if err := SQLiteBackup(context.Background(), database, filepath.Join(backup, "database.sqlite")); err != nil {
		t.Fatal(err)
	}
	if err := writeBackupComplete(backup); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE proof SET value='candidate'"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := restoreRollbackDatabase(context.Background(), layout, record); err != nil {
		t.Fatal(err)
	}
	restored, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var value string
	if err := restored.QueryRow("SELECT value FROM proof").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "original" {
		t.Fatalf("restored value %q", value)
	}
}
