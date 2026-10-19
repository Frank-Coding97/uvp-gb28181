package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteBackupRequirementsFailBeforeServiceStop(t *testing.T) {
	for _, deployment := range []string{"standalone", "systemd", "compose"} {
		for _, source := range []string{"unregistered", "missing", "directory"} {
			t.Run(deployment+"/"+source, func(t *testing.T) {
				root := t.TempDir()
				store, record := prepareWorkflowTask(t)
				record.Preflight = &PreflightSnapshot{DatabaseDialect: "sqlite"}
				if err := store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error { task.Preflight = record.Preflight; return nil }); err != nil {
					t.Fatal(err)
				}
				layout := RegisteredLayout{Root: root, State: filepath.Join(root, "state"), Control: filepath.Join(root, "ctl.sh"), ProjectDir: root, Project: "uvp"}
				switch source {
				case "missing":
					layout.SQLiteDatabase = filepath.Join(root, "missing.db")
				case "directory":
					layout.SQLiteDatabase = root
				}
				runner := &fakeCommandRunner{}
				var adapter DeploymentAdapter = &StandaloneAdapter{Layout: layout, Runner: runner}
				if deployment == "systemd" {
					adapter = &SystemdAdapter{StandaloneAdapter: StandaloneAdapter{Layout: layout, Runner: runner}, Unit: "uvp.service"}
				} else if deployment == "compose" {
					file := filepath.Join(root, "compose.yml")
					if err := os.WriteFile(file, []byte("services:\n  backend:\n    image: uvp:old\n"), 0600); err != nil {
						t.Fatal(err)
					}
					adapter = &ComposeAdapter{Layout: layout, Runner: runner, File: file}
				}
				if err := (&WorkflowExecutor{Store: store, Adapter: adapter}).Apply(context.Background(), record); err == nil || !strings.Contains(err.Error(), "sqlite") {
					t.Fatalf("expected sqlite backup preflight failure, got %v", err)
				}
				if len(runner.calls) != 0 {
					t.Fatalf("service command ran before database validation: %v", runner.calls)
				}
				updated, err := store.Get(record.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				if updated.Phase != PhaseFailed || updated.FailurePhase != PhaseStopping {
					t.Fatalf("unexpected failure boundary: %#v", updated)
				}
			})
		}
	}
}

func TestStandaloneSQLitePreparationDoesNotCreateSnapshotWhileServiceRuns(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "data", "uvp.db")
	createSQLiteValue(t, database, "original")
	adapter := &StandaloneAdapter{Layout: RegisteredLayout{Root: root, State: filepath.Join(root, "state"), SQLiteDatabase: database}}
	record := TaskRecord{TaskID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Preflight: &PreflightSnapshot{DatabaseDialect: "sqlite"}}
	if err := adapter.PreparePackage(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "state", "backups")); !os.IsNotExist(err) {
		t.Fatalf("preparation created backup state: %v", err)
	}
	if got := readSQLiteValue(t, database); got != "original" {
		t.Fatalf("source was modified: %s", got)
	}
}
