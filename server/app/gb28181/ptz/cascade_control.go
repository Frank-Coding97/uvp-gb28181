package ptz

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

type CascadeDeviceControlAuthorization struct {
	PlatformID         uint64 `json:"platformId"`
	ConfigRevision     uint64 `json:"configRevision"`
	ProjectionRevision uint64 `json:"projectionRevision"`
	PublishedID        string `json:"publishedId"`
	Action             string `json:"action"`
}

type cascadeAdvancedControlPayload struct {
	Action        string                            `json:"action"`
	TargetCode    string                            `json:"targetCode"`
	AlarmMethod   string                            `json:"alarmMethod,omitempty"`
	AlarmType     string                            `json:"alarmType,omitempty"`
	StreamNumber  *int                              `json:"streamNumber,omitempty"`
	Authorization CascadeDeviceControlAuthorization `json:"authorization"`
}

func (s *Service) AdvancedControlForCascade(ctx context.Context, target Target, action, targetCode string, alarm manscdp.AlarmResetOptions, streamNumber *int, authorization CascadeDeviceControlAuthorization, key string) ([]byte, error) {
	if !s.RequiresDeviceAuthorization() || target.DeviceEpoch <= 0 || key == "" {
		return nil, fmt.Errorf("authorized device-control service unavailable")
	}
	if targetCode == "" {
		targetCode = target.ChannelCode
	}
	payload := cascadeAdvancedControlPayload{Action: action, TargetCode: targetCode, AlarmMethod: alarm.AlarmMethod, AlarmType: alarm.AlarmType, StreamNumber: streamNumber, Authorization: authorization}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	operation := gbmodels.GbPTZOperation{DeviceID: target.DeviceID, DeviceCode: target.DeviceCode, ChannelID: target.ChannelID, ChannelCode: target.ChannelCode, TargetCode: targetCode, Action: action, IdempotencyKey: key, PayloadJSON: string(raw)}
	if err := s.validateCascadeAdvancedControlAuthorization(ctx, operation); err != nil {
		return nil, err
	}
	identity := fmt.Sprintf("advanced-control:%x", sha256.Sum256(raw))
	targetScope := gbmodels.ControlTargetScopeChannel
	if action == "guard_set" || action == "guard_reset" || action == "alarm_reset" {
		targetScope = gbmodels.ControlTargetScopeAlarm
	}
	pages, err := s.queryForCascade(ctx, target, identity, key, func() (gbmodels.GbPTZOperation, error) {
		return s.Execute(ctx, target, Command{CmdType: manscdp.CmdDeviceControl, Action: action, IdempotencyKey: key, Payload: map[string]interface{}{"action": action, "targetCode": targetCode, "alarmMethod": alarm.AlarmMethod, "alarmType": alarm.AlarmType, "streamNumber": streamNumber, "authorization": authorization}, ResponseRequired: true, MaxAttempts: 1, TargetScope: targetScope, TargetCode: targetCode, Profile: target.Profile, Build: func(sn int) ([]byte, error) {
			return buildCascadeAdvancedControl(target.Profile, targetCode, sn, payload)
		}})
	})
	if err != nil {
		return nil, err
	}
	if len(pages) != 1 {
		return nil, fmt.Errorf("advanced control requires one application result")
	}
	return pages[0], nil
}

func buildCascadeAdvancedControl(profile protocol.Profile, targetCode string, sn int, payload cascadeAdvancedControlPayload) ([]byte, error) {
	switch payload.Action {
	case "record_start":
		return manscdp.BuildRecordControlForStreamWithProfile(profile, targetCode, sn, manscdp.RecordStart, payload.StreamNumber)
	case "record_stop":
		return manscdp.BuildRecordControlForStreamWithProfile(profile, targetCode, sn, manscdp.RecordStop, payload.StreamNumber)
	case "guard_set":
		return manscdp.BuildGuardControlWithProfile(profile, targetCode, sn, manscdp.GuardSet)
	case "guard_reset":
		return manscdp.BuildGuardControlWithProfile(profile, targetCode, sn, manscdp.GuardReset)
	case "alarm_reset":
		return manscdp.BuildAlarmResetControlWithProfile(profile, targetCode, sn, manscdp.AlarmResetOptions{AlarmMethod: payload.AlarmMethod, AlarmType: payload.AlarmType})
	default:
		return nil, fmt.Errorf("unsupported cascaded device control action %q", payload.Action)
	}
}

func buildCascadeAdvancedControlOperation(operation gbmodels.GbPTZOperation) ([]byte, error) {
	var payload cascadeAdvancedControlPayload
	if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
		return nil, err
	}
	return buildCascadeAdvancedControl(operationProtocolProfile(operation), payload.TargetCode, operation.SN, payload)
}

func (s *Service) validateCascadeAdvancedControlAuthorization(ctx context.Context, operation gbmodels.GbPTZOperation) error {
	if operation.ID != 0 {
		epoch := int64(0)
		if operation.DeviceEpoch != nil {
			epoch = *operation.DeviceEpoch
		}
		key := cascadeQueryWaiterKey(operation.DeviceID, operation.ChannelID, operation.DeviceCode, operation.ChannelCode, epoch, operation.IdempotencyKey)
		s.cascadeQueryMu.Lock()
		waiter := s.cascadeQueries[key]
		pending := waiter != nil && waiter.refs > 0 && !waiter.complete && waiter.err == nil
		s.cascadeQueryMu.Unlock()
		if !pending {
			return fmt.Errorf("cascaded control request no longer pending")
		}
	}
	var payload cascadeAdvancedControlPayload
	if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
		return err
	}
	a := payload.Authorization
	if a.PlatformID == 0 || a.ConfigRevision == 0 || a.ProjectionRevision == 0 || a.PublishedID == "" || a.Action != payload.Action {
		return fmt.Errorf("cascaded control authorization missing")
	}
	var count int64
	err := ptzWriter(s.db).WithContext(ctx).Table("gb_cascade_platform AS platform").Joins("JOIN gb_cascade_channel_projection AS channel ON channel.platform_id=platform.id").Joins("JOIN gb_cascade_device_projection AS device ON device.id=channel.device_projection_id AND device.platform_id=platform.id").Joins("JOIN gb_device AS source_device ON source_device.id=device.source_device_id").Joins("JOIN gb_channel AS source_channel ON source_channel.id=channel.source_channel_id AND source_channel.device_id=source_device.device_id AND source_channel.owner_dept_id=source_device.owner_dept_id").Where("platform.id=? AND platform.enabled=? AND platform.device_control_enabled=? AND platform.deleted_at IS NULL AND platform.config_revision=? AND platform.projection_revision=?", a.PlatformID, true, true, a.ConfigRevision, a.ProjectionRevision).Where("channel.active=? AND device.active=? AND channel.published_channel_id=? AND device.source_device_id=? AND channel.source_channel_id=?", true, true, a.PublishedID, operation.DeviceID, operation.ChannelID).Where("source_device.deleted_at IS NULL AND source_channel.deleted_at IS NULL AND source_device.device_id=? AND source_channel.channel_id=?", operation.DeviceCode, operation.ChannelCode).Count(&count).Error
	if err != nil {
		return fmt.Errorf("check cascaded control publication: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("cascaded control publication authorization revoked")
	}
	return nil
}

func isCascadeAdvancedControlOperation(operation gbmodels.GbPTZOperation) bool {
	var payload cascadeAdvancedControlPayload
	if json.Unmarshal([]byte(operation.PayloadJSON), &payload) != nil {
		return false
	}
	return payload.Authorization.PlatformID != 0 && payload.Authorization.Action == operation.Action && payload.TargetCode != ""
}
