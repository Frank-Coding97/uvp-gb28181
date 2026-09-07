package ymlconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateYamlFactoryFromFileReadsExactFile(t *testing.T) {
	root := t.TempDir()
	configFile := filepath.Join(root, "配置 目录", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configFile), 0o755))
	require.NoError(t, os.WriteFile(configFile, []byte("server:\n  marker: exact-file\n  appdebug: false\n"), 0o600))

	config := CreateYamlFactoryFromFile(configFile)

	require.False(t, config.GetBool("server.appdebug"))
	require.Equal(t, "exact-file", config.GetString("server.marker"))
}

func TestLegacyDirectoryFactoryStillLoadsConfig(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "config.yml"), []byte("server:\n  marker: legacy-directory\n"), 0600))
	config := CreateYamlFactory(root)
	require.Equal(t, "legacy-directory", config.GetString("server.marker"))
}
