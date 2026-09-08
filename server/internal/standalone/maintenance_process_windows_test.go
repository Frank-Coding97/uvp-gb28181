//go:build windows

package standalone

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupComponentsRejectsExactRunningImage(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = backupComponentsStopped(Release{BackendExe: executable}); err == nil {
		t.Fatal("running component image accepted")
	}
}

func TestBackupComponentsDoesNotConfuseAnotherInstallation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), filepath.Base(executable))
	if err = backupComponentsStopped(Release{BackendExe: other}); err != nil {
		t.Fatalf("unrelated image with same basename rejected: %v", err)
	}
}
