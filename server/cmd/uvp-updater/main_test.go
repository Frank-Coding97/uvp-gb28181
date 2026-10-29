package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"uvplatform.com/uvp-gb28181/app/utils/ymlconfig"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

func TestWriteRegistrationIsPrivateAndDoesNotContainPassword(t *testing.T) {
	// The root has to be an absolute path on the host running the test: a
	// literal "/opt/uvp" is not absolute on Windows, where registration
	// validation would reject it before the assertions below ever run.
	root := t.TempDir()
	path := filepath.Join(root, "nested", "registration.json")
	err := writeRegistration(path, upgrade.Registration{
		Deployment: "standalone",
		Standalone: upgrade.RegisteredLayout{Root: root, State: filepath.Join(root, "data", "updater-state")},
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
	// Windows does not keep POSIX mode bits: access is expressed by the parent
	// directory ACL instead, so only assert the Unix permission contract there.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
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

func TestBuildServeRuntimeRejectsBlankSocketPath(t *testing.T) {
	if _, err := buildServeRuntime(serveOptions{StatePath: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "socket") {
		t.Fatalf("blank socket error = %v", err)
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

type lifecycleWatcher struct {
	started chan struct{}
	stopped chan struct{}
}

func (w *lifecycleWatcher) Run(ctx context.Context) error {
	close(w.started)
	<-ctx.Done()
	close(w.stopped)
	return ctx.Err()
}

func TestRunServeLifecycleStopsPeriodicCheckWithTheServer(t *testing.T) {
	server := &lifecycleServer{started: make(chan struct{}), release: make(chan struct{})}
	watcher := &lifecycleWatcher{started: make(chan struct{}), stopped: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- runServeLifecycle(context.Background(), server, watcher) }()
	select {
	case <-server.started:
	case <-time.After(time.Second):
		t.Fatal("server did not start")
	}
	select {
	case <-watcher.started:
	case <-time.After(time.Second):
		t.Fatal("periodic update check did not start")
	}
	if err := runServeLifecycle(context.Background(), nil, nil); err == nil {
		t.Fatal("nil server accepted")
	}
	close(server.release)
	// The periodic check must be cancelled with the server: leaving it running
	// would keep probing the release source after the helper exits.
	select {
	case <-watcher.stopped:
	case <-time.After(time.Second):
		t.Fatal("periodic update check outlived the server")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("lifecycle did not return after server exit")
	}
}

func TestRunServeLifecycleServesWithoutPeriodicCheck(t *testing.T) {
	server := &lifecycleServer{started: make(chan struct{}), release: make(chan struct{})}
	close(server.release)
	if err := runServeLifecycle(context.Background(), server, nil); err != nil {
		t.Fatal(err)
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

func TestRegistrationLoadsTrustedKeysFromApplicationConfig(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(public)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yml")
	configText := fmt.Sprintf("upgrade:\n  trusted_keys:\n    release-generated: %q\n", encoded)
	if err := os.WriteFile(configPath, []byte(configText), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := ymlconfig.LoadYamlFactory(configDir)
	if err != nil {
		t.Fatal(err)
	}
	keys := registrationTrustedKeys(config.Get("upgrade.trusted_keys"))
	if len(keys) != 1 || keys["release-generated"] != encoded {
		t.Fatalf("registration keys from config = %#v", keys)
	}

	registrationPath := filepath.Join(configDir, "updater-registration.json")
	if err := writeRegistration(registrationPath, upgrade.Registration{
		Deployment:  "standalone",
		TrustedKeys: keys,
		Standalone:  upgrade.RegisteredLayout{Root: configDir},
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(registrationPath)
	if err != nil {
		t.Fatal(err)
	}
	var registration upgrade.Registration
	if err := json.Unmarshal(data, &registration); err != nil {
		t.Fatal(err)
	}
	if len(registration.TrustedKeys) != 1 || registration.TrustedKeys["release-generated"] != encoded {
		t.Fatalf("persisted registration keys = %#v", registration.TrustedKeys)
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

func TestRegistrationSQLiteDatabaseResolvesInstallationConfig(t *testing.T) {
	root := t.TempDir()
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.yml"), []byte("gormv2:\n  usedbtype: sqlite\n  sqlite:\n    isinitglobalgormsqlite: 1\n    write:\n      database: ./custom/installed.db\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := ymlconfig.LoadYamlFactory(configDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, deployment := range []string{"standalone", "systemd"} {
		t.Run(deployment, func(t *testing.T) {
			path := filepath.Join(configDir, deployment+".json")
			runRegistration([]string{"--config-dir", configDir, "--registration", path, "--root", root, "--deployment", deployment, "--readiness-url", "http://127.0.0.1:51002/internal/updater/readiness"})
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var registration upgrade.Registration
			if err := json.Unmarshal(data, &registration); err != nil {
				t.Fatal(err)
			}
			if got, want := registration.Standalone.SQLiteDatabase, filepath.Join(root, "custom", "installed.db"); got != want {
				t.Fatalf("persisted sqlite database = %q, want %q", got, want)
			}
		})
	}
	for _, tt := range []struct {
		name, deployment, configured, explicit, want string
		wantError                                    bool
	}{
		{name: "absolute", deployment: "standalone", configured: filepath.Join(root, "elsewhere.db"), want: filepath.Join(root, "elsewhere.db")},
		{name: "explicit", deployment: "standalone", configured: "./data/uvp.db", explicit: filepath.Join(root, "registered.db"), want: filepath.Join(root, "registered.db")},
		{name: "missing", deployment: "standalone", wantError: true},
		{name: "relative override", deployment: "standalone", explicit: "data/uvp.db", wantError: true},
		{name: "compose requires host path", deployment: "compose", configured: "/app/data/uvp.db", wantError: true},
		{name: "compose host override", deployment: "compose", configured: "/app/data/uvp.db", explicit: filepath.Join(root, "host.db"), want: filepath.Join(root, "host.db")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config.Set("gormv2.sqlite.write.database", tt.configured)
			got, err := registrationSQLiteDatabase(config, tt.deployment, root, tt.explicit)
			if (err != nil) != tt.wantError || got != tt.want {
				t.Fatalf("path = %q, error = %v; want %q, error %v", got, err, tt.want, tt.wantError)
			}
		})
	}
	config.Set("gormv2.usedbtype", "mysql")
	got, err := registrationSQLiteDatabase(config, "standalone", root, "")
	if err != nil || got != "" {
		t.Fatalf("external database unexpectedly selected sqlite: %q, %v", got, err)
	}
}
