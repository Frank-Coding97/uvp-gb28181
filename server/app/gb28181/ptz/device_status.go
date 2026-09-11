package ptz

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func (s *Service) applyDeviceStatusResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, body []byte) error {
	targetCode := operationTargetCode(operation)
	status, parseErr := manscdp.ParseDeviceStatusResponseFor(body, manscdp.DeviceStatusExpectation{SN: operation.SN, DeviceID: targetCode})
	if parseErr != nil {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "ERROR", ptzErrorProtocolInvalid, parseErr.Error())
	}
	completedAt := s.now()
	return schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		applied, err := applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
			Status: gbmodels.PTZOperationAccepted, DeviceResult: "OK",
		}, completedAt)
		if err != nil {
			return err
		}
		if !applied {
			return fmt.Errorf("DeviceStatus operation 已完成或已超时")
		}
		return s.persistDeviceStatusWithDB(ctx, tx, operation, status, body)
	})
}

func (s *Service) persistDeviceStatus(ctx context.Context, operation gbmodels.GbPTZOperation, status manscdp.DeviceStatus, body []byte) error {
	return s.persistDeviceStatusWithDB(ctx, s.db, operation, status, body)
}

func (s *Service) persistDeviceStatusWithDB(ctx context.Context, db *gorm.DB, operation gbmodels.GbPTZOperation, status manscdp.DeviceStatus, body []byte) error {
	if operation.ID == 0 || operation.DeviceID == 0 {
		return fmt.Errorf("DeviceStatus operation 缺少可靠序列或设备标识")
	}
	recordState := string(status.Record)
	if recordState == "" {
		recordState = gbmodels.ControlStateUnknown
	}
	if err := s.persistControlStateFact(ctx, db, operation, targetScopeOrChannel(operation), operationTargetCode(operation), operation.ChannelID, map[string]interface{}{
		"record_state": recordState,
	}, body); err != nil {
		return err
	}

	expectedAlarmCode := deviceStatusExpectedAlarmCode(operation.PayloadJSON)
	seenAlarmCodes := make(map[string]struct{}, len(status.AlarmItems))
	for _, item := range status.AlarmItems {
		alarmCode := strings.TrimSpace(item.DeviceID)
		if alarmCode == "" {
			continue
		}
		seenAlarmCodes[alarmCode] = struct{}{}
		if err := s.persistControlStateFact(ctx, db, operation, gbmodels.ControlTargetScopeAlarm, alarmCode, 0, map[string]interface{}{
			"guard_state": deviceStatusGuardState(item.DutyStatus),
		}, body); err != nil {
			return err
		}
	}
	if expectedAlarmCode != "" {
		if _, found := seenAlarmCodes[expectedAlarmCode]; !found {
			if err := s.persistControlStateFact(ctx, db, operation, gbmodels.ControlTargetScopeAlarm, expectedAlarmCode, 0, map[string]interface{}{
				"guard_state": gbmodels.ControlStateUnknown,
			}, body); err != nil {
				return err
			}
		}
	}
	return nil
}

// persistDeviceControlAck records only facts that the standard Result=OK
// response actually confirms. It deliberately shares the same sequence CAS
// as DeviceStatus so a late ACK cannot overwrite a newer fact.
func (s *Service) persistDeviceControlAck(ctx context.Context, db *gorm.DB, operation gbmodels.GbPTZOperation, body []byte) error {
	var scope, targetCode string
	var channelID uint
	var fact map[string]interface{}
	switch operation.Action {
	case "record_start":
		scope, targetCode, channelID = gbmodels.ControlTargetScopeChannel, operationTargetCode(operation), operation.ChannelID
		fact = map[string]interface{}{"record_state": gbmodels.ControlStateOn}
	case "record_stop":
		scope, targetCode, channelID = gbmodels.ControlTargetScopeChannel, operationTargetCode(operation), operation.ChannelID
		fact = map[string]interface{}{"record_state": gbmodels.ControlStateOff}
	case "guard_set":
		scope, targetCode = gbmodels.ControlTargetScopeAlarm, operationTargetCode(operation)
		fact = map[string]interface{}{"guard_state": gbmodels.ControlStateOn}
	case "guard_reset":
		scope, targetCode = gbmodels.ControlTargetScopeAlarm, operationTargetCode(operation)
		fact = map[string]interface{}{"guard_state": gbmodels.ControlStateOff}
	default:
		return nil
	}
	return s.persistControlStateFactFrom(ctx, db, operation, scope, targetCode, channelID, fact, body, "control_ack")
}

func (s *Service) persistControlStateFact(ctx context.Context, db *gorm.DB, operation gbmodels.GbPTZOperation, scope, targetCode string, channelID uint, fact map[string]interface{}, body []byte) error {
	return s.persistControlStateFactFrom(ctx, db, operation, scope, targetCode, channelID, fact, body, "device_status")
}

func (s *Service) persistControlStateFactFrom(ctx context.Context, db *gorm.DB, operation gbmodels.GbPTZOperation, scope, targetCode string, channelID uint, fact map[string]interface{}, body []byte, source string) error {
	scope = strings.TrimSpace(scope)
	targetCode = strings.TrimSpace(targetCode)
	source = strings.TrimSpace(source)
	if scope == "" || targetCode == "" {
		return fmt.Errorf("DeviceStatus fact 目标不完整")
	}
	if source == "" {
		source = "device_status"
	}
	now := s.now()
	values := map[string]interface{}{
		"device_id": operation.DeviceID, "channel_id": channelID,
		"target_scope": scope, "target_code": targetCode,
		"freshness":   gbmodels.ControlStateFresh,
		"observed_at": now, "source": source, "source_sn": operation.SN,
		"source_operation_id": operation.OperationID, "source_operation_seq": operation.ID,
		"raw_summary": summarizePTZBody(body), "updated_at": now,
	}
	for key, value := range fact {
		values[key] = value
	}
	db = db.WithContext(ctx)
	updateNewer := func() (*gorm.DB, error) {
		result := db.Model(&gbmodels.GbDeviceControlState{}).
			Where("device_id = ? AND target_scope = ? AND target_code = ? AND source_operation_seq <= ?",
				operation.DeviceID, scope, targetCode, operation.ID).
			Updates(values)
		return result, result.Error
	}
	if result, err := updateNewer(); err != nil {
		return err
	} else if result.RowsAffected == 1 {
		return nil
	}

	var current gbmodels.GbDeviceControlState
	result := db.Where("device_id = ? AND target_scope = ? AND target_code = ?",
		operation.DeviceID, scope, targetCode).Limit(1).Find(&current)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		if current.SourceOperationSeq > operation.ID {
			return nil
		}
		if retried, err := updateNewer(); err != nil {
			return err
		} else if retried.RowsAffected == 1 {
			return nil
		}
		return fmt.Errorf("DeviceStatus cache CAS 未应用 operation %d", operation.ID)
	}

	values["record_state"] = valueOrDefault(values, "record_state", gbmodels.ControlStateUnknown)
	values["guard_state"] = valueOrDefault(values, "guard_state", gbmodels.ControlStateUnknown)
	values["created_at"] = now
	createErr := db.Model(&gbmodels.GbDeviceControlState{}).Create(values).Error
	if createErr == nil {
		return nil
	}
	// Another response may have inserted the composite key after the lookup.
	// Retry the sequence-guarded update without relying on dialect-specific
	// duplicate-key error inspection.
	if retried, err := updateNewer(); err != nil {
		return err
	} else if retried.RowsAffected == 1 {
		return nil
	}
	result = db.Where("device_id = ? AND target_scope = ? AND target_code = ?",
		operation.DeviceID, scope, targetCode).Limit(1).Find(&current)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 && current.SourceOperationSeq >= operation.ID {
		return nil
	}
	return createErr
}

func valueOrDefault(values map[string]interface{}, key string, fallback interface{}) interface{} {
	if value, ok := values[key]; ok {
		return value
	}
	return fallback
}

func deviceStatusGuardState(status manscdp.DutyStatus) string {
	switch status {
	case manscdp.DutyStatusOnDuty:
		return gbmodels.ControlStateOn
	case manscdp.DutyStatusOffDuty:
		return gbmodels.ControlStateOff
	case manscdp.DutyStatusAlarm:
		return gbmodels.ControlStateAlarm
	default:
		return gbmodels.ControlStateUnknown
	}
}

func deviceStatusExpectedAlarmCode(payloadJSON string) string {
	var payload struct {
		AlarmTargetCode string `json:"alarmTargetCode"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.AlarmTargetCode)
}

func targetScopeOrChannel(operation gbmodels.GbPTZOperation) string {
	if strings.TrimSpace(operation.TargetScope) != "" {
		return strings.TrimSpace(operation.TargetScope)
	}
	return gbmodels.ControlTargetScopeChannel
}
