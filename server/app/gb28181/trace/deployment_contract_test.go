package trace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptionalClickHouseDeploymentContract(t *testing.T) {
	root := filepath.Join("..", "..", "..", "deploy", "sip-trace-clickhouse")
	read := func(name string) string {
		content, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err, name)
		return string(content)
	}

	compose := read("compose.yml")
	require.Contains(t, compose, "clickhouse/clickhouse-server:26.3.17.4")
	require.Contains(t, compose, "healthcheck:")
	require.Contains(t, compose, "clickhouse-data:")
	require.Contains(t, compose, "CLICKHOUSE_BIND_IP")

	initScript := read("init/01-sip-trace.sh")
	initInfo, err := os.Stat(filepath.Join(root, "init", "01-sip-trace.sh"))
	require.NoError(t, err)
	require.NotZero(t, initInfo.Mode().Perm()&0o111)
	require.Contains(t, initScript, "CREATE DATABASE IF NOT EXISTS")
	require.Contains(t, initScript, "CREATE USER IF NOT EXISTS")
	require.Contains(t, initScript, "GRANT CREATE TABLE, CREATE VIEW, SELECT, INSERT")
	require.NotContains(t, strings.ToUpper(initScript), "GRANT ALL")

	envExample := read(".env.example")
	require.Contains(t, envExample, "CHANGE_ME")
	require.NotContains(t, envExample, "192.168.10.220")

	documentation := read("README.md")
	for _, required := range []string{"enabled: false", "openssl rand -base64 32", "7 天", "30 天", "70%", "85%", "恢复"} {
		require.Contains(t, documentation, required)
	}
}

func TestTestEnvironmentClickHouseDeploymentContract(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "deploy", "test")
	read := func(name string) string {
		content, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err, name)
		return string(content)
	}

	compose := read("compose.yml")
	require.Contains(t, compose, "sip-trace-backend.env")
	require.Contains(t, compose, "clickhouse/clickhouse-server:26.3.17.4")
	require.Contains(t, compose, "sip-trace-clickhouse.env")
	require.Contains(t, compose, "./clickhouse/init:/docker-entrypoint-initdb.d:ro")
	require.Contains(t, compose, "clickhouse-data:/var/lib/clickhouse")
	require.NotContains(t, compose, "8123:8123")
	require.NotContains(t, compose, "9000:9000")

	configure := read("configure_server.py")
	require.Contains(t, configure, "UVP_SIP_TRACE_CLICKHOUSE_PASSWORD")
	require.Contains(t, configure, "UVP_SIP_TRACE_ENCRYPTION_KEY")
	require.Contains(t, configure, `("gb28181", "trace", "enabled"): True`)
	require.Contains(t, configure, `("gb28181", "trace", "address"): "uvp-clickhouse:9000"`)

	deploy := read("deploy-uvp.sh")
	require.Contains(t, deploy, "clickhouse/init/01-sip-trace.sh")
}
