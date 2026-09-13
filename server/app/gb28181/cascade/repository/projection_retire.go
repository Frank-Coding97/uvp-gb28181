package repository

import (
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
)

// RetireDeletedSources 回收一批"源已经不存在"的共享投影,在源数据的删除事务里调用。
//
// 为什么必须回收,而不是留一行 active=false:
// 两张投影表的唯一索引(uk_cascade_device_published / uk_cascade_channel_published)
// 只包含 (platform_id, published_*),不含 deleted_at 也不含 active。只要行还在,
// 哪怕它指向的源设备早就被删了,这个键位就被永久占住。
// 设备从设备列表删掉、之后再重新接入时,gb_device 拿到的是一条新的自增 id,
// 而 published_device_id 仍然是同一个国标号 —— upsertDeviceProjection 按
// source_device_id 查不到任何行,于是走去 INSERT,直接撞 1062 Duplicate entry,
// 共享接口只能回 409「资源状态冲突」。这就是"设备删了、级联数据没跟着删"的后果。
//
// 反过来,源还在、只是被取消共享的行必须保留(active=false):
// allocatePublishedChannelIDs 明确依赖 "Inactive mappings remain reserved",
// 保证同一路通道重新共享时对上级的 published id 不抖动。所以这里只回收
// "源已经不存在"的行,不做全量清理,也不碰 active=false 的活源行。
//
// 注意别把这条规则套到 catalog pipeline 的软删上:catalog/pipeline.go 收到上级
// Catalog DEL 时是软删 gb_channel(行还在,之后 ADD 会用同一个自增 id 恢复),
// 那种情况下源还会带着原 id 回来,投影必须留着,不能当成"源已消失"回收。
// 只有设备列表里的硬删(gb_device/gb_channel 被 Unscoped 真删)才走这里。
//
// 必须 Unscoped 硬删:软删的行同样占着唯一键位,留着等于把键位钉死。
// 这与 device_delete.go 处理 gb_device/gb_channel 的口径一致(那里也必须 Unscoped)。
func RetireDeletedSources(tx *gorm.DB, sourceDeviceIDs, sourceChannelIDs []uint64) (int64, error) {
	if tx == nil || (len(sourceDeviceIDs) == 0 && len(sourceChannelIDs) == 0) {
		return 0, nil
	}

	var deviceRows []model.GbCascadeDeviceProjection
	if len(sourceDeviceIDs) > 0 {
		if err := tx.Unscoped().Where("source_device_id IN ?", sourceDeviceIDs).Find(&deviceRows).Error; err != nil {
			return 0, err
		}
	}
	deviceProjectionIDs := make([]uint64, 0, len(deviceRows))
	platforms := make([]uint64, 0, len(deviceRows))
	for _, row := range deviceRows {
		deviceProjectionIDs = append(deviceProjectionIDs, row.ID)
		platforms = append(platforms, row.PlatformID)
	}

	// 先查通道行再删,顺序上是"先通道后设备":通道行通过 device_projection_id
	// 挂在设备行上,先删设备行不会报错,但会让这些通道行变成新的孤儿。
	channelQuery := tx.Unscoped()
	switch {
	case len(deviceProjectionIDs) > 0 && len(sourceChannelIDs) > 0:
		channelQuery = channelQuery.Where("device_projection_id IN ? OR source_channel_id IN ?", deviceProjectionIDs, sourceChannelIDs)
	case len(deviceProjectionIDs) > 0:
		channelQuery = channelQuery.Where("device_projection_id IN ?", deviceProjectionIDs)
	default:
		channelQuery = channelQuery.Where("source_channel_id IN ?", sourceChannelIDs)
	}
	var channelRows []model.GbCascadeChannelProjection
	if err := channelQuery.Find(&channelRows).Error; err != nil {
		return 0, err
	}
	channelIDs := make([]uint64, 0, len(channelRows))
	for _, row := range channelRows {
		channelIDs = append(channelIDs, row.ID)
		platforms = append(platforms, row.PlatformID)
	}

	retired := int64(0)
	if len(channelIDs) > 0 {
		result := tx.Unscoped().Where("id IN ?", channelIDs).Delete(&model.GbCascadeChannelProjection{})
		if result.Error != nil {
			return 0, result.Error
		}
		retired += result.RowsAffected
	}
	if len(deviceProjectionIDs) > 0 {
		result := tx.Unscoped().Where("id IN ?", deviceProjectionIDs).Delete(&model.GbCascadeDeviceProjection{})
		if result.Error != nil {
			return 0, result.Error
		}
		retired += result.RowsAffected
	}
	if retired == 0 {
		return 0, nil
	}
	if err := bumpProjectionRevisionForPlatforms(tx, platforms); err != nil {
		return 0, err
	}
	return retired, nil
}

// bumpProjectionRevisionForPlatforms 推进受影响平台的投影修订号。
//
// 目的不是记账,而是让还开着共享编辑弹窗的人拿到冲突(409)而不是静默生效:
// 他的旧快照里可能仍然勾着这台已经被删掉的设备,提交上来会重新造出一条
// 指向不存在设备的幽灵投影,而且这次因为键位空出来了,连 1062 都不会报。
func bumpProjectionRevisionForPlatforms(tx *gorm.DB, platformIDs []uint64) error {
	unique := make([]uint64, 0, len(platformIDs))
	seen := make(map[uint64]struct{}, len(platformIDs))
	for _, id := range platformIDs {
		if id == 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return nil
	}
	return tx.Model(&model.GbCascadePlatform{}).
		Where("id IN ?", unique).
		Updates(map[string]any{
			"projection_revision": gorm.Expr("projection_revision + ?", 1),
			"updated_at":          time.Now().UTC(),
		}).Error
}
