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

func terminalPTZStatus(status gbmodels.PTZOperationStatus) bool {
	switch status {
	case gbmodels.PTZOperationAccepted, gbmodels.PTZOperationRejected, gbmodels.PTZOperationTimeout, gbmodels.PTZOperationCancelled:
		return true
	default:
		return false
	}
}

// ApplyResponse correlates an application-level response and updates a
// non-terminal operation exactly once. matched=false means the response was
// valid but did not identify a unique local operation.
func (s *Service) ApplyResponse(ctx context.Context, response Response) (gbmodels.GbPTZOperation, bool, error) {
	if s == nil || s.db == nil {
		return gbmodels.GbPTZOperation{}, false, fmt.Errorf("PTZ service 未就绪")
	}
	var operation gbmodels.GbPTZOperation
	query := s.db.WithContext(ctx)
	if response.OperationID != "" {
		query = query.Where("operation_id = ?", response.OperationID)
	} else if response.SN > 0 && response.DeviceCode != "" {
		query = query.Where("sn = ? AND device_code = ?", response.SN, response.DeviceCode)
		if response.ChannelCode != "" {
			query = query.Where("channel_code = ?", response.ChannelCode)
		}
	} else if response.CallID != "" && response.CSeq != "" {
		query = query.Where("call_id = ? AND cseq = ?", response.CallID, response.CSeq)
	} else {
		return gbmodels.GbPTZOperation{}, false, nil
	}
	if result := query.Order("id DESC").Limit(1).Find(&operation); result.Error != nil {
		return gbmodels.GbPTZOperation{}, false, result.Error
	} else if result.RowsAffected == 0 {
		return gbmodels.GbPTZOperation{}, false, nil
	}
	if terminalPTZStatus(operation.Status) {
		return operation, true, nil
	}
	status := gbmodels.PTZOperationUnknown
	if response.SIPStatus >= 300 || strings.EqualFold(response.DeviceResult, "ERROR") {
		status = gbmodels.PTZOperationRejected
	} else if response.SIPStatus >= 200 && response.SIPStatus < 300 && strings.EqualFold(response.DeviceResult, "OK") {
		status = gbmodels.PTZOperationAccepted
	}
	completedAt := s.now()
	updates := map[string]interface{}{
		"status": status, "sip_status": response.SIPStatus, "device_result": response.DeviceResult,
		"device_error": response.DeviceError, "completed_at": completedAt,
	}
	if response.CallID != "" {
		updates["call_id"] = response.CallID
	}
	if response.CSeq != "" {
		updates["cseq"] = response.CSeq
	}
	if err := s.db.WithContext(ctx).Model(&operation).Updates(updates).Error; err != nil {
		return operation, true, err
	}
	operation.Status, operation.SIPStatus, operation.DeviceResult, operation.DeviceError, operation.CompletedAt = status, response.SIPStatus, response.DeviceResult, response.DeviceError, &completedAt
	return operation, true, nil
}

func (s *Service) MarkTimeout(ctx context.Context, operationID, message string) (gbmodels.GbPTZOperation, error) {
	var operation gbmodels.GbPTZOperation
	if result := s.db.WithContext(ctx).Where("operation_id = ?", operationID).Limit(1).Find(&operation); result.Error != nil {
		return operation, result.Error
	} else if result.RowsAffected == 0 {
		return operation, fmt.Errorf("PTZ 操作不存在")
	}
	if terminalPTZStatus(operation.Status) {
		return operation, nil
	}
	completedAt := s.now()
	if err := s.db.WithContext(ctx).Model(&operation).Updates(map[string]interface{}{"status": gbmodels.PTZOperationTimeout, "error_message": message, "completed_at": completedAt}).Error; err != nil {
		return operation, err
	}
	operation.Status, operation.ErrorMessage, operation.CompletedAt = gbmodels.PTZOperationTimeout, message, &completedAt
	return operation, nil
}

func (s *Service) GetOperation(ctx context.Context, operationID string) (gbmodels.GbPTZOperation, error) {
	var operation gbmodels.GbPTZOperation
	if operationID == "" {
		return operation, fmt.Errorf("操作编号不能为空")
	}
	result := s.db.WithContext(ctx).Where("operation_id = ?", operationID).Limit(1).Find(&operation)
	if result.Error != nil {
		return operation, result.Error
	}
	if result.RowsAffected == 0 {
		return operation, gorm.ErrRecordNotFound
	}
	return operation, nil
}
