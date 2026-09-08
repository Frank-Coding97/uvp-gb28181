//go:build windows

package launcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func TestWindowsMaintenanceGateBlocksBeforeComponentStartup(t *testing.T) {
	root := os.Getenv("UVP_MAINTENANCE_TEST_ROOT")
	if root == "" {
		t.Skip("requires isolated installed Windows fixture")
	}
	release, err := standalone.LoadRelease(root)
	if err != nil || release.Version != "t18-setup" {
		t.Fatal("requires isolated t18-setup fixture")
	}
	before := backupTestHashes(t, filepath.Join(root, "data"))
	gate := filepath.Join(root, ".uvp-maintenance")
	if err := os.Mkdir(gate, 0700); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(gate); err != nil {
			t.Errorf("remove owned empty test gate: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	notified := false
	err = LaunchWithBrowser(ctx, root, "", func(Status) { notified = true }, func(string) { t.Error("maintenance unexpectedly opened business browser") })
	if !errors.Is(err, standalone.ErrMaintenanceRequired) || notified {
		t.Fatalf("gate did not stop preflight: %v", err)
	}
	lock, err := standalone.AcquireInstanceLock(root)
	if err != nil {
		t.Fatal("blocked startup did not release instance lock")
	}
	lock.Close()
	after := backupTestHashes(t, filepath.Join(root, "data"))
	if len(before) != len(after) {
		t.Fatal("blocked startup changed data files")
	}
	for path, hash := range before {
		if after[path] != hash {
			t.Fatal("blocked startup changed data")
		}
	}
	t.Log("incomplete journal blocked before component setup; data unchanged and ownership released")
}
