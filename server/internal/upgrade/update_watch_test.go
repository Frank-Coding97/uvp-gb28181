package upgrade

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newUpdateWatchFixture(t *testing.T, manifestVersion string) (*ControlService, *UpdateWatchStore) {
	t.Helper()
	root := t.TempDir()
	watch, err := OpenUpdateWatchStore(filepath.Join(root, "update-check.json"))
	if err != nil {
		t.Fatalf("open watch store: %v", err)
	}
	target := Target{Deployment: "standalone", OS: runtime.GOOS, Arch: runtime.GOARCH, UpdaterProtocol: UpdaterProtocolVersion}
	service := &ControlService{
		ReleaseSource: "https://updates.example.test/channels/stable/manifest.json",
		TrustedKeys:   map[string]ed25519.PublicKey{"release-key": make(ed25519.PublicKey, ed25519.PublicKeySize)},
		Target:        target,
		UpdateWatch:   watch,
		CheckRelease: func(context.Context) (Manifest, error) {
			return Manifest{
				SchemaVersion: 1, Channel: "stable", ReleaseID: "release-2", Version: manifestVersion, BuildID: "build-2",
				PublishedAt: "2026-10-20T08:00:00Z", ReleaseNotes: "修复若干问题",
				Artifacts: []Artifact{{
					Deployment: target.Deployment, OS: target.OS, Arch: target.Arch,
					Filename: "uvp.tar.gz", URL: "https://updates.example.test/uvp.tar.gz", SHA256: strings.Repeat("c", 64),
				}},
			}, nil
		},
		Preflight: func(context.Context) (PreflightSnapshot, error) {
			return PreflightSnapshot{
				CurrentVersion: "1.1.0", CurrentBuildID: "build-1", ConfigDigest: strings.Repeat("a", 64),
				DatabaseDialect: "sqlite", MigrationDigest: strings.Repeat("b", 64),
			}, nil
		},
	}
	return service, watch
}

func TestUpdateWatcherRecordsNewerRelease(t *testing.T) {
	service, watch := newUpdateWatchFixture(t, "1.2.0")
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	watcher := &UpdateWatcher{Service: service, Store: watch, SkipInitialDelay: true, Now: func() time.Time { return now }}
	watcher.CheckOnce(context.Background())

	state := watch.Snapshot()
	if !state.HasUpdate {
		t.Fatal("newer release was not detected")
	}
	if state.LatestVersion != "1.2.0" || state.LatestBuildID != "build-2" {
		t.Fatalf("latest = %q / %q", state.LatestVersion, state.LatestBuildID)
	}
	if state.CurrentVersion != "1.1.0" {
		t.Fatalf("current = %q", state.CurrentVersion)
	}
	if state.Error != "" {
		t.Fatalf("unexpected error: %s", state.Error)
	}
	if !state.CheckedAt.Equal(now) {
		t.Fatalf("checked at = %s", state.CheckedAt)
	}
}

func TestUpdateWatcherIgnoresSameOrOlderRelease(t *testing.T) {
	service, watch := newUpdateWatchFixture(t, "1.1.0")
	watcher := &UpdateWatcher{Service: service, Store: watch, SkipInitialDelay: true}
	watcher.CheckOnce(context.Background())
	if watch.Snapshot().HasUpdate {
		t.Fatal("same version must not be reported as an update")
	}
}

// A probe that fails (offline machine, TLS error, DNS) must not erase the
// release it found earlier: otherwise the badge would blink away whenever the
// network hiccups.
func TestUpdateWatcherKeepsPreviousUpdateAfterFailedCheck(t *testing.T) {
	service, watch := newUpdateWatchFixture(t, "1.2.0")
	watcher := &UpdateWatcher{Service: service, Store: watch, SkipInitialDelay: true}
	watcher.CheckOnce(context.Background())
	if !watch.Snapshot().HasUpdate {
		t.Fatal("expected initial update")
	}

	service.CheckRelease = func(context.Context) (Manifest, error) {
		return Manifest{}, errors.New("dial tcp: lookup updates.example.test: no such host")
	}
	watcher.CheckOnce(context.Background())

	state := watch.Snapshot()
	if !state.HasUpdate || state.LatestVersion != "1.2.0" {
		t.Fatalf("failed check dropped the known release: %#v", state)
	}
	if state.Error == "" {
		t.Fatal("failure reason was not recorded")
	}
}

func TestUpdateWatchStateSurvivesReopen(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "update-check.json")
	watch, err := OpenUpdateWatchStore(path)
	if err != nil {
		t.Fatal(err)
	}
	result := ControlCheckResult{
		Manifest:       Manifest{Version: "1.3.0", BuildID: "build-3", PublishedAt: "2026-10-28T00:00:00Z"},
		CurrentVersion: "1.1.0",
		HasUpdate:      true,
	}
	if err := watch.Record(&result, nil, time.Now()); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenUpdateWatchStore(path)
	if err != nil {
		t.Fatal(err)
	}
	state := reopened.Snapshot()
	if !state.HasUpdate || state.LatestVersion != "1.3.0" {
		t.Fatalf("persisted state = %#v", state)
	}
}

// The state file is a cache, not a source of truth. A truncated or corrupt
// file must not prevent the helper from starting.
func TestUpdateWatchStoreToleratesCorruptState(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "update-check.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	watch, err := OpenUpdateWatchStore(path)
	if err != nil {
		t.Fatalf("corrupt state blocked startup: %v", err)
	}
	if watch.Snapshot().HasUpdate {
		t.Fatal("corrupt state produced a phantom update")
	}
}

// Offline installations have no release source. Probing them would persist an
// error nobody can act on, so the watcher must stay silent.
func TestUpdateWatcherStaysSilentWithoutReleaseSource(t *testing.T) {
	service, watch := newUpdateWatchFixture(t, "1.2.0")
	service.ReleaseSource = ""
	var calls atomic.Int32
	service.CheckRelease = func(context.Context) (Manifest, error) {
		calls.Add(1)
		return Manifest{}, errors.New("must not be called")
	}
	watcher := &UpdateWatcher{Service: service, Store: watch, SkipInitialDelay: true}
	watcher.CheckOnce(context.Background())
	if calls.Load() != 0 {
		t.Fatal("offline installation probed the release source")
	}
	if snapshot := watch.Snapshot(); !snapshot.CheckedAt.IsZero() || snapshot.Error != "" {
		t.Fatalf("offline probe wrote state: %#v", snapshot)
	}
}

// A missing switch file means "enabled": scheduled checks shipped as the
// default, so a storage problem must never look like an administrator choice.
func TestReadAutoCheckPreferenceDefaultsToEnabled(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, AutoCheckPreferenceFilename)
	if !ReadAutoCheckPreference("") {
		t.Fatal("unconfigured switch must default to enabled")
	}
	if !ReadAutoCheckPreference(missing) {
		t.Fatal("missing switch file must default to enabled")
	}
	if err := os.WriteFile(missing, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if !ReadAutoCheckPreference(missing) {
		t.Fatal("corrupt switch file must default to enabled")
	}
}

func TestWriteAutoCheckPreferenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), AutoCheckPreferenceFilename)
	if err := WriteAutoCheckPreference(path, false); err != nil {
		t.Fatal(err)
	}
	if ReadAutoCheckPreference(path) {
		t.Fatal("disabled preference was not honoured")
	}
	if err := WriteAutoCheckPreference(path, true); err != nil {
		t.Fatal(err)
	}
	if !ReadAutoCheckPreference(path) {
		t.Fatal("re-enabled preference was not honoured")
	}
	// The business process writes this file, so it must not leave a temp file
	// that a later helper start could mistake for state.
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary file left behind: %v", err)
	}
}

// Turning the switch off must silence the timer without touching the last
// result: the badge keeps showing a release the machine already knew about.
func TestUpdateWatcherSkipsWhenAutoCheckDisabled(t *testing.T) {
	service, watch := newUpdateWatchFixture(t, "1.2.0")
	watcher := &UpdateWatcher{Service: service, Store: watch, SkipInitialDelay: true}
	watcher.CheckOnce(context.Background())
	if !watch.Snapshot().HasUpdate {
		t.Fatal("expected an update before disabling the schedule")
	}

	previous := watch.Snapshot()
	switched := filepath.Join(t.TempDir(), AutoCheckPreferenceFilename)
	if err := WriteAutoCheckPreference(switched, false); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	service.CheckRelease = func(context.Context) (Manifest, error) {
		calls.Add(1)
		return Manifest{}, errors.New("must not be called")
	}
	watcher.PreferencePath = switched
	watcher.CheckOnce(context.Background())

	if calls.Load() != 0 {
		t.Fatal("disabled schedule still probed the release source")
	}
	if state := watch.Snapshot(); state.CheckedAt != previous.CheckedAt || state.LatestVersion != previous.LatestVersion {
		t.Fatalf("disabled schedule rewrote cached state: %#v", state)
	}

	// Re-enabling must resume probing on the next tick, without a restart.
	if err := WriteAutoCheckPreference(switched, true); err != nil {
		t.Fatal(err)
	}
	service.CheckRelease = func(context.Context) (Manifest, error) {
		calls.Add(1)
		return Manifest{
			SchemaVersion: 1, Channel: "stable", Version: "1.2.0", BuildID: "build-2",
			Artifacts: []Artifact{{
				Deployment: service.Target.Deployment, OS: service.Target.OS, Arch: service.Target.Arch,
				Filename: "uvp.tar.gz", SHA256: strings.Repeat("c", 64),
			}},
		}, nil
	}
	watcher.CheckOnce(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("re-enabled schedule probed %d times, want 1", calls.Load())
	}
}

func TestUpdateWatcherRunStopsWithContext(t *testing.T) {
	service, watch := newUpdateWatchFixture(t, "1.2.0")
	var calls atomic.Int32
	service.CheckRelease = func(ctx context.Context) (Manifest, error) {
		calls.Add(1)
		return Manifest{
			SchemaVersion: 1, Channel: "stable", Version: "1.2.0", BuildID: "build-2",
			Artifacts: []Artifact{{
				Deployment: service.Target.Deployment, OS: service.Target.OS, Arch: service.Target.Arch,
				Filename: "uvp.tar.gz", SHA256: strings.Repeat("c", 64),
			}},
		}, nil
	}
	watcher := &UpdateWatcher{Service: service, Store: watch, Interval: 20 * time.Millisecond, SkipInitialDelay: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- watcher.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop with its context")
	}
	if calls.Load() < 2 {
		t.Fatalf("watcher ran %d probes, want at least 2 (immediate + interval)", calls.Load())
	}
}
