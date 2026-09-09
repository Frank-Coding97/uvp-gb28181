package ptz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func canonicalPayload(payload map[string]interface{}) (string, error) {
	if payload == nil {
		return "{}", nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("PTZ 参数摘要编码失败: %w", err)
	}
	return canonicalPayloadJSON(string(encoded))
}

func canonicalPayloadJSON(payload string) (string, error) {
	if strings.TrimSpace(payload) == "" {
		return "{}", nil
	}
	decoder := json.NewDecoder(bytes.NewBufferString(payload))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("payload 包含多个 JSON 值")
		}
		return "", err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func normalizeIdempotencyKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return uuid.NewString(), nil
	}
	if !utf8.ValidString(key) {
		return "", operationError(ErrorCodeHomePositionInvalidArgument, "幂等键必须是有效 UTF-8", nil)
	}
	if len(key) > 128 {
		return "", operationError(ErrorCodeHomePositionInvalidArgument, "幂等键长度不能超过 128 字节", nil)
	}
	for _, char := range key {
		if unicode.IsControl(char) {
			return "", operationError(ErrorCodeHomePositionInvalidArgument, "幂等键不能包含控制字符", nil)
		}
	}
	return key, nil
}

func operationMatches(existing gbmodels.GbPTZOperation, command Command, payloadJSON string) bool {
	existingPayload, err := canonicalPayloadJSON(existing.PayloadJSON)
	if err != nil {
		return false
	}
	return existing.CmdType == strings.TrimSpace(command.CmdType) &&
		existing.Action == strings.TrimSpace(command.Action) &&
		existingPayload == payloadJSON
}

func idempotencyConflict(existing gbmodels.GbPTZOperation) error {
	return operationError(
		ErrorCodeHomePositionIdempotencyConflict,
		fmt.Sprintf("幂等键已用于不同 PTZ 请求: %s", existing.OperationID),
		nil,
	)
}

func (s *Service) findIdempotentOperation(ctx context.Context, channelID uint, key string) (gbmodels.GbPTZOperation, bool, error) {
	var operation gbmodels.GbPTZOperation
	result := ptzWriter(s.db).WithContext(ctx).
		Where("channel_id = ? AND idempotency_key = ?", channelID, key).
		Limit(1).Find(&operation)
	return operation, result.RowsAffected > 0, result.Error
}

func triggerOperationPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func (s *Service) createOperation(
	ctx context.Context,
	target Target,
	command Command,
	payloadJSON string,
	sn int,
	createdAt time.Time,
) (gbmodels.GbPTZOperation, error) {
	profile := command.Profile
	if profile.Version == "" {
		profile = target.Profile
		if profile.Version == "" {
			profile = protocol.ProfileFor(protocol.Version2016)
		}
	}
	targetScope := strings.TrimSpace(command.TargetScope)
	if targetScope == "" {
		targetScope = gbmodels.ControlTargetScopeChannel
	}
	targetCode := strings.TrimSpace(command.TargetCode)
	if targetCode == "" {
		if targetScope != gbmodels.ControlTargetScopeChannel {
			return gbmodels.GbPTZOperation{}, operationError(ErrorCodeHomePositionInvalidArgument, "PTZ 非通道目标编码不能为空", nil)
		}
		targetCode = strings.TrimSpace(target.ChannelCode)
	}
	scopeKey, err := controlScopeKey(targetScope, targetCode)
	if err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	operation := gbmodels.GbPTZOperation{
		OperationID: uuid.NewString(), IdempotencyKey: command.IdempotencyKey,
		DeviceID: target.DeviceID, DeviceCode: target.DeviceCode, ChannelID: target.ChannelID, ChannelCode: target.ChannelCode,
		CmdType: strings.TrimSpace(command.CmdType), Action: strings.TrimSpace(command.Action), PayloadJSON: payloadJSON, SN: sn,
		ProfileVersion: string(profile.Version), ProfileCharset: string(profile.Charset), TargetScope: targetScope, TargetCode: targetCode, ScopeKey: scopeKey,
		Status: gbmodels.PTZOperationQueued, Attempt: 1, MaxAttempts: 1,
		ActorID: command.ActorID, ActorDeptID: command.ActorDeptID,
		TriggerOperationID: triggerOperationPointer(command.TriggerOperationID), CreatedAt: createdAt,
	}
	if !command.ResponseRequired {
		return operation, s.db.WithContext(ctx).Create(&operation).Error
	}

	maxAttempts := command.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	queueDeadline := createdAt.Add(5 * time.Second)
	values := map[string]interface{}{
		"operation_id": operation.OperationID, "idempotency_key": operation.IdempotencyKey,
		"device_id": operation.DeviceID, "device_code": operation.DeviceCode,
		"channel_id": operation.ChannelID, "channel_code": operation.ChannelCode,
		"cmd_type": operation.CmdType, "action": operation.Action, "payload_json": operation.PayloadJSON,
		"profile_version": operation.ProfileVersion, "profile_charset": operation.ProfileCharset,
		"target_scope": operation.TargetScope, "target_code": operation.TargetCode, "scope_key": operation.ScopeKey,
		"sn": operation.SN, "status": operation.Status, "attempt": 0,
		"response_required": true, "max_attempts": maxAttempts,
		"actor_id": operation.ActorID, "actor_dept_id": operation.ActorDeptID,
		"trigger_operation_id": operation.TriggerOperationID,
		"queue_deadline_at":    queueDeadline, "created_at": createdAt,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Model(&gbmodels.GbPTZOperation{}).Create(values).Error
	}); err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	if err := ptzWriter(s.db).WithContext(ctx).Where("operation_id = ?", operation.OperationID).First(&operation).Error; err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	return operation, nil
}

func controlScopeKey(scope, code string) (string, error) {
	scope = strings.TrimSpace(scope)
	code = strings.TrimSpace(code)
	switch scope {
	case gbmodels.ControlTargetScopeChannel, gbmodels.ControlTargetScopeDevice, gbmodels.ControlTargetScopeAlarm:
	default:
		return "", operationError(ErrorCodeHomePositionInvalidArgument, "PTZ 目标 scope 不合法", nil)
	}
	if code == "" {
		return "", operationError(ErrorCodeHomePositionInvalidArgument, "PTZ 目标编码不能为空", nil)
	}
	return scope + ":" + code, nil
}

// Execute creates one immutable operation. Response-required operations are
// left queued for the persistent scheduler; legacy one-way commands retain
// their synchronous send semantics.
func (s *Service) Execute(ctx context.Context, target Target, command Command) (gbmodels.GbPTZOperation, error) {
	if s == nil || s.db == nil {
		return gbmodels.GbPTZOperation{}, operationError(ErrorCodeHomePositionUnavailable, "PTZ service 未就绪", nil)
	}
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if s.retired {
		return gbmodels.GbPTZOperation{}, operationError(ErrorCodeHomePositionUnavailable, "PTZ service 已卸载", nil)
	}
	if err := validateTargetIdentity(target); err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	if strings.TrimSpace(command.CmdType) == "" || command.Build == nil {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("PTZ 命令不完整")
	}
	idempotencyKey, err := normalizeIdempotencyKey(command.IdempotencyKey)
	if err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	command.IdempotencyKey = idempotencyKey
	payloadJSON, err := canonicalPayload(command.Payload)
	if err != nil {
		return gbmodels.GbPTZOperation{}, err
	}

	entry := s.lockChannel(target.ChannelID)
	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		s.unlockChannel(target.ChannelID, entry)
	}()

	existing, found, err := s.findIdempotentOperation(ctx, target.ChannelID, command.IdempotencyKey)
	if err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	if found {
		if !operationMatches(existing, command, payloadJSON) {
			return existing, idempotencyConflict(existing)
		}
		return existing, nil
	}
	if s.sender == nil {
		return gbmodels.GbPTZOperation{}, operationError(ErrorCodeHomePositionUnavailable, "SIP UAC 未就绪", nil)
	}
	if err := validateTargetAvailability(target); err != nil {
		return gbmodels.GbPTZOperation{}, err
	}

	sn := s.nextSN()
	body, err := command.Build(sn)
	if err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	operation, err := s.createOperation(ctx, target, command, payloadJSON, sn, s.now())
	if err != nil {
		raced, racedFound, readErr := s.findIdempotentOperation(ctx, target.ChannelID, command.IdempotencyKey)
		if readErr == nil && racedFound {
			if !operationMatches(raced, command, payloadJSON) {
				return raced, idempotencyConflict(raced)
			}
			return raced, nil
		}
		return gbmodels.GbPTZOperation{}, err
	}
	if command.ResponseRequired {
		return operation, nil
	}
	return s.sendLegacyOperation(ctx, target, operation, body)
}

func (s *Service) sendLegacyOperation(ctx context.Context, target Target, operation gbmodels.GbPTZOperation, body []byte) (gbmodels.GbPTZOperation, error) {
	destination := net.JoinHostPort(target.IP, strconv.Itoa(target.Port))
	result, sendErr := s.sender.SendMessageTracked(ctx, target.DeviceCode, destination, target.Transport, body)
	observedAt := s.now()
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()

	if sendErr != nil {
		status := gbmodels.PTZOperationRejected
		if ctx.Err() != nil {
			status = gbmodels.PTZOperationTimeout
		}
		updates := map[string]interface{}{
			"status": status, "error_message": sendErr.Error(), "completed_at": observedAt,
		}
		_ = s.db.WithContext(persistCtx).Model(&gbmodels.GbPTZOperation{}).
			Where("id = ? AND status = ?", operation.ID, gbmodels.PTZOperationQueued).
			Updates(updates).Error
		_ = s.persistFirstOutboundMetadata(persistCtx, operation.ID, result, observedAt)
		_ = ptzWriter(s.db).WithContext(persistCtx).First(&operation, operation.ID).Error
		return operation, sendErr
	}

	if _, err := s.MarkSent(persistCtx, operation.OperationID, result, observedAt); err != nil {
		return operation, err
	}
	if err := ptzWriter(s.db).WithContext(persistCtx).First(&operation, operation.ID).Error; err != nil {
		return operation, err
	}
	if syncErr := s.SyncPresetOperation(persistCtx, operation); syncErr != nil {
		app.Log(persistCtx).Named("ptz").Warn("预置位乐观入库失败", zap.String("event", "ptz.preset_cache_failed"), zap.String("operationId", operation.OperationID), logging.Error(syncErr))
	}
	return operation, nil
}

func (s *Service) persistFirstOutboundMetadata(ctx context.Context, operationID uint, result uac.TrackedMessageResult, observedAt time.Time) error {
	updates := map[string]interface{}{
		"call_id": result.CallID, "cseq": result.CSeq, "sip_status": result.StatusCode,
	}
	if result.StatusCode > 0 {
		updates["sent_at"] = observedAt
	}
	return s.db.WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
		Where("id = ? AND (call_id = '' OR call_id IS NULL)", operationID).
		Updates(updates).Error
}

// MarkSent records the first outbound SIP result and only advances queued to
// sent. A fast application response can win without being overwritten.
func (s *Service) MarkSent(ctx context.Context, operationID string, result uac.TrackedMessageResult, observedAt time.Time) (gbmodels.GbPTZOperation, error) {
	operation, err := s.GetOperation(ctx, operationID)
	if err != nil {
		return operation, err
	}
	if observedAt.IsZero() {
		observedAt = s.now()
	}
	if err := s.persistFirstOutboundMetadata(ctx, operation.ID, result, observedAt); err != nil {
		return operation, err
	}
	updates := map[string]interface{}{"status": gbmodels.PTZOperationSent}
	if operation.ResponseRequired {
		updates["deadline_at"] = observedAt.Add(15 * time.Second)
	}
	if err := s.db.WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
		Where("id = ? AND status = ?", operation.ID, gbmodels.PTZOperationQueued).
		Updates(updates).Error; err != nil {
		return operation, err
	}
	return s.GetOperation(ctx, operationID)
}

func terminalPTZStatus(status gbmodels.PTZOperationStatus) bool {
	switch status {
	case gbmodels.PTZOperationAccepted, gbmodels.PTZOperationRejected, gbmodels.PTZOperationTimeout, gbmodels.PTZOperationCancelled:
		return true
	default:
		return false
	}
}

func (s *Service) findResponseOperation(ctx context.Context, response Response) (gbmodels.GbPTZOperation, bool, error) {
	query := ptzWriter(s.db).WithContext(ctx)
	if response.OperationID != "" {
		var operation gbmodels.GbPTZOperation
		result := query.Where("operation_id = ?", response.OperationID).Limit(1).Find(&operation)
		return operation, result.RowsAffected == 1, result.Error
	}
	if response.SN > 0 && response.DeviceCode != "" {
		query = query.Where("sn = ? AND device_code = ?", response.SN, response.DeviceCode)
		if response.ChannelCode != "" {
			query = query.Where("channel_code = ?", response.ChannelCode)
		}
	} else if response.CallID != "" && response.CSeq != "" {
		query = query.Where("call_id = ? AND cseq = ?", response.CallID, response.CSeq)
	} else {
		return gbmodels.GbPTZOperation{}, false, nil
	}
	var candidates []gbmodels.GbPTZOperation
	if err := query.Order("id DESC").Limit(2).Find(&candidates).Error; err != nil {
		return gbmodels.GbPTZOperation{}, false, err
	}
	if len(candidates) != 1 {
		return gbmodels.GbPTZOperation{}, false, nil
	}
	return candidates[0], true, nil
}

// ApplyResponse records inbound metadata separately from the first outbound
// attempt and uses a status CAS. Unknown may only converge to a device result.
func (s *Service) ApplyResponse(ctx context.Context, response Response) (gbmodels.GbPTZOperation, bool, error) {
	if s == nil || s.db == nil {
		return gbmodels.GbPTZOperation{}, false, operationError(ErrorCodeHomePositionUnavailable, "PTZ service 未就绪", nil)
	}
	operation, matched, err := s.findResponseOperation(ctx, response)
	if err != nil || !matched {
		return operation, matched, err
	}
	if terminalPTZStatus(operation.Status) {
		s.discardQueryStage(operation.OperationID)
		return operation, true, nil
	}

	status := gbmodels.PTZOperationUnknown
	if strings.EqualFold(strings.TrimSpace(response.DeviceResult), "ERROR") || response.SIPStatus >= 300 {
		status = gbmodels.PTZOperationRejected
	} else if strings.EqualFold(strings.TrimSpace(response.DeviceResult), "OK") {
		status = gbmodels.PTZOperationAccepted
	}
	if operation.Status == gbmodels.PTZOperationUnknown && status == gbmodels.PTZOperationUnknown {
		return operation, true, nil
	}

	completedAt := s.now()
	updates := map[string]interface{}{
		"status": status, "device_result": response.DeviceResult,
		"device_error": response.DeviceError, "completed_at": completedAt, "response_at": completedAt,
	}
	if response.CallID != "" {
		updates["response_call_id"] = response.CallID
	}
	if response.CSeq != "" {
		updates["response_cseq"] = response.CSeq
	}
	allowed := []gbmodels.PTZOperationStatus{gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent}
	if status == gbmodels.PTZOperationAccepted || status == gbmodels.PTZOperationRejected {
		allowed = append(allowed, gbmodels.PTZOperationUnknown)
	}
	if err := schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		update := tx.Model(&gbmodels.GbPTZOperation{}).
			Where("id = ? AND status IN ?", operation.ID, allowed)
		if operation.ResponseRequired {
			update = update.Where(`
				(status = ? AND transport_deadline_at IS NOT NULL AND transport_deadline_at > ?)
				OR (status = ? AND deadline_at IS NOT NULL AND deadline_at > ?)
				OR (status = ? AND attempt = 0 AND queue_deadline_at IS NOT NULL AND queue_deadline_at > ?)
				OR (status = ? AND attempt > 0 AND transport_deadline_at IS NOT NULL AND transport_deadline_at > ?)`,
				gbmodels.PTZOperationUnknown, completedAt,
				gbmodels.PTZOperationSent, completedAt,
				gbmodels.PTZOperationQueued, completedAt,
				gbmodels.PTZOperationQueued, completedAt,
			)
		}
		result := update.Updates(updates)
		return result.Error
	}); err != nil {
		return operation, true, err
	}
	updated, err := s.GetOperation(ctx, operation.OperationID)
	if err == nil && terminalPTZStatus(updated.Status) {
		s.discardQueryStage(updated.OperationID)
	}
	return updated, true, err
}

func (s *Service) MarkTimeout(ctx context.Context, operationID, message string) (gbmodels.GbPTZOperation, error) {
	operation, err := s.GetOperation(ctx, operationID)
	if err != nil {
		return operation, err
	}
	completedAt := s.now()
	if err := s.db.WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
		Where("id = ? AND status IN ?", operation.ID, []gbmodels.PTZOperationStatus{gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent}).
		Updates(map[string]interface{}{"status": gbmodels.PTZOperationTimeout, "error_message": message, "completed_at": completedAt}).Error; err != nil {
		return operation, err
	}
	updated, err := s.GetOperation(ctx, operationID)
	if err == nil && terminalPTZStatus(updated.Status) {
		s.discardQueryStage(updated.OperationID)
	}
	return updated, err
}

func (s *Service) GetOperation(ctx context.Context, operationID string) (gbmodels.GbPTZOperation, error) {
	var operation gbmodels.GbPTZOperation
	if strings.TrimSpace(operationID) == "" {
		return operation, fmt.Errorf("操作编号不能为空")
	}
	result := ptzWriter(s.db).WithContext(ctx).Where("operation_id = ?", operationID).Limit(1).Find(&operation)
	if result.Error != nil {
		return operation, result.Error
	}
	if result.RowsAffected == 0 {
		return operation, gorm.ErrRecordNotFound
	}
	return operation, nil
}
