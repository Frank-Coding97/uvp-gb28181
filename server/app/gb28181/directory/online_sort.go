package directory

import "sort"

// SortOnlineDevicesFirst 让同一目录下的在线设备排在离线设备前面
// （产品要求：设备目录树上「在线的设备在最上边」）。
//
// 三条约束，少一条这棵树就会每次刷新都换位置：
//  1. 只重排同一父节点下的兄弟，层级和 Depth 一律不动；
//  2. 只重排设备叶子（Device != nil），同一在线状态内保持调用方给的原始顺序
//     （稳定排序，原始顺序 = 区划/部门编码、设备 ID 升序）；
//  3. 目录节点（区域/部门/业务分组/未分组）的槽位不动 —— 那是由区划编码、
//     部门 SortOrder、分组名决定的业务顺序，按「下面几台在线」重排等于把目录树打散。
//
// 原地修改；调用方在树拼好、资源过滤之后调一次即可（放在增量快照之前）。
func SortOnlineDevicesFirst(nodes []DirectoryNodeVO) {
	for i := range nodes {
		if len(nodes[i].Children) > 0 {
			SortOnlineDevicesFirst(nodes[i].Children)
		}
	}
	slots := make([]int, 0, len(nodes))
	for i := range nodes {
		if nodes[i].Device != nil {
			slots = append(slots, i)
		}
	}
	if len(slots) < 2 {
		return
	}
	ordered := make([]DirectoryNodeVO, len(slots))
	for i, slot := range slots {
		ordered[i] = nodes[slot]
	}
	sort.SliceStable(ordered, func(a, b int) bool {
		return ordered[a].Device.Online && !ordered[b].Device.Online
	})
	for i, slot := range slots {
		nodes[slot] = ordered[i]
	}
}
