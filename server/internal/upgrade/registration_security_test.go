package upgrade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRegistrationAcceptsPrivateRegularFile(t *testing.T) {
	root := secureRegistrationTestDir(t)
	path := filepath.Join(root, "registration.json")
	want := Registration{
		Deployment:  "standalone",
		AllowedUIDs: []uint32{1000},
		Standalone:  RegisteredLayout{Root: root},
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRegistration(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Deployment != want.Deployment || len(got.AllowedUIDs) != 1 || got.AllowedUIDs[0] != 1000 {
		t.Fatalf("registration = %#v", got)
	}
}

func secureRegistrationTestDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(home, ".uvp-updater-registration-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		_ = os.RemoveAll(root)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}
