//go:build windows

package launcher

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func TestWindowsDirectBackendMaintenanceGatePrecedesInitialization(t *testing.T) {
	backend := os.Getenv("UVP_MAINTENANCE_BACKEND_PATH")
	if backend == "" {
		t.Skip("requires built backend executable")
	}
	for _, operation := range []string{"", "-bootstrap-db", "-db-check", "-migrate-up", "-migrate-down=test.sql"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".uvp-maintenance"), 0700); err != nil {
				t.Fatal(err)
			}
			paths, err := standalone.ResolvePaths(standalone.PathOptions{
				InstallDir: root, ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"),
				ResourceDir: filepath.Join(root, "resource"), WebDir: filepath.Join(root, "web"),
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			args := []string{}
			if operation != "" {
				args = append(args, operation)
			}
			cmd := exec.CommandContext(ctx, backend, args...)
			cmd.Env = componentEnvironment(paths)
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), standalone.ErrMaintenanceRequired.Error()) {
				t.Fatalf("backend did not reject maintenance before path/config/DB initialization: %v; output=%s", err, output)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != ".uvp-maintenance" {
				t.Fatal("blocked backend created persistent state")
			}
		})
	}
}

func TestWindowsDirectBackendWithoutMaintenanceSupportsDBCheck(t *testing.T) {
	backend := os.Getenv("UVP_MAINTENANCE_BACKEND_PATH")
	if backend == "" {
		t.Skip("requires built backend executable")
	}
	root := t.TempDir()
	for _, dir := range []string{"config", "data", "resource", "web", "recordings"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{InstallDir: root, ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), ResourceDir: filepath.Join(root, "resource"), WebDir: filepath.Join(root, "web")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := standalone.InitializeConfig(paths); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, backend, "-db-check")
	cmd.Env = componentEnvironment(paths)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ungated DB check failed: %v; output=%s", err, output)
	}
	if _, err := os.Stat(paths.DatabasePath); err != nil {
		t.Fatal(err)
	}
}
