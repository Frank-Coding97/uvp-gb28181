package setup

import (
	"fmt"
	"net"
)

// ActiveSIPAddresses returns the concrete addresses devices can currently use.
// A wildcard LAN listener deliberately ignores the persisted advertise_ip: the
// kernel route and the current interface scan are authoritative, not stale DB data.
func ActiveSIPAddresses(config SIPConfigView, provider InterfaceProvider) ([]string, error) {
	if config.DeploymentMode == DeploymentPublic {
		if !validIPv4(config.AdvertiseIP, false) {
			return nil, fmt.Errorf("public SIP advertise address is unavailable")
		}
		return []string{config.AdvertiseIP}, nil
	}
	if config.ListenIP != wildcardIPv4 {
		if !validIPv4(config.ListenIP, false) {
			return nil, fmt.Errorf("LAN SIP listen address is unavailable")
		}
		return []string{config.ListenIP}, nil
	}

	items, err := EnumerateNetworkAddresses(provider)
	if err != nil {
		return nil, err
	}
	addresses := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		ip := net.ParseIP(item.IP)
		if item.Loopback || item.ListenOnly || ip == nil || ip.To4() == nil ||
			ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
			continue
		}
		if _, ok := seen[item.IP]; ok {
			continue
		}
		seen[item.IP] = struct{}{}
		addresses = append(addresses, item.IP)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("no active SIP network address")
	}
	return addresses, nil
}
