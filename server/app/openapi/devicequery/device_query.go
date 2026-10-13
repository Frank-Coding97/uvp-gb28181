// Package devicequery exposes scoped, read-only snapshots of device-managed
// data. It never sends SIP commands and never returns persistence models.
package devicequery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	cacheFreshFor   = time.Minute
)

type Service struct {
	db        *gorm.DB
	resources *resource.Service
	now       func() time.Time
}

func New(db *gorm.DB) *Service {
	return &Service{db: db, resources: resource.New(db), now: time.Now}
}

type DeviceAlarm struct {
	ID             string     `json:"id"`
	ReceivedAt     time.Time  `json:"receivedAt"`
	AlarmTime      *time.Time `json:"alarmTime"`
	ChannelCode    *string    `json:"channelCode,omitempty"`
	SourceCode     string     `json:"sourceCode"`
	Priority       *int       `json:"priority"`
	Method         *int       `json:"method"`
	AlarmType      *int       `json:"alarmType"`
	AlarmTypeParam string     `json:"alarmTypeParam"`
	Description    string     `json:"description"`
	Longitude      *float64   `json:"longitude"`
	Latitude       *float64   `json:"latitude"`
}

type AlarmPage struct {
	Items    []DeviceAlarm `json:"items"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
	Total    int64         `json:"total"`
}

type DeviceStatusEvent struct {
	ID                string                           `json:"id"`
	DeviceCode        string                           `json:"deviceCode"`
	EventType         gbmodels.DeviceStatusEventType   `json:"eventType"`
	EventName         string                           `json:"eventName"`
	FromStatus        *int8                            `json:"fromStatus"`
	ToStatus          int8                             `json:"toStatus"`
	OccurredAt        time.Time                        `json:"occurredAt"`
	Source            gbmodels.DeviceStatusEventSource `json:"source"`
	RegisterExpires   *int                             `json:"registerExpires"`
	KeepaliveInterval *int                             `json:"keepaliveInterval"`
}

type StatusEventPage struct {
	Items      []DeviceStatusEvent `json:"items"`
	NextCursor string              `json:"nextCursor"`
	HasMore    bool                `json:"hasMore"`
}

type PreciseStatus struct {
	Pan        *float64   `json:"pan"`
	Tilt       *float64   `json:"tilt"`
	Zoom       *float64   `json:"zoom"`
	Focus      *float64   `json:"focus"`
	Iris       *float64   `json:"iris"`
	DeviceTime *time.Time `json:"deviceTime"`
}

type PreciseStatusSnapshot struct {
	State      *PreciseStatus        `json:"state"`
	Freshness  gbmodels.PTZFreshness `json:"freshness"`
	ObservedAt *time.Time            `json:"observedAt,omitempty"`
}

type HomePositionValue struct {
	Enabled      bool                                 `json:"enabled"`
	ResetTime    *int                                 `json:"resetTime"`
	PresetID     *int                                 `json:"presetId"`
	ConfirmedAt  time.Time                            `json:"confirmedAt"`
	Source       gbmodels.PTZHomePositionSource       `json:"source"`
	Verification gbmodels.PTZHomePositionVerification `json:"verification"`
}

type HomePositionSnapshot struct {
	HomePosition *HomePositionValue    `json:"homePosition"`
	Freshness    gbmodels.PTZFreshness `json:"freshness"`
	ObservedAt   *time.Time            `json:"observedAt,omitempty"`
}

type CruisePoint struct {
	PresetIndex int `json:"presetIndex"`
	StayTime    int `json:"stayTime"`
	Speed       int `json:"speed"`
}

type CruiseTrackDetail struct {
	SumNum       *int          `json:"sumNum,omitempty"`
	CruisePoints []CruisePoint `json:"cruisePoints"`
}

type CruiseTrack struct {
	TrackID    int                `json:"trackId"`
	Name       string             `json:"name"`
	Enabled    *bool              `json:"enabled"`
	Detail     *CruiseTrackDetail `json:"detail,omitempty"`
	Source     string             `json:"source"`
	DeviceTime *time.Time         `json:"deviceTime"`
	ObservedAt *time.Time         `json:"observedAt,omitempty"`
}

type CruiseTrackSnapshot struct {
	Items      []CruiseTrack         `json:"items"`
	Freshness  gbmodels.PTZFreshness `json:"freshness"`
	ObservedAt *time.Time            `json:"observedAt,omitempty"`
}

type TargetTrackArea struct {
	Length    *int `json:"length,omitempty"`
	Width     *int `json:"width,omitempty"`
	MidPointX *int `json:"midPointX,omitempty"`
	MidPointY *int `json:"midPointY,omitempty"`
	LengthX   *int `json:"lengthX,omitempty"`
	LengthY   *int `json:"lengthY,omitempty"`
}

type TargetTrackIntent struct {
	Mode        gbmodels.TargetTrackMode `json:"mode"`
	DeviceID2   string                   `json:"deviceId2,omitempty"`
	Area        *TargetTrackArea         `json:"area,omitempty"`
	CommandedAt time.Time                `json:"commandedAt"`
}

type TargetTrackSnapshot struct {
	Intent             *TargetTrackIntent        `json:"intent"`
	DeviceAcknowledged bool                      `json:"deviceAcknowledged"`
	ResponseRequired   bool                      `json:"responseRequired"`
	TargetCode         string                    `json:"targetCode"`
	Capability         manscdp.ControlCapability `json:"capability"`
}

func (s *Service) ListDeviceAlarmsInScope(ctx context.Context, scope resource.DepartmentScope, deviceCode string, options resource.DeviceListOptions) (AlarmPage, error) {
	page, pageSize, err := normalizePage(options, maxPageSize)
	if err != nil {
		return AlarmPage{}, err
	}
	device, err := s.scopedDevice(ctx, scope, deviceCode)
	if err != nil {
		return AlarmPage{}, err
	}
	query := s.db.WithContext(ctx).Model(&gbmodels.GbAlarmEvent{}).Where("device_id = ?", device.ID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return AlarmPage{}, err
	}
	type alarmRow struct {
		ID             uint64
		ReceivedAt     time.Time
		AlarmTime      *time.Time
		ChannelCode    *string
		SourceCode     string
		Priority       *int
		Method         *int
		AlarmType      *int
		AlarmTypeParam string
		Description    string
		Longitude      *float64
		Latitude       *float64
	}
	rows := make([]alarmRow, 0)
	result := s.db.WithContext(ctx).Table("gb_alarm_event AS a").
		Joins("LEFT JOIN gb_channel AS c ON c.id = a.channel_id AND c.device_id = ? AND c.owner_dept_id = ? AND c.deleted_at IS NULL", device.DeviceID, device.OwnerDeptID).
		Where("a.device_id = ?", device.ID).
		Select("a.id, a.received_at, a.alarm_time, c.channel_id AS channel_code, a.source_code, a.priority, a.method, a.alarm_type, a.alarm_type_param, a.description, a.longitude, a.latitude").
		Order("a.received_at DESC, a.id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows)
	if result.Error != nil {
		return AlarmPage{}, result.Error
	}
	items := make([]DeviceAlarm, 0, len(rows))
	for _, row := range rows {
		items = append(items, DeviceAlarm{
			ID: strconv.FormatUint(row.ID, 10), ReceivedAt: row.ReceivedAt, AlarmTime: row.AlarmTime,
			ChannelCode: row.ChannelCode, SourceCode: row.SourceCode, Priority: row.Priority,
			Method: row.Method, AlarmType: row.AlarmType, AlarmTypeParam: row.AlarmTypeParam,
			Description: row.Description, Longitude: row.Longitude, Latitude: row.Latitude,
		})
	}
	return AlarmPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

// ListDeviceStatusEventsInScope uses the append-only event ID as a resumable
// high-water cursor. New events can be polled by sending the returned cursor.
func (s *Service) ListDeviceStatusEventsInScope(ctx context.Context, scope resource.DepartmentScope, deviceCode string, pageSize int, cursor string) (StatusEventPage, error) {
	pageSize, err := normalizePageSize(pageSize, maxPageSize)
	if err != nil {
		return StatusEventPage{}, err
	}
	device, err := s.scopedDevice(ctx, scope, deviceCode)
	if err != nil {
		return StatusEventPage{}, err
	}
	afterID, err := decodeCursor(cursor, deviceCode)
	if err != nil {
		return StatusEventPage{}, resource.ErrInvalidListOptions
	}
	var events []gbmodels.GbDeviceStatusEvent
	if err := s.db.WithContext(ctx).Where("device_id = ? AND deleted_at IS NULL AND id > ?", device.ID, afterID).
		Order("id ASC").Limit(pageSize + 1).Find(&events).Error; err != nil {
		return StatusEventPage{}, err
	}
	hasMore := len(events) > pageSize
	if hasMore {
		events = events[:pageSize]
	}
	items := make([]DeviceStatusEvent, 0, len(events))
	nextCursor := cursor
	for _, event := range events {
		items = append(items, DeviceStatusEvent{
			ID: strconv.FormatUint(event.ID, 10), DeviceCode: event.DeviceCode,
			EventType: event.EventType, EventName: event.EventType.DisplayName(),
			FromStatus: event.FromStatus, ToStatus: event.ToStatus,
			OccurredAt: event.OccurredAt, Source: event.Source,
			RegisterExpires: event.RegisterExpires, KeepaliveInterval: event.KeepaliveInterval,
		})
		nextCursor = encodeCursor(deviceCode, event.ID)
	}
	return StatusEventPage{Items: items, NextCursor: nextCursor, HasMore: hasMore}, nil
}

func (s *Service) GetChannelPreciseStatusInScope(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode string) (PreciseStatusSnapshot, error) {
	_, channel, err := s.scopedChannel(ctx, scope, deviceCode, channelCode)
	if err != nil {
		return PreciseStatusSnapshot{}, err
	}
	var state gbmodels.GbPTZState
	result := s.db.WithContext(ctx).Where("channel_id = ?", channel.ID).Limit(1).Find(&state)
	if result.Error != nil {
		return PreciseStatusSnapshot{}, result.Error
	}
	if result.RowsAffected == 0 {
		return PreciseStatusSnapshot{Freshness: gbmodels.PTZFreshnessUnknown}, nil
	}
	observedAt := state.ReceivedAt
	return PreciseStatusSnapshot{
		State:     &PreciseStatus{Pan: state.Pan, Tilt: state.Tilt, Zoom: state.Zoom, Focus: state.Focus, Iris: state.Iris, DeviceTime: state.DeviceTime},
		Freshness: cacheFreshness(observedAt, s.currentTime()), ObservedAt: &observedAt,
	}, nil
}

func (s *Service) GetChannelHomePositionInScope(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode string) (HomePositionSnapshot, error) {
	_, channel, err := s.scopedChannel(ctx, scope, deviceCode, channelCode)
	if err != nil {
		return HomePositionSnapshot{}, err
	}
	var home gbmodels.GbPTZHomePosition
	result := s.db.WithContext(ctx).Where("channel_id = ?", channel.ID).Limit(1).Find(&home)
	if result.Error != nil {
		return HomePositionSnapshot{}, result.Error
	}
	if result.RowsAffected == 0 {
		return HomePositionSnapshot{Freshness: gbmodels.PTZFreshnessUnknown}, nil
	}
	var latestQuery gbmodels.GbPTZOperation
	queryResult := s.db.WithContext(ctx).Where("channel_id = ? AND cmd_type = ?", channel.ID, manscdp.CmdHomePositionQuery).
		Order("id DESC").Limit(1).Find(&latestQuery)
	if queryResult.Error != nil {
		return HomePositionSnapshot{}, queryResult.Error
	}
	var latest *gbmodels.GbPTZOperation
	if queryResult.RowsAffected > 0 {
		latest = &latestQuery
	}
	value := &HomePositionValue{
		Enabled: home.Enabled, ResetTime: home.ResetTime, PresetID: home.PresetID,
		ConfirmedAt: home.ConfirmedAt, Source: home.Source, Verification: home.Verification,
	}
	observedAt := home.ConfirmedAt
	return HomePositionSnapshot{
		HomePosition: value, Freshness: ptz.HomePositionFreshness(&home, latest, s.currentTime()), ObservedAt: &observedAt,
	}, nil
}

func (s *Service) ListChannelCruiseTracksInScope(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode string) (CruiseTrackSnapshot, error) {
	_, channel, err := s.scopedChannel(ctx, scope, deviceCode, channelCode)
	if err != nil {
		return CruiseTrackSnapshot{}, err
	}
	var tracks []gbmodels.GbPTZCruiseTrack
	if err := s.db.WithContext(ctx).Where("channel_id = ?", channel.ID).Order("track_id ASC").Find(&tracks).Error; err != nil {
		return CruiseTrackSnapshot{}, err
	}
	items := make([]CruiseTrack, 0, len(tracks))
	var observedAt *time.Time
	for _, track := range tracks {
		source := "device_cache"
		var itemObservedAt *time.Time
		if track.RawSummary == "reconcile-pending" {
			source = "platform_pending_reconcile"
		} else {
			observed := track.UpdatedAt
			itemObservedAt = &observed
			if observedAt == nil || observed.After(*observedAt) {
				observedAt = &observed
			}
		}
		var detail *CruiseTrackDetail
		if strings.TrimSpace(track.DetailJSON) != "" {
			var cached struct {
				SumNum       *int                  `json:"sumNum"`
				CruisePoints []manscdp.CruisePoint `json:"cruisePoints"`
			}
			if err := json.Unmarshal([]byte(track.DetailJSON), &cached); err != nil {
				return CruiseTrackSnapshot{}, fmt.Errorf("invalid cruise track cache: %w", resource.ErrResourceUnavailable)
			}
			points := make([]CruisePoint, 0, len(cached.CruisePoints))
			for _, point := range cached.CruisePoints {
				points = append(points, CruisePoint{PresetIndex: point.PresetIndex, StayTime: point.StayTime, Speed: point.Speed})
			}
			detail = &CruiseTrackDetail{SumNum: cached.SumNum, CruisePoints: points}
		}
		items = append(items, CruiseTrack{
			TrackID: track.TrackID, Name: track.Name, Enabled: track.Enabled, Detail: detail,
			Source: source, DeviceTime: track.DeviceTime, ObservedAt: itemObservedAt,
		})
	}
	freshness := gbmodels.PTZFreshnessUnknown
	if observedAt != nil {
		freshness = cacheFreshness(*observedAt, s.currentTime())
	}
	return CruiseTrackSnapshot{Items: items, Freshness: freshness, ObservedAt: observedAt}, nil
}

func (s *Service) GetChannelTargetTrackInScope(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode string) (TargetTrackSnapshot, error) {
	device, channel, err := s.scopedChannel(ctx, scope, deviceCode, channelCode)
	if err != nil {
		return TargetTrackSnapshot{}, err
	}
	var channelModel gbmodels.GbChannel
	result := s.db.WithContext(ctx).Select("id, capabilities, ptz_type").Where("id = ?", channel.ID).Limit(1).Find(&channelModel)
	if result.Error != nil {
		return TargetTrackSnapshot{}, result.Error
	}
	if result.RowsAffected == 0 {
		return TargetTrackSnapshot{}, resource.ErrResourceNotFound
	}
	capability := manscdp.ParseControlCapabilities(channelModel.Capabilities, channelModel.PTZType).TargetTrack
	var stored gbmodels.GbDeviceTargetTrack
	result = s.db.WithContext(ctx).Where("device_id = ? AND target_code = ?", device.ID, channel.ChannelID).Limit(1).Find(&stored)
	if result.Error != nil {
		return TargetTrackSnapshot{}, result.Error
	}
	snapshot := TargetTrackSnapshot{
		DeviceAcknowledged: false, ResponseRequired: false,
		TargetCode: channel.ChannelID, Capability: capability,
	}
	if result.RowsAffected > 0 {
		intent := &TargetTrackIntent{Mode: stored.Mode, DeviceID2: stored.DeviceID2, CommandedAt: stored.CommandedAt}
		if stored.AreaLength != nil || stored.AreaWidth != nil || stored.AreaMidPointX != nil || stored.AreaMidPointY != nil || stored.AreaLengthX != nil || stored.AreaLengthY != nil {
			intent.Area = &TargetTrackArea{
				Length: stored.AreaLength, Width: stored.AreaWidth, MidPointX: stored.AreaMidPointX,
				MidPointY: stored.AreaMidPointY, LengthX: stored.AreaLengthX, LengthY: stored.AreaLengthY,
			}
		}
		snapshot.Intent = intent
	}
	return snapshot, nil
}

func (s *Service) scopedDevice(ctx context.Context, scope resource.DepartmentScope, deviceCode string) (gbmodels.GbDevice, error) {
	if s == nil || s.db == nil || s.resources == nil {
		return gbmodels.GbDevice{}, resource.ErrResourceUnavailable
	}
	if _, err := s.resources.GetDeviceInScope(ctx, scope, deviceCode); err != nil {
		return gbmodels.GbDevice{}, err
	}
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, s.db, scope)
	if err != nil {
		return gbmodels.GbDevice{}, err
	}
	var devices []gbmodels.GbDevice
	if err := s.db.WithContext(ctx).Where("device_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", deviceCode, departmentIDs).Limit(2).Find(&devices).Error; err != nil {
		return gbmodels.GbDevice{}, err
	}
	if len(devices) != 1 {
		return gbmodels.GbDevice{}, resource.ErrResourceNotFound
	}
	return devices[0], nil
}

func (s *Service) scopedChannel(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode string) (gbmodels.GbDevice, gbmodels.GbChannel, error) {
	device, err := s.scopedDevice(ctx, scope, deviceCode)
	if err != nil {
		return gbmodels.GbDevice{}, gbmodels.GbChannel{}, err
	}
	if _, err := s.resources.GetChannelInScope(ctx, scope, deviceCode, channelCode); err != nil {
		return gbmodels.GbDevice{}, gbmodels.GbChannel{}, err
	}
	var channels []gbmodels.GbChannel
	if err := s.db.WithContext(ctx).Where("device_id = ? AND channel_id = ? AND owner_dept_id = ? AND deleted_at IS NULL", device.DeviceID, channelCode, device.OwnerDeptID).
		Limit(2).Find(&channels).Error; err != nil {
		return gbmodels.GbDevice{}, gbmodels.GbChannel{}, err
	}
	if len(channels) != 1 {
		return gbmodels.GbDevice{}, gbmodels.GbChannel{}, resource.ErrResourceNotFound
	}
	return device, channels[0], nil
}

func normalizePage(options resource.DeviceListOptions, maxSize int) (int, int, error) {
	if options.Keyword != "" || options.Status != "" {
		return 0, 0, resource.ErrInvalidListOptions
	}
	page := options.Page
	if page == 0 {
		page = 1
	}
	pageSize, err := normalizePageSize(options.PageSize, maxSize)
	if err != nil || page < 1 || page-1 > int(^uint(0)>>1)/pageSize {
		return 0, 0, resource.ErrInvalidListOptions
	}
	return page, pageSize, nil
}

func normalizePageSize(pageSize, maxSize int) (int, error) {
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize < 1 || pageSize > maxSize {
		return 0, resource.ErrInvalidListOptions
	}
	return pageSize, nil
}

func encodeCursor(deviceCode string, id uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte("v1|" + deviceCode + "|" + strconv.FormatUint(id, 10)))
}

func decodeCursor(value, deviceCode string) (uint64, error) {
	if value == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, errors.New("invalid event cursor")
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] != deviceCode {
		return 0, errors.New("invalid event cursor")
	}
	id, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil || id == 0 || encodeCursor(deviceCode, id) != value {
		return 0, errors.New("invalid event cursor")
	}
	return id, nil
}

func cacheFreshness(observedAt, now time.Time) gbmodels.PTZFreshness {
	if observedAt.IsZero() {
		return gbmodels.PTZFreshnessUnknown
	}
	if now.Sub(observedAt) > cacheFreshFor {
		return gbmodels.PTZFreshnessStale
	}
	return gbmodels.PTZFreshnessFresh
}

func (s *Service) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}
