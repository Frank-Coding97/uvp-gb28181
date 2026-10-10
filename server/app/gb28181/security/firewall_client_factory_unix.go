//go:build !windows

package security

import "time"

// NewFirewallAgentClient selects the Unix socket client on non-Windows hosts.
func NewFirewallAgentClient(socketPath string, timeout time.Duration) FirewallAgentClient {
	return NewUnixFirewallClient(socketPath, timeout)
}
