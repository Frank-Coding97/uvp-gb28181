package devicequery

import (
	"context"
	"time"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

// ControlStatusFact exposes device-reported facts only. A nil report remains
// nil; it is never converted to an off state or a zero input count.
type ControlStatusFact struct {
	TargetCode      string     `json:"targetCode"`
	TargetScope     string     `json:"targetScope"`
	RecordState     string     `json:"recordState"`
	GuardState      string     `json:"guardState"`
	OnlineState     *string    `json:"onlineState"`
	SelfTestState   *string    `json:"selfTestState"`
	EncodeState     *string    `json:"encodeState"`
	DeviceTime      *time.Time `json:"deviceTime"`
	AlarmInputCount *int       `json:"alarmInputCount"`
	Freshness       string     `json:"freshness"`
	ObservedAt      *time.Time `json:"observedAt"`
	Source          string     `json:"source"`
}
type ChannelControlStatus struct {
	DeviceID    string              `json:"deviceId"`
	ChannelID   string              `json:"channelId"`
	Record      *ControlStatusFact  `json:"record"`
	AlarmInputs []ControlStatusFact `json:"alarmInputs"`
}

func (s *Service) GetChannelControlStatusInScope(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode string) (ChannelControlStatus, error) {
	device, _, err := s.scopedChannel(ctx, scope, deviceCode, channelCode)
	if err != nil {
		return ChannelControlStatus{}, err
	}
	var rows []gbmodels.GbDeviceControlState
	if err = s.db.WithContext(ctx).Where("device_id = ? AND ((target_scope = ? AND target_code = ?) OR target_scope = ?)", device.ID, gbmodels.ControlTargetScopeChannel, channelCode, gbmodels.ControlTargetScopeAlarm).Order("target_code ASC").Find(&rows).Error; err != nil {
		return ChannelControlStatus{}, err
	}
	result := ChannelControlStatus{DeviceID: deviceCode, ChannelID: channelCode, AlarmInputs: make([]ControlStatusFact, 0)}
	for _, row := range rows {
		freshness := row.Freshness
		var observedAt *time.Time
		if row.ObservedAt.IsZero() {
			freshness = gbmodels.ControlStateUnknownFreshness
		} else {
			observed := row.ObservedAt
			observedAt = &observed
			if row.ObservedAt.Add(cacheFreshFor).Before(s.currentTime()) {
				freshness = gbmodels.ControlStateStale
			}
		}
		fact := ControlStatusFact{TargetCode: row.TargetCode, TargetScope: row.TargetScope, RecordState: string(row.RecordState), GuardState: string(row.GuardState), OnlineState: row.OnlineState, SelfTestState: row.SelfTestState, EncodeState: row.EncodeState, DeviceTime: row.DeviceTime, AlarmInputCount: row.AlarmInputCount, Freshness: freshness, ObservedAt: observedAt, Source: row.Source}
		if row.TargetScope == gbmodels.ControlTargetScopeChannel {
			result.Record = &fact
		} else {
			result.AlarmInputs = append(result.AlarmInputs, fact)
		}
	}
	return result, nil
}
