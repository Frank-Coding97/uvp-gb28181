package node

// CapacityThresholdPortUsage 端口使用率超过此阈值视为接近容量(0.8 = 80%)
const CapacityThresholdPortUsage = 0.8

// CapacityThresholdCPU CPU 负载超过此阈值视为接近容量(0.8 = 80%)
const CapacityThresholdCPU = 0.8

// PortUsage 当前已用 RTP 端口数 / 端口范围总数;无端口范围返 0
func (n Node) PortUsage() float64 {
	total := n.RTPPortEnd - n.RTPPortStart
	if total <= 0 {
		return 0
	}
	return float64(n.Stats.MediaSourceCount) / float64(total)
}

// CPULoad 综合 CPU 负载(网络 + 工作线程加权)
func (n Node) CPULoad() float64 {
	return n.Stats.NetThreadLoadAvg*0.6 + n.Stats.WorkThreadLoadAvg*0.4
}

// IsNearCapacity 是否接近容量(端口使用 >= 80% 或 CPU >= 80%)
//
// 调度器 Pick 时会自动跳过 NearCapacity 节点;UI 上黄色高亮提示运维。
func (n Node) IsNearCapacity() bool {
	return n.PortUsage() >= CapacityThresholdPortUsage || n.CPULoad() >= CapacityThresholdCPU
}
