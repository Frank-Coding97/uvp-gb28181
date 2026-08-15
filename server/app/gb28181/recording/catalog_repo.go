package recording

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const (
	AvailabilityAvailable         = "available"
	AvailabilityNodeOffline       = "node_offline"
	AvailabilityNodeMissing       = "node_missing"
	AvailabilityFileMissing       = "file_missing"
	AvailabilityAccessUnavailable = "access_unavailable"
)

var ErrRecordingFileNotFound = errors.New("recording file not found")

type FileQuery struct {
	Page              int
	PageSize          int
	Start             *time.Time
	End               *time.Time
	ChannelID         *uint
	DeviceID          string
	NodeID            *int64
	Keyword           string
	MetadataState     string
	Availability      string
	AllowedDeptIDs    []uint
	FullAccess        bool
	KnownNodeIDs      []int64
	OfflineNodeIDs    []int64
	AccessibleNodeIDs []int64
}

type FilePage struct {
	Files    []models.GbRecordingFile
	Total    int64
	Page     int
	PageSize int
}

type CatalogOptions struct {
	NodeIDs []int64
}

func (r *GormRepo) ListCatalogFiles(ctx context.Context, query FileQuery) (FilePage, error) {
	query = normalizeFileQuery(query)
	base := r.catalogFileQuery(ctx, query)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return FilePage{}, err
	}
	files := make([]models.GbRecordingFile, 0, query.PageSize)
	err := base.
		Order("CASE WHEN start_time IS NULL THEN 1 ELSE 0 END").
		Order("start_time DESC").Order("id DESC").
		Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).
		Find(&files).Error
	return FilePage{Files: files, Total: total, Page: query.Page, PageSize: query.PageSize}, err
}

func (r *GormRepo) CatalogOptions(ctx context.Context, query FileQuery) (CatalogOptions, error) {
	query = normalizeFileQuery(query)
	result := CatalogOptions{NodeIDs: make([]int64, 0)}
	if err := r.catalogFileQuery(ctx, query).Distinct("node_id").Order("node_id").Pluck("node_id", &result.NodeIDs).Error; err != nil {
		return CatalogOptions{}, err
	}
	return result, nil
}

func (r *GormRepo) GetCatalogFile(ctx context.Context, id uint64, allowedDeptIDs []uint, fullAccess bool) (*models.GbRecordingFile, error) {
	query := FileQuery{AllowedDeptIDs: allowedDeptIDs, FullAccess: fullAccess}
	var file models.GbRecordingFile
	result := r.catalogFileQuery(ctx, query).Where("id = ?", id).Limit(1).Find(&file)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrRecordingFileNotFound
	}
	return &file, nil
}

func (r *GormRepo) ListActiveCatalogSessions(ctx context.Context, allowedDeptIDs []uint, fullAccess bool) ([]ActiveCatalogSession, error) {
	query := r.db.WithContext(ctx).
		Table("gb_recording_session AS recording_session").
		Select(`recording_session.id, recording_session.channel_id, channel.channel_id AS channel_code,
			CASE WHEN channel.alias <> '' THEN channel.alias ELSE channel.name END AS channel_name,
			recording_session.device_id, recording_session.node_id, recording_session.state,
			recording_session.started_at, recording_session.updated_at`).
		Joins("JOIN gb_channel AS channel ON channel.id = recording_session.channel_id").
		Where("recording_session.state IN ?", []string{
			models.RecordingSessionStateStarting,
			models.RecordingSessionStateRecording,
			models.RecordingSessionStateStopping,
		})
	if !fullAccess {
		if len(allowedDeptIDs) == 0 {
			query = query.Where("1 = 0")
		} else {
			query = query.Where("channel.owner_dept_id IN ?", allowedDeptIDs)
		}
	}
	result := make([]ActiveCatalogSession, 0)
	err := query.Order("recording_session.started_at DESC").Order("recording_session.id DESC").Scan(&result).Error
	return result, err
}

func (r *GormRepo) ListCatalogReconcileStates(ctx context.Context) ([]models.GbRecordingReconcileState, error) {
	states := make([]models.GbRecordingReconcileState, 0)
	err := r.db.WithContext(ctx).Order("node_id").Find(&states).Error
	return states, err
}

func (r *GormRepo) catalogFileQuery(ctx context.Context, query FileQuery) *gorm.DB {
	db := r.db.WithContext(ctx).Model(&models.GbRecordingFile{})
	if !query.FullAccess {
		if len(query.AllowedDeptIDs) == 0 {
			return db.Where("1 = 0")
		}
		db = db.Where("owner_dept_id IN ?", query.AllowedDeptIDs)
	}
	if query.Start != nil {
		db = db.Where("start_time >= ?", *query.Start)
	}
	if query.End != nil {
		db = db.Where("start_time < ?", *query.End)
	}
	if query.ChannelID != nil {
		db = db.Where("channel_id = ?", *query.ChannelID)
	}
	if query.DeviceID != "" {
		db = db.Where("device_id = ?", query.DeviceID)
	}
	if query.NodeID != nil {
		db = db.Where("node_id = ?", *query.NodeID)
	}
	if query.MetadataState != "" {
		db = db.Where("metadata_state = ?", query.MetadataState)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		like := "%" + strings.ToLower(keyword) + "%"
		db = db.Where("LOWER(file_name) LIKE ? OR LOWER(channel_name) LIKE ? OR LOWER(channel_code) LIKE ? OR LOWER(device_id) LIKE ? OR LOWER(device_name) LIKE ?", like, like, like, like, like)
	}
	return applyAvailabilityFilter(db, query)
}

func applyAvailabilityFilter(db *gorm.DB, query FileQuery) *gorm.DB {
	switch query.Availability {
	case AvailabilityNodeMissing:
		if len(query.KnownNodeIDs) == 0 {
			return db
		}
		return db.Where("node_id NOT IN ?", query.KnownNodeIDs)
	case AvailabilityNodeOffline:
		return whereIDs(db, "node_id", query.OfflineNodeIDs)
	case AvailabilityFileMissing:
		return whereIDs(db, "node_id", onlineNodeIDs(query.KnownNodeIDs, query.OfflineNodeIDs)).Where("missing_at IS NOT NULL")
	case AvailabilityAccessUnavailable:
		online := onlineNodeIDs(query.KnownNodeIDs, query.OfflineNodeIDs)
		if len(online) == 0 {
			return db.Where("1 = 0")
		}
		db = db.Where("node_id IN ?", online).Where("missing_at IS NULL")
		if len(query.AccessibleNodeIDs) == 0 {
			return db
		}
		return db.Where("node_id NOT IN ?", query.AccessibleNodeIDs)
	case AvailabilityAvailable:
		return whereIDs(db, "node_id", query.AccessibleNodeIDs).Where("missing_at IS NULL")
	default:
		return db
	}
}

func whereIDs(db *gorm.DB, column string, ids []int64) *gorm.DB {
	if len(ids) == 0 {
		return db.Where("1 = 0")
	}
	return db.Where(column+" IN ?", ids)
}

func onlineNodeIDs(known, offline []int64) []int64 {
	blocked := make(map[int64]struct{}, len(offline))
	for _, id := range offline {
		blocked[id] = struct{}{}
	}
	result := make([]int64, 0, len(known))
	for _, id := range known {
		if _, found := blocked[id]; !found {
			result = append(result, id)
		}
	}
	return result
}

func CatalogAvailability(file models.GbRecordingFile, knownNodeIDs, offlineNodeIDs, accessibleNodeIDs []int64) string {
	if !containsNodeID(knownNodeIDs, file.NodeID) {
		return AvailabilityNodeMissing
	}
	if containsNodeID(offlineNodeIDs, file.NodeID) {
		return AvailabilityNodeOffline
	}
	if file.MissingAt != nil {
		return AvailabilityFileMissing
	}
	if !containsNodeID(accessibleNodeIDs, file.NodeID) {
		return AvailabilityAccessUnavailable
	}
	return AvailabilityAvailable
}

func containsNodeID(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func normalizeFileQuery(query FileQuery) FileQuery {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	if query.PageSize > 200 {
		query.PageSize = 200
	}
	return query
}

func (r *GormRepo) currentTime() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}
