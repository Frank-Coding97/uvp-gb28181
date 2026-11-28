//go:build linux

package upgrade

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenTrustedRegistrationClosesFinalParentDescriptor(t *testing.T) {
	root := secureRegistrationTestDir(t)
	path := filepath.Join(root, "registration.json")
	if err := os.WriteFile(path, []byte(`{"deployment":"standalone","standalone":{"root":"/opt/uvp"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	before := linuxOpenFileDescriptorCount(t)
	for range 64 {
		file, err := openTrustedRegistration(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	after := linuxOpenFileDescriptorCount(t)
	if after > before+2 {
		t.Fatalf("open registration leaked parent descriptors: before=%d after=%d", before, after)
	}
}

func linuxOpenFileDescriptorCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}
