package traffic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type Direction string

var accountingLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

const (
	DirectionUpstream   Direction = "upstream"
	DirectionDownstream Direction = "downstream"
)

func (d Direction) valid() bool { return d == DirectionUpstream || d == DirectionDownstream }

type ApplyRequest struct {
	BusinessKey     string
	NodeID          int64
	MediaServerUUID string
	ZLMSessionID    string
	Direction       Direction
	DeviceCode      string
	ChannelCode     string
	OwnerDeptID     uint
	MediaKind       MediaKind
	Schema          string
	VHost           string
	App             string
	Stream          string
	CreateStamp     uint64
	AbsoluteBytes   uint64
	At              time.Time
	SettleAt        time.Time
	DurationSeconds int64
}

func (r ApplyRequest) WithAbsolute(value uint64) ApplyRequest     { r.AbsoluteBytes = value; return r }
func (r ApplyRequest) WithDirection(value Direction) ApplyRequest { r.Direction = value; return r }
func (r ApplyRequest) withBusinessKey(value string) ApplyRequest  { r.BusinessKey = value; return r }
func (r ApplyRequest) Settled(duration int64) ApplyRequest {
	r.SettleAt = r.At
	r.DurationSeconds = duration
	return r
}

type ApplyResult struct {
	DeltaBytes uint64
	Reset      bool
	Settled    bool
}

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) (*GormRepository, error) {
	if db == nil {
		return nil, errors.New("traffic repository db 不能为空")
	}
	return &GormRepository{db: db}, nil
}

func (r *GormRepository) ActiveBusinessKey(ctx context.Context, nodeID int64, app, stream string, direction Direction) (string, bool, error) {
	var row gbmodels.GbDeviceTrafficSession
	query := r.db.WithContext(ctx).Where("node_id = ? AND app = ? AND stream = ? AND direction = ? AND state = ?", nodeID, app, stream, direction, SessionActive).
		Order("id DESC").Limit(1).Find(&row)
	if query.Error != nil {
		return "", false, query.Error
	}
	if query.RowsAffected == 0 {
		return "", false, nil
	}
	return row.BusinessKey, true, nil
}

func (r *GormRepository) OpenGap(ctx context.Context, nodeID int64, reason string, at time.Time) error {
	if r == nil || r.db == nil || nodeID == 0 || reason == "" || at.IsZero() {
		return errors.New("invalid traffic gap")
	}
	if !isSQLiteDialect(r.db) {
		var existing gbmodels.GbDeviceTrafficGap
		query := r.db.WithContext(ctx).Where("node_id = ? AND reason = ? AND state = ?", nodeID, reason, "open").Limit(1).Find(&existing)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected > 0 {
			return nil
		}
		return r.db.WithContext(ctx).Create(&gbmodels.GbDeviceTrafficGap{NodeID: nodeID, Reason: reason, State: "open", StartedAt: at.UTC()}).Error
	}
	// The open-gap predicate has no unique index because closed gaps are
	// retained. Serialize the check and insert in SQLite's bounded write
	// transaction so two processes cannot create duplicate open gaps.
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing gbmodels.GbDeviceTrafficGap
		query := tx.Where("node_id = ? AND reason = ? AND state = ?", nodeID, reason, "open").Limit(1).Find(&existing)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected > 0 {
			return nil
		}
		return tx.Create(&gbmodels.GbDeviceTrafficGap{NodeID: nodeID, Reason: reason, State: "open", StartedAt: at.UTC()}).Error
	})
}

func isSQLiteDialect(db *gorm.DB) bool {
	return db != nil && strings.EqualFold(db.Dialector.Name(), "sqlite")
}

func (r *GormRepository) CloseGap(ctx context.Context, nodeID int64, reason string, at time.Time) error {
	if r == nil || r.db == nil || nodeID == 0 || reason == "" || at.IsZero() {
		return errors.New("invalid traffic gap")
	}
	return r.db.WithContext(ctx).Model(&gbmodels.GbDeviceTrafficGap{}).
		Where("node_id = ? AND reason = ? AND state = ?", nodeID, reason, "open").
		Updates(map[string]interface{}{"state": "closed", "ended_at": at.UTC()}).Error
}

func (r *GormRepository) PruneSettledBefore(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if r == nil || r.db == nil || cutoff.IsZero() || batchSize <= 0 {
		return 0, errors.New("invalid traffic session prune request")
	}
	var ids []uint64
	if err := r.db.WithContext(ctx).Model(&gbmodels.GbDeviceTrafficSession{}).
		Where("state = ? AND ended_at IS NOT NULL AND ended_at < ?", SessionSettled, cutoff.UTC()).
		Order("id ASC").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).
		Where("id IN ?", ids).
		Delete(&gbmodels.GbDeviceTrafficSession{})
	return result.RowsAffected, result.Error
}

func (r *GormRepository) Apply(ctx context.Context, request ApplyRequest) (result ApplyResult, err error) {
	if r == nil || r.db == nil {
		return result, errors.New("traffic repository unavailable")
	}
	if request.BusinessKey == "" || request.NodeID == 0 || request.DeviceCode == "" || request.ChannelCode == "" || !request.Direction.valid() || request.At.IsZero() {
		return result, errors.New("invalid traffic apply request")
	}
	if request.MediaKind == "" {
		request.MediaKind = MediaKindLive
	}
	return result, r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row gbmodels.GbDeviceTrafficSession
		query := tx.Where("business_key = ?", request.BusinessKey).Limit(1).Find(&row)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected == 0 {
			startedAt := request.At.UTC()
			row = gbmodels.GbDeviceTrafficSession{
				BusinessKey: request.BusinessKey, NodeID: request.NodeID, MediaServerUUID: request.MediaServerUUID,
				ZLMSessionID: request.ZLMSessionID, Direction: string(request.Direction), DeviceCode: request.DeviceCode,
				ChannelCode: request.ChannelCode, OwnerDeptID: request.OwnerDeptID, MediaKind: string(request.MediaKind),
				Schema: request.Schema, VHost: request.VHost, App: request.App, Stream: request.Stream,
				CreateStamp: request.CreateStamp, State: string(SessionActive), StartedAt: &startedAt,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}

		state := Session{LastTotalBytes: row.LastTotalBytes, SettledTotalBytes: row.SettledTotalBytes, State: SessionState(row.State)}
		state.LastSeenAt = valueOrZero(row.LastSeenAt)
		state.EndedAt = valueOrZero(row.EndedAt)
		var delta uint64
		var reset bool
		if row.State != string(SessionSettled) {
			if !request.SettleAt.IsZero() {
				delta, err = (Accumulator{}).SettleAbsolute(&state, request.AbsoluteBytes, request.SettleAt, request.DurationSeconds)
			} else {
				delta, reset, err = (Accumulator{}).ObserveAbsolute(&state, request.AbsoluteBytes, request.At)
			}
		}
		if err != nil {
			return err
		}
		becameSettled := !request.SettleAt.IsZero() && row.State != string(SessionSettled)
		endedAt := state.EndedAt
		lastSeen := state.LastSeenAt
		updates := map[string]interface{}{
			"last_total_bytes": state.LastTotalBytes, "settled_total_bytes": state.SettledTotalBytes,
			"duration_seconds": state.DurationSeconds, "state": string(state.State),
			"last_seen_at": lastSeen, "ended_at": nullableTime(endedAt),
		}
		if row.StartedAt == nil {
			updates["started_at"] = request.At.UTC()
		}
		if reset {
			updates["unattributed_reason"] = "absolute_reset"
		}
		if err := tx.Model(&gbmodels.GbDeviceTrafficSession{}).
			Where("business_key = ?", request.BusinessKey).
			Updates(updates).Error; err != nil {
			return err
		}
		if delta > 0 {
			if err := upsertDaily(tx, request, delta, becameSettled); err != nil {
				return err
			}
			if err := upsertHourly(tx, request, delta, becameSettled); err != nil {
				return err
			}
		}
		result = ApplyResult{DeltaBytes: delta, Reset: reset, Settled: state.State == SessionSettled}
		return nil
	})
}

func upsertHourly(tx *gorm.DB, request ApplyRequest, delta uint64, newSettled bool) error {
	localAt := request.At.In(accountingLocation)
	hour := time.Date(localAt.Year(), localAt.Month(), localAt.Day(), localAt.Hour(), 0, 0, 0, accountingLocation)
	row := gbmodels.GbDeviceTrafficHourly{
		StatHour: hour, DeviceCode: request.DeviceCode, ChannelCode: request.ChannelCode, OwnerDeptID: request.OwnerDeptID,
	}
	updates := map[string]interface{}{"updated_at": request.At.UTC()}
	if request.Direction == DirectionUpstream {
		row.UpstreamBytes = delta
		updates["upstream_bytes"] = gorm.Expr("upstream_bytes + ?", delta)
		if newSettled {
			row.UpstreamSessions = 1
			row.UpstreamDurationSeconds = request.DurationSeconds
			updates["upstream_sessions"] = gorm.Expr("upstream_sessions + 1")
			updates["upstream_duration_seconds"] = gorm.Expr("upstream_duration_seconds + ?", request.DurationSeconds)
		}
	} else {
		row.DownstreamBytes = delta
		updates["downstream_bytes"] = gorm.Expr("downstream_bytes + ?", delta)
		if newSettled {
			row.DownstreamSessions = 1
			row.DownstreamDurationSeconds = request.DurationSeconds
			updates["downstream_sessions"] = gorm.Expr("downstream_sessions + 1")
			updates["downstream_duration_seconds"] = gorm.Expr("downstream_duration_seconds + ?", request.DurationSeconds)
		}
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "stat_hour"}, {Name: "device_code"}, {Name: "channel_code"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&row).Error
}

func upsertDaily(tx *gorm.DB, request ApplyRequest, delta uint64, newSettled bool) error {
	localAt := request.At.In(accountingLocation)
	day := time.Date(localAt.Year(), localAt.Month(), localAt.Day(), 0, 0, 0, 0, accountingLocation)
	row := gbmodels.GbDeviceTrafficDaily{
		StatDate: day, DeviceCode: request.DeviceCode, ChannelCode: request.ChannelCode, OwnerDeptID: request.OwnerDeptID,
	}
	updates := map[string]interface{}{"updated_at": request.At.UTC()}
	if request.Direction == DirectionUpstream {
		row.UpstreamBytes = delta
		updates["upstream_bytes"] = gorm.Expr("upstream_bytes + ?", delta)
		if newSettled {
			row.UpstreamSessions = 1
			row.UpstreamDurationSeconds = request.DurationSeconds
			updates["upstream_sessions"] = gorm.Expr("upstream_sessions + 1")
			updates["upstream_duration_seconds"] = gorm.Expr("upstream_duration_seconds + ?", request.DurationSeconds)
		}
	} else {
		row.DownstreamBytes = delta
		updates["downstream_bytes"] = gorm.Expr("downstream_bytes + ?", delta)
		if newSettled {
			row.DownstreamSessions = 1
			row.DownstreamDurationSeconds = request.DurationSeconds
			updates["downstream_sessions"] = gorm.Expr("downstream_sessions + 1")
			updates["downstream_duration_seconds"] = gorm.Expr("downstream_duration_seconds + ?", request.DurationSeconds)
		}
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "stat_date"}, {Name: "device_code"}, {Name: "channel_code"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&row).Error
}

func valueOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

func nullableTime(value time.Time) interface{} {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func (r ApplyRequest) String() string {
	return fmt.Sprintf("%s/%s", r.BusinessKey, r.Direction)
}
