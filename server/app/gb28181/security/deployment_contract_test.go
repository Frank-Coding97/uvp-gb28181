package security

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFirewallAgentReleaseAndDeploymentContract(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(current), "../../../.."))
	read := func(path string) string {
		data, err := os.ReadFile(filepath.Join(repoRoot, path))
		require.NoError(t, err, path)
		return string(data)
	}

	workflow := read(".github/workflows/ci-deploy-test.yml")
	require.Contains(t, workflow, "uvp-firewall-agent-linux-amd64")
	require.Contains(t, workflow, "uvp-firewall-agent.service")

	compose := read("deploy/test/compose.yml")
	require.Contains(t, compose, "/run/uvp:/run/uvp")
	require.Contains(t, compose, `"0.0.0.0:56002:56002/tcp"`)
	require.Contains(t, compose, `"0.0.0.0:56002:56002/udp"`)

	deploy := read("deploy/test/deploy-uvp.sh")
	require.Contains(t, deploy, "agent-current")
	require.Contains(t, deploy, `install -m 0644 "$RELEASES/$PREVIOUS/agent/uvp-firewall-agent.service"`)
	require.Contains(t, deploy, "systemctl enable uvp-firewall-agent")
	require.Contains(t, deploy, "systemctl restart uvp-firewall-agent")
	require.Contains(t, deploy, "for _ in {1..50}")
	require.Contains(t, deploy, "delete table inet uvp_sip_guard")
	require.NotContains(t, strings.ToLower(deploy), "flush ruleset")

	unit := read("deploy/test/uvp-firewall-agent.service")
	require.Contains(t, unit, "CapabilityBoundingSet=CAP_NET_ADMIN")
	require.Contains(t, unit, "/opt/uvp-gb28181/agent-current/uvp-firewall-agent")
}
