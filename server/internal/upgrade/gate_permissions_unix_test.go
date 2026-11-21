//go:build linux || darwin

package upgrade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSharedGateStoreInheritsProvisionedParentGroup(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "updater-gate")
	if err := os.Mkdir(parent, 0750); err != nil {
		t.Fatal(err)
	}
	var parentStat unix.Stat_t
	if err := unix.Stat(parent, &parentStat); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "gate.json")
	store, err := OpenSharedGateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(GateState{Mode: GateCandidate, TaskID: "task"}); err != nil {
		t.Fatal(err)
	}
	var gateStat unix.Stat_t
	if err := unix.Stat(path, &gateStat); err != nil {
		t.Fatal(err)
	}
	if gateStat.Gid != parentStat.Gid {
		t.Fatalf("shared gate gid = %d, want parent gid %d", gateStat.Gid, parentStat.Gid)
	}
	if gateStat.Mode&00777 != 0640 {
		t.Fatalf("shared gate mode = %04o, want 0640", gateStat.Mode&00777)
	}
}

func TestSharedGateStoreRejectsBusinessWritableParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "updater-gate")
	if err := os.Mkdir(parent, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0770); err != nil {
		t.Fatal(err)
	}
	_, err := OpenSharedGateStore(filepath.Join(parent, "gate.json"))
	if err == nil || !strings.Contains(err.Error(), "mode must be 0750") {
		t.Fatalf("writable shared gate parent error = %v", err)
	}
}

func TestSharedGateStoreRejectsSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0750); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "updater-gate")
	if err := os.Symlink(realParent, parent); err != nil {
		t.Fatal(err)
	}
	_, err := OpenSharedGateStore(filepath.Join(parent, "gate.json"))
	if err == nil || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("symlinked shared gate parent error = %v", err)
	}
}

func TestSharedGateStoreRejectsParentOwnedByAnotherAccount(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing directory ownership requires root")
	}
	parent := filepath.Join(t.TempDir(), "updater-gate")
	if err := os.Mkdir(parent, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(parent, 1, -1); err != nil {
		t.Fatal(err)
	}
	_, err := OpenSharedGateStore(filepath.Join(parent, "gate.json"))
	if err == nil || !strings.Contains(err.Error(), "owner is not trusted") {
		t.Fatalf("untrusted shared gate parent owner error = %v", err)
	}
}
