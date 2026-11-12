package setup

import (
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

// virtualOnlyProvider 模拟「只有一张虚拟网卡」的机器（如装了 VMware 的 Windows）。
func virtualOnlyProvider(t *testing.T) InterfaceProvider {
	t.Helper()
	return fakeInterfaceProvider{
		interfaces: []net.Interface{{Index: 1, Name: "VMware Network Adapter VMnet1", Flags: net.FlagUp}},
		addresses:  map[int][]net.Addr{1: {mustCIDR(t, "192.168.56.1/24")}},
	}
}

func physicalProvider(t *testing.T) InterfaceProvider {
	t.Helper()
	return fakeInterfaceProvider{
		interfaces: []net.Interface{{Index: 1, Name: "en0", Flags: net.FlagUp}},
		addresses:  map[int][]net.Addr{1: {mustCIDR(t, "192.168.1.10/24")}},
	}
}

// 局域网 + 真实网卡：三项都跟随该网卡，可留空。
func TestResolveMediaAddressPolicyLANPhysicalNIC(t *testing.T) {
	policy := ResolveMediaAddressPolicy(DeploymentLAN, "192.168.1.10", "192.168.1.10", physicalProvider(t))
	require.Equal(t, "192.168.1.10", policy.HookIP)
	require.Equal(t, "192.168.1.10", policy.SDPIP)
	require.Equal(t, "192.168.1.10", policy.StreamIP)
	require.Equal(t, MediaAddressRequirement{}, policy.Requirements())
}

// 局域网 + 虚拟网卡：地址设备侧不可达，三项都必须手填。
func TestResolveMediaAddressPolicyLANVirtualNIC(t *testing.T) {
	policy := ResolveMediaAddressPolicy(DeploymentLAN, "192.168.56.1", "192.168.56.1", virtualOnlyProvider(t))
	require.Empty(t, policy.HookIP)
	require.Empty(t, policy.SDPIP)
	require.Empty(t, policy.StreamIP)
	require.Equal(t, MediaAddressRequirement{HookIP: true, SDPIP: true, StreamIP: true}, policy.Requirements())
}

// 局域网 + 0.0.0.0：推断不出唯一地址，三项都必须手填。
func TestResolveMediaAddressPolicyLANWildcard(t *testing.T) {
	policy := ResolveMediaAddressPolicy(DeploymentLAN, wildcardIPv4, "", physicalProvider(t))
	require.Empty(t, policy.HookIP)
	require.Empty(t, policy.SDPIP)
	require.Equal(t, MediaAddressRequirement{HookIP: true, SDPIP: true, StreamIP: true}, policy.Requirements())
}

// 公网 + 真实网卡：SDP / Stream 跟随公网接入 IP，Hook 走监听网卡。
func TestResolveMediaAddressPolicyPublicPhysicalNIC(t *testing.T) {
	policy := ResolveMediaAddressPolicy(DeploymentPublic, "10.0.8.3", "203.0.113.10", fakeInterfaceProvider{
		interfaces: []net.Interface{{Index: 1, Name: "ens18", Flags: net.FlagUp}},
		addresses:  map[int][]net.Addr{1: {mustCIDR(t, "10.0.8.3/24")}},
	})
	require.Equal(t, "10.0.8.3", policy.HookIP)
	require.Equal(t, "203.0.113.10", policy.SDPIP)
	require.Equal(t, "203.0.113.10", policy.StreamIP)
	require.Equal(t, MediaAddressRequirement{}, policy.Requirements())
}

// 公网 + 虚拟网卡：只有 Hook 必填 —— SDP / Stream 与网卡无关。
func TestResolveMediaAddressPolicyPublicVirtualNIC(t *testing.T) {
	policy := ResolveMediaAddressPolicy(DeploymentPublic, "192.168.56.1", "203.0.113.10", virtualOnlyProvider(t))
	require.Empty(t, policy.HookIP)
	require.Equal(t, "203.0.113.10", policy.SDPIP)
	require.Equal(t, MediaAddressRequirement{HookIP: true, SDPIP: false, StreamIP: false}, policy.Requirements())
}

// 公网模式下对外地址还没填时，SDP / Stream 也推断不出来。
func TestResolveMediaAddressPolicyPublicWithoutAdvertiseIP(t *testing.T) {
	policy := ResolveMediaAddressPolicy(DeploymentPublic, "10.0.8.3", "", physicalProvider(t))
	require.Empty(t, policy.SDPIP)
	require.Empty(t, policy.StreamIP)
}

// 枚举网卡失败时**放行**而不是收紧：该地址仍是具体的本机地址，
// 用它比留空好；在这里 fail-close 会让一次网卡读取抖动变成「配置存不进去」。
func TestResolveMediaAddressPolicyToleratesEnumerationFailure(t *testing.T) {
	policy := ResolveMediaAddressPolicy(DeploymentLAN, "192.168.1.10", "192.168.1.10", fakeInterfaceProvider{
		err: errors.New("list network interfaces: permission denied"),
	})
	require.Equal(t, "192.168.1.10", policy.SDPIP)
}

// 手填了不在网卡列表里的出口地址：无从判断虚拟，按可用处理
// （收紧会让这类编辑直接存不进去）。
func TestResolveMediaAddressPolicyAcceptsAddressOutsideInterfaceList(t *testing.T) {
	policy := ResolveMediaAddressPolicy(DeploymentLAN, "10.20.30.40", "10.20.30.40", physicalProvider(t))
	require.Equal(t, "10.20.30.40", policy.SDPIP)
}

func TestMediaAddressPolicyRequirements(t *testing.T) {
	require.Equal(t,
		MediaAddressRequirement{HookIP: true, SDPIP: false, StreamIP: true},
		MediaAddressPolicy{SDPIP: "203.0.113.10"}.Requirements(),
	)
}
