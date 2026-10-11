// Package deviceevents exposes client-scoped subscriptions and persisted device
// events without adding a webhook delivery path.
package deviceevents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/subscribe"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
	minExpires      = 60
	maxExpires      = 7 * 24 * 60 * 60
)

type EventKind string

const (
	EventAlarm              EventKind = "alarm"
	EventDeviceStatus       EventKind = "device_status"
	EventMobilePosition     EventKind = "mobile_position"
	EventCatalog            EventKind = "catalog"
	EventPTZPrecisePosition EventKind = "ptz_precise_position"
)

var (
	ErrInvalidRequest      = errors.New("device event request is invalid")
	ErrNotFound            = errors.New("device event resource not found")
	ErrUnavailable         = errors.New("device event capability is unavailable")
	ErrInvalidCursor       = errors.New("device event cursor is invalid")
	ErrExpiredCursor       = errors.New("device event cursor expired; resync required")
	ErrSubscriptionExpired = errors.New("device event subscription expired")
)

// SubscriptionLifecycle is implemented by subscribe.Service. Disable is
// intentionally absent: gb_device_subscription is shared by the platform and
// every OpenAPI client, so one client's cancellation cannot release the SIP
// dialog for other consumers.
type SubscriptionLifecycle interface {
	Configure(context.Context, *gbmodels.GbDevice, gbmodels.SubscriptionKind, *bool, *int, *int) (*gbmodels.GbDeviceSubscription, error)
	Renew(context.Context, *gbmodels.GbDevice, gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error)
}

type Config struct {
	CursorKey              []byte
	CursorTTL              time.Duration
	Now                    func() time.Time
	AlarmEventsEnabled     func() bool
	PositionHistoryEnabled func() bool
}

// OpenAPIDeviceSubscription is a client-owned logical read subscription. It
// is separate from the single shared SIP subscription row in gb_device_subscription.
// The table needs a production migration before these operations are wired.
type OpenAPIDeviceSubscription struct {
	ID              string    `gorm:"primaryKey;size:36" json:"id"`
	OpenAPIClientID int64     `gorm:"column:client_id;not null;uniqueIndex:uk_openapi_device_subscription,priority:1;index:idx_openapi_device_sub_client" json:"-"`
	DeviceID        uint      `gorm:"column:device_id;not null;uniqueIndex:uk_openapi_device_subscription,priority:2" json:"-"`
	Kind            EventKind `gorm:"column:kind;size:32;not null;uniqueIndex:uk_openapi_device_subscription,priority:3" json:"kind"`
	Enabled         bool      `gorm:"not null;default:true;index:idx_openapi_device_sub_enabled" json:"enabled"`
	ExpiresSeconds  int       `gorm:"column:expires_seconds;not null;default:3600" json:"expiresSeconds"`
	ExpiresAt       time.Time `gorm:"column:expires_at;not null;index:idx_openapi_device_sub_expires" json:"expiresAt"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func (OpenAPIDeviceSubscription) TableName() string { return "sys_openapi_device_subscription" }

// Request contains only the authenticated OpenAPI client identity. It has no
// user ID field; callers must populate OpenAPIClientID from authentication.
type Request struct {
	OpenAPIClientID int64
	ResourceScope   resource.DepartmentScope
	DeviceID        string
	SubscriptionID  string
	Kind            EventKind
	ExpiresSeconds  int
	IntervalSeconds int
	Cursor          string
	PageSize        int
}

type ConfiguredSubscription struct {
	ID             string    `json:"id"`
	DeviceID       string    `json:"deviceId"`
	Kind           EventKind `json:"kind"`
	Enabled        bool      `json:"enabled"`
	Status         string    `json:"status"`
	ExpiresAt      time.Time `json:"expiresAt"`
	ExpiresSeconds int       `json:"expiresSeconds"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Event struct {
	ID         string         `json:"id"`
	Kind       EventKind      `json:"kind"`
	SourceCode string         `json:"sourceCode,omitempty"`
	OccurredAt time.Time      `json:"occurredAt"`
	ReceivedAt time.Time      `json:"receivedAt"`
	Alarm      *AlarmPayload  `json:"alarm,omitempty"`
	Status     *StatusPayload `json:"status,omitempty"`
	Position   *Position      `json:"position,omitempty"`
}

type AlarmPayload struct {
	AlarmTime      *time.Time `json:"alarmTime,omitempty"`
	Priority       *int       `json:"priority,omitempty"`
	Method         *int       `json:"method,omitempty"`
	AlarmType      *int       `json:"alarmType,omitempty"`
	AlarmTypeParam string     `json:"alarmTypeParam,omitempty"`
	Description    string     `json:"description,omitempty"`
	Longitude      *float64   `json:"longitude,omitempty"`
	Latitude       *float64   `json:"latitude,omitempty"`
}

type StatusPayload struct {
	EventType         gbmodels.DeviceStatusEventType   `json:"eventType"`
	FromStatus        *int8                            `json:"fromStatus,omitempty"`
	ToStatus          int8                             `json:"toStatus"`
	Source            gbmodels.DeviceStatusEventSource `json:"source"`
	RegisterExpires   *int                             `json:"registerExpires,omitempty"`
	KeepaliveInterval *int                             `json:"keepaliveInterval,omitempty"`
}

type Position struct {
	SourceCode string    `json:"sourceCode"`
	EventTime  time.Time `json:"eventTime"`
	ReceivedAt time.Time `json:"receivedAt"`
	Longitude  float64   `json:"longitude"`
	Latitude   float64   `json:"latitude"`
	Speed      *float64  `json:"speed,omitempty"`
	Direction  *float64  `json:"direction,omitempty"`
	Altitude   *float64  `json:"altitude,omitempty"`
}

type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"nextCursor"`
	HasMore    bool    `json:"hasMore"`
}

type PositionPage struct {
	Items []Position `json:"items"`
}

type Service struct {
	db       *gorm.DB
	upstream SubscriptionLifecycle
	cursor   cursorCodec
	now      func() time.Time
	config   Config
}

func NewService(db *gorm.DB, upstream SubscriptionLifecycle, config Config) *Service {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.CursorTTL <= 0 {
		config.CursorTTL = 24 * time.Hour
	}
	key := append([]byte(nil), config.CursorKey...)
	return &Service{db: db, upstream: upstream, cursor: cursorCodec{key: key, ttl: config.CursorTTL, now: config.Now}, now: config.Now, config: config}
}

func (s *Service) CreateSubscription(ctx context.Context, request Request) (ConfiguredSubscription, error) {
	if s == nil || s.db == nil || ctx == nil {
		return ConfiguredSubscription{}, ErrUnavailable
	}
	if request.OpenAPIClientID <= 0 {
		return ConfiguredSubscription{}, ErrInvalidRequest
	}
	if !supportedKind(request.Kind) {
		return ConfiguredSubscription{}, ErrUnavailable
	}
	expires, interval, err := validateSubscriptionOptions(request)
	if err != nil {
		return ConfiguredSubscription{}, err
	}
	if request.Kind == EventAlarm && !s.alarmEventsEnabled() || request.Kind == EventMobilePosition && !s.positionHistoryEnabled() {
		return ConfiguredSubscription{}, ErrUnavailable
	}
	device, err := s.resolveDevice(ctx, request)
	if err != nil {
		return ConfiguredSubscription{}, err
	}
	var existing OpenAPIDeviceSubscription
	result := s.db.WithContext(ctx).Where("client_id = ? AND device_id = ? AND kind = ?", request.OpenAPIClientID, device.ID, request.Kind).Limit(1).Find(&existing)
	if result.Error != nil {
		return ConfiguredSubscription{}, ErrUnavailable
	}
	if result.RowsAffected > 0 && existing.Enabled && existing.ExpiresAt.After(s.now()) {
		return s.subscriptionView(ctx, &existing, device.DeviceID), nil
	}
	if kindNeedsSIP(request.Kind) {
		if s.upstream == nil {
			return ConfiguredSubscription{}, ErrUnavailable
		}
		var shared gbmodels.GbDeviceSubscription
		result := s.db.WithContext(ctx).Where("device_id = ? AND kind = ?", device.ID, sipKind(request.Kind)).Limit(1).Find(&shared)
		if result.Error != nil {
			return ConfiguredSubscription{}, ErrUnavailable
		}
		if result.RowsAffected == 0 || !shared.Enabled {
			enabled := true
			if _, err := s.upstream.Configure(ctx, device, sipKind(request.Kind), &enabled, &expires, interval); err != nil {
				return ConfiguredSubscription{}, ErrUnavailable
			}
		}
	}
	now := s.now()
	if result.RowsAffected == 0 {
		existing.ID, err = newID()
		if err != nil {
			return ConfiguredSubscription{}, ErrUnavailable
		}
		existing.OpenAPIClientID = request.OpenAPIClientID
		existing.DeviceID = device.ID
		existing.Kind = request.Kind
		existing.CreatedAt = now
	}
	existing.Enabled = true
	existing.ExpiresSeconds = expires
	existing.ExpiresAt = now.Add(time.Duration(expires) * time.Second)
	existing.UpdatedAt = now
	if err := s.db.WithContext(ctx).Save(&existing).Error; err != nil {
		return ConfiguredSubscription{}, ErrUnavailable
	}
	return s.subscriptionView(ctx, &existing, device.DeviceID), nil
}

func (s *Service) GetSubscription(ctx context.Context, request Request) (ConfiguredSubscription, error) {
	subscription, device, err := s.ownedSubscription(ctx, request)
	if err != nil {
		return ConfiguredSubscription{}, err
	}
	return s.subscriptionView(ctx, subscription, device.DeviceID), nil
}

func (s *Service) RenewSubscription(ctx context.Context, request Request) (ConfiguredSubscription, error) {
	subscription, device, err := s.ownedSubscription(ctx, request)
	if err != nil {
		return ConfiguredSubscription{}, err
	}
	if !subscription.Enabled {
		return ConfiguredSubscription{}, ErrNotFound
	}
	if kindNeedsSIP(subscription.Kind) {
		if s.upstream == nil {
			return ConfiguredSubscription{}, ErrUnavailable
		}
		if _, err := s.upstream.Renew(ctx, device, sipKind(subscription.Kind)); err != nil {
			return ConfiguredSubscription{}, ErrUnavailable
		}
	}
	now := s.now()
	subscription.ExpiresAt = now.Add(time.Duration(subscription.ExpiresSeconds) * time.Second)
	subscription.UpdatedAt = now
	if err := s.db.WithContext(ctx).Save(subscription).Error; err != nil {
		return ConfiguredSubscription{}, ErrUnavailable
	}
	return s.subscriptionView(ctx, subscription, device.DeviceID), nil
}

func (s *Service) CancelSubscription(ctx context.Context, request Request) (ConfiguredSubscription, error) {
	subscription, device, err := s.ownedSubscription(ctx, request)
	if err != nil {
		return ConfiguredSubscription{}, err
	}
	if subscription.Enabled {
		subscription.Enabled = false
		subscription.UpdatedAt = s.now()
		if err := s.db.WithContext(ctx).Save(subscription).Error; err != nil {
			return ConfiguredSubscription{}, ErrUnavailable
		}
	}
	return s.subscriptionView(ctx, subscription, device.DeviceID), nil
}

func (s *Service) PollEvents(ctx context.Context, request Request) (EventPage, error) {
	subscription, device, err := s.ownedSubscription(ctx, request)
	if err != nil {
		return EventPage{}, err
	}
	if !subscription.Enabled {
		return EventPage{}, ErrNotFound
	}
	if !subscription.ExpiresAt.After(s.now()) {
		return EventPage{}, ErrSubscriptionExpired
	}
	return s.readEventPage(ctx, request, subscription.Kind, device.ID, subscription.ID)
}

func (s *Service) GetLatestPositions(ctx context.Context, request Request) (PositionPage, error) {
	device, err := s.resolveDevice(ctx, request)
	if err != nil {
		return PositionPage{}, err
	}
	var rows []gbmodels.GbMobilePositionLatest
	if err := s.db.WithContext(ctx).Where("device_id = ?", device.ID).Order("source_code ASC, id ASC").Find(&rows).Error; err != nil {
		return PositionPage{}, ErrUnavailable
	}
	items := make([]Position, 0, len(rows))
	for _, row := range rows {
		items = append(items, positionFromRow(row.SourceCode, row.EventTime, row.ReceivedAt, row.Longitude, row.Latitude, row.Speed, row.Direction, row.Altitude))
	}
	return PositionPage{Items: items}, nil
}

func (s *Service) ListPositionHistory(ctx context.Context, request Request) (EventPage, error) {
	device, err := s.resolveDevice(ctx, request)
	if err != nil {
		return EventPage{}, err
	}
	if request.OpenAPIClientID <= 0 || !s.positionHistoryEnabled() {
		return EventPage{}, ErrUnavailable
	}
	return s.readEventPage(ctx, request, EventMobilePosition, device.ID, "")
}

func (s *Service) readEventPage(ctx context.Context, request Request, kind EventKind, deviceID uint, subscriptionID string) (EventPage, error) {
	if s == nil || s.db == nil || ctx == nil || request.OpenAPIClientID <= 0 {
		return EventPage{}, ErrUnavailable
	}
	if !persistedEventKind(kind) {
		return EventPage{}, ErrUnavailable
	}
	if kind == EventAlarm && !s.alarmEventsEnabled() || kind == EventMobilePosition && !s.positionHistoryEnabled() {
		return EventPage{}, ErrUnavailable
	}
	pageSize, err := normalizePageSize(request.PageSize)
	if err != nil {
		return EventPage{}, err
	}
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, s.db, request.ResourceScope)
	if err != nil {
		return EventPage{}, mapResourceError(err)
	}
	var device gbmodels.GbDevice
	if err := s.db.WithContext(ctx).Where("id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", deviceID, departmentIDs).Take(&device).Error; err != nil {
		return EventPage{}, ErrNotFound
	}
	if strings.TrimSpace(request.Cursor) == "" && request.Cursor != "" {
		return EventPage{}, ErrInvalidCursor
	}
	lastID := uint64(0)
	highWater := uint64(0)
	if request.Cursor != "" {
		binding := cursorBinding{ClientID: request.OpenAPIClientID, OwnerDeptID: request.ResourceScope.OwnerDeptID, DataScope: request.ResourceScope.DataScope, DeviceID: device.ID, Kind: kind, SubscriptionID: subscriptionID}
		payload, err := s.cursor.decode(request.Cursor, binding)
		if err != nil {
			return EventPage{}, err
		}
		lastID, highWater = payload.LastID, payload.HighWater
	}
	if highWater == 0 {
		highWater, err = s.maxEventID(ctx, kind, device.ID)
		if err != nil {
			return EventPage{}, err
		}
	}
	items, more, err := s.loadEvents(ctx, kind, device.ID, lastID, highWater, pageSize)
	if err != nil {
		return EventPage{}, err
	}
	checkpoint := lastID
	if more && len(items) > 0 {
		checkpoint, err = strconv.ParseUint(items[len(items)-1].ID, 10, 64)
		if err != nil {
			return EventPage{}, ErrUnavailable
		}
	} else {
		checkpoint = highWater
		highWater = 0
	}
	requestCursor := cursorPayload{
		Version: 1, ClientID: request.OpenAPIClientID, OwnerDeptID: request.ResourceScope.OwnerDeptID,
		DataScope: request.ResourceScope.DataScope, DeviceID: device.ID, Kind: kind,
		SubscriptionID: subscriptionID, LastID: checkpoint, HighWater: highWater, IssuedAt: s.now().Unix(),
	}
	token, err := s.cursor.encode(requestCursor)
	if err != nil {
		return EventPage{}, err
	}
	return EventPage{Items: items, NextCursor: token, HasMore: more}, nil
}

func (s *Service) maxEventID(ctx context.Context, kind EventKind, deviceID uint) (uint64, error) {
	var result struct{ MaxID uint64 }
	var err error
	switch kind {
	case EventAlarm:
		err = s.db.WithContext(ctx).Model(&gbmodels.GbAlarmEvent{}).Select("COALESCE(MAX(id), 0) AS max_id").Where("device_id = ?", deviceID).Scan(&result).Error
	case EventDeviceStatus:
		err = s.db.WithContext(ctx).Model(&gbmodels.GbDeviceStatusEvent{}).Select("COALESCE(MAX(id), 0) AS max_id").Where("device_id = ?", deviceID).Scan(&result).Error
	case EventMobilePosition:
		err = s.db.WithContext(ctx).Model(&gbmodels.GbMobilePositionHistory{}).Select("COALESCE(MAX(id), 0) AS max_id").Where("device_id = ?", deviceID).Scan(&result).Error
	default:
		return 0, ErrUnavailable
	}
	if err != nil {
		return 0, ErrUnavailable
	}
	return result.MaxID, nil
}

func (s *Service) loadEvents(ctx context.Context, kind EventKind, deviceID uint, after, through uint64, pageSize int) ([]Event, bool, error) {
	items := make([]Event, 0, pageSize)
	more := false
	switch kind {
	case EventAlarm:
		var rows []gbmodels.GbAlarmEvent
		if err := s.db.WithContext(ctx).Where("device_id = ? AND id > ? AND id <= ?", deviceID, after, through).Order("id ASC").Limit(pageSize + 1).Find(&rows).Error; err != nil {
			return nil, false, ErrUnavailable
		}
		more = len(rows) > pageSize
		if more {
			rows = rows[:pageSize]
		}
		for _, row := range rows {
			occurredAt := row.ReceivedAt
			if row.AlarmTime != nil {
				occurredAt = *row.AlarmTime
			}
			items = append(items, Event{ID: strconv.FormatUint(row.ID, 10), Kind: kind, SourceCode: row.SourceCode, OccurredAt: occurredAt, ReceivedAt: row.ReceivedAt, Alarm: &AlarmPayload{
				AlarmTime: row.AlarmTime, Priority: row.Priority, Method: row.Method, AlarmType: row.AlarmType, AlarmTypeParam: row.AlarmTypeParam,
				Description: row.Description, Longitude: row.Longitude, Latitude: row.Latitude,
			}})
		}
	case EventDeviceStatus:
		var rows []gbmodels.GbDeviceStatusEvent
		if err := s.db.WithContext(ctx).Where("device_id = ? AND id > ? AND id <= ?", deviceID, after, through).Order("id ASC").Limit(pageSize + 1).Find(&rows).Error; err != nil {
			return nil, false, ErrUnavailable
		}
		more = len(rows) > pageSize
		if more {
			rows = rows[:pageSize]
		}
		for _, row := range rows {
			items = append(items, Event{ID: strconv.FormatUint(row.ID, 10), Kind: kind, SourceCode: row.DeviceCode, OccurredAt: row.OccurredAt, ReceivedAt: row.CreatedAt, Status: &StatusPayload{
				EventType: row.EventType, FromStatus: row.FromStatus, ToStatus: row.ToStatus, Source: row.Source, RegisterExpires: row.RegisterExpires, KeepaliveInterval: row.KeepaliveInterval,
			}})
		}
	case EventMobilePosition:
		var rows []gbmodels.GbMobilePositionHistory
		if err := s.db.WithContext(ctx).Where("device_id = ? AND id > ? AND id <= ?", deviceID, after, through).Order("id ASC").Limit(pageSize + 1).Find(&rows).Error; err != nil {
			return nil, false, ErrUnavailable
		}
		more = len(rows) > pageSize
		if more {
			rows = rows[:pageSize]
		}
		for _, row := range rows {
			items = append(items, Event{ID: strconv.FormatUint(row.ID, 10), Kind: kind, SourceCode: row.SourceCode, OccurredAt: row.EventTime, ReceivedAt: row.ReceivedAt, Position: positionPointer(row.SourceCode, row.EventTime, row.ReceivedAt, row.Longitude, row.Latitude, row.Speed, row.Direction, row.Altitude)})
		}
	default:
		return nil, false, ErrUnavailable
	}
	return items, more, nil
}

func (s *Service) resolveDevice(ctx context.Context, request Request) (*gbmodels.GbDevice, error) {
	if s == nil || s.db == nil || ctx == nil || request.OpenAPIClientID <= 0 || strings.TrimSpace(request.DeviceID) == "" {
		return nil, ErrInvalidRequest
	}
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, s.db, request.ResourceScope)
	if err != nil {
		return nil, mapResourceError(err)
	}
	var devices []gbmodels.GbDevice
	if err := s.db.WithContext(ctx).Where("device_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", request.DeviceID, departmentIDs).Limit(2).Find(&devices).Error; err != nil {
		return nil, ErrUnavailable
	}
	if len(devices) != 1 {
		return nil, ErrNotFound
	}
	return &devices[0], nil
}

func (s *Service) ownedSubscription(ctx context.Context, request Request) (*OpenAPIDeviceSubscription, *gbmodels.GbDevice, error) {
	if s == nil || s.db == nil || ctx == nil || request.OpenAPIClientID <= 0 || strings.TrimSpace(request.SubscriptionID) == "" {
		return nil, nil, ErrInvalidRequest
	}
	var subscription OpenAPIDeviceSubscription
	if err := s.db.WithContext(ctx).Where("id = ? AND client_id = ?", request.SubscriptionID, request.OpenAPIClientID).Take(&subscription).Error; err != nil {
		return nil, nil, ErrNotFound
	}
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, s.db, request.ResourceScope)
	if err != nil {
		return nil, nil, mapResourceError(err)
	}
	var device gbmodels.GbDevice
	if err := s.db.WithContext(ctx).Where("id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", subscription.DeviceID, departmentIDs).Take(&device).Error; err != nil {
		return nil, nil, ErrNotFound
	}
	if request.DeviceID != "" && request.DeviceID != device.DeviceID {
		return nil, nil, ErrNotFound
	}
	return &subscription, &device, nil
}

func (s *Service) subscriptionView(ctx context.Context, subscription *OpenAPIDeviceSubscription, deviceCode string) ConfiguredSubscription {
	status := "enabled"
	if !subscription.Enabled {
		status = "cancelled"
	} else if !subscription.ExpiresAt.After(s.now()) {
		status = "expired"
	}
	return ConfiguredSubscription{ID: subscription.ID, DeviceID: deviceCode, Kind: subscription.Kind, Enabled: subscription.Enabled, Status: status,
		ExpiresAt: subscription.ExpiresAt, ExpiresSeconds: subscription.ExpiresSeconds, CreatedAt: subscription.CreatedAt, UpdatedAt: subscription.UpdatedAt}
}

func (s *Service) alarmEventsEnabled() bool {
	if s.config.AlarmEventsEnabled != nil {
		return s.config.AlarmEventsEnabled()
	}
	return gbconfig.SaveAlarmMessages()
}

func (s *Service) positionHistoryEnabled() bool {
	if s.config.PositionHistoryEnabled != nil {
		return s.config.PositionHistoryEnabled()
	}
	return subscribe.PositionHistoryEnabled()
}

func supportedKind(kind EventKind) bool {
	switch kind {
	case EventAlarm, EventDeviceStatus, EventMobilePosition:
		return true
	default:
		return false
	}
}

func persistedEventKind(kind EventKind) bool { return supportedKind(kind) }

func kindNeedsSIP(kind EventKind) bool { return kind == EventAlarm || kind == EventMobilePosition }

func sipKind(kind EventKind) gbmodels.SubscriptionKind {
	return gbmodels.SubscriptionKind(kind)
}

func validateSubscriptionOptions(request Request) (int, *int, error) {
	expires := request.ExpiresSeconds
	if expires == 0 {
		expires = 3600
	}
	if expires < minExpires || expires > maxExpires {
		return 0, nil, ErrInvalidRequest
	}
	if request.IntervalSeconds != 0 && request.Kind != EventMobilePosition {
		return 0, nil, ErrInvalidRequest
	}
	if request.IntervalSeconds < 0 || request.IntervalSeconds > 24*60*60 {
		return 0, nil, ErrInvalidRequest
	}
	var interval *int
	if request.IntervalSeconds > 0 {
		interval = &request.IntervalSeconds
	}
	return expires, interval, nil
}

func normalizePageSize(size int) (int, error) {
	if size == 0 {
		return defaultPageSize, nil
	}
	if size < 1 || size > maxPageSize {
		return 0, ErrInvalidRequest
	}
	return size, nil
}

func mapResourceError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, resource.ErrResourceNotFound):
		return ErrNotFound
	case errors.Is(err, resource.ErrInvalidDepartmentScope), errors.Is(err, resource.ErrInvalidListOptions):
		return ErrInvalidRequest
	default:
		return ErrUnavailable
	}
}

func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate subscription id: %w", err)
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	hexID := hex.EncodeToString(raw[:])
	return hexID[:8] + "-" + hexID[8:12] + "-" + hexID[12:16] + "-" + hexID[16:20] + "-" + hexID[20:], nil
}

func positionFromRow(source string, eventTime, receivedAt time.Time, longitude, latitude float64, speed, direction, altitude *float64) Position {
	return Position{SourceCode: source, EventTime: eventTime, ReceivedAt: receivedAt, Longitude: longitude, Latitude: latitude, Speed: speed, Direction: direction, Altitude: altitude}
}

func positionPointer(source string, eventTime, receivedAt time.Time, longitude, latitude float64, speed, direction, altitude *float64) *Position {
	position := positionFromRow(source, eventTime, receivedAt, longitude, latitude, speed, direction, altitude)
	return &position
}
