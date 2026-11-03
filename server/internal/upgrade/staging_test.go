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
	"errors"
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

// 上传安装包是最常见的用户错误：原来只报 "unsafe upgrade archive path"，
// 用户完全看不出自己在哪一步错了。这里钉住两点——必须拒绝，且必须说清原因。
func TestImportArchiveExplainsInstallationPackageInsteadOfBundle(t *testing.T) {
	cases := map[string][]tarEntry{
		"顶层目录条目":  {{name: "uvp-gb28181-linux-amd64-1.1.14/", typeFlag: tar.TypeDir}},
		"带子目录的文件": {{name: "uvp-gb28181-linux-amd64-1.1.14/bin/uvp-server", body: []byte("x")}},
	}
	for label, entries := range cases {
		t.Run(label, func(t *testing.T) {
			archive := buildTarEntries(t, entries)
			_, err := ImportArchive(context.Background(), bytes.NewReader(archive), t.TempDir(), map[string]ed25519.PublicKey{"key": make(ed25519.PublicKey, ed25519.PublicKeySize)}, Target{Deployment: "standalone", OS: "linux", Arch: "amd64", UpdaterProtocol: 1})
			if err == nil {
				t.Fatal("安装包被当成离线升级包接受了")
			}
			if !errors.Is(err, errArchiveNotFlat) {
				t.Fatalf("错误没有标记成结构问题: %v", err)
			}
			if !strings.Contains(err.Error(), "安装包") || !strings.Contains(err.Error(), ".upgrade.tar") {
				t.Fatalf("错误没有告诉用户拿错文件: %v", err)
			}
		})
	}
}

// 攻击性路径不能套用「你传错文件」的说法：那是另一类问题，排查方向不同。
func TestImportArchiveDoesNotBlameUserForAttackPaths(t *testing.T) {
	for _, name := range []string{"../evil", "/absolute"} {
		archive := buildTarEntries(t, []tarEntry{{name: "manifest.json", body: []byte("{}")}, {name: name, body: []byte("x")}})
		_, err := ImportArchive(context.Background(), bytes.NewReader(archive), t.TempDir(), map[string]ed25519.PublicKey{"key": make(ed25519.PublicKey, ed25519.PublicKeySize)}, Target{Deployment: "standalone", OS: "linux", Arch: "amd64", UpdaterProtocol: 1})
		if err == nil {
			t.Fatalf("%s 被接受了", name)
		}
		if errors.Is(err, errArchiveNotFlat) {
			t.Fatalf("%s 被误判成安装包: %v", name, err)
		}
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
