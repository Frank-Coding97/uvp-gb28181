package ptz

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

const (
	ptzErrorDeviceRejected        = "DEVICE_REJECTED"
	ptzErrorProtocolInvalid       = "PROTOCOL_INVALID_RESPONSE"
	ptzErrorReconcileMismatch     = "HOME_POSITION_RECONCILE_MISMATCH"
	homePositionReconcileAttempts = 3
)

type ptzResponseTransition struct {
	Status          gbmodels.PTZOperationStatus
	DeviceResult    string
	DeviceError     string
	ErrorCode       string
	ErrorMessage    string
	ResponseHasData *bool
}

type homePositionControlPayload struct {
	Enabled   *bool `json:"enabled"`
	ResetTime *int  `json:"resetTime"`
	PresetID  *int  `json:"presetId"`
}

type homePositionControlValue struct {
	Enabled   bool
	ResetTime *int
	PresetID  *int
}

// OnPTZMessage adapts inbound MANSCDP responses to the operation state machine.
func (s *Service) OnPTZMessage(ctx context.Context, deviceCode, callID, cseq string, body []byte) error {
	head, err := manscdp.ParseHead(body)
	if err != nil {
		return err
	}
	operation, matched, candidates, reason, err := s.findPTZMessageOperation(ctx, deviceCode, callID, cseq, *head)
	if err != nil {
		return err
	}
	if !matched {
		logUnmatchedPTZResponse(ctx, deviceCode, callID, cseq, *head, candidates, reason, body)
		return nil
	}
	if operation.CmdType == manscdp.CmdDeviceControl && !operation.ResponseRequired {
		app.Log(ctx).Named("ptz").Warn("GB28181 单向操作收到非预期业务应答，保持 sent",
			zap.String("event", "ptz.response.unexpected"),
			zap.String("operationId", operation.OperationID),
			zap.String("action", operation.Action),
			zap.Int("body_bytes", len(body)),
		)
		return nil
	}
	if reason == "attempt" && (operation.DeviceCode != deviceCode || operationTargetCode(operation) != head.DeviceID ||
		operation.SN != headSN(*head) || operation.CmdType != head.CmdType) {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "", ptzErrorProtocolInvalid,
			"PTZ 应答标识与 operation 不一致")
	}

	switch head.CmdType {
	case manscdp.CmdDeviceStatus:
		return s.applyDeviceStatusResponse(ctx, operation, callID, cseq, body)
	case manscdp.CmdPresetQuery, manscdp.CmdHomePositionQuery, manscdp.CmdCruiseTrackListQuery, manscdp.CmdCruiseTrackQuery, manscdp.CmdPTZPreciseStatusQuery, manscdp.CmdPTZPosition:
		return s.applyQueryResponse(ctx, operation, callID, cseq, *head, body)
	case manscdp.CmdDeviceControl:
		return s.applyDeviceControlResponse(ctx, operation, callID, cseq, body)
	}
	// PTZPreciseCtrl has a different response schema. Keep its legacy result
	// handling isolated from the strict DeviceControl parser.
	sn, _ := strconv.Atoi(head.SN)
	result := ""
	if strings.Contains(string(body), "<Result>OK</Result>") {
		result = "OK"
	} else if strings.Contains(string(body), "<Result>ERROR</Result>") {
		result = "ERROR"
	}
	operation, matched, err = s.ApplyResponse(ctx, Response{OperationID: operation.OperationID, DeviceCode: deviceCode, ChannelCode: head.DeviceID, SN: sn, CallID: callID, CSeq: cseq, SIPStatus: 200, DeviceResult: result})
	if err != nil || !matched {
		return err
	}
	// 设备回 accepted 后再同步一次预置位状态,作为 Execute 时乐观入库的对账(幂等)。
	// 设备明确回错的情况下不推翻本地记录,保留 active/deleted 标记等运营处理。
	if operation.Status != gbmodels.PTZOperationAccepted {
		return nil
	}
	return s.SyncPresetOperation(ctx, operation)
}

func (s *Service) findPTZMessageOperation(ctx context.Context, deviceCode, callID, cseq string, head manscdp.MessageHead) (gbmodels.GbPTZOperation, bool, []string, string, error) {
	db := ptzWriter(s.db).WithContext(ctx)
	if strings.TrimSpace(callID) != "" && strings.TrimSpace(cseq) != "" {
		var operationIDs []uint
		if err := db.Model(&gbmodels.GbPTZOperationAttempt{}).
			Distinct("operation_id").
			Where("call_id = ? AND cseq = ?", callID, cseq).
			Limit(2).Pluck("operation_id", &operationIDs).Error; err != nil {
			return gbmodels.GbPTZOperation{}, false, nil, "attempt_lookup_failed", err
		}
		if len(operationIDs) > 0 {
			if len(operationIDs) > 1 {
				return gbmodels.GbPTZOperation{}, false, nil, "ambiguous_attempt", nil
			}
			var candidates []gbmodels.GbPTZOperation
			if err := db.Where("id IN ? AND status IN ?", operationIDs, []gbmodels.PTZOperationStatus{
				gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent, gbmodels.PTZOperationUnknown,
			}).Order("id DESC").Limit(2).Find(&candidates).Error; err != nil {
				return gbmodels.GbPTZOperation{}, false, nil, "attempt_operation_lookup_failed", err
			}
			candidateIDs := ptzOperationIDs(candidates)
			if len(candidates) > 1 {
				return gbmodels.GbPTZOperation{}, false, candidateIDs, "ambiguous_attempt", nil
			}
			if len(candidates) == 1 {
				return candidates[0], true, candidateIDs, "attempt", nil
			}
			return gbmodels.GbPTZOperation{}, false, nil, "attempt_terminal", nil
		}
	}

	sn := headSN(head)
	if strings.TrimSpace(deviceCode) == "" || strings.TrimSpace(head.DeviceID) == "" || sn <= 0 || strings.TrimSpace(head.CmdType) == "" {
		return gbmodels.GbPTZOperation{}, false, nil, "invalid_correlation_key", nil
	}
	var candidates []gbmodels.GbPTZOperation
	query := db.Where("device_code = ? AND (target_code = ? OR (target_code IS NULL AND channel_code = ?)) AND sn = ? AND cmd_type = ?", deviceCode, head.DeviceID, head.DeviceID, sn, head.CmdType)
	if head.CmdType == manscdp.CmdDeviceStatus || head.CmdType == manscdp.CmdPresetQuery ||
		head.CmdType == manscdp.CmdHomePositionQuery || head.CmdType == manscdp.CmdCruiseTrackListQuery ||
		head.CmdType == manscdp.CmdCruiseTrackQuery || head.CmdType == manscdp.CmdDeviceControl ||
		head.CmdType == manscdp.CmdPTZPreciseStatusQuery || head.CmdType == manscdp.CmdPTZPosition {
		query = query.Where("status IN ?", []gbmodels.PTZOperationStatus{
			gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent, gbmodels.PTZOperationUnknown,
		})
	}
	if err := query.Order("id DESC").Limit(2).Find(&candidates).Error; err != nil {
		return gbmodels.GbPTZOperation{}, false, nil, "candidate_lookup_failed", err
	}
	candidateIDs := ptzOperationIDs(candidates)
	if len(candidates) == 0 {
		return gbmodels.GbPTZOperation{}, false, candidateIDs, "no_candidate", nil
	}
	if len(candidates) != 1 {
		return gbmodels.GbPTZOperation{}, false, candidateIDs, "ambiguous_candidate", nil
	}
	return candidates[0], true, candidateIDs, "protocol_key", nil
}

func ptzOperationIDs(operations []gbmodels.GbPTZOperation) []string {
	ids := make([]string, 0, len(operations))
	for _, operation := range operations {
		ids = append(ids, operation.OperationID)
	}
	return ids
}

func operationTargetCode(operation gbmodels.GbPTZOperation) string {
	if value := strings.TrimSpace(operation.TargetCode); value != "" {
		return value
	}
	return strings.TrimSpace(operation.ChannelCode)
}

func operationProtocolProfile(operation gbmodels.GbPTZOperation) protocol.Profile {
	profile := protocol.ProfileFor(protocol.Version2016)
	if strings.TrimSpace(operation.ProfileVersion) == string(protocol.Version2022) {
		profile = protocol.ProfileFor(protocol.Version2022)
	}
	charset := protocol.Charset(strings.TrimSpace(operation.ProfileCharset))
	if protocol.IsSupportedCharset(charset) {
		profile.Charset = charset
	}
	return profile
}

func logUnmatchedPTZResponse(ctx context.Context, deviceCode, callID, cseq string, head manscdp.MessageHead, candidateIDs []string, reason string, body []byte) {
	app.Log(ctx).Named("ptz").Warn("GB28181 PTZ 应答无法唯一关联",
		zap.String("event", "ptz.response.unmatched"),
		zap.String("deviceCode", deviceCode),
		zap.String("channelCode", head.DeviceID),
		zap.Int("sn", headSN(head)),
		zap.String("cmdType", head.CmdType),
		zap.String("responseCallId", callID),
		zap.String("responseCseq", cseq),
		zap.Strings("candidateOperationIds", candidateIDs),
		zap.String("reason", reason),
		zap.Int("body_bytes", len(body)),
	)
}

func logIgnoredPTZResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, head manscdp.MessageHead, body []byte) {
	app.Log(ctx).Named("ptz").Warn("GB28181 PTZ 应答已关联但 operation 未推进，忽略事实写入",
		zap.String("event", "ptz.response.ignored"),
		zap.String("operationId", operation.OperationID),
		zap.String("cmdType", head.CmdType),
		zap.String("responseCallId", callID),
		zap.String("responseCseq", cseq),
		zap.String("status", string(operation.Status)),
		zap.Int("body_bytes", len(body)),
	)
}

func ptzResponseOperationUpdate(tx *gorm.DB, operation gbmodels.GbPTZOperation, observedAt time.Time) *gorm.DB {
	update := tx.Model(&gbmodels.GbPTZOperation{}).
		Where("id = ? AND status IN ?", operation.ID, []gbmodels.PTZOperationStatus{
			gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent, gbmodels.PTZOperationUnknown,
		})
	if operation.ResponseRequired {
		update = update.Where(`
			(status = ? AND transport_deadline_at IS NOT NULL AND transport_deadline_at > ?)
			OR (status = ? AND deadline_at IS NOT NULL AND deadline_at > ?)
			OR (status = ? AND attempt = 0 AND queue_deadline_at IS NOT NULL AND queue_deadline_at > ?)
			OR (status = ? AND attempt > 0 AND transport_deadline_at IS NOT NULL AND transport_deadline_at > ?)`,
			gbmodels.PTZOperationUnknown, observedAt,
			gbmodels.PTZOperationSent, observedAt,
			gbmodels.PTZOperationQueued, observedAt,
			gbmodels.PTZOperationQueued, observedAt,
		)
	}
	return update
}

// applyPTZResponseObservation records a valid response while keeping the
// operation active. This is used for paginated query responses: each page is
// a state-guarded observation, and the final page performs the terminal CAS.
func applyPTZResponseObservation(tx *gorm.DB, operation gbmodels.GbPTZOperation, callID, cseq string, observedAt time.Time) (bool, error) {
	updates := map[string]interface{}{
		"response_at": observedAt, "next_attempt_at": nil,
	}
	if callID != "" {
		updates["response_call_id"] = callID
	}
	if cseq != "" {
		updates["response_cseq"] = cseq
	}
	result := ptzResponseOperationUpdate(tx, operation, observedAt).Updates(updates)
	return result.RowsAffected == 1, result.Error
}

func applyPTZResponseTransition(tx *gorm.DB, operation gbmodels.GbPTZOperation, callID, cseq string, transition ptzResponseTransition, completedAt time.Time) (bool, error) {
	if transition.Status != gbmodels.PTZOperationAccepted && transition.Status != gbmodels.PTZOperationRejected {
		return false, fmt.Errorf("不支持的 PTZ 应答终态: %s", transition.Status)
	}
	updates := map[string]interface{}{
		"status": transition.Status, "device_result": transition.DeviceResult,
		"device_error": transition.DeviceError, "error_code": transition.ErrorCode,
		"error_message": transition.ErrorMessage, "completed_at": completedAt,
		"response_at": completedAt, "next_attempt_at": nil,
	}
	if callID != "" {
		updates["response_call_id"] = callID
	}
	if cseq != "" {
		updates["response_cseq"] = cseq
	}
	if transition.ResponseHasData != nil {
		updates["response_has_data"] = *transition.ResponseHasData
	}

	result := ptzResponseOperationUpdate(tx, operation, completedAt).Updates(updates)
	return result.RowsAffected == 1, result.Error
}

func (s *Service) applyDeviceControlResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, body []byte) error {
	response, parseErr := manscdp.ParseDeviceControlResponseWithProfile(operationProtocolProfile(operation), body)
	if parseErr != nil || response.SN != operation.SN || response.DeviceID != operationTargetCode(operation) {
		if parseErr == nil {
			parseErr = fmt.Errorf("DeviceControl 应答标识与 operation 不一致")
		}
		app.Log(ctx).Named("ptz").Warn("GB28181 DeviceControl 应答协议非法",
			zap.String("event", "ptz.response.protocol_invalid"),
			zap.String("operationId", operation.OperationID),
			zap.Int("body_bytes", len(body)),
			zap.Error(parseErr),
		)
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "", ptzErrorProtocolInvalid, parseErr.Error())
	}
	if response.Result == manscdp.DeviceControlResultError {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, string(response.Result), ptzErrorDeviceRejected, "设备返回 Result=ERROR")
	}
	if operation.Action == "home_position" {
		return s.applyAcceptedHomePositionControl(ctx, operation, callID, cseq, body)
	}

	applied := false
	completedAt := s.now()
	if err := schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		applied = false
		var err error
		applied, err = applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
			Status: gbmodels.PTZOperationAccepted, DeviceResult: string(response.Result),
		}, completedAt)
		if err == nil && applied {
			err = s.persistDeviceControlAck(ctx, tx, operation, body)
		}
		return err
	}); err != nil {
		return err
	}
	if applied {
		return s.SyncPresetOperation(ctx, operation)
	}
	return nil
}

func (s *Service) applyRejectedPTZResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq, deviceResult, errorCode, message string) error {
	completedAt := s.now()
	applied := false
	err := schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		var err error
		applied, err = applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
			Status: gbmodels.PTZOperationRejected, DeviceResult: deviceResult,
			DeviceError: message, ErrorCode: errorCode, ErrorMessage: message,
		}, completedAt)
		return err
	})
	if err == nil && applied {
		s.discardQueryStage(operation.OperationID)
	}
	return err
}

func (s *Service) applyAcceptedHomePositionControl(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, body []byte) error {
	payload, err := decodeHomePositionControlPayload(operation.PayloadJSON)
	if err != nil {
		return err
	}
	entry := s.lockChannel(operation.ChannelID)
	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		s.unlockChannel(operation.ChannelID, entry)
	}()

	completedAt := s.now()
	rawSummary := summarizePTZBody(body)
	return schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		applied, err := applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
			Status: gbmodels.PTZOperationAccepted, DeviceResult: string(manscdp.DeviceControlResultOK),
		}, completedAt)
		if err != nil || !applied {
			return err
		}
		if _, _, err := s.applyHomePositionDB(tx, HomePositionUpdate{
			DeviceID: operation.DeviceID, ChannelID: operation.ChannelID, ChannelCode: operation.ChannelCode,
			Enabled: payload.Enabled, ResetTime: payload.ResetTime, PresetID: payload.PresetID,
			EnabledEncoding: gbmodels.PTZHomePositionEnabledNumeric, ConfirmedAt: completedAt,
			Source: gbmodels.PTZHomePositionSourceControlACK, Verification: gbmodels.PTZHomePositionVerificationUnverified,
			SourceSN: operation.SN, SourceOperationID: operation.OperationID, SourceOperationSeq: operation.ID,
			RawSummary: rawSummary,
		}); err != nil {
			return err
		}
		allowed, err := automaticHomePositionReconcileAllowed(tx, operation.ChannelID)
		if err != nil || !allowed {
			return err
		}
		return s.createHomePositionReconcile(tx, operation, completedAt)
	})
}
func decodeHomePositionControlPayload(payloadJSON string) (homePositionControlValue, error) {
	var payload homePositionControlPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return homePositionControlValue{}, fmt.Errorf("解析看守位控制 operation 参数失败: %w", err)
	}
	if payload.Enabled == nil {
		return homePositionControlValue{}, fmt.Errorf("看守位控制 operation 缺少 enabled")
	}
	decoded := homePositionControlValue{Enabled: *payload.Enabled, ResetTime: payload.ResetTime, PresetID: payload.PresetID}
	if !decoded.Enabled {
		decoded.ResetTime = nil
		decoded.PresetID = nil
	}
	return decoded, nil
}

func automaticHomePositionReconcileAllowed(tx *gorm.DB, channelID uint) (bool, error) {
	var channel gbmodels.GbChannel
	result := tx.Select("id", "capabilities").Where("id = ?", channelID).Limit(1).Find(&channel)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 || channel.Capabilities == nil {
		return true, nil
	}
	profile, reason := parseHomePositionProfile(channel.Capabilities)
	capability, declared := profileHomePositionCapability(profile, reason, "home_position_query", "homePositionQuery")
	return !declared || capability.Status != HomePositionCapabilityUnsupported, nil
}

func (s *Service) createHomePositionReconcile(tx *gorm.DB, parent gbmodels.GbPTZOperation, createdAt time.Time) error {
	payloadJSON, err := canonicalPayload(map[string]interface{}{"triggerOperationId": parent.OperationID})
	if err != nil {
		return err
	}
	operationID := uuid.NewString()
	values := map[string]interface{}{
		"operation_id": operationID, "idempotency_key": "home-reconcile:" + parent.OperationID,
		"device_id": parent.DeviceID, "device_code": parent.DeviceCode,
		"channel_id": parent.ChannelID, "channel_code": parent.ChannelCode,
		"cmd_type": manscdp.CmdHomePositionQuery, "action": "refresh_home_position",
		"profile_version": parent.ProfileVersion, "profile_charset": parent.ProfileCharset,
		"target_scope": parent.TargetScope, "target_code": parent.TargetCode,
		"payload_json": payloadJSON, "sn": s.nextSN(), "status": gbmodels.PTZOperationQueued,
		"attempt": 0, "response_required": true, "max_attempts": homePositionReconcileAttempts,
		"actor_id": parent.ActorID, "actor_dept_id": parent.ActorDeptID,
		"trigger_operation_id": parent.OperationID,
		"queue_deadline_at":    createdAt.Add(5 * time.Second), "created_at": createdAt,
	}
	if err := tx.Model(&gbmodels.GbPTZOperation{}).Create(values).Error; err != nil {
		return err
	}
	result := tx.Model(&gbmodels.GbPTZOperation{}).
		Where("id = ? AND status = ? AND reconcile_operation_id IS NULL", parent.ID, gbmodels.PTZOperationAccepted).
		Update("reconcile_operation_id", operationID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("关联看守位自动对账 operation 失败")
	}
	return nil
}

// SyncPresetOperation 把一次预置位设/删操作的效果写入 gb_ptz_preset 表。
// 与 op.Status 无关:Execute 在 SIP 200 后乐观调用,ApplyResponse 在 accepted 后再校准一次。
// GB/T 28181 里 preset_set 属于单向控制,大量设备不回 Response,依赖 accepted 会永远等不到。
func (s *Service) SyncPresetOperation(ctx context.Context, operation gbmodels.GbPTZOperation) error {
	if operation.Action != string(manscdp.PTZActionSetPreset) && operation.Action != string(manscdp.PTZActionDeletePreset) {
		return nil
	}
	var payload struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil || payload.ID <= 0 {
		return fmt.Errorf("解析预置位操作参数失败")
	}
	status := gbmodels.PTZPresetActive
	if operation.Action == string(manscdp.PTZActionDeletePreset) {
		status = gbmodels.PTZPresetDeleted
	}
	preset := gbmodels.GbPTZPreset{
		DeviceID: operation.DeviceID, ChannelID: operation.ChannelID, PresetID: payload.ID,
		Name: payload.Name, Status: status, LastOperationID: operation.OperationID,
	}
	var existing gbmodels.GbPTZPreset
	result := s.db.WithContext(ctx).Where("channel_id = ? AND preset_id = ?", operation.ChannelID, payload.ID).Limit(1).Find(&existing)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return s.db.WithContext(ctx).Create(&preset).Error
	}
	updates := map[string]interface{}{"status": status, "last_operation_id": operation.OperationID, "updated_at": s.now()}
	if payload.Name != "" {
		updates["name"] = payload.Name
	}
	return s.db.WithContext(ctx).Model(&existing).Updates(updates).Error
}

// OnPTZNotify resolves a channel code to platform IDs and persists a precise
// state notification. Unknown channels are rejected without creating assets.
func (s *Service) OnPTZNotify(ctx context.Context, deviceCode, callID, cseq string, body []byte) error {
	notify, err := manscdp.ParsePTZPrecisePositionNotify(body)
	if err != nil {
		return err
	}
	var device gbmodels.GbDevice
	if result := s.db.WithContext(ctx).Where("device_id = ?", deviceCode).Limit(1).Find(&device); result.Error != nil {
		return result.Error
	} else if result.RowsAffected == 0 {
		return fmt.Errorf("PTZ 通知设备不存在")
	}
	var channel gbmodels.GbChannel
	if result := s.db.WithContext(ctx).Where("device_id = ? AND channel_id = ?", deviceCode, notify.DeviceID).Limit(1).Find(&channel); result.Error != nil {
		return result.Error
	} else if result.RowsAffected == 0 {
		return fmt.Errorf("PTZ 通知通道不存在")
	}
	var deviceTime *time.Time
	if strings.TrimSpace(notify.Time) != "" {
		parsed, parseErr := time.Parse(time.RFC3339, notify.Time)
		if parseErr != nil {
			parsed, parseErr = time.Parse("2006-01-02T15:04:05", notify.Time)
		}
		if parseErr == nil {
			deviceTime = &parsed
		}
	}
	return func() error {
		_, err := s.ApplyPreciseNotify(ctx, PreciseNotify{DeviceID: device.ID, DeviceCode: deviceCode, ChannelID: channel.ID, ChannelCode: notify.DeviceID,
			SN: notify.SN, Pan: notify.Pan, Tilt: notify.Tilt, Zoom: notify.Zoom, Focus: notify.Focus, Iris: notify.Iris,
			DeviceTime: deviceTime, ReceivedAt: s.now(), DedupeKey: callID + ":" + cseq + ":" + strconv.Itoa(notify.SN), RawSummary: summarizePTZBody(body)})
		return err
	}()
}
