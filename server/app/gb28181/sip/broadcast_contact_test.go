package sip

import (
	"net"
	"testing"
)

func TestResolveBroadcastContactHostPrefersConcreteAddresses(t *testing.T) {
	cases := []struct {
		name        string
		advertiseIP string
		listenIP    string
		want        string
	}{
		{name: "静态 advertise 优先", advertiseIP: "192.168.10.106", listenIP: "0.0.0.0", want: "192.168.10.106"},
		{name: "advertise 为空时用具体 listen", advertiseIP: "", listenIP: "10.10.1.5", want: "10.10.1.5"},
		{name: "advertise 为通配时用具体 listen", advertiseIP: "0.0.0.0", listenIP: "10.10.1.5", want: "10.10.1.5"},
		{name: "两侧空白要裁剪", advertiseIP: " 192.168.1.9 ", listenIP: "   ", want: "192.168.1.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveBroadcastContactHost(tc.advertiseIP, tc.listenIP)
			if got != tc.want {
				t.Fatalf("resolveBroadcastContactHost(%q, %q) = %q, want %q", tc.advertiseIP, tc.listenIP, got, tc.want)
			}
		})
	}
}

// LAN + 通配监听(动态 advertise)是允许的部署,此时必须能落到一个真实本机地址上。
// 否则 Broadcast UAS 根本不会创建,设备发来的 INVITE 会被静默回 503 ——
// 现象就是前端一直停在「正在建立广播」。
func TestResolveBroadcastContactHostFallsBackWhenWildcard(t *testing.T) {
	got := resolveBroadcastContactHost("", "0.0.0.0")
	if got == "" {
		t.Skip("本机没有可用的非回环 IPv4,无法验证兜底路径")
	}
	t.Logf("通配兜底选中的本机地址: %s", got)
	ip := net.ParseIP(got)
	if ip == nil || ip.To4() == nil {
		t.Fatalf("兜底地址不是合法 IPv4: %q", got)
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		t.Fatalf("兜底地址设备访问不到: %q", got)
	}
}

func TestConcreteSIPHostRejectsWildcards(t *testing.T) {
	for _, host := range []string{"0.0.0.0", "::", "", "   "} {
		if got := concreteSIPHost(host); got != "" {
			t.Fatalf("concreteSIPHost(%q) = %q, want empty", host, got)
		}
	}
	if got := concreteSIPHost(" 192.168.0.2 "); got != "192.168.0.2" {
		t.Fatalf("concreteSIPHost 未裁剪空白: %q", got)
	}
}

func TestIsVirtualInterfaceName(t *testing.T) {
	// 判据实现在 app/gb28181/netif（setup 包共用同一份），这里锁住"Contact Host 这一侧"的契约：
	// 虚拟/VPN 网卡必须被跳过，物理网卡一个都不能被跳过。
	for _, name := range []string{
		"utun3", "UTUN0", "docker0", "tap0", "wg0", "br-abc123", "vmnet8",
		// ZeroTier 在 Linux 上的接口名（zt + 10 位十六进制）
		"zt8056c2e21c",
		// Windows 友好名
		"VMware Network Adapter VMnet1",
		"VirtualBox Host-Only Network",
		"vEthernet (Default Switch)",
		"Hyper-V Virtual Ethernet Adapter",
		"OpenVPN Wintun",
	} {
		if !isVirtualInterfaceName(name) {
			t.Fatalf("%q 应判为虚拟接口", name)
		}
	}
	// ⛔ br0 / vmbr0 必须**非**虚拟：KVM/Proxmox 宿主机上它们承载 LAN IP，
	// 一旦跳过它们，resolveBroadcastContactHost 的兜底会拿不到地址（对讲/广播 503）。
	for _, name := range []string{"en0", "eth0", "ens18", "bond0", "wlan0", "Ethernet", "br0", "vmbr0"} {
		if isVirtualInterfaceName(name) {
			t.Fatalf("%q 不应判为虚拟接口", name)
		}
	}
}
