package traffic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

type Direction string

var accountingLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

const (
	DirectionUpstream   Direction = "upstream"
	DirectionDownstream Direction = "downstream"
)

func (d Direction) valid() bool { return d == DirectionUpstream || d == DirectionDownstream }

// downstreamBusinessKey 是下行(观看连接)流量会话的唯一键。
// ⛔ 采样器(Sampler)与 on_flow_report 收尾(FlowService.CollectFlow)必须共用同一个键,
// 否则采样累计与断开时的最终结算会落到两条会话上 → 重复计数。
// 键里只放 ZLM 会话标识(identifier),因为 on_flow_report 能拿到的只有它;
// ZLM 重启后 identifier 会复用,靠 Accumulator 的 absolute_reset 语义自愈。
func downstreamBusinessKey(nodeID int64, zlmSessionID string) string {
	return fmt.Sprintf("flow:%d:%s:%s", nodeID, zlmSessionID, DirectionDownstream)
}

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

func (r *GormRepository) CloseGap(ctx context.Context, nodeID int64, reason string, at time.Time) error {
	if r == nil || r.db == nil || nodeID == 0 || reason == "" || at.IsZero() {
		return errors.New("invalid traffic gap")
	}
	return r.db.WithContext(ctx).Model(&gbmodels.GbDeviceTrafficGap{}).
		Where("node_id = ? AND reason = ? AND state = ?", nodeID, reason, "open").
		Updates(map[string]interface{}{"state": "closed", "ended_at": at.UTC()}).Error
}

// SettleStaleDownstream 把长时间未再被采样到的活跃下行会话收尾为 settled。
// 场景:观看连接已断开,但 ZLM 未上报 on_flow_report(流量低于
// flowThreshold 阈值 / hook 丢失 / ZLM 宕机),会话会永远挂在 active。
// 字节已按采样增量入账,这里不再补字节(避免重复计),
// 但要补三件 DLL 结算路径(flow_report → Accumulator.SettleAbsolute)会写的东西:
//
//	① settled_total_bytes 置为本会话最终累计字节 —— 会话详情页读的就是它,
//	   留 0 会让一条十几分钟的下行会话在明细里显示成 0 字节;
//	② hourly/daily 的下行会话数 +1;
//	③ hourly/daily 的下行时长累加 —— 否则「观看时长」这类指标随 perspective 丢失。
//
// ended_at 取最后一次采样时间,duration 由 started_at 推算,保证明细页可展示。
// 注意:若迟到的 flow_report 在此之后才到,Apply 会因 settled 守卫丢弃其
// 尾差(最多一个采样周期的量),这是可接受的取舍。
func (r *GormRepository) SettleStaleDownstream(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if r == nil || r.db == nil || cutoff.IsZero() || batchSize <= 0 {
		return 0, errors.New("invalid stale downstream settle request")
	}
	var rows []gbmodels.GbDeviceTrafficSession
	if err := r.db.WithContext(ctx).
		Select("id", "started_at", "last_seen_at", "duration_seconds", "last_total_bytes",
			"settled_total_bytes", "device_code", "channel_code", "owner_dept_id", "direction").
		Where("direction = ? AND state = ? AND last_seen_at IS NOT NULL AND last_seen_at < ?", DirectionDownstream, SessionActive, cutoff.UTC()).
		Order("id ASC").Limit(batchSize).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	ids := make([]uint64, 0, len(rows))
	durationByID := make(map[uint64]int64, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		if row.StartedAt != nil && row.LastSeenAt != nil {
			if duration := row.LastSeenAt.Sub(*row.StartedAt).Milliseconds() / 1000; duration > row.DurationSeconds {
				durationByID[row.ID] = duration
			}
		}
	}
	return r.commitStaleSettle(ctx, ids, durationByID, rows)
}

func (r *GormRepository) commitStaleSettle(ctx context.Context, ids []uint64, durationByID map[uint64]int64, rows []gbmodels.GbDeviceTrafficSession) (int64, error) {
	return r.commitStaleSettleTx(ctx, ids, durationByID, rows)
}

func (r *GormRepository) commitStaleSettleTx(ctx context.Context, ids []uint64, durationByID map[uint64]int64, rows []gbmodels.GbDeviceTrafficSession) (int64, error) {
	var affected int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			duration := row.DurationSeconds
			if candidate, ok := durationByID[row.ID]; ok {
				duration = candidate
			}
			updates := map[string]interface{}{
				"state":    string(SessionSettled),
				"ended_at": row.LastSeenAt.UTC(),
				// 会话明细页展示的「本次结算字节」:走采样路径时它就是这条连接
				// 最终累计的绝对字节,与 flow_report 结算口径一致。
				"settled_total_bytes": row.LastTotalBytes,
			}
			if candidate, ok := durationByID[row.ID]; ok {
				updates["duration_seconds"] = candidate
			}
			result := tx.Model(&gbmodels.GbDeviceTrafficSession{}).
				// ⛔ 状态守卫必须进 WHERE:并发下 flow_report 可能已把它结算,
				// 没有这道守卫会把 ended_at/duration 回写成较早的采样值
				Where("id = ? AND state = ?", row.ID, SessionActive).
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				continue
			}
			affected += result.RowsAffected
			if row.LastSeenAt == nil {
				continue
			}
			if err := bumpSettledSessionStats(tx, Direction(row.Direction), row.DeviceCode, row.ChannelCode,
				row.OwnerDeptID, row.LastSeenAt.UTC(), duration); err != nil {
				return err
			}
		}
		return nil
	})
	return affected, err
}

// bumpSettledSessionStats 把会话数与时补进小时/日汇总,字节不动。
// 用于「字节已随采样增量入账、但没有收到 flow_report」的收尾场景:
// 少了这一步,明细页的会话数与实际连接数不符、观看时长恒为 0。
func bumpSettledSessionStats(tx *gorm.DB, direction Direction, deviceCode, channelCode string, ownerDeptID uint, at time.Time, duration int64) error {
	if deviceCode == "" || !direction.valid() {
		return nil
	}
	if duration < 0 {
		duration = 0
	}
	request := ApplyRequest{Direction: direction, DeviceCode: deviceCode, ChannelCode: channelCode,
		OwnerDeptID: ownerDeptID, At: at, DurationSeconds: duration}
	if err := bumpHourlySessionStats(tx, request); err != nil {
		return err
	}
	return bumpDailySessionStats(tx, request)
}

func bumpHourlySessionStats(tx *gorm.DB, request ApplyRequest) error {
	localAt := request.At.In(accountingLocation)
	hour := time.Date(localAt.Year(), localAt.Month(), localAt.Day(), localAt.Hour(), 0, 0, 0, accountingLocation)
	row := gbmodels.GbDeviceTrafficHourly{
		StatHour: hour, DeviceCode: request.DeviceCode, ChannelCode: request.ChannelCode, OwnerDeptID: request.OwnerDeptID,
	}
	updates := map[string]interface{}{"updated_at": request.At.UTC()}
	if request.Direction == DirectionUpstream {
		// ⛔ 结构体初值不能省:汇总行还不存在时走的是纯 INSERT,DoUpdates 不执行,
		// 少了初值第一次补记会落 0。
		row.UpstreamSessions = 1
		row.UpstreamDurationSeconds = request.DurationSeconds
		updates["upstream_sessions"] = gorm.Expr("upstream_sessions + 1")
		updates["upstream_duration_seconds"] = gorm.Expr("upstream_duration_seconds + ?", request.DurationSeconds)
	} else {
		// ⛔ 同理:汇总行不存在时是纯 INSERT,结构体初值就是插入值
		row.DownstreamSessions = 1
		row.DownstreamDurationSeconds = request.DurationSeconds
		updates["downstream_sessions"] = gorm.Expr("downstream_sessions + 1")
		updates["downstream_duration_seconds"] = gorm.Expr("downstream_duration_seconds + ?", request.DurationSeconds)
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "stat_hour"}, {Name: "device_code"}, {Name: "channel_code"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&row).Error
}

func bumpDailySessionStats(tx *gorm.DB, request ApplyRequest) error {
	localAt := request.At.In(accountingLocation)
	day := time.Date(localAt.Year(), localAt.Month(), localAt.Day(), 0, 0, 0, 0, accountingLocation)
	row := gbmodels.GbDeviceTrafficDaily{
		StatDate: day, DeviceCode: request.DeviceCode, ChannelCode: request.ChannelCode, OwnerDeptID: request.OwnerDeptID,
	}
	updates := map[string]interface{}{"updated_at": request.At.UTC()}
	if request.Direction == DirectionUpstream {
		row.UpstreamSessions = 1
		row.UpstreamDurationSeconds = request.DurationSeconds
		updates["upstream_sessions"] = gorm.Expr("upstream_sessions + 1")
		updates["upstream_duration_seconds"] = gorm.Expr("upstream_duration_seconds + ?", request.DurationSeconds)
	} else {
		row.DownstreamSessions = 1
		row.DownstreamDurationSeconds = request.DurationSeconds
		updates["downstream_sessions"] = gorm.Expr("downstream_sessions + 1")
		updates["downstream_duration_seconds"] = gorm.Expr("downstream_duration_seconds + ?", request.DurationSeconds)
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "stat_date"}, {Name: "device_code"}, {Name: "channel_code"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&row).Error
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
