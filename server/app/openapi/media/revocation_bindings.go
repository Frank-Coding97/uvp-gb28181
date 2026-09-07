package media

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

var ErrRevocationBindingsUnavailable = errors.New("openapi revocation bindings unavailable")

type FileNodeControlBindings struct {
	bindings map[string]NodeControlBinding
}

type nodeControlFile struct {
	Version int                `yaml:"version"`
	Nodes   []nodeControlEntry `yaml:"nodes"`
}

type nodeControlEntry struct {
	NodeID          int64  `yaml:"node_id"`
	NodeUUID        string `yaml:"node_uuid"`
	BindingRevision uint64 `yaml:"binding_revision"`
	Enabled         bool   `yaml:"enabled"`
	Endpoint        string `yaml:"endpoint"`
	CAMode          string `yaml:"ca_mode"`
	CAPEM           string `yaml:"ca_pem"`
	SPKISHA256      string `yaml:"spki_sha256"`
	HookBase        string `yaml:"hook_base"`
}

// LoadNodeControlBindings takes one bounded startup snapshot. The deployment
// must protect this file AND its parent directory; this is not an ACL audit.
// There is no hot reload, path discovery, default certificate, or node fallback.
func LoadNodeControlBindings(path string) (*FileNodeControlBindings, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrRevocationBindingsUnavailable
	}
	before, err := os.Stat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0022 != 0 {
		return nil, ErrRevocationBindingsUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrRevocationBindingsUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || !os.SameFile(before, info) {
		return nil, ErrRevocationBindingsUnavailable
	}
	const maxBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return nil, ErrRevocationBindingsUnavailable
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document nodeControlFile
	if decoder.Decode(&document) != nil || document.Version != 1 || len(document.Nodes) == 0 || len(document.Nodes) > 128 {
		return nil, ErrRevocationBindingsUnavailable
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, ErrRevocationBindingsUnavailable
	}
	provider := &FileNodeControlBindings{bindings: make(map[string]NodeControlBinding, len(document.Nodes))}
	ids := make(map[int64]bool, len(document.Nodes))
	for _, entry := range document.Nodes {
		if entry.NodeID <= 0 || !validBindingNodeUUID(entry.NodeUUID) || entry.BindingRevision == 0 || ids[entry.NodeID] || !validBindingURL(entry.Endpoint, true) || !validBindingURL(entry.HookBase, false) {
			return nil, ErrRevocationBindingsUnavailable
		}
		if _, exists := provider.bindings[entry.NodeUUID]; exists {
			return nil, ErrRevocationBindingsUnavailable
		}
		pin, err := hex.DecodeString(entry.SPKISHA256)
		if err != nil || len(pin) != 32 {
			return nil, ErrRevocationBindingsUnavailable
		}
		binding := NodeControlBinding{NodeID: entry.NodeID, NodeUUID: entry.NodeUUID, BindingRevision: entry.BindingRevision, Enabled: entry.Enabled, HookBase: entry.HookBase,
			TLS: zlm.OpenAPIControlTLS{Endpoint: entry.Endpoint, SPKISHA256: [32]byte(pin)}}
		if binding.TLS.SPKISHA256 == ([32]byte{}) {
			return nil, ErrRevocationBindingsUnavailable
		}
		switch entry.CAMode {
		case "system":
			if entry.CAPEM != "" {
				return nil, ErrRevocationBindingsUnavailable
			}
			binding.UseSystemRoots = true
		case "private":
			roots, err := bindingRoots(entry.CAPEM)
			if err != nil {
				return nil, err
			}
			binding.TLS.Roots = roots
		default:
			return nil, ErrRevocationBindingsUnavailable
		}
		provider.bindings[entry.NodeUUID] = binding
		ids[entry.NodeID] = true
	}
	return provider, nil
}

func (p *FileNodeControlBindings) Lookup(ctx context.Context, uuid string) (NodeControlBinding, error) {
	if p == nil || ctx == nil || ctx.Err() != nil {
		return NodeControlBinding{}, ErrRevocationBindingsUnavailable
	}
	binding, found := p.bindings[uuid]
	if !found {
		return NodeControlBinding{}, ErrRevocationBindingsUnavailable
	}
	if binding.TLS.Roots != nil {
		binding.TLS.Roots = binding.TLS.Roots.Clone()
	}
	return binding, nil
}

func validBindingURL(value string, control bool) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawPath == "" && u.Opaque == "" && u.String() == value && (!control || u.Path == "/index/api")
}

func validBindingNodeUUID(value string) bool {
	if value == "" || len(value) > 64 || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	return strings.IndexFunc(value, unicode.IsControl) == -1
}

func bindingRoots(value string) (*x509.CertPool, error) {
	remaining := bytes.TrimSpace([]byte(value))
	if len(remaining) == 0 {
		return nil, ErrRevocationBindingsUnavailable
	}
	roots := x509.NewCertPool()
	for len(remaining) > 0 {
		if !bytes.HasPrefix(remaining, []byte("-----BEGIN CERTIFICATE-----")) {
			return nil, ErrRevocationBindingsUnavailable
		}
		block, rest := pem.Decode(remaining)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, ErrRevocationBindingsUnavailable
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, ErrRevocationBindingsUnavailable
		}
		roots.AddCert(cert)
		remaining = bytes.TrimSpace(rest)
	}
	return roots, nil
}
