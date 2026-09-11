package recording

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
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
