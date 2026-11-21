package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRollbackRecoveryKeepsPreMigrationDatabaseState(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			var record TaskRecord
			data := `{"task_id":"` + strings.Repeat("a", 32) + `","phase":"recovery_required","failure_phase":"backing_up","preflight":{"current_version":"1.1.0","current_build_id":"source","database_dialect":"` + dialect + `","migration_digest":"` + strings.Repeat("b", 64) + `"}}`
			if err := json.Unmarshal([]byte(data), &record); err != nil {
				t.Fatal(err)
			}
			if err := restoreRollbackDatabase(context.Background(), RegisteredLayout{Root: t.TempDir()}, record); err != nil {
				t.Fatalf("migration never started; recovery must not require or restore a snapshot: %v", err)
			}
			identity, err := expectedRollbackIdentity(record)
			if err != nil {
				t.Fatal(err)
			}
			if identity.MigrationDigest != record.Preflight.MigrationDigest {
				t.Fatal("pre-migration failure changed the expected database state")
			}
		})
	}
}

func TestWorkflowPersistsOriginalFailurePhaseAcrossRecoveryAttempts(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	adapter := &fakeWorkflowAdapter{failAt: "backup", rollbackErr: errors.New("old process unavailable")}
	workflow := &WorkflowExecutor{Store: store, Adapter: adapter}
	if err := workflow.Apply(context.Background(), record); err == nil {
		t.Fatal("expected backup failure")
	}
	for attempt := 0; attempt < 2; attempt++ {
		recovered, err := store.Get(record.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(recovered)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["failure_phase"] != string(PhaseBackingUp) {
			t.Fatalf("lost original failure stage on recovery attempt %d: %s", attempt, encoded)
		}
		if err := workflow.Rollback(context.Background(), recovered); err == nil {
			t.Fatal("expected unavailable old process")
		}
	}
}
