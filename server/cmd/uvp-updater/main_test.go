package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

func TestWriteRegistrationIsPrivateAndDoesNotContainPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "registration.json")
	err := writeRegistration(path, upgrade.Registration{
		Deployment: "standalone",
		Standalone: upgrade.RegisteredLayout{Root: "/opt/uvp", State: "/opt/uvp/data/updater-state"},
		DatabaseBackup: &upgrade.ExternalBackupRegistration{
			Dialect: "mysql", Host: "db.internal", Port: 3306, Database: "uvp", User: "backup", PasswordEnv: "UVP_UPGRADE_DB_PASSWORD",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("registration permissions = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "password\":\"secret") || strings.Contains(string(data), "pass\":\"secret") {
		t.Fatalf("registration contains a database password: %s", data)
	}
	var loaded upgrade.Registration
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.DatabaseBackup == nil || loaded.DatabaseBackup.PasswordEnv != "UVP_UPGRADE_DB_PASSWORD" {
		t.Fatalf("loaded registration = %#v", loaded)
	}
}

func TestWriteRegistrationDefaultsStateInsideDeploymentRoot(t *testing.T) {
	for _, deployment := range []string{"standalone", "systemd", "compose"} {
		t.Run(deployment, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "registration.json")
			registration := upgrade.Registration{Deployment: deployment}
			if deployment == "compose" {
				registration.Compose = upgrade.RegisteredLayout{Root: root}
			} else {
				registration.Standalone = upgrade.RegisteredLayout{Root: root}
			}
			if err := writeRegistration(path, registration); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var stored upgrade.Registration
			if err := json.Unmarshal(data, &stored); err != nil {
				t.Fatal(err)
			}
			layout := stored.Standalone
			if deployment == "compose" {
				layout = stored.Compose
			}
			if layout.State != filepath.Join(root, "updater-state") {
				t.Fatalf("registered state = %q", layout.State)
			}
		})
	}
}

func TestBuildServeRuntimeUsesRegisteredStateAndRejectsConflict(t *testing.T) {
	root := t.TempDir()
	registrationPath := secureRegistrationPath(t)
	registration := upgrade.Registration{
		Deployment: "standalone",
		Standalone: upgrade.RegisteredLayout{
			Root: root,
		},
	}
	data, err := json.Marshal(registration) // Simulate a registration written before State was persisted.
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registrationPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	wantState := filepath.Join(root, "updater-state")
	assembly, err := buildServeRuntime(serveOptions{
		SocketPath:       filepath.Join(root, "updater.sock"),
		RegistrationPath: registrationPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if assembly.state != wantState {
		t.Fatalf("state = %q, want %q", assembly.state, wantState)
	}
	if _, err := buildServeRuntime(serveOptions{
		SocketPath:       filepath.Join(root, "updater.sock"),
		StatePath:        filepath.Join(root, "other-state"),
		RegistrationPath: registrationPath,
	}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("conflicting state error = %v", err)
	}
}

func TestProvisionSharedGatePreservesRecoveryState(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "updater-gate")
	if err := os.Mkdir(parent, 0750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "gate.json")
	gate, err := upgrade.OpenSharedGateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Save(upgrade.GateState{Mode: upgrade.GateRecovery, TaskID: "task", Reason: "rollback failed"}); err != nil {
		t.Fatal(err)
	}

	provisioned, err := provisionSharedGate(upgrade.RegisteredLayout{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	state, err := provisioned.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != upgrade.GateRecovery || state.TaskID != "task" || state.Reason != "rollback failed" {
		t.Fatalf("shared gate was overwritten: %#v", state)
	}
}

func TestProvisionSharedGateUsesRegisteredBusinessGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing the gate group requires root")
	}
	root := t.TempDir()
	const businessGID = 23456
	if _, err := provisionSharedGate(upgrade.RegisteredLayout{Root: root}, businessGID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "updater-gate"), filepath.Join(root, "updater-gate", "gate.json")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := fileGroupID(info); !ok || got != businessGID {
			t.Fatalf("%s group = %d (available=%v), want %d", path, got, ok, businessGID)
		}
	}
}

func TestProvisionSharedGateRejectsWritableExistingParentBeforeWritingGate(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "updater-gate")
	if err := os.Mkdir(parent, 0770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0770); err != nil {
		t.Fatal(err)
	}
	_, err := provisionSharedGate(upgrade.RegisteredLayout{Root: root})
	if err == nil || !strings.Contains(err.Error(), "mode must be 0750") {
		t.Fatalf("insecure gate parent error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(parent, "gate.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate file was written into insecure parent: %v", err)
	}
}

func TestBuildServeRuntimeRejectsBlankSocketPath(t *testing.T) {
	if _, err := buildServeRuntime(serveOptions{StatePath: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "socket") {
		t.Fatalf("blank socket error = %v", err)
	}
}

func TestBuildServeRuntimeWiresRegisteredPreflightAndScheduler(t *testing.T) {
	root := t.TempDir()
	registrationPath := secureRegistrationPath(t)
	if err := writeRegistration(registrationPath, upgrade.Registration{
		Deployment:  "systemd",
		Standalone:  upgrade.RegisteredLayout{Root: root},
		SystemdUnit: "uvp-backend.service",
	}); err != nil {
		t.Fatal(err)
	}
	assembly, err := buildServeRuntime(serveOptions{
		SocketPath:       filepath.Join(root, "updater.sock"),
		RegistrationPath: registrationPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if assembly.workflow == nil || assembly.workflow.Preflight == nil {
		t.Fatal("registered workflow has no independent preflight")
	}
	scheduler, ok := assembly.scheduler.(*upgrade.HelperScheduler)
	if !ok || scheduler == nil {
		t.Fatalf("scheduler = %T, want *upgrade.HelperScheduler", assembly.scheduler)
	}
	if scheduler.Preflight == nil || scheduler.Apply == nil {
		t.Fatal("helper scheduler is missing preflight or apply")
	}
	if scheduler.Target.Deployment != "systemd" || scheduler.Target.OS != runtime.GOOS || scheduler.Target.Arch != runtime.GOARCH || scheduler.Target.UpdaterProtocol != upgrade.UpdaterProtocolVersion {
		t.Fatalf("scheduler target = %#v", scheduler.Target)
	}
}

func TestBuildServeRuntimeWiresRegisteredControl(t *testing.T) {
	root := t.TempDir()
	registrationPath := secureRegistrationPath(t)
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	inbox := filepath.Join(root, "inbox")
	if err := writeRegistration(registrationPath, upgrade.Registration{
		Deployment:    "standalone",
		ReleaseSource: "https://updates.example.test/uvp-gb28181/channels/stable/manifest.json",
		TrustedKeys:   map[string]string{"release-1": base64.StdEncoding.EncodeToString(public)},
		StagingRoot:   inbox,
		Standalone:    upgrade.RegisteredLayout{Root: root},
	}); err != nil {
		t.Fatal(err)
	}
	assembly, err := buildServeRuntime(serveOptions{
		SocketPath:       filepath.Join(root, "updater.sock"),
		RegistrationPath: registrationPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	server, ok := assembly.server.(*upgrade.UnixServer)
	if !ok || server.Control == nil {
		t.Fatalf("server control = %#v", server)
	}
	control, ok := server.Control.(*upgrade.ControlService)
	if !ok {
		t.Fatalf("control = %T, want *upgrade.ControlService", server.Control)
	}
	if control.ImportStore == nil {
		t.Fatal("control import status is not persisted by the updater")
	}
	if control.ReleaseSource != "https://updates.example.test/uvp-gb28181/channels/stable/manifest.json" {
		t.Fatalf("release source = %q", control.ReleaseSource)
	}
	if control.InboxRoot != inbox || control.StagingRoot != filepath.Join(root, "updater-state", "verified") {
		t.Fatalf("control roots = inbox %q staging %q", control.InboxRoot, control.StagingRoot)
	}
	if control.SequencePath != filepath.Join(root, "updater-state", "sequences.json") {
		t.Fatalf("control sequence path = %q", control.SequencePath)
	}
}

func secureRegistrationPath(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(home, ".uvp-updater-main-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		_ = os.RemoveAll(dir)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "registration.json")
}

type lifecycleServer struct {
	started chan struct{}
	release chan struct{}
}

func (s *lifecycleServer) Serve(context.Context) error {
	close(s.started)
	<-s.release
	return nil
}

type lifecycleScheduler struct {
	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
}

func (s *lifecycleScheduler) Run(ctx context.Context) error {
	close(s.started)
	<-ctx.Done()
	close(s.canceled)
	<-s.release
	return ctx.Err()
}

func TestRunServeLifecycleCancelsAndWaitsForScheduler(t *testing.T) {
	server := &lifecycleServer{started: make(chan struct{}), release: make(chan struct{})}
	scheduler := &lifecycleScheduler{started: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- runServeLifecycle(context.Background(), server, scheduler) }()
	select {
	case <-server.started:
	case <-time.After(time.Second):
		t.Fatal("server did not start")
	}
	select {
	case <-scheduler.started:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not start")
	}
	close(server.release)
	select {
	case <-scheduler.canceled:
	case <-time.After(time.Second):
		t.Fatal("scheduler was not canceled after server exit")
	}
	select {
	case err := <-done:
		t.Fatalf("lifecycle returned before scheduler completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(scheduler.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("lifecycle did not wait for scheduler")
	}
}

func TestRegistrationCopiesOnlyValidReleasePublicKeys(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(make([]byte, 32))
	for _, input := range []interface{}{
		map[string]interface{}{"release-1": encoded, "invalid": "not-a-key"},
		map[string]string{"release-1": encoded, "invalid": "not-a-key"},
		[]interface{}{"release-1=" + encoded, "invalid=bad"},
		[]string{"release-1=" + encoded, "invalid=bad"},
	} {
		keys := registrationTrustedKeys(input)
		if len(keys) != 1 || keys["release-1"] != encoded {
			t.Fatalf("registered keys: %#v", keys)
		}
	}
}

func TestRegistrationReadinessURLUsesBackendLoopbackPort(t *testing.T) {
	for _, address := range []string{":8080", "0.0.0.0:8080", "127.0.0.1:8080"} {
		value, err := registrationReadinessURL(address)
		if err != nil {
			t.Fatal(err)
		}
		if value != "http://127.0.0.1:8080"+upgrade.RuntimeIdentityPath {
			t.Fatalf("URL %q", value)
		}
	}
	for _, address := range []string{"8080", ":0", ":70000", "192.0.2.1:8080", "backend:8080"} {
		if _, err := registrationReadinessURL(address); err == nil {
			t.Fatalf("invalid listen address %q accepted", address)
		}
	}
}

func TestRegistrationExternalBackupMaxBytes(t *testing.T) {
	for input, want := range map[string]uint64{
		"":           upgrade.DefaultExternalBackupMaxBytes,
		"1073741824": 1 << 30,
		"8589934592": upgrade.DefaultExternalBackupMaxBytes,
	} {
		got, err := registrationExternalBackupMaxBytes(input)
		if err != nil || got != want {
			t.Fatalf("input %q: got %d, err %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"0", "-1", "eight-gib", "18446744073709551616"} {
		if _, err := registrationExternalBackupMaxBytes(input); err == nil {
			t.Fatalf("input %q was accepted", input)
		}
	}
}
