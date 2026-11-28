package upgrade

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProgressStaticHTTPTracksUpgradeWithoutBusinessBackend(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	root := filepath.Join(t.TempDir(), "updater-progress")
	publisher, err := store.EnableProgress(root)
	if err != nil {
		t.Fatal(err)
	}
	watch, err := publisher.Watch(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	// Only static HTTP exists; there is no business server or database.
	server := httptest.NewServer(http.StripPrefix(ProgressURLPrefix, http.FileServer(http.Dir(root))))
	defer server.Close()
	for _, phase := range []TaskPhase{PhaseBackingUp, PhaseMigrating, PhaseActivating, PhaseValidating, PhaseCommitting, PhaseSucceeded} {
		if err := store.MarkPhase(record.TaskID, phase); err != nil {
			t.Fatal(err)
		}
		response, err := http.Get(server.URL + watch.URL)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot ProgressSnapshot
		err = json.NewDecoder(response.Body).Decode(&snapshot)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 || snapshot.Task.TaskID != record.TaskID || snapshot.Task.Phase != phase {
			t.Fatalf("static progress = %#v (HTTP %d), want %s", snapshot, response.StatusCode, phase)
		}
		if _, err := time.Parse(time.RFC3339Nano, snapshot.ObservedAt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProgressRedactsSecretsAndSurvivesHelperRestart(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	if err := store.UpdateMetadata(record.TaskID, func(r *TaskRecord) error {
		r.BackupPath, r.StagingPath, r.Actor = "/private/backups/secret", "/private/staging/secret", "secret-admin"
		r.Error = "database password=supersecret /private/path"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "updater-progress")
	publisher, err := store.EnableProgress(root)
	if err != nil {
		t.Fatal(err)
	}
	watch, err := publisher.Watch(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, strings.TrimPrefix(watch.URL, ProgressURLPrefix)))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private", "secret-admin", "supersecret", "backup_path", "staging_path", "actor", "idempotency_key"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("progress leaks %q: %s", secret, data)
		}
	}
	restarted, err := OpenTaskStore(store.root)
	if err != nil {
		t.Fatal(err)
	}
	next, err := restarted.EnableProgress(root)
	if err != nil {
		t.Fatal(err)
	}
	again, err := next.Watch(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if again != watch {
		t.Fatalf("watch changed across restart: %#v / %#v", watch, again)
	}
	if _, err := publisher.Watch("../tasks.json"); err == nil {
		t.Fatal("invalid task ID accepted")
	}
	info, err := os.Stat(filepath.Join(store.root, "progress.key"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("private key mode = %o", info.Mode().Perm())
	}
}

func TestProgressHeartbeatAndPublicationFailureDoNotCorruptTask(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	root := filepath.Join(t.TempDir(), "updater-progress")
	publisher, err := store.EnableProgress(root)
	if err != nil {
		t.Fatal(err)
	}
	watch, err := publisher.Watch(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, strings.TrimPrefix(watch.URL, ProgressURLPrefix))
	before, _ := os.ReadFile(path)
	if err := publisher.Refresh(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) == string(after) {
		t.Fatal("heartbeat did not refresh observed_at")
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPhase(record.TaskID, PhaseBackingUp); err != nil {
		t.Fatalf("display failure interrupted upgrade: %v", err)
	}
	saved, err := store.Get(record.TaskID)
	if err != nil || saved.Phase != PhaseBackingUp {
		t.Fatalf("durable task corrupted: %#v %v", saved, err)
	}
	if _, err := publisher.Watch(record.TaskID); err == nil {
		t.Fatal("unavailable progress advertised")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publisher.Run(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestProgressStatusAdvertisesCapabilityOnlyWithPublisher(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	server := &UnixServer{Store: store}
	request := ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionStatus}
	if response := server.dispatch(context.Background(), request); response.Progress != nil {
		t.Fatal("unconfigured progress advertised")
	}
	publisher, err := store.EnableProgress(filepath.Join(t.TempDir(), "updater-progress"))
	if err != nil {
		t.Fatal(err)
	}
	response := server.dispatch(context.Background(), request)
	watch, err := publisher.Watch(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if response.Progress == nil || *response.Progress != watch {
		t.Fatalf("progress missing from status: %#v", response.Progress)
	}
}

func TestProgressRefreshAndPhasePublicationDoNotDeadlock(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	publisher, err := store.EnableProgress(filepath.Join(t.TempDir(), "updater-progress"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 20; i++ {
			if err := publisher.Refresh(); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for _, phase := range []TaskPhase{PhaseBackingUp, PhaseMigrating, PhaseActivating, PhaseValidating, PhaseCommitting, PhaseSucceeded} {
		if err := store.MarkPhase(record.TaskID, phase); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("publication deadlocked with store")
	}
}

func TestProgressExpiredTerminalSnapshotIsRemoved(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	root := filepath.Join(t.TempDir(), "updater-progress")
	publisher, err := store.EnableProgress(root)
	if err != nil {
		t.Fatal(err)
	}
	watch, err := publisher.Watch(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateMetadata(record.TaskID, func(r *TaskRecord) error {
		r.Phase = PhaseFailed
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	records, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	records[0].UpdatedAt = time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)
	if err := store.save(records); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, strings.TrimPrefix(watch.URL, ProgressURLPrefix))); !os.IsNotExist(err) {
		t.Fatalf("expired snapshot remains: %v", err)
	}
	if _, err := publisher.Watch(record.TaskID); err == nil {
		t.Fatal("expired capability advertised")
	}
}
