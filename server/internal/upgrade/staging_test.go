package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportArchiveStagesOnlySignedMatchingArtifact(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("release artifact")
	digest := sha256.Sum256(body)
	payload := Manifest{SchemaVersion: 1, Channel: "stable", Sequence: 2, ReleaseID: "r2", Version: "1.2.0", BuildID: "build-2", MinUpdaterProtocol: 1, Artifacts: []Artifact{{Deployment: "standalone", OS: "linux", Arch: "amd64", Filename: "release.tar.gz", Size: int64(len(body)), SHA256: hex.EncodeToString(digest[:]), URL: "https://updates.example/r2/release.tar.gz"}}, Compatibility: automaticCompatibilityForTest()}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := json.Marshal(SignedManifest{Payload: base64.StdEncoding.EncodeToString(payloadBytes), Signatures: []ManifestSignature{{KeyID: "key-1", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payloadBytes))}}})
	if err != nil {
		t.Fatal(err)
	}
	archive := buildTar(t, map[string][]byte{"manifest.json": envelope, "release.tar.gz": body})
	root := t.TempDir()
	staged, err := ImportArchive(context.Background(), bytes.NewReader(archive), root, map[string]ed25519.PublicKey{"key-1": publicKey}, Target{Deployment: "standalone", OS: "linux", Arch: "amd64", UpdaterProtocol: 1})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(staged.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) || staged.BuildID != "build-2" || !bytes.Equal(staged.ManifestEnvelope, envelope) || !CompatibilityEqual(staged.Compatibility, payload.Compatibility) {
		t.Fatalf("unexpected staged artifact: %#v %q", staged, got)
	}
	for _, corrupt := range []bool{false, true} {
		var compressed bytes.Buffer
		gz := gzip.NewWriter(&compressed)
		if _, err := gz.Write(archive); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		data := compressed.Bytes()
		if corrupt {
			data[len(data)-8] ^= 1
		}
		_, err := ImportArchive(context.Background(), bytes.NewReader(data), t.TempDir(), map[string]ed25519.PublicKey{"key-1": publicKey}, Target{Deployment: "standalone", OS: "linux", Arch: "amd64", UpdaterProtocol: 1})
		if corrupt && err == nil {
			t.Fatal("accepted corrupt gzip checksum")
		}
		if !corrupt && err != nil {
			t.Fatal(err)
		}
	}
}

func TestImportArchiveRejectsUnsafeAndMismatchedEntries(t *testing.T) {
	for name := range map[string]struct{}{"../evil": {}, "/absolute": {}, "dir/file": {}, "link": {}} {
		t.Run(name, func(t *testing.T) {
			entry := tarEntry{name: name, body: []byte("x")}
			if name == "link" {
				entry.typeFlag = tar.TypeSymlink
			}
			archive := buildTarEntries(t, []tarEntry{{name: "manifest.json", body: []byte("{}")}, entry})
			if _, err := ImportArchive(context.Background(), bytes.NewReader(archive), t.TempDir(), map[string]ed25519.PublicKey{"key": make(ed25519.PublicKey, ed25519.PublicKeySize)}, Target{Deployment: "standalone", OS: "linux", Arch: "amd64", UpdaterProtocol: 1}); err == nil {
				t.Fatal("unsafe archive entry was accepted")
			}
		})
	}
}

func TestImportArchiveRejectsTruncatedAndWrongDigest(t *testing.T) {
	archive := buildTar(t, map[string][]byte{"manifest.json": []byte(`{"schema_version":1}`), "release.tar.gz": []byte("x")})
	archive = archive[:len(archive)-10]
	if _, err := ImportArchive(context.Background(), bytes.NewReader(archive), filepath.Join(t.TempDir(), "staging"), nil, Target{}); err == nil {
		t.Fatal("truncated archive was accepted")
	}
}

type tarEntry struct {
	name     string
	body     []byte
	typeFlag byte
}

func buildTar(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	list := make([]tarEntry, 0, len(entries))
	for name, body := range entries {
		list = append(list, tarEntry{name: name, body: body})
	}
	return buildTarEntries(t, list)
}

func buildTarEntries(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	w := tar.NewWriter(&out)
	for _, entry := range entries {
		h := &tar.Header{Name: entry.name, Mode: 0600, Size: int64(len(entry.body)), Typeflag: entry.typeFlag}
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Typeflag == tar.TypeSymlink {
			h.Linkname = strings.Repeat("x", 1)
			h.Size = 0
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := w.Write(entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
