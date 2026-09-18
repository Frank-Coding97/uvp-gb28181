package media

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func bindingFileFixture(t *testing.T) nodeControlFile {
	t.Helper()
	server := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	pin := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	return nodeControlFile{Version: 1, Nodes: []nodeControlEntry{{NodeID: 1, NodeUUID: "node-a", BindingRevision: 3, Enabled: true,
		Endpoint: "https://control.example.test/index/api", CAMode: "private", CAPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})),
		SPKISHA256: hex.EncodeToString(pin[:]), HookBase: "https://hooks.example.test/hook"}}}
}

func writeBindingFile(t *testing.T, document nodeControlFile) string {
	t.Helper()
	data, err := yaml.Marshal(document)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "bindings.yml")
	require.NoError(t, os.WriteFile(path, data, 0600))
	return path
}

func TestNodeControlBindingsLoadImmutableSnapshots(t *testing.T) {
	document := bindingFileFixture(t)
	path := writeBindingFile(t, document)
	p, err := LoadNodeControlBindings(path)
	require.NoError(t, err)
	got, err := p.Lookup(context.Background(), "node-a")
	require.NoError(t, err)
	require.Equal(t, uint64(3), got.BindingRevision)
	require.False(t, got.UseSystemRoots)
	require.NotNil(t, got.TLS.Roots)
	wantRoots := got.TLS.Roots.Clone()
	got.TLS.Roots.AddCert(&x509.Certificate{Raw: []byte("other fixture"), RawSubject: []byte("other")})
	got.TLS.Endpoint = "https://other.example.test/index/api"
	require.NoError(t, os.WriteFile(path, []byte("invalid now"), 0600))
	next, err := p.Lookup(context.Background(), "node-a")
	require.NoError(t, err)
	require.Equal(t, document.Nodes[0].Endpoint, next.TLS.Endpoint)
	require.True(t, next.TLS.Roots.Equal(wantRoots), "returned roots must not mutate the provider")
	_, err = p.Lookup(context.Background(), "node-b")
	require.ErrorIs(t, err, ErrRevocationBindingsUnavailable)
}

func TestNodeControlBindingsRejectInvalidConfig(t *testing.T) {
	for _, kind := range []string{"version", "empty", "duplicate-uuid", "duplicate-id", "node-id", "revision", "uuid", "http", "endpoint-query", "pin", "zero-pin", "ca-mode", "ca", "system-with-ca", "hook-http", "hook-query"} {
		t.Run(kind, func(t *testing.T) {
			doc := bindingFileFixture(t)
			n := &doc.Nodes[0]
			switch kind {
			case "version":
				doc.Version = 2
			case "empty":
				doc.Nodes = nil
			case "duplicate-uuid":
				extra := *n
				extra.NodeID = 2
				doc.Nodes = append(doc.Nodes, extra)
			case "duplicate-id":
				extra := *n
				extra.NodeUUID = "node-b"
				doc.Nodes = append(doc.Nodes, extra)
			case "node-id":
				n.NodeID = 0
			case "revision":
				n.BindingRevision = 0
			case "uuid":
				n.NodeUUID = " node-a "
			case "http":
				n.Endpoint = "http://control.example.test/index/api"
			case "endpoint-query":
				n.Endpoint += "?secret=fixture-secret"
			case "pin":
				n.SPKISHA256 = "no-pin"
			case "zero-pin":
				n.SPKISHA256 = strings.Repeat("0", 64)
			case "ca-mode":
				n.CAMode = "auto"
			case "ca":
				n.CAPEM = "not-a-certificate"
			case "system-with-ca":
				n.CAMode = "system"
			case "hook-http":
				n.HookBase = "http://hooks.example.test/hook"
			case "hook-query":
				n.HookBase += "?anything=1"
			}
			p, err := LoadNodeControlBindings(writeBindingFile(t, doc))
			require.ErrorIs(t, err, ErrRevocationBindingsUnavailable)
			require.Nil(t, p)
			require.NotContains(t, err.Error(), "fixture-secret")
		})
	}
}

func TestNodeControlBindingsStrictFileBoundary(t *testing.T) {
	for _, kind := range []string{"unknown", "duplicate-key", "extra-document", "too-large", "writable", "relative", "missing", "directory"} {
		t.Run(kind, func(t *testing.T) {
			path := writeBindingFile(t, bindingFileFixture(t))
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			switch kind {
			case "unknown":
				data = append(data, []byte("api_secret: forbidden\n")...)
			case "duplicate-key":
				data = append(data, []byte("version: 1\n")...)
			case "extra-document":
				data = append(data, []byte("---\nversion: 1\n")...)
			case "too-large":
				data = []byte(strings.Repeat(" ", (1<<20)+1))
			case "writable":
				require.NoError(t, os.Chmod(path, 0666))
			case "relative":
				path = "bindings.yml"
			case "missing":
				path = filepath.Join(t.TempDir(), "absent.yml")
			case "directory":
				path = t.TempDir()
			}
			if kind == "unknown" || kind == "duplicate-key" || kind == "extra-document" || kind == "too-large" {
				require.NoError(t, os.WriteFile(path, data, 0600))
			}
			_, err = LoadNodeControlBindings(path)
			require.ErrorIs(t, err, ErrRevocationBindingsUnavailable)
		})
	}
}

func TestNodeControlBindingsExplicitSystemRootsAndDisabledEntry(t *testing.T) {
	doc := bindingFileFixture(t)
	doc.Nodes[0].CAMode, doc.Nodes[0].CAPEM, doc.Nodes[0].Enabled = "system", "", false
	p, err := LoadNodeControlBindings(writeBindingFile(t, doc))
	require.NoError(t, err)
	got, err := p.Lookup(context.Background(), "node-a")
	require.NoError(t, err)
	require.True(t, got.UseSystemRoots)
	require.Nil(t, got.TLS.Roots)
	require.False(t, got.Enabled)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Lookup(ctx, "node-a")
	require.ErrorIs(t, err, ErrRevocationBindingsUnavailable)
}
