package traffic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type Direction string

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
	err := r.db.WithContext(ctx).Where("node_id = ? AND app = ? AND stream = ? AND direction = ? AND state = ?", nodeID, app, stream, direction, SessionActive).
		Order("id DESC").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return row.BusinessKey, true, nil
}

func (r *GormRepository) OpenGap(ctx context.Context, nodeID int64, reason string, at time.Time) error {
	if r == nil || r.db == nil || nodeID == 0 || reason == "" || at.IsZero() {
		return errors.New("invalid traffic gap")
	}
	var existing gbmodels.GbDeviceTrafficGap
	err := r.db.WithContext(ctx).Where("node_id = ? AND reason = ? AND state = ?", nodeID, reason, "open").Take(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return r.db.WithContext(ctx).Create(&gbmodels.GbDeviceTrafficGap{NodeID: nodeID, Reason: reason, State: "open", StartedAt: at.UTC()}).Error
}

func (r *GormRepository) CloseGap(ctx context.Context, nodeID int64, reason string, at time.Time) error {
	if r == nil || r.db == nil || nodeID == 0 || reason == "" || at.IsZero() {
		return errors.New("invalid traffic gap")
	}
	return r.db.WithContext(ctx).Model(&gbmodels.GbDeviceTrafficGap{}).
		Where("node_id = ? AND reason = ? AND state = ?", nodeID, reason, "open").
		Updates(map[string]interface{}{"state": "closed", "ended_at": at.UTC()}).Error
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
		err := tx.Where("business_key = ?", request.BusinessKey).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = gbmodels.GbDeviceTrafficSession{
				BusinessKey: request.BusinessKey, NodeID: request.NodeID, MediaServerUUID: request.MediaServerUUID,
				ZLMSessionID: request.ZLMSessionID, Direction: string(request.Direction), DeviceCode: request.DeviceCode,
				ChannelCode: request.ChannelCode, OwnerDeptID: request.OwnerDeptID, MediaKind: string(request.MediaKind),
				Schema: request.Schema, VHost: request.VHost, App: request.App, Stream: request.Stream,
				CreateStamp: request.CreateStamp, State: string(SessionActive),
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
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
		if reset {
			updates["unattributed_reason"] = "absolute_reset"
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		if delta > 0 {
			if err := upsertDaily(tx, request, delta, becameSettled); err != nil {
				return err
			}
		}
		result = ApplyResult{DeltaBytes: delta, Reset: reset, Settled: state.State == SessionSettled}
		return nil
	})
}

func upsertDaily(tx *gorm.DB, request ApplyRequest, delta uint64, newSettled bool) error {
	day := time.Date(request.At.UTC().Year(), request.At.UTC().Month(), request.At.UTC().Day(), 0, 0, 0, 0, time.UTC)
	var row gbmodels.GbDeviceTrafficDaily
	err := tx.Where("stat_date = ? AND device_code = ? AND channel_code = ?", day, request.DeviceCode, request.ChannelCode).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = gbmodels.GbDeviceTrafficDaily{StatDate: day, DeviceCode: request.DeviceCode, ChannelCode: request.ChannelCode, OwnerDeptID: request.OwnerDeptID}
	} else if err != nil {
		return err
	}
	if request.Direction == DirectionUpstream {
		row.UpstreamBytes += delta
		if newSettled {
			row.UpstreamSessions++
			row.UpstreamDurationSeconds += request.DurationSeconds
		}
	} else {
		row.DownstreamBytes += delta
		if newSettled {
			row.DownstreamSessions++
			row.DownstreamDurationSeconds += request.DurationSeconds
		}
	}
	if row.ID == 0 {
		return tx.Create(&row).Error
	}
	return tx.Save(&row).Error
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
