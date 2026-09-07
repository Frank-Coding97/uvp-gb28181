//go:build windows

package routes

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveStaticFileRejectsWindowsJunction(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "secret.txt"), []byte("junction-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "junction")
	command := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, target)
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("mklink /J unavailable in this Windows test environment: %v (%s)", err, output)
	}
	t.Cleanup(func() { _ = os.Remove(junction) })

	resolved, err := filepath.EvalSymlinks(junction)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("junction=%q EvalSymlinks=%q", junction, resolved)

	_, err = resolveStaticFile(root, filepath.Join("junction", "secret.txt"))
	if !errors.Is(err, errStaticReparsePoint) {
		t.Fatalf("resolveStaticFile error=%v, want %v", err, errStaticReparsePoint)
	}
}
