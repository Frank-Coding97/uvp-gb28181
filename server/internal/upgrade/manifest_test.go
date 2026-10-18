package upgrade

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestVerifySignedManifestAcceptsTrustedPayload(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"schema_version":1,"channel":"stable","sequence":7,"release_id":"r-7","version":"1.2.3","build_id":"build-7","min_updater_protocol":1,"artifacts":[{"deployment":"standalone","os":"linux","arch":"amd64","filename":"uvp-gb28181-linux-amd64-1.2.3.tar.gz","url":"https://updates.example.test/releases/r-7/uvp-gb28181-linux-amd64-1.2.3.tar.gz","size":12,"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}]}`
	signature := ed25519.Sign(privateKey, []byte(payload))
	envelope := signedEnvelopeForTest(t, payload, "release-key-1", signature)

	manifest, err := VerifySignedManifest(envelope, map[string]ed25519.PublicKey{"release-key-1": publicKey})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if manifest.ReleaseID != "r-7" || manifest.Artifacts[0].Arch != "amd64" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
}

func TestSignedManifestPersistsCompatibilityAndEnvelope(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	payloadBytes, err := json.Marshal(automaticManifestForTest())
	if err != nil {
		t.Fatal(err)
	}
	envelope := signedEnvelopeForTest(t, string(payloadBytes), "release-key-1", ed25519.Sign(privateKey, payloadBytes))
	manifest, err := VerifySignedManifest(envelope, map[string]ed25519.PublicKey{"release-key-1": publicKey})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(manifest.Envelope, envelope) {
		t.Fatal("verified manifest did not retain the original signed envelope")
	}
	if err := manifest.AllowsAutomaticUpgrade(automaticPreflightForTest()); err != nil {
		t.Fatalf("automatic compatibility rejected its declared source: %v", err)
	}
	if !manifest.Compatibility.AllowsAutomaticRollback() {
		t.Fatal("explicit rollback compatibility was not recognized")
	}
	manifest.Compatibility.Rollback.DependenciesCompatible = false
	if manifest.Compatibility.AllowsAutomaticRollback() {
		t.Fatal("incomplete rollback compatibility was accepted")
	}
}

func TestAutomaticCompatibilityRequiresExactSourceAndDatabaseState(t *testing.T) {
	manifest := automaticManifestForTest()
	for _, test := range []struct {
		name   string
		mutate func(*PreflightSnapshot)
	}{
		{name: "different build", mutate: func(snapshot *PreflightSnapshot) { snapshot.CurrentBuildID = "other-build" }},
		{name: "different dialect", mutate: func(snapshot *PreflightSnapshot) { snapshot.DatabaseDialect = "mysql" }},
		{name: "different migration", mutate: func(snapshot *PreflightSnapshot) { snapshot.MigrationDigest = strings.Repeat("c", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := automaticPreflightForTest()
			test.mutate(&snapshot)
			if err := manifest.AllowsAutomaticUpgrade(snapshot); err == nil {
				t.Fatal("undeclared installation state was accepted")
			}
		})
	}
	manual := automaticManifestForTest()
	manual.Compatibility = nil
	if err := manual.AllowsAutomaticUpgrade(automaticPreflightForTest()); err == nil {
		t.Fatal("release without compatibility declaration was accepted automatically")
	}
}

func TestManifestRejectsMalformedAutomaticCompatibility(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*Manifest)
	}{
		{name: "missing source builds", mutate: func(manifest *Manifest) { manifest.Compatibility.SourceBuilds = nil }},
		{name: "noncanonical dialect", mutate: func(manifest *Manifest) { manifest.Compatibility.Database.Dialect = "postgres" }},
		{name: "invalid source digest", mutate: func(manifest *Manifest) {
			manifest.Compatibility.Database.AllowedSourceMigrationDigests = []string{"not-a-digest"}
		}},
		{name: "duplicate source build", mutate: func(manifest *Manifest) {
			manifest.Compatibility.SourceBuilds = append(manifest.Compatibility.SourceBuilds, manifest.Compatibility.SourceBuilds[0])
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest := automaticManifestForTest()
			test.mutate(&manifest)
			payload, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			envelope := signedEnvelopeForTest(t, string(payload), "release-key-1", ed25519.Sign(privateKey, payload))
			if _, err := VerifySignedManifest(envelope, map[string]ed25519.PublicKey{"release-key-1": publicKey}); err == nil {
				t.Fatal("malformed compatibility declaration was accepted")
			}
		})
	}
}

func TestVerifySignedManifestRejectsTamperingUnknownKeyAndDuplicateFields(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"schema_version":1,"channel":"stable","sequence":7,"release_id":"r-7","version":"1.2.3","build_id":"build-7","min_updater_protocol":1,"artifacts":[]}`
	signature := ed25519.Sign(privateKey, []byte(payload))

	t.Run("tampered payload", func(t *testing.T) {
		envelope := signedEnvelopeForTest(t, payload+" ", "release-key-1", signature)
		if _, err := VerifySignedManifest(envelope, map[string]ed25519.PublicKey{"release-key-1": publicKey}); err == nil {
			t.Fatal("tampered payload was accepted")
		}
	})
	t.Run("unknown key", func(t *testing.T) {
		envelope := signedEnvelopeForTest(t, payload, "unknown", signature)
		if _, err := VerifySignedManifest(envelope, map[string]ed25519.PublicKey{"release-key-1": publicKey}); err == nil {
			t.Fatal("unknown key was accepted")
		}
	})
	t.Run("duplicate field", func(t *testing.T) {
		duplicate := strings.Replace(payload, `"sequence":7`, `"sequence":7,"sequence":8`, 1)
		envelope := signedEnvelopeForTest(t, duplicate, "release-key-1", ed25519.Sign(privateKey, []byte(duplicate)))
		if _, err := VerifySignedManifest(envelope, map[string]ed25519.PublicKey{"release-key-1": publicKey}); err == nil {
			t.Fatal("duplicate field was accepted")
		}
	})
}

func TestManifestRejectsInvalidReleaseIdentity(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		payload string
	}{
		{
			name:    "invalid version",
			payload: `{"schema_version":1,"channel":"stable","sequence":1,"release_id":"r-1","version":"latest","build_id":"b-1","min_updater_protocol":1,"artifacts":[{"deployment":"standalone","os":"linux","arch":"amd64","filename":"uvp.tar.gz","url":"https://updates.example.test/r-1/uvp.tar.gz","size":1,"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}]}`,
		},
		{
			name:    "empty sequence",
			payload: `{"schema_version":1,"channel":"stable","sequence":0,"release_id":"r-1","version":"1.0.0","build_id":"b-1","min_updater_protocol":1,"artifacts":[{"deployment":"standalone","os":"linux","arch":"amd64","filename":"uvp.tar.gz","url":"https://updates.example.test/r-1/uvp.tar.gz","size":1,"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}]}`,
		},
		{
			name:    "missing artifacts",
			payload: `{"schema_version":1,"channel":"stable","sequence":1,"release_id":"r-1","version":"1.0.0","build_id":"b-1","min_updater_protocol":1,"artifacts":[]}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			envelope := signedEnvelopeForTest(t, test.payload, "release-key-1", ed25519.Sign(privateKey, []byte(test.payload)))
			if _, err := VerifySignedManifest(envelope, map[string]ed25519.PublicKey{"release-key-1": publicKey}); err == nil {
				t.Fatal("invalid release identity was accepted")
			}
		})
	}
}

func TestManifestArtifactForChecksTargetAndDuplicates(t *testing.T) {
	manifest := Manifest{
		MinUpdaterProtocol: 2,
		Artifacts:          []Artifact{{Deployment: "compose", OS: "linux", Arch: "arm64", Filename: "uvp.tar", URL: "https://updates.example.test/r/uvp.tar", SHA256: strings.Repeat("a", 64)}},
	}
	artifact, err := manifest.ArtifactFor(Target{Deployment: "compose", OS: "linux", Arch: "arm64", UpdaterProtocol: 2})
	if err != nil || artifact.Filename != "uvp.tar" {
		t.Fatalf("artifact lookup: %#v, %v", artifact, err)
	}
	if _, err := manifest.ArtifactFor(Target{Deployment: "compose", OS: "linux", Arch: "arm64", UpdaterProtocol: 1}); err == nil {
		t.Fatal("older updater protocol was accepted")
	}
	if _, err := manifest.ArtifactFor(Target{Deployment: "systemd", OS: "linux", Arch: "arm64", UpdaterProtocol: 2}); err == nil {
		t.Fatal("unsupported deployment was accepted")
	}
	manifest.Artifacts = append(manifest.Artifacts, manifest.Artifacts[0])
	if _, err := manifest.ArtifactFor(Target{Deployment: "compose", OS: "linux", Arch: "arm64", UpdaterProtocol: 2}); err == nil {
		t.Fatal("duplicate target artifact was accepted")
	}
}

func TestManifestAcceptsReleaseMetadataAndRejectsInvalidPublishedAt(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"schema_version":1,"channel":"stable","sequence":8,"release_id":"r-8","version":"1.2.4","build_id":"build-8","published_at":"2026-10-16T02:00:00Z","release_notes":"修复升级流程","min_version":"1.2.0","min_updater_protocol":1,"artifacts":[{"deployment":"standalone","os":"linux","arch":"amd64","filename":"uvp.tar.gz","url":"https://updates.example.test/r-8/uvp.tar.gz","size":1,"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}]}`
	envelope := signedEnvelopeForTest(t, payload, "release-key-1", ed25519.Sign(privateKey, []byte(payload)))
	manifest, err := VerifySignedManifest(envelope, map[string]ed25519.PublicKey{"release-key-1": publicKey})
	if err != nil || manifest.ReleaseNotes != "修复升级流程" || manifest.MinVersion != "1.2.0" {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
	invalid := strings.Replace(payload, "2026-10-16T02:00:00Z", "tomorrow", 1)
	invalidEnvelope := signedEnvelopeForTest(t, invalid, "release-key-1", ed25519.Sign(privateKey, []byte(invalid)))
	if _, err := VerifySignedManifest(invalidEnvelope, map[string]ed25519.PublicKey{"release-key-1": publicKey}); err == nil {
		t.Fatal("invalid published_at was accepted")
	}
}

func TestIsVersionAtLeastHandlesReleaseCandidates(t *testing.T) {
	for _, test := range []struct {
		current string
		minimum string
		want    bool
	}{
		{"1.2.3", "1.2.3", true},
		{"1.2.3", "1.2.4", false},
		{"1.2.3", "1.1.9", true},
		{"1.2.3", "1.2.3-rc.9", true},
		{"1.2.3-rc.2", "1.2.3-rc.3", false},
		{"1.2.3-rc.2", "1.2.3", false},
		{"garbage", "1.0.0", false},
	} {
		t.Run(test.current+"_from_"+test.minimum, func(t *testing.T) {
			if got := IsVersionAtLeast(test.current, test.minimum); got != test.want {
				t.Fatalf("IsVersionAtLeast(%q, %q) = %v, want %v", test.current, test.minimum, got, test.want)
			}
		})
	}
}

func signedEnvelopeForTest(t *testing.T, payload, keyID string, signature []byte) []byte {
	t.Helper()
	envelope, err := json.Marshal(SignedManifest{
		Payload:    base64.StdEncoding.EncodeToString([]byte(payload)),
		Signatures: []ManifestSignature{{KeyID: keyID, Signature: base64.StdEncoding.EncodeToString(signature)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func automaticManifestForTest() Manifest {
	return Manifest{
		SchemaVersion:      1,
		Channel:            "stable",
		Sequence:           1,
		ReleaseID:          "r1",
		Version:            "1.2.0",
		BuildID:            "build-1",
		MinUpdaterProtocol: 1,
		Artifacts: []Artifact{{
			Deployment: "standalone",
			OS:         "linux",
			Arch:       "amd64",
			Filename:   "x.tar.gz",
			URL:        "https://updates.example/x",
			SHA256:     strings.Repeat("a", 64),
		}},
		Compatibility: automaticCompatibilityForTest(),
	}
}

func automaticCompatibilityForTest() *CompatibilityDeclaration {
	return &CompatibilityDeclaration{
		SchemaVersion:    1,
		AutomaticAllowed: true,
		SourceBuilds:     []ReleaseIdentity{{Version: "1.1.0", BuildID: "installed-build-1"}},
		Database: DatabaseCompatibility{
			Dialect:                       "sqlite",
			AllowedSourceMigrationDigests: []string{strings.Repeat("a", 64)},
			TargetMigrationDigest:         strings.Repeat("b", 64),
		},
		Rollback: &RollbackCompatibility{AutomaticAllowed: true, DatabaseCompatible: true, DependenciesCompatible: true},
	}
}

func automaticPreflightForTest() PreflightSnapshot {
	return PreflightSnapshot{
		CurrentVersion:  "1.1.0",
		CurrentBuildID:  "installed-build-1",
		ConfigDigest:    "config-a",
		DatabaseDialect: "sqlite",
		MigrationDigest: strings.Repeat("a", 64),
	}
}
