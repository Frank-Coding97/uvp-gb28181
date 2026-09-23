package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNodeControlBindingsExplicitQualification(t *testing.T) {
	doc := bindingFileFixture(t)
	body, err := yaml.Marshal(doc)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, yaml.Unmarshal(body, &raw))
	nodes := raw["nodes"].([]any)
	nodes[0].(map[string]any)["qualification"] = map[string]any{
		"id":            "aef3e620-28b8-4ba3-a19c-378387d86a15",
		"topology":      "direct-http1",
		"media_origins": map[string]string{"https-flv": "https://media.example.test", "wss-flv": "wss://media.example.test"},
	}
	body, err = yaml.Marshal(raw)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "qualified.yml")
	require.NoError(t, os.WriteFile(path, body, 0600))
	p, err := LoadNodeControlBindings(path)
	require.NoError(t, err, "explicit deployment qualification belongs to the same immutable trust file")
	q, err := p.Qualification(context.Background(), "node-a", "https-flv")
	require.NoError(t, err)
	require.Equal(t, uint64(3), q.BindingRevision)
	require.Equal(t, "https://media.example.test", q.MediaOrigin)
	q.MediaOrigin = "https://changed.invalid"
	require.NoError(t, os.WriteFile(path, []byte("invalid now"), 0600))
	next, err := p.Qualification(context.Background(), "node-a", "https-flv")
	require.NoError(t, err)
	require.Equal(t, "https://media.example.test", next.MediaOrigin)
	_, err = p.Qualification(context.Background(), "node-a", "rtsp")
	require.Error(t, err)
}

func TestQualificationBindingRejectsMissingOrInvalidDeclaration(t *testing.T) {
	for _, kind := range []string{"missing", "disabled", "empty-id", "nil-id", "topology", "empty-protocols", "unknown-protocol", "wrong-scheme", "credentials", "path", "query", "fragment"} {
		t.Run(kind, func(t *testing.T) {
			doc := bindingFileFixture(t)
			q := &deploymentQualification{ID: "aef3e620-28b8-4ba3-a19c-378387d86a15", Topology: "direct-http1", MediaOrigins: map[string]string{"https-flv": "https://media.example.test"}}
			doc.Nodes[0].Qualification = q
			switch kind {
			case "missing":
				doc.Nodes[0].Qualification = nil
			case "disabled":
				doc.Nodes[0].Enabled = false
			case "empty-id":
				q.ID = ""
			case "nil-id":
				q.ID = "00000000-0000-0000-0000-000000000000"
			case "topology":
				q.Topology = "load-balanced"
			case "empty-protocols":
				q.MediaOrigins = nil
			case "unknown-protocol":
				q.MediaOrigins["rtsp"] = "rtsp://media.example.test"
			case "wrong-scheme":
				q.MediaOrigins["https-flv"] = "http://media.example.test"
			case "credentials":
				q.MediaOrigins["https-flv"] = "https://user:secret@media.example.test"
			case "path":
				q.MediaOrigins["https-flv"] += "/rtp"
			case "query":
				q.MediaOrigins["https-flv"] += "?secret=hidden"
			case "fragment":
				q.MediaOrigins["https-flv"] += "#fragment"
			}
			p, err := LoadNodeControlBindings(writeBindingFile(t, doc))
			if kind == "missing" || kind == "disabled" {
				require.NoError(t, err, "legacy cleanup-only bindings remain supported")
				_, err = p.Qualification(context.Background(), "node-a", "https-flv")
			} else {
				require.Nil(t, p)
			}
			require.ErrorIs(t, err, ErrRevocationBindingsUnavailable)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}
