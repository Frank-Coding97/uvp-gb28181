package ptz

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
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
	PTZType       int8
}

type Command struct {
	CmdType        string
	Action         string
	IdempotencyKey string
	Payload        map[string]interface{}
	Build          func(sn int) ([]byte, error)
}

type Service struct {
	db     *gorm.DB
	sender TrackedSender
	now    func() time.Time
	sn     atomic.Uint64
	locks  sync.Map
}

func NewService(db *gorm.DB, sender TrackedSender, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, sender: sender, now: now}
}

func (s *Service) lockFor(channelID uint) *sync.Mutex {
	value, _ := s.locks.LoadOrStore(channelID, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (s *Service) nextSN() int {
	return int(s.sn.Add(1))
}

func (s *Service) Execute(ctx context.Context, target Target, command Command) (gbmodels.GbPTZOperation, error) {
	if s == nil || s.db == nil {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("PTZ service 未就绪")
	}
	if s.sender == nil {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("SIP UAC 未就绪")
	}
	if err := validateTarget(target); err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	if strings.TrimSpace(command.CmdType) == "" || command.Build == nil {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("PTZ 命令不完整")
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" {
		command.IdempotencyKey = uuid.NewString()
	}
	mutex := s.lockFor(target.ChannelID)
	mutex.Lock()
	defer mutex.Unlock()

	var existing gbmodels.GbPTZOperation
	if result := s.db.WithContext(ctx).Where("channel_id = ? AND idempotency_key = ?", target.ChannelID, command.IdempotencyKey).Limit(1).Find(&existing); result.Error != nil {
		return gbmodels.GbPTZOperation{}, result.Error
	} else if result.RowsAffected > 0 {
		return existing, nil
	}

	sn := s.nextSN()
	body, err := command.Build(sn)
	if err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	payloadJSON := "{}"
	if command.Payload != nil {
		encoded, marshalErr := json.Marshal(command.Payload)
		if marshalErr != nil {
			return gbmodels.GbPTZOperation{}, fmt.Errorf("PTZ 参数摘要编码失败: %w", marshalErr)
		}
		payloadJSON = string(encoded)
	}
	now := s.now()
	op := gbmodels.GbPTZOperation{
		OperationID: uuid.NewString(), IdempotencyKey: command.IdempotencyKey,
		DeviceID: target.DeviceID, DeviceCode: target.DeviceCode, ChannelID: target.ChannelID, ChannelCode: target.ChannelCode,
		CmdType: command.CmdType, Action: command.Action, PayloadJSON: payloadJSON, SN: sn,
		Status: gbmodels.PTZOperationQueued, Attempt: 1, CreatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&op).Error; err != nil {
		var raced gbmodels.GbPTZOperation
		if result := s.db.WithContext(ctx).Where("channel_id = ? AND idempotency_key = ?", target.ChannelID, command.IdempotencyKey).Limit(1).Find(&raced); result.Error == nil && result.RowsAffected > 0 {
			return raced, nil
		}
		return gbmodels.GbPTZOperation{}, err
	}

	destination := net.JoinHostPort(target.IP, strconv.Itoa(target.Port))
	result, sendErr := s.sender.SendMessageTracked(ctx, target.DeviceCode, destination, target.Transport, body)
	if sendErr != nil {
		status := gbmodels.PTZOperationRejected
		if ctx.Err() != nil {
			status = gbmodels.PTZOperationTimeout
		}
		message := sendErr.Error()
		_ = s.db.WithContext(ctx).Model(&op).Updates(map[string]interface{}{
			"status": status, "error_message": message, "completed_at": s.now(),
		}).Error
		op.Status = status
		op.ErrorMessage = message
		return op, sendErr
	}
	sentAt := s.now()
	updates := map[string]interface{}{
		"status": gbmodels.PTZOperationSent, "call_id": result.CallID, "cseq": result.CSeq,
		"sip_status": result.StatusCode, "sent_at": sentAt,
	}
	if err := s.db.WithContext(ctx).Model(&op).Updates(updates).Error; err != nil {
		return op, err
	}
	op.Status = gbmodels.PTZOperationSent
	op.CallID, op.CSeq, op.SIPStatus, op.SentAt = result.CallID, result.CSeq, result.StatusCode, &sentAt
	return op, nil
}

func validateTarget(target Target) error {
	if target.DeviceID == 0 || target.ChannelID == 0 || strings.TrimSpace(target.DeviceCode) == "" || strings.TrimSpace(target.ChannelCode) == "" {
		return fmt.Errorf("PTZ 目标不完整")
	}
	if !target.DeviceOnline || !target.ChannelOnline {
		return fmt.Errorf("设备或通道离线")
	}
	if target.PTZType == 0 {
		return fmt.Errorf("通道未上报可用云台能力")
	}
	if strings.TrimSpace(target.IP) == "" || target.Port <= 0 {
		return fmt.Errorf("设备来源地址缺失")
	}
	return nil
}
