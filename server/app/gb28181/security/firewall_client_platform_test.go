package security

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUnsupportedFirewallClientIsStaticAndFailClosed(t *testing.T) {
	client := NewUnsupportedFirewallClient()
	status := client.Status()
	require.Equal(t, AgentCapabilityUnsupported, status.Capability)
	require.False(t, status.Connected)
	require.ErrorIs(t, client.Ban(BanDecision{SourceIP: "198.51.100.10"}), ErrFirewallUnsupported)
	require.ErrorIs(t, client.Unban("198.51.100.10"), ErrFirewallUnsupported)
	require.ErrorIs(t, client.Reconcile(nil), ErrFirewallUnsupported)
}

func TestFirewallAgentFactoryMatchesGOOS(t *testing.T) {
	client := NewFirewallAgentClient(filepath.Join(t.TempDir(), "missing.sock"), time.Millisecond)
	if runtime.GOOS == "windows" {
		require.IsType(t, &UnsupportedFirewallClient{}, client)
		require.Equal(t, AgentCapabilityUnsupported, client.(FirewallAgentCapabilityProvider).Capability())
		return
	}
	require.IsType(t, &UnixFirewallClient{}, client)
	require.Equal(t, AgentCapabilitySupported, client.(FirewallAgentCapabilityProvider).Capability())
}

func TestUnixFirewallClientReportsSupportedWhenSocketIsUnavailable(t *testing.T) {
	status := NewUnixFirewallClient(filepath.Join(t.TempDir(), "missing.sock"), time.Millisecond).Status()
	require.Equal(t, AgentCapabilitySupported, status.Capability)
	require.False(t, status.Connected)
}

func TestAgentStatusCapabilityIsSerialized(t *testing.T) {
	payload, err := json.Marshal(AgentStatus{Capability: AgentCapabilityUnsupported})
	require.NoError(t, err)
	require.Contains(t, string(payload), `"capability":"unsupported"`)
}

func TestUnsupportedFirewallClientErrorIsTyped(t *testing.T) {
	require.True(t, errors.Is(NewUnsupportedFirewallClient().Ban(BanDecision{}), ErrFirewallUnsupported))
}
