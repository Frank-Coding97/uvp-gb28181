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
	"gorm.io/plugin/dbresolver"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
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
	// Profile is the device's effective protocol profile at dispatch time. It
	// is copied into each operation by Execute so retries remain deterministic.
	Profile protocol.Profile
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
	// Profile and target are captured on the operation. A zero profile keeps
	// existing callers on the 2016 compatibility path.
	Profile     protocol.Profile
	TargetScope string
	TargetCode  string
	Build       func(sn int) ([]byte, error)
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
	db           *gorm.DB
	sender       TrackedSender
	now          func() time.Time
	sn           atomic.Uint64
	locks        sync.Map
	queryStageMu sync.Mutex
	queryStages  map[string]queryResponseStage
	lifecycleMu  sync.RWMutex
	retired      bool
}

func NewService(db *gorm.DB, sender TrackedSender, now func() time.Time) (*Service, error) {
	if db == nil {
		return nil, operationError(ErrorCodeHomePositionUnavailable, "PTZ 数据库未就绪", nil)
	}
	if now == nil {
		now = time.Now
	}
	var maxSN int64
	if err := ptzWriter(db).Model(&gbmodels.GbPTZOperation{}).Select("COALESCE(MAX(sn), 0)").Scan(&maxSN).Error; err != nil {
		return nil, operationError(ErrorCodeHomePositionUnavailable, "读取 PTZ operation SN 失败", err)
	}
	if maxSN < 0 || uint64(maxSN) >= uint64(^uint(0)>>1) {
		return nil, operationError(ErrorCodeHomePositionUnavailable, "PTZ operation SN 非法", nil)
	}
	service := &Service{db: db, sender: sender, now: now, queryStages: make(map[string]queryResponseStage)}
	service.sn.Store(uint64(maxSN))
	return service, nil
}

func ptzWriter(db *gorm.DB) *gorm.DB {
	return db.Clauses(dbresolver.Write)
}

// channelLockEntry 带引用计数的通道锁:引用归零后可安全从锁表回收,
// 防止锁表随历史通道 ID 无限累积
type channelLockEntry struct {
	mu   sync.Mutex
	refs atomic.Int32
}

// lockChannel 获取通道锁并计数,返回的 entry 必须与 unlockChannel 成对使用.
func (s *Service) lockChannel(channelID uint) *channelLockEntry {
	for {
		value, _ := s.locks.LoadOrStore(channelID, &channelLockEntry{})
		entry := value.(*channelLockEntry)
		entry.refs.Add(1)
		current, ok := s.locks.Load(channelID)
		if ok && current == entry {
			return entry
		}
		// 条目在计数期间已被回收替换:回退计数并重试到新条目
		entry.refs.Add(-1)
	}
}

// unlockChannel 释放引用;归零时若仍是当前条目则从锁表移除.
func (s *Service) unlockChannel(channelID uint, entry *channelLockEntry) {
	if entry.refs.Add(-1) == 0 {
		if current, ok := s.locks.Load(channelID); ok && current == entry {
			s.locks.Delete(channelID)
		}
	}
}

func (s *Service) nextSN() int {
	return int(s.sn.Add(1))
}

// Retire prevents future operation creation and waits for every Execute that
// already entered the old runtime. Reload calls it before a new Service reads
// MAX(sn), so two generations cannot allocate the same sequence number.
func (s *Service) Retire() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	s.retired = true
	s.lifecycleMu.Unlock()
	s.clearQueryStages()
}

func validateTargetIdentity(target Target) error {
	if target.DeviceID == 0 || target.ChannelID == 0 || strings.TrimSpace(target.DeviceCode) == "" || strings.TrimSpace(target.ChannelCode) == "" {
		return operationError(ErrorCodeHomePositionUnavailable, "PTZ 目标不完整", nil)
	}
	return nil
}

func validateTargetAvailability(target Target) error {
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
		state = gbmodels.GbPTZState{DeviceID: notify.DeviceID, DeviceCode: notify.DeviceCode, ChannelID: notify.ChannelID, ChannelCode: notify.ChannelCode,
			Pan: notify.Pan, Tilt: notify.Tilt, Zoom: notify.Zoom, Focus: notify.Focus, Iris: notify.Iris,
			DeviceTime: notify.DeviceTime, ReceivedAt: notify.ReceivedAt, SourceSN: notify.SN,
			Freshness: gbmodels.PTZFreshnessFresh, DedupeKey: notify.DedupeKey, RawSummary: notify.RawSummary}
		return tx.Create(&state).Error
	})
	if err != nil {
		return gbmodels.GbPTZState{}, err
	}
	// 事务后回读必须走写库句柄:dbresolver 读写分离时读副本可能滞后,
	// 调用方(精准位置通知刚写入)会拿到旧状态
	if err := ptzWriter(s.db).WithContext(ctx).Where("channel_id = ?", notify.ChannelID).Limit(1).Find(&state).Error; err != nil {
		return gbmodels.GbPTZState{}, err
	}
	return state, nil
}
