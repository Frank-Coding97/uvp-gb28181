package catalog

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type AlarmTargetStatus string

const (
	AlarmTargetResolved    AlarmTargetStatus = "resolved"
	AlarmTargetAmbiguous   AlarmTargetStatus = "ambiguous"
	AlarmTargetUnavailable AlarmTargetStatus = "unavailable"
)

type AlarmTargetSource string

const (
	AlarmTargetSourceManual       AlarmTargetSource = "manual"
	AlarmTargetSourceDirectParent AlarmTargetSource = "direct_parent"
	AlarmTargetSourceUniqueDevice AlarmTargetSource = "unique_device"
)

type AlarmTargetResolution struct {
	Status     AlarmTargetStatus          `json:"status"`
	Source     AlarmTargetSource          `json:"source,omitempty"`
	Target     *gbmodels.GbAlarmResource  `json:"target,omitempty"`
	Candidates []gbmodels.GbAlarmResource `json:"candidates,omitempty"`
}

// ResolveAlarmTarget maps a playback channel to a concrete 134 alarm input.
// Catalog does not guarantee a one-to-one video/alarm relationship, so every
// non-unique result is explicit and callers must not pick a candidate silently.
func ResolveAlarmTarget(ctx context.Context, db *gorm.DB, deviceID uint, deviceCode, channelCode string) (AlarmTargetResolution, error) {
	if db == nil {
		return AlarmTargetResolution{}, fmt.Errorf("catalog: alarm target database is nil")
	}
	deviceCode = strings.TrimSpace(deviceCode)
	channelCode = strings.TrimSpace(channelCode)
	if deviceID == 0 && deviceCode == "" {
		return AlarmTargetResolution{}, fmt.Errorf("catalog: alarm target device is empty")
	}
	if channelCode == "" {
		return AlarmTargetResolution{}, fmt.Errorf("catalog: alarm target channel is empty")
	}

	if deviceID != 0 {
		var manuallyBound []gbmodels.GbAlarmResource
		query := alarmInputQuery(db.WithContext(ctx), deviceID, deviceCode).
			Joins("JOIN gb_alarm_binding binding ON binding.alarm_resource_id = gb_alarm_resource.id").
			Where("binding.device_id = ? AND binding.channel_code = ?", deviceID, channelCode)
		if err := query.Order("gb_alarm_resource.alarm_code").Find(&manuallyBound).Error; err != nil {
			return AlarmTargetResolution{}, err
		}
		if result, ok := resolvedAlarmCandidates(manuallyBound, AlarmTargetSourceManual); ok {
			return result, nil
		}
	}

	var direct []gbmodels.GbAlarmResource
	query := alarmInputQuery(db.WithContext(ctx), deviceID, deviceCode).
		Joins("JOIN gb_alarm_resource_parent parent ON parent.alarm_resource_id = gb_alarm_resource.id").
		Where("parent.parent_code = ?", channelCode)
	if err := query.Order("gb_alarm_resource.alarm_code").Find(&direct).Error; err != nil {
		return AlarmTargetResolution{}, err
	}
	if len(direct) > 0 {
		result, _ := resolvedAlarmCandidates(direct, AlarmTargetSourceDirectParent)
		return result, nil
	}

	var candidates []gbmodels.GbAlarmResource
	if err := alarmInputQuery(db.WithContext(ctx), deviceID, deviceCode).
		Order("alarm_code").Find(&candidates).Error; err != nil {
		return AlarmTargetResolution{}, err
	}
	if len(candidates) == 0 {
		return AlarmTargetResolution{Status: AlarmTargetUnavailable}, nil
	}
	result, _ := resolvedAlarmCandidates(candidates, AlarmTargetSourceUniqueDevice)
	return result, nil
}

func alarmInputQuery(db *gorm.DB, deviceID uint, deviceCode string) *gorm.DB {
	query := db.Model(&gbmodels.GbAlarmResource{}).
		Where("gb_alarm_resource.resource_type = ?", gbmodels.AlarmResourceInput)
	switch {
	case deviceID != 0:
		return query.Where("gb_alarm_resource.device_id = ?", deviceID)
	default:
		return query.Where("gb_alarm_resource.device_code = ?", deviceCode)
	}
}

func resolvedAlarmCandidates(candidates []gbmodels.GbAlarmResource, source AlarmTargetSource) (AlarmTargetResolution, bool) {
	if len(candidates) == 0 {
		return AlarmTargetResolution{}, false
	}
	if len(candidates) > 1 {
		return AlarmTargetResolution{Status: AlarmTargetAmbiguous, Source: source, Candidates: candidates}, true
	}
	target := candidates[0]
	return AlarmTargetResolution{Status: AlarmTargetResolved, Source: source, Target: &target, Candidates: candidates}, true
}
