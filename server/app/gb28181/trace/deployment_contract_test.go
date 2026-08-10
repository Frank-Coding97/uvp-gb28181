package trace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRelationalTraceDeploymentContract(t *testing.T) {
	serverRoot := filepath.Join("..", "..", "..")
	repoRoot := filepath.Join(serverRoot, "..")
	forbidden := strings.Join([]string{"click", "house"}, "")
	paths := []string{
		filepath.Join(serverRoot, "go.mod"),
		filepath.Join(serverRoot, "config", "config.example.yml"),
		filepath.Join(repoRoot, "deploy", "test", "compose.yml"),
		filepath.Join(repoRoot, "deploy", "test", "configure_server.py"),
		filepath.Join(repoRoot, "deploy", "test", "deploy-uvp.sh"),
		filepath.Join(repoRoot, ".github", "workflows", "ci-deploy-test.yml"),
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NotContains(t, strings.ToLower(string(body)), forbidden, path)
	}
	_, err := os.Stat(filepath.Join(serverRoot, "deploy", "sip-trace-"+forbidden))
	require.ErrorIs(t, err, os.ErrNotExist)

	schema, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "uvp-gb28181.sql"))
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(string(schema)), "gb_sip_trace_message")
}
