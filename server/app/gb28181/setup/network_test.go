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
