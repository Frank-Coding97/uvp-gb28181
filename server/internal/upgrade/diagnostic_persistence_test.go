package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	diagnosticURLSecret = "upgrade-http-query-secret-7f6d"
	diagnosticPassword  = "upgrade-password-secret-8a2e"
	diagnosticDSNSecret = "upgrade-dsn-secret-9b3f"
)

func unsafeUpgradeDiagnostic() string {
	return "download failed: https://operator:" + diagnosticPassword + "@updates.example/release?token=" + diagnosticURLSecret + "&channel=stable; mysql://uvp:" + diagnosticDSNSecret + "@tcp(db.example:3306)/uvp?parseTime=true; password=" + diagnosticPassword
}

func assertDiagnosticSafe(t *testing.T, value string) {
	t.Helper()
	for _, secret := range []string{diagnosticURLSecret, diagnosticPassword, diagnosticDSNSecret} {
		if strings.Contains(value, secret) {
			t.Fatalf("diagnostic leaked %q in %q", secret, value)
		}
	}
	for _, want := range []string{"updates.example", "db.example", "[REDACTED]"} {
		if !strings.Contains(value, want) {
			t.Fatalf("diagnostic lost useful detail %q in %q", want, value)
		}
	}
}

func TestUpgradeEventLogSanitizesPersistedAndReloadedReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log, err := OpenFileEventLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Append(context.Background(), UpgradeEvent{Event: "blocked", Reason: unsafeUpgradeDiagnostic()}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := OpenFileEventLog(path)
	if err != nil {
		t.Fatal(err)
	}
	events, err := reloaded.Recent(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	assertDiagnosticSafe(t, events[0].Reason)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertDiagnosticSafe(t, string(data))
}

func TestUpgradeTaskStoreSanitizesPersistedAndReloadedError(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(root)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Submit(TaskRequest{IdempotencyKey: "diagnostic-key", Actor: "admin", TargetDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateMetadata(record.TaskID, func(record *TaskRecord) error {
		record.Error = unsafeUpgradeDiagnostic()
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := OpenTaskStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	assertDiagnosticSafe(t, got.Error)

	data, err := os.ReadFile(filepath.Join(root, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertDiagnosticSafe(t, string(data))
}

func TestUpgradeGateStoreSanitizesPersistedAndReloadedReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gate.json")
	gate, err := OpenGateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Save(GateState{Mode: GateRecovery, TaskID: "task", Reason: unsafeUpgradeDiagnostic()}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := OpenGateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	state, err := reloaded.Load()
	if err != nil {
		t.Fatal(err)
	}
	assertDiagnosticSafe(t, state.Reason)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertDiagnosticSafe(t, string(data))
}
