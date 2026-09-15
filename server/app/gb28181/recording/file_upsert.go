package recording

import (
	"context"
	"errors"
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

func loadRecordingSnapshots(tx *gorm.DB, file *models.GbRecordingFile) error {
	var channel models.GbChannel
	result := tx.First(&channel, file.ChannelID)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}
	if result.Error == nil {
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
	result = tx.Where("device_id = ?", file.DeviceID).Limit(1).Find(&device)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		file.DeviceName = preferredName(device.Alias, device.Name)
	}
	return nil
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
