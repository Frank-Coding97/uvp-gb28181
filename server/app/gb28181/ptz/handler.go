package ptz

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// OnPTZMessage adapts inbound MANSCDP responses to the operation state machine.
func (s *Service) OnPTZMessage(ctx context.Context, deviceCode, callID, cseq string, body []byte) error {
	head, err := manscdp.ParseHead(body)
	if err != nil {
		return err
	}
	switch head.CmdType {
	case manscdp.CmdPresetQuery, manscdp.CmdHomePositionQuery, manscdp.CmdCruiseTrackListQuery, manscdp.CmdCruiseTrackQuery, manscdp.CmdPTZPreciseStatusQuery:
		return s.applyQueryResponse(ctx, deviceCode, callID, cseq, *head, body)
	}
	sn, _ := strconv.Atoi(head.SN)
	result := ""
	if strings.Contains(string(body), "<Result>OK</Result>") {
		result = "OK"
	} else if strings.Contains(string(body), "<Result>ERROR</Result>") {
		result = "ERROR"
	}
	operation, matched, err := s.ApplyResponse(ctx, Response{DeviceCode: deviceCode, ChannelCode: head.DeviceID, SN: sn, CallID: callID, CSeq: cseq, SIPStatus: 200, DeviceResult: result})
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
			DeviceTime: deviceTime, ReceivedAt: s.now(), DedupeKey: callID + ":" + cseq + ":" + strconv.Itoa(notify.SN), RawSummary: string(body)})
		return err
	}()
}
