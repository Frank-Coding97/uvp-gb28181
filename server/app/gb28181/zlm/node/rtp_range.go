package node

import "strconv"

// RTP 收流范围（GB28181 设备把流推进 ZLM 的那段端口）**只有一个真源**：
// ZLM 自己配置里的 `[rtp_proxy] port_range`。
//
// 平台侧的 `meta_node.rtp_port_start/rtp_port_end` 只是**登记值/镜像**：
//   - 调度用它算端口容量（见 capacity.go 的 PortUsage），
//   - 节点列表与编辑弹窗展示它。
//
// 两者一旦漂移，容量就按错的范围算 —— 登记 30000-35000（5000 个）而 ZLM 实际
// 只开 51014-51063（49 个）时，平台会一直以为节点很空，直到 ZLM 端口真的耗光、
// 推流/点播开始失败才暴露。所以凡是"读到 ZLM 实际范围"的地方都必须回写登记值，
// 本函数就是那个唯一解析入口（下发校验与读值回填共用一套规则，不许各写一份）。

// ParseRTPPortRange 解析形如 `51014-51063` 的收流范围。
//
// 规则（与「服务配置」下发校验完全一致，刻意从严）：
//   - 必须恰好一个 `-` 分成两段；
//   - 两段都是纯十进制整数（`"1 - 2"` / `" 51014-51063"` / `"1e3"` 都判非法，
//     不做 trim、不认科学计数法）；
//   - `1 <= start <= end <= 65535`（start == end 合法：容量为 0，但不是脏数据）。
//
// ok=false 时调用方**必须保留原值**，不许把返回的 0 当端口用。
func ParseRTPPortRange(value string) (start, end int, ok bool) {
	sep := -1
	for i := 0; i < len(value); i++ {
		if value[i] == '-' {
			if sep >= 0 {
				return 0, 0, false // 多于一个 '-'
			}
			sep = i
		}
	}
	if sep <= 0 || sep == len(value)-1 {
		return 0, 0, false
	}
	start, startErr := strconv.Atoi(value[:sep])
	end, endErr := strconv.Atoi(value[sep+1:])
	if startErr != nil || endErr != nil || start < 1 || end > 65535 || start > end {
		return 0, 0, false
	}
	return start, end, true
}

// RTPPortRangeString 把范围渲染成 ZLM 配置里的写法，供日志/对比用。
func RTPPortRangeString(start, end int) string {
	if start <= 0 || end <= 0 {
		return ""
	}
	return strconv.Itoa(start) + "-" + strconv.Itoa(end)
}
