package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisteredPreflightUsesFixedCurrentBinaryAndVersion(t *testing.T) {
	for _, deployment := range []string{"standalone", "systemd", "compose"} {
		t.Run(deployment, func(t *testing.T) {
			root := t.TempDir()
			if deployment != "compose" {
				source := filepath.Join(root, "releases", "source")
				if err := os.MkdirAll(source, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(source, filepath.Join(root, "current")); err != nil {
					t.Fatal(err)
				}
			}
			layout := RegisteredLayout{Root: root, ProjectDir: filepath.Join(root, "project"), Project: "uvp-prod", MigrationService: "backend"}
			registration := Registration{Deployment: deployment, Standalone: layout, Compose: layout, ComposeFile: filepath.Join(root, "project", "compose.yml")}
			want := PreflightSnapshot{CurrentVersion: "1.2.3", CurrentBuildID: "source-build", ConfigDigest: strings.Repeat("a", 64), DatabaseDialect: "sqlite", MigrationDigest: strings.Repeat("b", 64)}
			probe := registeredPreflight{registration: registration, run: func(_ context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
				if deployment == "compose" {
					command := name + " " + strings.Join(args, " ")
					if !strings.Contains(command, "run --rm --no-deps --pull never --entrypoint /app/uvp-gb28181 backend -upgrade-preflight") || !strings.Contains(command, "--project-name uvp-prod") {
						t.Fatalf("unexpected compose preflight: %s", command)
					}
				} else {
					wantDir, wantBinary := root, filepath.Join(root, "current", "bin", "uvp-server")
					if deployment == "systemd" {
						wantDir = filepath.Join(root, "current", "backend")
						wantBinary = filepath.Join(wantDir, "uvp-gb28181")
					}
					if name != wantBinary || dir != wantDir || strings.Join(args, " ") != "-upgrade-preflight" {
						t.Fatalf("preflight command dir=%s binary=%s args=%v", dir, name, args)
					}
					versionRoot := filepath.Join(root, "current")
					if deployment == "systemd" {
						versionRoot = wantDir
					}
					if !strings.Contains(strings.Join(env, "\n"), "UVP_VERSION_PATH="+filepath.Join(versionRoot, "version.json")) {
						t.Fatalf("missing current version path: %v", env)
					}
				}
				return json.Marshal(want)
			}}
			got, err := probe.read(context.Background())
			if err != nil || got != want {
				t.Fatalf("preflight = %#v, %v", got, err)
			}
		})
	}
}

func TestRegisteredPreflightRejectsCurrentOutsideRegisteredReleases(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	called := false
	probe := registeredPreflight{registration: Registration{Deployment: "standalone", Standalone: RegisteredLayout{Root: root}}, run: func(context.Context, string, []string, string, ...string) ([]byte, error) {
		called = true
		return nil, nil
	}}
	if _, err := probe.read(context.Background()); err == nil {
		t.Fatal("accepted unregistered executable location")
	}
	if called {
		t.Fatal("executed outside registered release root")
	}
}

func TestRegisteredPreflightSupportsInitialFlatStandaloneInstall(t *testing.T) {
	root := t.TempDir()
	called := false
	probe := registeredPreflight{registration: Registration{Deployment: "standalone", Standalone: RegisteredLayout{Root: root}}, run: func(_ context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
		called = true
		if dir != root || name != filepath.Join(root, "bin", "uvp-server") || !strings.Contains(strings.Join(env, "\n"), "UVP_VERSION_PATH="+filepath.Join(root, "version.json")) {
			t.Fatalf("flat install command dir=%s name=%s env=%v", dir, name, env)
		}
		return nil, errors.New("test reached current flat source")
	}}
	_, _ = probe.read(context.Background())
	if !called {
		t.Fatal("flat installed source was not queried")
	}
}

func TestRegisteredPreflightRejectsIncompleteAndFailedEvidence(t *testing.T) {
	for _, output := range []string{`{}`, `{"current_version":"1.2.3","current_build_id":"build","config_digest":"bad","database_dialect":"sqlite","migration_digest":"bad"}`, `startup log\n{}`, strings.Repeat("x", 8193)} {
		probe := registeredPreflight{registration: Registration{Deployment: "standalone", Standalone: RegisteredLayout{Root: t.TempDir()}}, run: func(context.Context, string, []string, string, ...string) ([]byte, error) { return []byte(output), nil }}
		if _, err := probe.read(context.Background()); err == nil {
			t.Fatalf("accepted invalid evidence: %.80s", output)
		}
	}
	probe := registeredPreflight{registration: Registration{Deployment: "standalone", Standalone: RegisteredLayout{Root: t.TempDir()}}, run: func(context.Context, string, []string, string, ...string) ([]byte, error) {
		return nil, errors.New("database unavailable")
	}}
	if _, err := probe.read(context.Background()); err == nil {
		t.Fatal("accepted failed preflight process")
	}
}
