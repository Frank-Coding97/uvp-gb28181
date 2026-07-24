package ptz

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type TrackedSender interface {
	SendMessageTracked(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error)
}

type Target struct {
	DeviceID      uint
	DeviceCode    string
	ChannelID     uint
	ChannelCode   string
	IP            string
	Port          int
	Transport     string
	DeviceOnline  bool
	ChannelOnline bool
}

type Command struct {
	CmdType            string
	Action             string
	IdempotencyKey     string
	Payload            map[string]interface{}
	ResponseRequired   bool
	MaxAttempts        int
	ActorID            uint
	ActorDeptID        uint
	TriggerOperationID string
	Build              func(sn int) ([]byte, error)
}

type Response struct {
	OperationID  string
	DeviceCode   string
	ChannelCode  string
	SN           int
	CallID       string
	CSeq         string
	SIPStatus    int
	DeviceResult string
	DeviceError  string
}

type PreciseNotify struct {
	DeviceID    uint
	DeviceCode  string
	ChannelID   uint
	ChannelCode string
	SN          int
	Pan         *float64
	Tilt        *float64
	Zoom        *float64
	Focus       *float64
	Iris        *float64
	DeviceTime  *time.Time
	ReceivedAt  time.Time
	DedupeKey   string
	RawSummary  string
}

type Service struct {
	db     *gorm.DB
	sender TrackedSender
	now    func() time.Time
	sn     atomic.Uint64
	locks  sync.Map
}

func NewService(db *gorm.DB, sender TrackedSender, now func() time.Time) (*Service, error) {
	if db == nil {
		return nil, operationError(ErrorCodeHomePositionUnavailable, "PTZ 数据库未就绪", nil)
	}
	if now == nil {
		now = time.Now
	}
	var maxSN int64
	if err := db.Model(&gbmodels.GbPTZOperation{}).Select("COALESCE(MAX(sn), 0)").Scan(&maxSN).Error; err != nil {
		return nil, operationError(ErrorCodeHomePositionUnavailable, "读取 PTZ operation SN 失败", err)
	}
	if maxSN < 0 || uint64(maxSN) >= uint64(^uint(0)>>1) {
		return nil, operationError(ErrorCodeHomePositionUnavailable, "PTZ operation SN 非法", nil)
	}
	service := &Service{db: db, sender: sender, now: now}
	service.sn.Store(uint64(maxSN))
	return service, nil
}

func (s *Service) lockFor(channelID uint) *sync.Mutex {
	value, _ := s.locks.LoadOrStore(channelID, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (s *Service) nextSN() int {
	return int(s.sn.Add(1))
}

func validateTarget(target Target) error {
	if target.DeviceID == 0 || target.ChannelID == 0 || strings.TrimSpace(target.DeviceCode) == "" || strings.TrimSpace(target.ChannelCode) == "" {
		return operationError(ErrorCodeHomePositionUnavailable, "PTZ 目标不完整", nil)
	}
	if !target.DeviceOnline || !target.ChannelOnline {
		return operationError(ErrorCodeHomePositionDeviceOffline, "设备或通道离线", nil)
	}
	if strings.TrimSpace(target.IP) == "" || target.Port <= 0 {
		return operationError(ErrorCodeHomePositionUnavailable, "设备来源地址缺失", nil)
	}
	return nil
}

func validatePreciseValue(name string, value *float64) error {
	if value == nil {
		return nil
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) {
		return fmt.Errorf("PTZ %s 数值非法", name)
	}
	switch name {
	case "pan":
		if *value < -360 || *value > 360 {
			return fmt.Errorf("PTZ pan 超出范围")
		}
	case "tilt":
		if *value < -180 || *value > 180 {
			return fmt.Errorf("PTZ tilt 超出范围")
		}
	case "zoom", "focus", "iris":
		if *value < 0 || *value > 100000 {
			return fmt.Errorf("PTZ %s 超出范围", name)
		}
	}
	return nil
}

// ApplyPreciseNotify applies one valid precise-position notification in a
// transaction. Older device timestamps and duplicate dedupe keys never
// overwrite the latest valid values.
func (s *Service) ApplyPreciseNotify(ctx context.Context, notify PreciseNotify) (gbmodels.GbPTZState, error) {
	if s == nil || s.db == nil {
		return gbmodels.GbPTZState{}, fmt.Errorf("PTZ service 未就绪")
	}
	if notify.DeviceID == 0 || notify.ChannelID == 0 || notify.DeviceCode == "" || notify.ChannelCode == "" {
		return gbmodels.GbPTZState{}, fmt.Errorf("精准通知目标不完整")
	}
	for name, value := range map[string]*float64{"pan": notify.Pan, "tilt": notify.Tilt, "zoom": notify.Zoom, "focus": notify.Focus, "iris": notify.Iris} {
		if err := validatePreciseValue(name, value); err != nil {
			return gbmodels.GbPTZState{}, err
		}
	}
	if notify.ReceivedAt.IsZero() {
		notify.ReceivedAt = s.now()
	}
	var state gbmodels.GbPTZState
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current gbmodels.GbPTZState
		result := tx.Where("channel_id = ?", notify.ChannelID).Limit(1).Find(&current)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			state = current
			if notify.DedupeKey != "" && current.DedupeKey == notify.DedupeKey {
				return tx.Model(&current).Updates(map[string]interface{}{"received_at": notify.ReceivedAt}).Error
			}
			if notify.DeviceTime != nil && current.DeviceTime != nil && notify.DeviceTime.Before(*current.DeviceTime) {
				return tx.Model(&current).Updates(map[string]interface{}{"received_at": notify.ReceivedAt, "dedupe_key": notify.DedupeKey}).Error
			}
			updates := map[string]interface{}{
				"device_id": notify.DeviceID, "device_code": notify.DeviceCode, "received_at": notify.ReceivedAt,
				"source_sn": notify.SN, "freshness": gbmodels.PTZFreshnessFresh, "dedupe_key": notify.DedupeKey,
				"raw_summary": notify.RawSummary,
			}
			if notify.DeviceTime != nil {
				updates["device_time"] = notify.DeviceTime
			}
			if notify.Pan != nil {
				updates["pan"] = notify.Pan
			}
			if notify.Tilt != nil {
				updates["tilt"] = notify.Tilt
			}
			if notify.Zoom != nil {
				updates["zoom"] = notify.Zoom
			}
			if notify.Focus != nil {
				updates["focus"] = notify.Focus
			}
			if notify.Iris != nil {
				updates["iris"] = notify.Iris
			}
			return tx.Model(&current).Updates(updates).Error
		}
		state = gbmodels.GbPTZState{DeviceID: notify.DeviceID, ChannelID: notify.ChannelID, ChannelCode: notify.ChannelCode,
			Pan: notify.Pan, Tilt: notify.Tilt, Zoom: notify.Zoom, Focus: notify.Focus, Iris: notify.Iris,
			DeviceTime: notify.DeviceTime, ReceivedAt: notify.ReceivedAt, SourceSN: notify.SN,
			Freshness: gbmodels.PTZFreshnessFresh, DedupeKey: notify.DedupeKey, RawSummary: notify.RawSummary}
		return tx.Create(&state).Error
	})
	if err != nil {
		return gbmodels.GbPTZState{}, err
	}
	if err := s.db.WithContext(ctx).Where("channel_id = ?", notify.ChannelID).Limit(1).Find(&state).Error; err != nil {
		return gbmodels.GbPTZState{}, err
	}
	return state, nil
}
