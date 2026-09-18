package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Deployment-file contracts do not attest to a live host's permissions or FS.
func TestProcessAuthorityDeploymentUsesDedicatedPersistentDirectory(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	config, err := os.ReadFile(filepath.Join(root, "server/config/config.example.yml"))
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, yaml.Unmarshal(config, &settings))
	require.Equal(t, "", settings["processauthority"].(map[string]any)["state_dir"], "no insecure default directory")
	data, err := os.ReadFile(filepath.Join(root, "deploy/test/compose.yml"))
	require.NoError(t, err)
	var compose struct {
		Services map[string]struct {
			Volumes []any `yaml:"volumes"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(data, &compose))
	found := false
	for _, volume := range compose.Services["backend"].Volumes {
		mount, ok := volume.(map[string]any)
		if !ok || mount["target"] != "/var/lib/uvp/process-authority" {
			continue
		}
		found = true
		require.Equal(t, "bind", mount["type"])
		require.Equal(t, "${UVP_ROOT:-/opt/uvp-gb28181}/data/process-authority", mount["source"])
		require.Equal(t, false, mount["bind"].(map[string]any)["create_host_path"])
	}
	require.True(t, found, "authority must have its own persistent private mount")
	deploy, err := os.ReadFile(filepath.Join(root, "deploy/test/deploy-uvp.sh"))
	require.NoError(t, err)
	source := string(deploy)
	gate := strings.Index(source, `stat -c '%a:%u:%g' "$ROOT/data/process-authority"`)
	require.GreaterOrEqual(t, gate, 0)
	require.Less(t, gate, strings.Index(source, `install -d -m 0755 "$RELEASES"`))
	require.Contains(t, source, `! -L "$ROOT/data/process-authority"`)
}
