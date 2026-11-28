// Package netif 集中判定「虚拟网卡」与「是否运行在容器中」。
//
// 为什么「虚拟网卡」判定必须只有一处实现：这个判据同时服务两条链路 ——
//  1. 平台接入向导给候选网卡打「推荐 / 虚拟网卡」标记（gb28181/setup/network.go）；
//  2. SIP Contact Host、广播兜底地址的挑选（gb28181/sip/server.go 的 firstLocalIPv4）。
//
// 两处一旦漂移，就会出现「页面推荐了 A 网卡、信令却挑了 B 网卡」这种极难定位的错位：
// 页面看着没问题，设备却注册不上 / 收不到流。所以判定只写在这里，两处都委托进来。
package netif

import (
	"regexp"
	"strings"
)

// virtualNamePrefixes 覆盖 Linux / macOS 上以设备族名开头的虚拟网卡。
//
// ⛔ 反向契约（改这个清单前先看测试）：en0 / eth0 / ens18 / bond0 / br0 必须判为**非**虚拟。
// 特别是别把 "bond" 加进来 —— 那是服务器的物理聚合网卡，是最该被推荐的一张。
var virtualNamePrefixes = []string{
	"br-", "bridge", "cali", "cni", "docker", "flannel", "kube", "lxc",
	"podman", "tap", "tailscale", "tun", "utun", "vboxnet", "veth",
	"virbr", "vmnet", "wg",
}

// virtualNamePatterns 收拾前缀匹配表达不了的写法。
var virtualNamePatterns = []*regexp.Regexp{
	// ZeroTier 在 Linux 上的接口名是 zt + 网络 ID 的 10 位十六进制（如 zt8056c2e21c），
	// 和 Windows 的友好名 "ZeroTier One [...]" 完全是两个样子 —— 只写关键字会漏掉 Linux。
	regexp.MustCompile(`^zt[0-9a-f]{10}$`),
}

// virtualNameKeywords 覆盖 Windows 的「友好名」。
//
// ⛔ Windows 上 net.Interfaces().Name 拿到的不是设备名而是友好名，形如
// "VMware Network Adapter VMnet1" / "VirtualBox Host-Only Network" / "Hyper-V Virtual Ethernet Adapter"，
// 它们**不以** vmnet / vboxnet 这类设备族前缀开头，纯前缀匹配一个都命中不了，
// 结果就是「装了 VMware 的 Windows 机器默认选中虚拟机网卡」。
// 所以这里用包含匹配（Contains），而不是前缀匹配。
var virtualNameKeywords = []string{
	"vmware",           // VMware Network Adapter VMnet1 / VMnet8
	"virtualbox",       // VirtualBox Host-Only Network
	"hyper-v",          // Hyper-V Virtual Ethernet Adapter
	"vethernet",        // vEthernet (Default Switch)（前缀 "veth" 也兜得住，这里写全更稳）
	"loopback",         // Loopback Pseudo-Interface 1 / Microsoft KM-TEST Loopback Adapter
	"zerotier",         // ZeroTier One [xxxxxxxx]（Windows 友好名）
	"wireguard",        // WireGuard Tunnel
	"openvpn",          // OpenVPN Wintun / OpenVPN Data Channel Offload
	"wintun",           // Wintun Userspace Tunnel
	"anyconnect",       // Cisco AnyConnect Virtual Miniport
	"forticlient",      // FortiClient VPN
	"pangp",            // Palo Alto GlobalProtect
	"virtual ethernet", // 各类 "XXX Virtual Ethernet Adapter"
	"virtual adapter",  // 各类 "XXX Virtual Adapter"
	"vpn",              // 兜底：任何自称 VPN 的适配器都不该被推荐
}

// ⛔⛔ 关于 Linux 网桥 br0 / vmbr0（2026-10-11 一度加进来，当天又撤回，别再踩）：
//
//	原来的清单用的是带横杠的 `br-`，这不是笔误，是**有意**只匹配 Docker 的自定义网桥
//	（`br-<12位十六进制>`）。而 `br0` / `vmbr0`(Proxmox) / `virbr0` 中最常见的 `br0`、`vmbr0`
//	在 KVM/Proxmox 宿主机上是**把物理网卡桥接出去、承载 LAN IP 的那张网卡** ——
//	设备是能访问到它的，标记成「虚拟」有两个真实后果：
//	  a) `firstLocalIPv4()` 会**直接跳过**它 ⇒ 通配监听下的广播兜底拿不到地址 ⇒ 对讲/广播 503；
//	  b) 向导里那张真正该选的网卡被贴上「虚拟网卡」标签，误导用户。
//	所以 `br\d+` / `vmbr` 一律不加。真正不可达的 libvirt NAT 网桥是 `virbr0`，已在清单里。

// IsVirtualName 判断网卡名是否属于虚拟/VPN/容器类网卡。
// 判定只看名字（Go 的 net.Interface 不提供"是否虚拟"的标志位），
// 名字匹配不到时**宁可判为物理**——漏判的后果是"默认选项不够好"，
// 误判的后果是"物理网卡消失、用户选不到"，后者更糟。
func IsVirtualName(name string) bool {
	lowered := strings.ToLower(strings.TrimSpace(name))
	if lowered == "" {
		return false
	}

	for _, prefix := range virtualNamePrefixes {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	for _, pattern := range virtualNamePatterns {
		if pattern.MatchString(lowered) {
			return true
		}
	}
	for _, keyword := range virtualNameKeywords {
		if strings.Contains(lowered, keyword) {
			return true
		}
	}
	return false
}
