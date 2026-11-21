package upgrade

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func signChannelFixture(t *testing.T, private ed25519.PrivateKey, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := json.Marshal(SignedManifest{Payload: base64.StdEncoding.EncodeToString(payload), Signatures: []ManifestSignature{{KeyID: "test", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, payload))}}})
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func TestChannelReleaseRejectsExpiryTamperingAndMismatchedRelease(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"valid", "refreshed-pointer", "expired", "future", "wrong-digest", "wrong-channel", "wrong-release", "cross-host", "tampered-channel", "unsigned-release"} {
		t.Run(scenario, func(t *testing.T) {
			manifest := Manifest{SchemaVersion: 1, Channel: "stable", Sequence: 7, ReleaseID: "r7", Version: "1.2.3", BuildID: "b7", MinUpdaterProtocol: 1, Artifacts: []Artifact{{Deployment: "standalone", OS: "linux", Arch: "amd64", Filename: "app.tar.gz", URL: "https://updates.example/app.tar.gz", Size: 1, SHA256: strings.Repeat("a", 64)}}}
			if scenario == "wrong-channel" {
				manifest.Channel = "beta"
			}
			if scenario == "wrong-release" {
				manifest.ReleaseID = "r6"
			}
			release := signChannelFixture(t, private, manifest)
			if scenario == "unsigned-release" {
				release = []byte(`{}`)
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(release) }))
			defer server.Close()
			sum := sha256.Sum256(release)
			pointer := ChannelManifest{SchemaVersion: 1, Channel: "stable", Sequence: 7, ReleaseID: "r7", PublishedAt: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), ManifestURL: server.URL + "/releases/r7/manifest.json", ManifestSHA256: hex.EncodeToString(sum[:])}
			switch scenario {
			case "refreshed-pointer":
				pointer.Sequence = 8
			case "expired":
				pointer.ExpiresAt = now.Add(-time.Minute).Format(time.RFC3339)
			case "future":
				pointer.PublishedAt = now.Add(time.Hour).Format(time.RFC3339)
			case "wrong-digest":
				pointer.ManifestSHA256 = strings.Repeat("0", 64)
			case "cross-host":
				pointer.ManifestURL = "https://untrusted.example/manifest.json"
			}
			envelope := signChannelFixture(t, private, pointer)
			if scenario == "tampered-channel" {
				envelope = []byte(strings.Replace(string(envelope), "test", "unknown", 1))
			}
			manifestResult, channelResult, err := FetchChannelRelease(context.Background(), server.Client(), server.URL+"/channels/stable/manifest.json", envelope, map[string]ed25519.PublicKey{"test": public}, now)
			if scenario != "valid" && scenario != "refreshed-pointer" {
				if err == nil {
					t.Fatalf("accepted %s", scenario)
				}
				return
			}
			if err != nil || manifestResult.BuildID != "b7" || channelResult.Sequence != pointer.Sequence {
				t.Fatalf("release=%#v pointer=%#v err=%v", manifestResult, channelResult, err)
			}
		})
	}
}

func TestChannelManifestRejectsUnknownDuplicateAndMissingTime(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	for _, payload := range []string{`{"schema_version":1,"channel":"stable","sequence":1}`, `{"schema_version":1,"schema_version":1}`, `{"schema_version":1,"unknown":true}`} {
		envelope := signChannelFixture(t, private, json.RawMessage(payload))
		if _, err := VerifySignedChannel(envelope, map[string]ed25519.PublicKey{"test": public}, time.Now()); err == nil {
			t.Fatalf("accepted %s", payload)
		}
	}
}
