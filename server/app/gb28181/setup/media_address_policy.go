package setup

// 媒体网络地址（Hook IP / SDP IP / Stream IP）的填写规则。
//
// ⛔⛔ 这里是**后端唯一规则实现**，前端镜像在
// `web/src/views/gb28181/sip/mediaNetworkAddress.ts`（`resolveMediaAddressPolicy`）。
// 两边注释互相钉住：改规则必须同时改两处，并跑
// `setup/media_address_policy_test.go` 与前端对应的 18 条用例。
//
// 规则（老板 2026-10-11 定）：
//  ① 选中的监听地址是**虚拟网卡**或 **0.0.0.0** ⇒ 三个地址都必须手填
//     （虚拟网卡地址设备侧不可达；0.0.0.0 推断不出唯一地址）；
//  ② 选中**具体真实网卡** ⇒ 三个地址可留空，留空即跟随该网卡；
//  ③ 公网模式另算：SDP IP / Stream IP 跟随「公网接入 IP」（设备与浏览器在公网侧），
//     Hook IP 仍走监听网卡（ZLMediaKit 回调平台，不出本机）。
//
// 留空时的落地动作在运行时完成（见 bootstrap 的装配点），库里存的仍是空值
// ⇒ 换网卡后自动跟着变，不需要用户回去重填。

// MediaAddressPolicy 描述三个媒体地址留空时的兜底地址。
// 字段为空字符串表示「推断不出来，必须手填」。
type MediaAddressPolicy struct {
	HookIP   string
	SDPIP    string
	StreamIP string
}

// Requirements 把策略翻译成 validator 需要的「是否必填」。
func (p MediaAddressPolicy) Requirements() MediaAddressRequirement {
	return MediaAddressRequirement{
		HookIP:   p.HookIP == "",
		SDPIP:    p.SDPIP == "",
		StreamIP: p.StreamIP == "",
	}
}

// MediaAddressRequirement 声明三个媒体地址是否必须手填。
type MediaAddressRequirement struct {
	HookIP   bool
	SDPIP    bool
	StreamIP bool
}

// ResolveMediaAddressPolicy 按「部署模式 + 选中的监听地址」推导兜底地址。
//
// provider 用于判断选中的地址是否落在虚拟网卡上（生产传 SystemInterfaceProvider，
// 测试传 fake，保证结果与机器无关）。
func ResolveMediaAddressPolicy(mode DeploymentMode, listenIP, advertiseIP string, provider InterfaceProvider) MediaAddressPolicy {
	listenFallback := ""
	if listenAddressInferable(listenIP, provider) {
		listenFallback = listenIP
	}
	publicFallback := ""
	if validIPv4(advertiseIP, false) {
		publicFallback = advertiseIP
	}

	if mode == DeploymentPublic {
		return MediaAddressPolicy{HookIP: listenFallback, SDPIP: publicFallback, StreamIP: publicFallback}
	}
	// 局域网：三者都跟随选中的网卡。
	return MediaAddressPolicy{HookIP: listenFallback, SDPIP: listenFallback, StreamIP: listenFallback}
}

// SystemMediaAddressPolicy 用真实本机网卡解析（运行时装配点用它）。
func SystemMediaAddressPolicy(mode DeploymentMode, listenIP, advertiseIP string) MediaAddressPolicy {
	return ResolveMediaAddressPolicy(mode, listenIP, advertiseIP, SystemInterfaceProvider{})
}

// listenAddressInferable 判断选中的监听地址能否当兜底地址用。
//
// 三类不可用：
//   - 不是具体 IPv4：0.0.0.0 / 回环 / 非法格式（validIPv4 已经挡住）；
//   - 落在**虚拟网卡**上：那个地址设备侧访问不到，托底等于埋雷 —— 这正是当初
//     「Windows 装过 VMware 的机器默认选中虚拟网卡」那类现场问题的延伸；
//   - 枚举失败：**放行**（不是收紧）。枚举失败时该地址仍是具体的本机地址，
//     用它至少比留空好；而在这里 fail-close 会让一次网卡读取抖动变成"保存不了配置"。
//     注意这与设备清理闸门的 fail-closed 不同：那里失败会写到错误的设备上，这里只是选个地址。
func listenAddressInferable(listenIP string, provider InterfaceProvider) bool {
	if !validIPv4(listenIP, false) {
		return false
	}
	items, err := EnumerateNetworkAddresses(provider)
	if err != nil {
		return true
	}
	for _, item := range items {
		if item.IP == listenIP {
			return !item.Virtual && !item.Loopback && !item.ListenOnly
		}
	}
	// 不在本机网卡列表里（例如管理员手填了别的出口地址）：无从判断虚拟，
	// 但它是个具体地址，按可用处理 —— 收紧会让这类编辑直接存不进去。
	return true
}
