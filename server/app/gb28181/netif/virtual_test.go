package netif

import "testing"

// 虚拟网卡名单（含 2026-10-11 补的 Windows 友好名 / Linux 纯数字网桥）。
// ⛔ 这份名单同时是 setup.EnumerateNetworkAddresses 打「推荐」标记和
// sip.firstLocalIPv4 挑 Contact Host 的共享判据，删任何一条都要先想清楚后果。
func TestIsVirtualNameRecognizesVirtualAdapters(t *testing.T) {
	cases := []string{
		// Linux / macOS 容器与隧道（原有契约，别删）
		"utun3", "UTUN0", "docker0", "tap0", "wg0", "br-abc123", "vmnet8",
		"virbr0", "veth1a2b3c", "bridge0", "cali123", "cni0", "flannel.1",
		"kube-ipvs0", "podman1", "tailscale0", "tun0", "vboxnet0", "lxcbr0",
		"docker_gwbridge", "tunl0", "kube-bridge",
		// ZeroTier 在 Linux 上的接口名（Windows 友好名走关键字，Linux 走这条正则）
		"zt8056c2e21c", "ZT8056C2E21C",
		// Windows 友好名：这些**不以** vmnet/vboxnet 开头，纯前缀匹配一个都命中不了
		"VMware Network Adapter VMnet1",
		"VMware Network Adapter VMnet8",
		"VirtualBox Host-Only Network",
		"vEthernet (Default Switch)",
		"Hyper-V Virtual Ethernet Adapter",
		"Loopback Pseudo-Interface 1",
		"Microsoft KM-TEST Loopback Adapter",
		"ZeroTier One [8056c2e21c000001]",
		"WireGuard Tunnel",
		"OpenVPN Wintun",
		"Cisco AnyConnect Virtual Miniport Adapter for Windows",
		"Fortinet SSL VPN Virtual Ethernet Adapter",
	}
	for _, name := range cases {
		if !IsVirtualName(name) {
			t.Errorf("IsVirtualName(%q) = false, 应为虚拟网卡", name)
		}
	}
}

// ⛔ 反向契约：这些是**物理**网卡，一旦被判成虚拟，用户就在下拉里选不到它们了。
// 这条比「漏判虚拟」严重得多，任何新增关键字都要先过这一关。
//
// ⚠️ `br0` / `vmbr0` 刻意留在这里：KVM / Proxmox 宿主机上它们承载 LAN IP、设备能访问到，
// 被判成虚拟会连带搞坏 firstLocalIPv4 的广播兜底（详见 virtual.go 顶部的说明）。
func TestIsVirtualNameKeepsPhysicalAdapters(t *testing.T) {
	cases := []string{
		"en0", "en1", "eth0", "eth1", "ens18", "ens33", "bond0", "bond0.100",
		"wlan0", "wlp3s0", "enp0s3", "enp1s0f0", "em1", "eno1", "p3p1", "lo", "lo0",
		"br0", "br1", "vmbr0", "ovs-system",
		"Ethernet", "以太网", "本地连接",
		"Intel(R) Ethernet Connection I219-V",
		"Realtek PCIe GbE Family Controller",
		"Broadcom NetXtreme Gigabit Ethernet",
		"",
		"   ",
	}
	for _, name := range cases {
		if IsVirtualName(name) {
			t.Errorf("IsVirtualName(%q) = true, 物理网卡被误判为虚拟", name)
		}
	}
}

// 大小写与首尾空白不该影响判定（Windows 友好名大小写写法不统一）。
func TestIsVirtualNameNormalizesInput(t *testing.T) {
	for _, name := range []string{"  DOCKER0 ", "docker0", "Docker0"} {
		if !IsVirtualName(name) {
			t.Errorf("IsVirtualName(%q) = false, 应做小写+去空白归一化后判定", name)
		}
	}
}
