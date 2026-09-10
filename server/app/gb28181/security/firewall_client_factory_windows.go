//go:build windows

package security

import "time"

// NewFirewallAgentClient selects the static unsupported client on Windows.
func NewFirewallAgentClient(string, time.Duration) FirewallAgentClient {
	return NewUnsupportedFirewallClient()
}
