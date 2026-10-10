package recording

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
)

type ReconcileCandidate struct {
	NodeID    int64
	VHost     string
	App       string
	Stream    string
	SessionID *uint64
	ChannelID uint
	DeviceID  string
}

type ReconcileUnitResult struct {
	Discovered int
	Inserted   int
	Updated    int
	Missing    int
}

func (r *GormRepo) ListReconcileCandidates(ctx context.Context) ([]ReconcileCandidate, error) {
	var sessions []models.GbRecordingSession
	if err := r.db.WithContext(ctx).Order("id").Find(&sessions).Error; err != nil {
		return nil, err
	}
	var channels []models.GbChannel
	if err := r.db.WithContext(ctx).Where("stream_id IS NOT NULL AND stream_id <> ''").Order("id").Find(&channels).Error; err != nil {
		return nil, err
	}
	result := make([]ReconcileCandidate, 0, len(sessions)+len(channels))
	seen := make(map[string]int)
	for i := range sessions {
		session := sessions[i]
		candidate := ReconcileCandidate{NodeID: session.NodeID, VHost: session.VHost, App: session.App, Stream: session.Stream, SessionID: &session.ID, ChannelID: session.ChannelID, DeviceID: session.DeviceID}
		seen[reconcileCandidateKey(candidate)] = len(result)
		result = append(result, candidate)
	}
	for i := range channels {
		channel := channels[i]
		candidate := ReconcileCandidate{VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp, Stream: channel.StreamID, ChannelID: channel.ID, DeviceID: channel.DeviceID}
		key := reconcileCandidateKey(candidate)
		if _, exists := seen[key]; exists {
			continue
		}
		result = append(result, candidate)
	}
	return result, nil
}

func reconcileCandidateKey(candidate ReconcileCandidate) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%s", candidate.NodeID, candidate.VHost, candidate.App, candidate.Stream)
}

func (r *GormRepo) SaveReconcileState(ctx context.Context, state *models.GbRecordingReconcileState) error {
	return r.db.WithContext(ctx).Save(state).Error
}

// BackfillAttribution 回填历史录像缺失的归属名称（设备名/通道名/编码/部门）。
//
// ⛔ 为什么需要它：归属名称只在 `loadRecordingSnapshots` **写入那一刻**快照一次。
// 早期版本有两个缺陷叠加：
//  ① `if result.Error == nil` 判据在装了 `MaskNotDataError` 的生产环境里恒真
//     ⇒ 通道查不到时拿零值覆盖归属（见 file_upsert.go 的注释）；
//  ② `channel_id` 存的是 `gb_channel` 自增主键，通道行被删后主键关联彻底失效。
// ⇒ 存量库里已经有一批 `channel_name` / `device_name` 为空的记录，
// 光修写入路径救不回来（那些文件不会再被重新写一遍）。
//
// 这里按 `channel_code`（国标通道编码，稳定标识）反查通道与设备并回填。
// 实测开发库：有编码的记录 100% 能反查回通道与设备。
//
// 幂等：只更新"当前为空"的字段，已经有值的绝不覆盖（避免把用户改过的名字刷掉）。
func (r *GormRepo) BackfillAttribution(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}
	var files []models.GbRecordingFile
	// 条件：至少缺一项，且有 channel_code 可供反查
	err := r.db.WithContext(ctx).
		Where("(channel_name = '' OR device_name = '') AND channel_code <> ''").
		Order("id ASC").Limit(limit).Find(&files).Error
	if err != nil {
		return 0, err
	}
	updated := 0
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range files {
			file := files[i]
			// 复用写入路径的同一套解析逻辑，保证"回填"与"新写"口径一致
			before := file
			if err := loadRecordingSnapshots(tx, &file); err != nil {
				return err
			}
			updates := map[string]any{}
			if before.ChannelName == "" && file.ChannelName != "" {
				updates["channel_name"] = file.ChannelName
			}
			if before.DeviceName == "" && file.DeviceName != "" {
				updates["device_name"] = file.DeviceName
			}
			if before.ChannelCode == "" && file.ChannelCode != "" {
				updates["channel_code"] = file.ChannelCode
			}
			// 主键已失效但反查到了 ⇒ 顺手修正成有效行的主键
			if before.ChannelID != file.ChannelID {
				updates["channel_id"] = file.ChannelID
			}
			if before.DeviceID == "" && file.DeviceID != "" {
				updates["device_id"] = file.DeviceID
			}
			if len(updates) == 0 {
				continue
			}
			if err := tx.Model(&models.GbRecordingFile{}).Where("id = ?", file.ID).Updates(updates).Error; err != nil {
				return err
			}
			updated++
		}
		return nil
	})
	return updated, err
}

func (r *GormRepo) ApplyReconcileUnit(ctx context.Context, candidate ReconcileCandidate, recordDate time.Time, files []zlm.MP4RecordFile, now time.Time) (ReconcileUnitResult, error) {
	result := ReconcileUnitResult{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seen := make(map[string]struct{}, len(files))
		for _, listed := range files {
			key := BuildFileKey(candidate.NodeID, listed.FilePath)
			seen[key] = struct{}{}
			date := recordDate
			file := &models.GbRecordingFile{
				SessionID: candidate.SessionID, ChannelID: candidate.ChannelID, DeviceID: candidate.DeviceID,
				NodeID: candidate.NodeID, VHost: candidate.VHost, App: candidate.App, Stream: candidate.Stream,
				FileKey: key, FileName: listed.FileName, FilePath: listed.FilePath, Folder: listed.Folder, URL: listed.URL,
				MetadataState: models.RecordingMetadataPartial, Source: models.RecordingFileSourceReconcile,
				RecordDate: &date, DiscoveredAt: now, LastSeenAt: &now, UpdatedAt: now,
			}
			if err := loadRecordingSnapshots(tx, file); err != nil {
				return err
			}
			created, updated, err := upsertCatalogFile(tx, file, now)
			if err != nil {
				return err
			}
			result.Discovered++
			if created {
				result.Inserted++
			}
			if updated {
				result.Updated++
			}
		}

		var existing []models.GbRecordingFile
		query := tx.Where("node_id = ? AND vhost = ? AND app = ? AND stream = ? AND record_date = ?", candidate.NodeID, candidate.VHost, candidate.App, candidate.Stream, recordDate)
		if err := query.Find(&existing).Error; err != nil {
			return err
		}
		for i := range existing {
			file := existing[i]
			if _, found := seen[file.FileKey]; found {
				continue
			}
			misses := file.ReconcileMissCount + 1
			updates := map[string]any{"reconcile_miss_count": misses, "updated_at": now}
			if misses >= 2 && file.MissingAt == nil {
				updates["missing_at"] = now
				result.Missing++
			}
			if err := tx.Model(&models.GbRecordingFile{}).Where("id = ?", file.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}
