package recording

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

var ErrChannelNotFound = errors.New("通道不存在")

type GormRepo struct {
	db *gorm.DB
}

func NewGormRepo(db *gorm.DB) *GormRepo {
	return &GormRepo{db: db}
}

func (r *GormRepo) GetChannel(ctx context.Context, channelID uint) (*models.GbChannel, error) {
	var channel models.GbChannel
	result := r.db.WithContext(ctx).First(&channel, channelID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrChannelNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &channel, nil
}

func (r *GormRepo) FindChannelByStream(ctx context.Context, streamID string) (*models.GbChannel, error) {
	var channel models.GbChannel
	result := r.db.WithContext(ctx).Where("stream_id = ?", streamID).First(&channel)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &channel, nil
}

func (r *GormRepo) SetDesired(ctx context.Context, channelID uint, enabled bool) (*models.GbChannel, error) {
	now := time.Now()
	state := models.CloudRecordingStateStopping
	if enabled {
		state = models.CloudRecordingStateStarting
	}
	result := r.db.WithContext(ctx).Model(&models.GbChannel{}).
		Where("id = ?", channelID).
		Updates(map[string]any{
			"cloud_recording_enabled":    enabled,
			"cloud_recording_state":      state,
			"cloud_recording_error":      "",
			"cloud_recording_updated_at": now,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrChannelNotFound
	}
	return r.GetChannel(ctx, channelID)
}

func (r *GormRepo) MarkState(ctx context.Context, channelID uint, expectedEnabled bool, state, message string) (bool, error) {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&models.GbChannel{}).
		Where("id = ? AND cloud_recording_enabled = ?", channelID, expectedEnabled).
		Updates(map[string]any{
			"cloud_recording_state":      state,
			"cloud_recording_error":      message,
			"cloud_recording_updated_at": now,
		})
	return result.RowsAffected > 0, result.Error
}

func (r *GormRepo) ListEnabledChannels(ctx context.Context) ([]models.GbChannel, error) {
	var channels []models.GbChannel
	err := r.db.WithContext(ctx).
		Where("cloud_recording_enabled = ?", true).
		Order("id").
		Find(&channels).Error
	return channels, err
}

func (r *GormRepo) UpsertSession(ctx context.Context, session *models.GbRecordingSession) error {
	row := *session
	row.ID = 0
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "node_id"}, {Name: "vhost"}, {Name: "app"}, {Name: "stream"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"channel_id", "device_id", "state", "started_at", "stopped_at",
			"last_checked_at", "last_error", "updated_at",
		}),
	}).Create(&row).Error
	if err != nil {
		return err
	}
	stored, err := r.FindSessionByMedia(ctx, row.NodeID, row.VHost, row.App, row.Stream)
	if err != nil {
		return err
	}
	*session = *stored
	return nil
}

func (r *GormRepo) FindSessionByMedia(ctx context.Context, nodeID int64, vhost, appName, stream string) (*models.GbRecordingSession, error) {
	var session models.GbRecordingSession
	result := r.db.WithContext(ctx).
		Where("node_id = ? AND vhost = ? AND app = ? AND stream = ?", nodeID, vhost, appName, stream).
		First(&session)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &session, nil
}

func (r *GormRepo) FindLatestSessionByChannel(ctx context.Context, channelID uint) (*models.GbRecordingSession, error) {
	var session models.GbRecordingSession
	result := r.db.WithContext(ctx).
		Where("channel_id = ?", channelID).
		Order("id DESC").
		First(&session)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &session, nil
}

func (r *GormRepo) MarkSessionStopped(ctx context.Context, sessionID uint64, message string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&models.GbRecordingSession{}).
		Where("id = ?", sessionID).
		Updates(map[string]any{
			"state":           models.RecordingSessionStateStopped,
			"stopped_at":      now,
			"last_checked_at": now,
			"last_error":      message,
		}).Error
}

func (r *GormRepo) ListUnfinishedSessions(ctx context.Context) ([]models.GbRecordingSession, error) {
	var sessions []models.GbRecordingSession
	err := r.db.WithContext(ctx).
		Where("state <> ?", models.RecordingSessionStateStopped).
		Order("id").
		Find(&sessions).Error
	return sessions, err
}

func (r *GormRepo) InsertFile(ctx context.Context, file *models.GbRecordingFile) (bool, error) {
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "node_id"}, {Name: "file_path"}},
		DoNothing: true,
	}).Create(file)
	return result.RowsAffected > 0, result.Error
}
