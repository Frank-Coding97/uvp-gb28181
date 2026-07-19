package setup

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

const wildcardIPv4 = "0.0.0.0"

// InterfaceProvider isolates operating-system network calls for deterministic tests.
type InterfaceProvider interface {
	Interfaces() ([]net.Interface, error)
	Addrs(net.Interface) ([]net.Addr, error)
}

// SystemInterfaceProvider reads interfaces and addresses from the local machine.
type SystemInterfaceProvider struct{}

func (SystemInterfaceProvider) Interfaces() ([]net.Interface, error) {
	return net.Interfaces()
}

func (SystemInterfaceProvider) Addrs(iface net.Interface) ([]net.Addr, error) {
	return iface.Addrs()
}

// NetworkAddress describes one selectable local IPv4 address.
type NetworkAddress struct {
	IP            string `json:"ip"`
	InterfaceName string `json:"interfaceName,omitempty"`
	CIDR          string `json:"cidr"`
	Loopback      bool   `json:"loopback"`
	Virtual       bool   `json:"virtual"`
	Recommended   bool   `json:"recommended"`
	More          bool   `json:"more"`
	ListenOnly    bool   `json:"listenOnly"`
}

// EnumerateNetworkAddresses returns the local IPv4 addresses in a stable order.
// Passing nil uses the operating-system provider. The wildcard entry is always
// first and must never be used as an advertised SIP address.
func EnumerateNetworkAddresses(provider InterfaceProvider) ([]NetworkAddress, error) {
	if provider == nil {
		provider = SystemInterfaceProvider{}
	}

	interfaces, err := provider.Interfaces()
	if err != nil {
		return wildcardAddresses(), fmt.Errorf("list network interfaces: %w", err)
	}

	unique := make(map[string]NetworkAddress)
	for _, iface := range interfaces {
		addresses, err := provider.Addrs(iface)
		if err != nil {
			return wildcardAddresses(), fmt.Errorf("list addresses for interface %q: %w", iface.Name, err)
		}

		for _, address := range addresses {
			ip, cidr, ok := parseIPv4Address(address)
			if !ok || ip.IsUnspecified() {
				continue
			}

			loopback := iface.Flags&net.FlagLoopback != 0 || ip.IsLoopback()
			virtual := !loopback && isVirtualInterface(iface.Name)
			usable := !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
			recommended := iface.Flags&net.FlagUp != 0 && !loopback && !virtual && usable
			item := NetworkAddress{
				IP:            ip.String(),
				InterfaceName: iface.Name,
				CIDR:          cidr,
				Loopback:      loopback,
				Virtual:       virtual,
				Recommended:   recommended,
				More:          !recommended,
			}

			key := item.IP + "\x00" + item.InterfaceName
			if existing, found := unique[key]; !found || item.CIDR < existing.CIDR {
				unique[key] = item
			}
		}
	}

	result := make([]NetworkAddress, 0, len(unique)+1)
	result = append(result, wildcardAddresses()[0])
	for _, address := range unique {
		result = append(result, address)
	}
	sort.Slice(result[1:], func(i, j int) bool {
		left := result[i+1]
		right := result[j+1]
		if left.Recommended != right.Recommended {
			return left.Recommended
		}
		if comparison := compareIPv4(left.IP, right.IP); comparison != 0 {
			return comparison < 0
		}
		if left.InterfaceName != right.InterfaceName {
			return left.InterfaceName < right.InterfaceName
		}
		return left.CIDR < right.CIDR
	})

	return result, nil
}

func wildcardAddresses() []NetworkAddress {
	return []NetworkAddress{{
		IP:         wildcardIPv4,
		CIDR:       wildcardIPv4 + "/0",
		ListenOnly: true,
	}}
}

func parseIPv4Address(address net.Addr) (net.IP, string, bool) {
	if address == nil {
		return nil, "", false
	}

	value := address.String()
	ip, network, err := net.ParseCIDR(value)
	if err != nil {
		ip = net.ParseIP(value)
		if ip == nil {
			return nil, "", false
		}
		if ipv4 := ip.To4(); ipv4 != nil {
			return ipv4, ipv4.String() + "/32", true
		}
		return nil, "", false
	}

	ipv4 := ip.To4()
	if ipv4 == nil {
		return nil, "", false
	}
	ones, bits := network.Mask.Size()
	if bits != net.IPv4len*8 || ones < 0 {
		return nil, "", false
	}
	return ipv4, fmt.Sprintf("%s/%d", ipv4.String(), ones), true
}

func compareIPv4(left, right string) int {
	leftIP := net.ParseIP(left).To4()
	rightIP := net.ParseIP(right).To4()
	for i := 0; i < net.IPv4len; i++ {
		if leftIP[i] < rightIP[i] {
			return -1
		}
		if leftIP[i] > rightIP[i] {
			return 1
		}
	}
	return 0
}

func isVirtualInterface(name string) bool {
	name = strings.ToLower(name)
	virtualPrefixes := []string{
		"br-", "bridge", "cali", "cni", "docker", "flannel", "kube",
		"podman", "tap", "tailscale", "tun", "utun", "vboxnet", "veth",
		"virbr", "vmnet", "wg",
	}
	for _, prefix := range virtualPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
