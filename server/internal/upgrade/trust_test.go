package upgrade

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRegisteredTaskVerifierRechecksSignatureAndArtifactBeforeStop(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "valid-uncompressed-tar", "same-version", "downgrade", "minimum-version", "tampered-package", "untrusted-signature", "outside-inbox", "missing-envelope"} {
		t.Run(scenario, func(t *testing.T) {
			root := secureRegistrationTestDir(t)
			state := filepath.Join(root, "state")
			inbox := filepath.Join(state, "staging")
			if err := os.MkdirAll(inbox, 0700); err != nil {
				t.Fatal(err)
			}
			data := []byte("signed package contents")
			filename := "package.tar.gz"
			if scenario == "valid-uncompressed-tar" {
				filename = "package.tar"
				var archive bytes.Buffer
				writer := tar.NewWriter(&archive)
				contents := []byte("release contents")
				if err := writer.WriteHeader(&tar.Header{Name: "release/version.json", Mode: 0600, Size: int64(len(contents))}); err != nil {
					t.Fatal(err)
				}
				if _, err := writer.Write(contents); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				data = archive.Bytes()
			}
			sum := sha256.Sum256(data)
			digest := hex.EncodeToString(sum[:])
			artifact := Artifact{Deployment: "standalone", OS: runtime.GOOS, Arch: runtime.GOARCH, Filename: filename, URL: "https://updates.example/" + filename, Size: int64(len(data)), SHA256: digest}
			manifest := Manifest{SchemaVersion: 1, Channel: "stable", Sequence: 1, ReleaseID: "release-1", Version: "1.2.0", BuildID: "build-1", MinUpdaterProtocol: 1, Artifacts: []Artifact{artifact}}
			if scenario == "same-version" {
				manifest.Version = "1.1.0"
			}
			if scenario == "downgrade" {
				manifest.Version = "1.0.0"
			}
			if scenario == "minimum-version" {
				manifest.MinVersion = "1.1.1"
			}
			payload, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			envelope, err := json.Marshal(SignedManifest{Payload: base64.StdEncoding.EncodeToString(payload), Signatures: []ManifestSignature{{KeyID: "test", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, payload))}}})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(inbox, digest+"-package.tar.gz")
			if scenario == "outside-inbox" {
				path = filepath.Join(root, "outside.tar.gz")
			}
			if scenario == "tampered-package" {
				data = []byte("tampered package bytes")
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "untrusted-signature" {
				envelope = []byte(strings.Replace(string(envelope), "test", "unknown", 1))
			}
			if scenario == "missing-envelope" {
				envelope = nil
			}
			staged := StagedArtifact{Path: path, SHA256: digest, ManifestEnvelope: envelope}
			if err := SaveStagedMetadata(path+".json", staged); err != nil {
				t.Fatal(err)
			}
			registration := Registration{Deployment: "standalone", Standalone: RegisteredLayout{Root: root, State: state}, TrustedKeys: map[string]string{"test": base64.StdEncoding.EncodeToString(public)}}
			registrationData, err := json.Marshal(registration)
			if err != nil {
				t.Fatal(err)
			}
			registrationPath := filepath.Join(root, "registration.json")
			if err := os.WriteFile(registrationPath, registrationData, 0600); err != nil {
				t.Fatal(err)
			}
			verify, err := LoadRegisteredVerifier(registrationPath, state)
			if err != nil {
				t.Fatal(err)
			}
			task := TaskRecord{TaskID: strings.Repeat("a", 32), StagingPath: path, TargetDigest: digest, Version: "forged", BuildID: "forged", Preflight: &PreflightSnapshot{CurrentVersion: "1.1.0", CurrentBuildID: "source-build"}}
			checked, err := verify(context.Background(), task)
			if scenario != "valid" && scenario != "valid-uncompressed-tar" {
				if err == nil {
					t.Fatalf("accepted %s", scenario)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if checked.Version != manifest.Version || checked.BuildID != manifest.BuildID || checked.StagingPath == path {
				t.Fatalf("trusted fields not derived from signature: %#v", checked)
			}
			if scenario == "valid-uncompressed-tar" {
				if err := extractPackage(checked.StagingPath, t.TempDir()); err != nil {
					t.Fatalf("verified tar cannot be extracted: %v", err)
				}
				return
			}
			if err := os.WriteFile(path, []byte("inbox replaced after verification"), 0600); err != nil {
				t.Fatal(err)
			}
			immutable, err := os.ReadFile(checked.StagingPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(immutable) != "signed package contents" {
				t.Fatalf("verified copy changed: %q", immutable)
			}
		})
	}
}
