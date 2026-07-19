package ptz

import (
	"context"
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
	sn, _ := strconv.Atoi(head.SN)
	result := ""
	if strings.Contains(string(body), "<Result>OK</Result>") {
		result = "OK"
	} else if strings.Contains(string(body), "<Result>ERROR</Result>") {
		result = "ERROR"
	}
	_, _, err = s.ApplyResponse(ctx, Response{DeviceCode: deviceCode, ChannelCode: head.DeviceID, SN: sn, CallID: callID, CSeq: cseq, SIPStatus: 200, DeviceResult: result})
	return err
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
