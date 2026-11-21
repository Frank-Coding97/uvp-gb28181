package security

import "time"

// UnsupportedFirewallClient keeps the application security runtime usable on
// platforms without the privileged host firewall integration. Application
// admission remains authoritative; host enforcement reports a typed outcome.
type UnsupportedFirewallClient struct{}

func NewUnsupportedFirewallClient() *UnsupportedFirewallClient {
	return &UnsupportedFirewallClient{}
}

func (*UnsupportedFirewallClient) Capability() AgentCapabilityState {
	return AgentCapabilityUnsupported
}

func (*UnsupportedFirewallClient) Ban(BanDecision) error { return ErrFirewallUnsupported }

func (*UnsupportedFirewallClient) Unban(string) error { return ErrFirewallUnsupported }

func (*UnsupportedFirewallClient) Reconcile([]BanDecision) error { return ErrFirewallUnsupported }

func (*UnsupportedFirewallClient) Status() AgentStatus {
	return AgentStatus{
		Capability: AgentCapabilityUnsupported,
		LastError:  ErrFirewallUnsupported.Error(),
		CheckedAt:  time.Now(),
	}
}

var _ FirewallAgentClient = (*UnsupportedFirewallClient)(nil)
var _ FirewallAgentCapabilityProvider = (*UnsupportedFirewallClient)(nil)
