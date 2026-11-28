package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestControlSubmitImportRunsVerificationInBackground(t *testing.T) {
	fixture := newControlFixture(t)
	imports, err := OpenImportStore(filepath.Join(t.TempDir(), "imports"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.ImportStore = imports
	if err := fixture.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.service.Close)

	_, archive, digest := controlSignedArchive(t, fixture.private, fixture.target, "1.2.0")
	token := strings.Repeat("e", 32)
	if err := os.WriteFile(filepath.Join(fixture.inbox, token), archive, 0600); err != nil {
		t.Fatal(err)
	}

	record, err := fixture.service.SubmitImport(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if record.ImportID == "" || record.Phase != ImportPhaseQueued {
		t.Fatalf("submit response = %#v", record)
	}
	retry, err := fixture.service.SubmitImport(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ImportID != record.ImportID {
		t.Fatalf("retry created another import: first=%s retry=%s", record.ImportID, retry.ImportID)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := fixture.service.GetImport(context.Background(), record.ImportID)
		if getErr == nil && current.Phase == ImportPhaseSucceeded {
			if current.Staged == nil || current.Staged.SHA256 != digest {
				t.Fatalf("successful import = %#v", current)
			}
			if _, statErr := os.Stat(filepath.Join(fixture.inbox, token)); !os.IsNotExist(statErr) {
				t.Fatalf("inbox token still exists: %v", statErr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, err := fixture.service.GetImport(context.Background(), record.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("import did not finish: %#v", current)
}

func TestControlImportFailureIsDurableAndKeepsReason(t *testing.T) {
	fixture := newControlFixture(t)
	imports, err := OpenImportStore(filepath.Join(t.TempDir(), "imports"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.ImportStore = imports
	if err := fixture.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.service.Close)
	token := strings.Repeat("c", 32)
	if err := os.WriteFile(filepath.Join(fixture.inbox, token), []byte("not a signed archive"), 0600); err != nil {
		t.Fatal(err)
	}
	record, err := fixture.service.SubmitImport(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := imports.Get(record.ImportID)
		if getErr == nil && current.Phase == ImportPhaseFailed {
			if current.Error == "" {
				t.Fatalf("failed import has no reason: %#v", current)
			}
			reopened, err := OpenImportStore(imports.root)
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := reopened.Get(record.ImportID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Phase != ImportPhaseFailed || persisted.Error != current.Error {
				t.Fatalf("failed result was not durable: current=%#v persisted=%#v", current, persisted)
			}
			if _, err := os.Stat(filepath.Join(fixture.inbox, token)); !os.IsNotExist(err) {
				t.Fatalf("failed upload was not cleaned after outcome persistence: %v", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, err := imports.Get(record.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("invalid package did not reach failed state: %#v", current)
}

func TestControlStartRequeuesDurableImport(t *testing.T) {
	fixture := newControlFixture(t)
	importRoot := filepath.Join(t.TempDir(), "imports")
	imports, err := OpenImportStore(importRoot)
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.ImportStore = imports
	_, archive, digest := controlSignedArchive(t, fixture.private, fixture.target, "1.2.0")
	token := strings.Repeat("f", 32)
	if err := os.WriteFile(filepath.Join(fixture.inbox, token), archive, 0600); err != nil {
		t.Fatal(err)
	}
	record, err := imports.Submit(token)
	if err != nil {
		t.Fatal(err)
	}

	restarted := newControlServiceForFixture(fixture)
	restarted.ImportStore, err = OpenImportStore(importRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := restarted.GetImport(context.Background(), record.ImportID)
		if getErr == nil && current.Phase == ImportPhaseSucceeded {
			if current.Staged == nil || current.Staged.SHA256 != digest {
				t.Fatalf("successful recovered import = %#v", current)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, err := restarted.GetImport(context.Background(), record.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("durable import did not finish: %#v", current)
}
