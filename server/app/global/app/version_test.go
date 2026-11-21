package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeVersionIdentityRequiresMatchingEmbeddedBuildID(t *testing.T) {
	originalVersion, originalBuildID := AppVersion, BuildID
	t.Cleanup(func() {
		AppVersion, BuildID = originalVersion, originalBuildID
	})

	AppVersion = VersionInfo{Version: "1.2.3", BuildID: "release-build"}
	BuildID = "release-build"
	version, buildID := RuntimeVersionIdentity()
	if version != "1.2.3" || buildID != "release-build" {
		t.Fatalf("runtime identity = (%q, %q)", version, buildID)
	}

	AppVersion.BuildID = "forged-file-build"
	version, buildID = RuntimeVersionIdentity()
	if version != "" || buildID != "" {
		t.Fatalf("mismatched package metadata identified the runtime as (%q, %q)", version, buildID)
	}
}

func TestLoadVersionInfoUsesRegisteredReleasePath(t *testing.T) {
	originalVersion, originalBasePath := AppVersion, BasePath
	t.Cleanup(func() {
		AppVersion, BasePath = originalVersion, originalBasePath
	})

	path := filepath.Join(t.TempDir(), "candidate-version.json")
	if err := os.WriteFile(path, []byte(`{"name":"UVP","version":"2.0.0","build_id":"candidate-build"}`), 0600); err != nil {
		t.Fatal(err)
	}
	BasePath = t.TempDir()
	t.Setenv("UVP_VERSION_PATH", path)
	if err := LoadVersionInfo(); err != nil {
		t.Fatal(err)
	}
	if AppVersion.Version != "2.0.0" || AppVersion.BuildID != "candidate-build" {
		t.Fatalf("loaded version = %#v", AppVersion)
	}
}
