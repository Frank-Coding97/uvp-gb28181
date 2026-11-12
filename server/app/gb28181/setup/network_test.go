package setup

import (
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeInterfaceProvider struct {
	interfaces []net.Interface
	addresses  map[int][]net.Addr
	err        error
	addrErrors map[int]error
}

func (p fakeInterfaceProvider) Interfaces() ([]net.Interface, error) {
	return p.interfaces, p.err
}

func (p fakeInterfaceProvider) Addrs(iface net.Interface) ([]net.Addr, error) {
	if err := p.addrErrors[iface.Index]; err != nil {
		return nil, err
	}
	return p.addresses[iface.Index], nil
}

func TestEnumerateNetworkAddressesClassifiesAndSortsIPv4(t *testing.T) {
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{
			{Index: 3, Name: "docker0", Flags: net.FlagUp},
			{Index: 1, Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
			{Index: 4, Name: "en1", Flags: 0},
			{Index: 2, Name: "en0", Flags: net.FlagUp},
		},
		addresses: map[int][]net.Addr{
			1: {mustCIDR(t, "127.0.0.1/8")},
			2: {
				mustCIDR(t, "192.168.1.20/24"),
				mustCIDR(t, "192.168.1.20/24"),
				mustCIDR(t, "2001:db8::1/64"),
			},
			3: {mustCIDR(t, "172.17.0.1/16")},
			4: {mustCIDR(t, "10.0.0.8/24")},
		},
	}

	got, err := EnumerateNetworkAddresses(provider)
	require.NoError(t, err)
	require.Len(t, got, 5)

	assert.Equal(t, NetworkAddress{
		IP:         "0.0.0.0",
		CIDR:       "0.0.0.0/0",
		ListenOnly: true,
	}, got[0])
	assert.Equal(t, "192.168.1.20", got[1].IP)
	assert.Equal(t, "en0", got[1].InterfaceName)
	assert.Equal(t, "192.168.1.20/24", got[1].CIDR)
	assert.True(t, got[1].Recommended)
	assert.False(t, got[1].More)

	byInterface := addressesByInterface(got)
	assert.True(t, byInterface["lo"].Loopback)
	assert.False(t, byInterface["lo"].Recommended)
	assert.True(t, byInterface["lo"].More)
	assert.True(t, byInterface["docker0"].Virtual)
	assert.False(t, byInterface["docker0"].Recommended)
	assert.True(t, byInterface["docker0"].More)
	assert.False(t, byInterface["en1"].Virtual)
	assert.False(t, byInterface["en1"].Recommended)
	assert.True(t, byInterface["en1"].More)
}

func TestEnumerateNetworkAddressesKeepsSameIPOnDifferentInterfaces(t *testing.T) {
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{
			{Index: 2, Name: "en1", Flags: net.FlagUp},
			{Index: 1, Name: "en0", Flags: net.FlagUp},
		},
		addresses: map[int][]net.Addr{
			1: {mustCIDR(t, "10.0.0.2/24")},
			2: {mustCIDR(t, "10.0.0.2/24")},
		},
	}

	got, err := EnumerateNetworkAddresses(provider)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "en0", got[1].InterfaceName)
	assert.Equal(t, "en1", got[2].InterfaceName)
}

func TestEnumerateNetworkAddressesMarksVirtualInterfaceFamiliesAsMore(t *testing.T) {
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{
			{Index: 1, Name: "docker0", Flags: net.FlagUp},
			{Index: 2, Name: "br-abcd", Flags: net.FlagUp},
			{Index: 3, Name: "tun0", Flags: net.FlagUp},
		},
		addresses: map[int][]net.Addr{
			1: {mustCIDR(t, "172.17.0.1/16")},
			2: {mustCIDR(t, "172.18.0.1/16")},
			3: {mustCIDR(t, "10.8.0.1/24")},
		},
	}

	got, err := EnumerateNetworkAddresses(provider)
	require.NoError(t, err)
	require.Len(t, got, 4)
	for _, address := range got[1:] {
		assert.True(t, address.Virtual, address.InterfaceName)
		assert.True(t, address.More, address.InterfaceName)
		assert.False(t, address.Recommended, address.InterfaceName)
	}
}

// Windows 上的虚拟网卡名是"友好名"（VMware Network Adapter VMnet1 / VirtualBox Host-Only Network …），
// 不以 vmnet/vboxnet 这类设备族前缀开头。历史上纯前缀匹配一个都认不出来，
// 于是装了 VMware/VirtualBox 的 Windows 机器会把虚拟网卡标成「推荐」，默认选中它。
func TestEnumerateNetworkAddressesRecognizesWindowsVirtualAdapters(t *testing.T) {
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{
			{Index: 1, Name: "VMware Network Adapter VMnet1", Flags: net.FlagUp},
			{Index: 2, Name: "VirtualBox Host-Only Network", Flags: net.FlagUp},
			{Index: 3, Name: "vEthernet (Default Switch)", Flags: net.FlagUp},
			{Index: 4, Name: "Ethernet", Flags: net.FlagUp},
		},
		addresses: map[int][]net.Addr{
			1: {mustCIDR(t, "192.168.11.1/24")},
			2: {mustCIDR(t, "192.168.56.1/24")},
			3: {mustCIDR(t, "172.20.0.1/20")},
			4: {mustCIDR(t, "192.168.10.52/24")},
		},
	}

	got, err := EnumerateNetworkAddresses(provider)
	require.NoError(t, err)
	require.Len(t, got, 5)

	byInterface := addressesByInterface(got)
	for _, name := range []string{
		"VMware Network Adapter VMnet1",
		"VirtualBox Host-Only Network",
		"vEthernet (Default Switch)",
	} {
		assert.True(t, byInterface[name].Virtual, name)
		assert.False(t, byInterface[name].Recommended, name)
		assert.True(t, byInterface[name].More, name)
	}

	// 唯一的物理网卡必须成为推荐项，并被排到最前（前端默认选中的就是它）。
	assert.False(t, byInterface["Ethernet"].Virtual)
	assert.True(t, byInterface["Ethernet"].Recommended)
	assert.Equal(t, "Ethernet", got[1].InterfaceName)
	assert.Equal(t, "192.168.10.52", got[1].IP)
}

// ⛔ 反向契约：KVM / Proxmox 宿主机上 br0 / vmbr0 是把物理网卡桥接出去、承载 LAN IP 的那张网卡，
// 设备能访问到它 —— 必须保持「非虚拟 + 推荐」。
// （2026-10-11 一度把它们加进虚拟清单，会把 firstLocalIPv4 的广播兜底搞坏，当天已撤回。）
func TestEnumerateNetworkAddressesKeepsLinuxBridgesRecommended(t *testing.T) {
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{
			{Index: 1, Name: "vmbr0", Flags: net.FlagUp},
			{Index: 2, Name: "br0", Flags: net.FlagUp},
			{Index: 3, Name: "virbr0", Flags: net.FlagUp},
		},
		addresses: map[int][]net.Addr{
			1: {mustCIDR(t, "10.0.0.5/24")},
			2: {mustCIDR(t, "192.168.10.10/24")},
			3: {mustCIDR(t, "192.168.122.1/24")},
		},
	}

	got, err := EnumerateNetworkAddresses(provider)
	require.NoError(t, err)

	byInterface := addressesByInterface(got)
	for _, name := range []string{"vmbr0", "br0"} {
		assert.False(t, byInterface[name].Virtual, name)
		assert.True(t, byInterface[name].Recommended, name)
	}
	// libvirt 的 NAT 网桥设备路由不到，仍然算虚拟。
	assert.True(t, byInterface["virbr0"].Virtual)
	assert.False(t, byInterface["virbr0"].Recommended)
	// 推荐项按 IP 升序：10.0.0.5(vmbr0) 在前。
	assert.Equal(t, "10.0.0.5", got[1].IP)
	assert.Equal(t, "vmbr0", got[1].InterfaceName)
}

func TestEnumerateNetworkAddressesOmitsIPv6(t *testing.T) {
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{{Index: 1, Name: "en0", Flags: net.FlagUp}},
		addresses: map[int][]net.Addr{
			1: {mustCIDR(t, "2001:db8::1/64")},
		},
	}

	got, err := EnumerateNetworkAddresses(provider)
	require.NoError(t, err)
	assert.Equal(t, []NetworkAddress{{
		IP:         "0.0.0.0",
		CIDR:       "0.0.0.0/0",
		ListenOnly: true,
	}}, got)
}

func TestEnumerateNetworkAddressesReturnsWildcardWhenProviderFails(t *testing.T) {
	wantErr := errors.New("interfaces unavailable")

	got, err := EnumerateNetworkAddresses(fakeInterfaceProvider{err: wantErr})

	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, []NetworkAddress{{
		IP:         "0.0.0.0",
		CIDR:       "0.0.0.0/0",
		ListenOnly: true,
	}}, got)
}

func TestEnumerateNetworkAddressesReturnsWildcardWhenAddressLookupFails(t *testing.T) {
	wantErr := errors.New("addresses unavailable")
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{{Index: 1, Name: "en0", Flags: net.FlagUp}},
		addrErrors: map[int]error{1: wantErr},
	}

	got, err := EnumerateNetworkAddresses(provider)

	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, []NetworkAddress{{
		IP:         "0.0.0.0",
		CIDR:       "0.0.0.0/0",
		ListenOnly: true,
	}}, got)
}

func TestActiveSIPAddressesWildcardLANIgnoresStaleAdvertiseIP(t *testing.T) {
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{
			{Index: 1, Name: "en0", Flags: net.FlagUp},
			{Index: 2, Name: "utun4", Flags: net.FlagUp},
			{Index: 3, Name: "lo0", Flags: net.FlagUp | net.FlagLoopback},
		},
		addresses: map[int][]net.Addr{
			1: {mustCIDR(t, "192.168.126.126/24")},
			2: {mustCIDR(t, "10.8.0.3/32")},
			3: {mustCIDR(t, "127.0.0.1/8")},
		},
	}
	config := SIPConfigView{
		DeploymentMode: DeploymentLAN,
		ListenIP:       wildcardIPv4,
		AdvertiseIP:    "192.168.10.106",
	}

	addresses, err := ActiveSIPAddresses(config, provider)

	require.NoError(t, err)
	assert.Equal(t, []string{"192.168.126.126", "10.8.0.3"}, addresses)
}

func mustCIDR(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	ip, network, err := net.ParseCIDR(cidr)
	require.NoError(t, err)
	network.IP = ip
	return network
}

func addressesByInterface(addresses []NetworkAddress) map[string]NetworkAddress {
	result := make(map[string]NetworkAddress, len(addresses))
	for _, address := range addresses {
		result[address.InterfaceName] = address
	}
	return result
}
