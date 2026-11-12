package controllers

import (
	"context"

	"gorm.io/gorm"

	gbdirectory "uvplatform.com/uvp-gb28181/app/gb28181/directory"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// fillDirectoryDeviceChannelCounts 给目录树里的设备叶节点补上「通道数」。
//
// ⛔⛔ 为什么必须有：前端把设备节点的 count 当**通道数**用
// （`PlaybackSourceTree.vue` 的 `channelCount: node.count`），再据此决定要不要
// 渲染展开箭头。而构建层（national/department/custom/business）只给行政区、
// 组织、部门节点填过计数，设备叶节点（`deviceDirectoryNode`）的 Count 一直是零值
// ⇒ 多屏取流页里**任何设备节点都没有展开箭头**，连级联平台这类底下有一堆通道的
// 设备也点不开，通道根本选不到。
//
// 放在控制器层而不是各构建函数里，是因为五个视图的建树函数签名各不相同
// （有的只有 devices 没有 channels），一次回填能全部覆盖，新视图也自动受益。
//
// 也不加部门过滤：`gb_device.device_id` 全局唯一，同一台设备不可能挂两个部门，
// 树里出现的设备必然已在 `visibleDirectoryDeptIDs` 的范围里。
func fillDirectoryDeviceChannelCounts(ctx context.Context, db *gorm.DB, tree []gbdirectory.DirectoryNodeVO) error {
	codes := map[string]struct{}{}
	var collect func([]gbdirectory.DirectoryNodeVO)
	collect = func(nodes []gbdirectory.DirectoryNodeVO) {
		for i := range nodes {
			if nodes[i].Device != nil && nodes[i].Device.DeviceID != "" {
				codes[nodes[i].Device.DeviceID] = struct{}{}
			}
			collect(nodes[i].Children)
		}
	}
	collect(tree)
	if len(codes) == 0 {
		return nil
	}
	list := make([]string, 0, len(codes))
	for code := range codes {
		list = append(list, code)
	}

	type channelCount struct {
		DeviceID string `gorm:"column:device_id"`
		Total    int    `gorm:"column:total"`
		Online   int    `gorm:"column:online"`
	}
	var rows []channelCount
	if err := db.WithContext(ctx).Model(&gbmodels.GbChannel{}).
		Select("device_id, COUNT(*) AS total, SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS online", gbmodels.ChannelStatusOnline).
		Where("device_id IN ?", list).
		Group("device_id").
		Scan(&rows).Error; err != nil {
		return err
	}
	counts := make(map[string]channelCount, len(rows))
	for _, row := range rows {
		counts[row.DeviceID] = row
	}

	var apply func([]gbdirectory.DirectoryNodeVO)
	apply = func(nodes []gbdirectory.DirectoryNodeVO) {
		for i := range nodes {
			if nodes[i].Device != nil {
				row := counts[nodes[i].Device.DeviceID]
				nodes[i].Count = row.Total
				nodes[i].OnlineCount = row.Online
			}
			apply(nodes[i].Children)
		}
	}
	apply(tree)
	return nil
}
