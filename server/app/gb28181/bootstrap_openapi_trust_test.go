package gb28181

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	openapimedia "uvplatform.cn/uvp-gb28181/app/openapi/media"
)

type startupTrustConfig struct {
	shutdownRootConfig
	path  string
	reads atomic.Int32
}

func (c *startupTrustConfig) GetString(key string) string {
	if key == "openapi.revocation_bindings_file" {
		c.reads.Add(1)
		return c.path
	}
	return c.shutdownRootConfig.GetString(key)
}

// Each mode gets a real new process. No test-only reset can accidentally become
// a production trust replacement path, and test order cannot mask once state.
func TestOpenAPIStartupTrustIsProcessImmutable(t *testing.T) {
	mode := os.Getenv("UVP_TRUST_ONCE_CHILD")
	if mode == "" {
		for _, mode := range []string{"success", "invalid", "absent"} {
			t.Run(mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOpenAPIStartupTrustIsProcessImmutable$")
				cmd.Env = append(os.Environ(), "UVP_TRUST_ONCE_CHILD="+mode)
				out, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", out)
			})
		}
		return
	}
	path := filepath.Join(t.TempDir(), "trust.yml")
	valid := []byte(`version: 1
nodes:
  - node_id: 1
    node_uuid: node-a
    binding_revision: 3
    enabled: true
    endpoint: https://control.example.test/index/api
    ca_mode: system
    spki_sha256: 1111111111111111111111111111111111111111111111111111111111111111
    hook_base: https://hooks.example.test/hook
`)
	require.NoError(t, os.WriteFile(path, valid, 0600))
	cfg := &startupTrustConfig{path: path}
	if mode == "invalid" {
		require.NoError(t, os.WriteFile(path, []byte("invalid"), 0600))
	} else if mode == "absent" {
		cfg.path = ""
	}
	app.ConfigYml = cfg
	first, firstErr := LoadStartupOpenAPIControlBindingsOnce()
	switch mode {
	case "success":
		require.NoError(t, firstErr)
		require.NotNil(t, first)
	case "invalid":
		require.ErrorIs(t, firstErr, openapimedia.ErrRevocationBindingsUnavailable)
		require.Nil(t, first)
	case "absent":
		require.ErrorIs(t, firstErr, openapimedia.ErrRevocationNotConfigured)
		require.Nil(t, first)
	}
	// Repair failure / introduce absent configuration, or corrupt a good file.
	require.NoError(t, os.WriteFile(path, valid, 0600))
	cfg.path = path
	if mode == "success" {
		require.NoError(t, os.WriteFile(path, []byte("invalid now"), 0600))
	}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := LoadStartupOpenAPIControlBindingsOnce()
			if got != first || err != firstErr {
				t.Error("startup result changed within the process")
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), cfg.reads.Load())
	if first != nil {
		binding, err := first.Lookup(context.Background(), "node-a")
		require.NoError(t, err)
		require.Equal(t, uint64(3), binding.BindingRevision)
		require.Equal(t, "https://control.example.test/index/api", binding.TLS.Endpoint)
	}
}
