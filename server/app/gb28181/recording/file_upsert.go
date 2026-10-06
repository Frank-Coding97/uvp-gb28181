package recording

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func (r *GormRepo) UpsertCompleteFile(ctx context.Context, attribution RecordingFileAttribution, event RecordMP4Event) error {
	startTime, timeLen, fileSize := event.StartTime, event.TimeLen, event.FileSize
	now := r.currentTime()
	file := &models.GbRecordingFile{
		SessionID: attribution.SessionID, ChannelID: attribution.ChannelID, DeviceID: attribution.DeviceID,
		NodeID: attribution.NodeID, VHost: event.VHost, App: event.App, Stream: event.Stream,
		FileName: event.FileName, FilePath: event.FilePath, Folder: event.Folder, URL: event.URL,
		StartTime: &startTime, TimeLen: &timeLen, FileSize: &fileSize,
		Source: models.RecordingFileSourceHook, MetadataState: models.RecordingMetadataComplete,
		RecordDate: datePointer(startTime), DiscoveredAt: now, LastSeenAt: &now, UpdatedAt: now,
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := loadRecordingSnapshots(tx, file); err != nil {
			return err
		}
		_, _, err := upsertCatalogFile(tx, file, now)
		return err
	})
}

// loadRecordingSnapshots 在写入录像文件前补齐"设备名/通道名/归属"快照。
//
// ⛔⛔ 判据必须是"查没查到"，**不能只看 err** —— 生产每个 gorm 实例都注册了
// `MaskNotDataError`（app/utils/gormhelper/client.go），`First` 查不到时返回
// **nil error + 零值结构体**，所以 `if result.Error == nil` 在生产恒为真，
// 会拿零值当"查到了"，把归属信息覆盖成空（2026-10-06 定位）。
//
// ⛔ `file.ChannelID` 存的是 `gb_channel` 的**自增主键**，不是国标通道编码；
// 通道行被删后主键关联直接失效 ⇒ 名称快照永久缺失。
// 所以这里**先用主键查，查不到再按 `channel_code`（国标编码，稳定）反查一次**，
// 后者能把"通道被删但设备还在"的存量录像救回来。
func loadRecordingSnapshots(tx *gorm.DB, file *models.GbRecordingFile) error {
	channel, found := lookupChannel(tx, file)
	if found {
		file.ChannelCode = channel.ChannelID
		file.ChannelName = preferredName(channel.Alias, channel.Name)
		file.OwnerDeptID = channel.OwnerDeptID
		if file.DeviceID == "" {
			file.DeviceID = channel.DeviceID
		}
	}
	if file.DeviceID == "" {
		return nil
	}
	var device models.GbDevice
	result := tx.Where("device_id = ?", file.DeviceID).Limit(1).Find(&device)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		file.DeviceName = preferredName(device.Alias, device.Name)
	}
	return nil
}

// lookupChannel 先按主键查通道；查不到再按国标编码（channel_code）反查。
// 两条路都查不到才算真的没有。
//
// ⛔ 判据说明：这里**故意不把 `err != nil` 当成"查不到"就立即返回**。
// `First` 查不到时的 err 形态**取决于是否装了 `MaskNotDataError`**：
//   · 生产（装了屏蔽回调）⇒ err == nil，靠 RowsAffected 判断；
//   · 未装回调的环境（部分单测、外部工具、直接 new 的 gorm）⇒ err 可能是 ErrRecordNotFound。
// 两种都要走兜底，所以这里只把"确实查到了"当成功，其余一律继续尝试按编码反查。
func lookupChannel(tx *gorm.DB, file *models.GbRecordingFile) (models.GbChannel, bool) {
	if file.ChannelID != 0 {
		var channel models.GbChannel
		result := tx.First(&channel, file.ChannelID)
		if result.Error == nil && result.RowsAffected > 0 {
			return channel, true
		}
		// ⛔ 不在这里 return —— 没查到不等于结束，还要按国标编码兜底。
	}
	// 主键没命中 ⇒ 通道行可能已被清理，改用稳定的国标编码再试一次
	if file.ChannelCode == "" {
		return models.GbChannel{}, false
	}
	var channel models.GbChannel
	result := tx.Where("channel_id = ?", file.ChannelCode).Limit(1).Find(&channel)
	if result.Error != nil || result.RowsAffected == 0 {
		return models.GbChannel{}, false
	}
	// 主键已失效 ⇒ 把主键刷成当前有效行，后续写入不再指向死引用
	if channel.ID != file.ChannelID {
		file.ChannelID = channel.ID
	}
	return channel, true
}

func preferredName(alias, name string) string {
	if value := strings.TrimSpace(alias); value != "" {
		return value
	}
	return name
}

func (r *GormRepo) UpsertCatalogFile(ctx context.Context, file *models.GbRecordingFile) (bool, bool, error) {
	now := r.currentTime()
	var created, updated bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		created, updated, err = upsertCatalogFile(tx, file, now)
		return err
	})
	return created, updated, err
}

func upsertCatalogFile(tx *gorm.DB, file *models.GbRecordingFile, now time.Time) (bool, bool, error) {
	prepareCatalogFile(file, now)
	var existing models.GbRecordingFile
	result := tx.Where("file_key = ?", file.FileKey).Limit(1).Find(&existing)
	if result.Error != nil {
		return false, false, result.Error
	}
	if result.RowsAffected == 0 {
		insert := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "file_key"}}, DoNothing: true}).Create(file)
		if insert.Error != nil {
			return false, false, insert.Error
		}
		if insert.RowsAffected > 0 {
			return true, false, nil
		}
		if err := tx.Where("file_key = ?", file.FileKey).First(&existing).Error; err != nil {
			return false, false, err
		}
	}

	updates := map[string]any{
		"last_seen_at": now, "missing_at": nil, "reconcile_miss_count": 0, "updated_at": now,
	}
	if file.MetadataState == models.RecordingMetadataComplete {
		updates["session_id"] = file.SessionID
		updates["channel_id"] = file.ChannelID
		updates["device_id"] = file.DeviceID
		updates["channel_code"] = file.ChannelCode
		updates["channel_name"] = file.ChannelName
		updates["device_name"] = file.DeviceName
		updates["owner_dept_id"] = file.OwnerDeptID
		updates["vhost"] = file.VHost
		updates["app"] = file.App
		updates["stream"] = file.Stream
		updates["file_name"] = file.FileName
		updates["file_path"] = file.FilePath
		updates["folder"] = file.Folder
		updates["url"] = file.URL
		updates["start_time"] = file.StartTime
		updates["time_len"] = file.TimeLen
		updates["file_size"] = file.FileSize
		updates["source"] = models.RecordingFileSourceHook
		updates["metadata_state"] = models.RecordingMetadataComplete
		updates["record_date"] = file.RecordDate
	}
	if err := tx.Model(&models.GbRecordingFile{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return false, false, err
	}
	file.ID = existing.ID
	file.CreatedAt = existing.CreatedAt
	file.DiscoveredAt = existing.DiscoveredAt
	return false, true, nil
}

func prepareCatalogFile(file *models.GbRecordingFile, now time.Time) {
	if file.FileKey == "" {
		file.FileKey = BuildFileKey(file.NodeID, file.FilePath)
	}
	if file.Source == "" {
		file.Source = models.RecordingFileSourceHook
	}
	if file.MetadataState == "" {
		file.MetadataState = models.RecordingMetadataComplete
	}
	if file.DiscoveredAt.IsZero() {
		file.DiscoveredAt = now
	}
	if file.LastSeenAt == nil {
		file.LastSeenAt = &now
	}
	if file.RecordDate == nil && file.StartTime != nil {
		file.RecordDate = datePointer(*file.StartTime)
	}
	file.UpdatedAt = now
}

func datePointer(value time.Time) *time.Time {
	date := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
	return &date
}
