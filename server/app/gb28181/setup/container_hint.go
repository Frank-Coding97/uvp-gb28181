package setup

// containerAddressWarning 是给平台接入向导用的容器场景提示语。
const containerAddressWarning = "平台运行在容器中：以上是容器内地址，容器若未使用 host 网络，设备通常访问不到，请改填设备可达的宿主机地址。"

// ContainerAddressWarning 在"平台跑在容器里、且看不到任何宿主机侧网卡"时给出提示语，否则返回空串。
//
// 为什么需要它：Docker 默认的 bridge 网络下，容器里 `net.Interfaces()` 只有 `eth0` + `lo`，
// 一张"虚拟网卡"都没有 —— 于是虚拟网卡判定完全不触发，向导会把**容器内地址**（如 172.17.0.2）
// 当成推荐地址推荐出去。而 GB28181 要求设备主动把 RTP 推回平台，这个地址在局域网里路由不到，
// 设备侧表现为"注册上了但没画面"。这不是网卡判定的锅，是容器网络拓扑的固有限制，只能提示。
//
// 反向：容器用 `--net=host` 时能看见宿主机的 docker0 / br-xxx / vethxxx（都会被判为虚拟），
// 此时容器地址就是宿主机地址，提示反而是噪音 ⇒ 只要枚举结果里出现过虚拟网卡就不提示。
//
// inContainer 由调用方注入（生产用 netif.InContainer()），保证用例不受运行环境影响。
func ContainerAddressWarning(items []NetworkAddress, inContainer bool) string {
	if !inContainer {
		return ""
	}
	for _, item := range items {
		if item.Virtual {
			return ""
		}
	}
	return containerAddressWarning
}
