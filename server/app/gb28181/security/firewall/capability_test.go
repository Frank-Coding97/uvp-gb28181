package firewall

import (
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/security"
)

func TestAgentReportsSupportedFirewallCapability(t *testing.T) {
	agent := New(NewMemoryBackend(), security.RealClock(), nil)

	require.Equal(t, security.AgentCapabilitySupported, agent.Capability())
	require.Equal(t, security.AgentCapabilitySupported, agent.Status().Capability)
}
