package controllers

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
)

const deviceStatusFreshFor = ptzCacheFreshFor

// GetDeviceStatus exposes two independent standard facts: Record belongs to
// the playback channel, while DutyStatus belongs to a concrete 134 alarm
// input returned by the parent device. Catalog ambiguity is returned rather
// than guessed.
func (dc *DeviceMgmtController) GetDeviceStatus(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return
	}

	resolution, err := catalog.ResolveAlarmTarget(c.Request.Context(), dc.db(), target.DeviceID, target.DeviceCode, channel.ChannelID)
	if err != nil {
		dc.FailAndAbort(c, "解析报警设备目标失败", err)
		return
	}
	recordFact, err := dc.loadDeviceControlFact(c, target.DeviceID, gbmodels.ControlTargetScopeChannel, channel.ChannelID)
	if err != nil {
		dc.FailAndAbort(c, "查询录像状态失败", err)
		return
	}
	alarmFacts, err := dc.loadAlarmControlFacts(c, target.DeviceID)
	if err != nil {
		dc.FailAndAbort(c, "查询报警设备状态失败", err)
		return
	}
	alarmFact := deviceControlFact{Freshness: gbmodels.ControlStateUnknownFreshness}
	if resolution.Status == catalog.AlarmTargetResolved && resolution.Target != nil {
		for _, fact := range alarmFacts {
			if fact.State.TargetCode == resolution.Target.AlarmCode {
				alarmFact = fact
				break
			}
		}
	}

	data := deviceStatusResponse(channel.ChannelID, resolution, recordFact, alarmFact, alarmFacts)
	if c.Query("refresh") == "true" {
		service := dc.ptzServiceSnapshot()
		if service == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "PTZ Service 未就绪"})
			return
		}
		baseKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		recordOperation, recordErr := service.Execute(c.Request.Context(), target, ptz.Command{
			CmdType: manscdp.CmdDeviceStatus, Action: "device_status_record", IdempotencyKey: scopedDeviceStatusKey(baseKey, "record"),
			Profile: target.Profile, TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: channel.ChannelID,
			ResponseRequired: true, MaxAttempts: 3, Payload: map[string]interface{}{"targetCode": channel.ChannelID, "fact": "record"},
			Build: func(sn int) ([]byte, error) {
				return manscdp.BuildDeviceStatusQueryWithProfile(target.Profile, channel.ChannelID, sn)
			},
		})
		if recordErr == nil {
			data["refreshOperationId"] = recordOperation.OperationID
			data["recordRefreshOperationId"] = recordOperation.OperationID
		}

		alarmPayload := map[string]interface{}{"targetCode": target.DeviceCode, "fact": "alarm"}
		if resolution.Status == catalog.AlarmTargetResolved && resolution.Target != nil {
			alarmPayload["alarmTargetCode"] = resolution.Target.AlarmCode
		}
		alarmOperation, alarmErr := service.Execute(c.Request.Context(), target, ptz.Command{
			CmdType: manscdp.CmdDeviceStatus, Action: "device_status_alarm", IdempotencyKey: scopedDeviceStatusKey(baseKey, "alarm"),
			Profile: target.Profile, TargetScope: gbmodels.ControlTargetScopeDevice, TargetCode: target.DeviceCode,
			ResponseRequired: true, MaxAttempts: 3, Payload: alarmPayload,
			Build: func(sn int) ([]byte, error) {
				return manscdp.BuildDeviceStatusQueryWithProfile(target.Profile, target.DeviceCode, sn)
			},
		})
		if alarmErr == nil {
			data["alarmRefreshOperationId"] = alarmOperation.OperationID
		}
		data["refreshOperationIds"] = gin.H{
			"record": data["recordRefreshOperationId"],
			"alarm":  data["alarmRefreshOperationId"],
		}
		if recordErr != nil || alarmErr != nil {
			data["refreshError"] = deviceStatusRefreshError(recordErr, alarmErr)
		}
	}
	dc.Success(c, data)
}

type deviceControlFact struct {
	Found     bool
	State     gbmodels.GbDeviceControlState
	Freshness string
}

func (dc *DeviceMgmtController) loadDeviceControlFact(c *gin.Context, deviceID uint, scope, code string) (deviceControlFact, error) {
	var state gbmodels.GbDeviceControlState
	result := dc.db().WithContext(c.Request.Context()).
		Where("device_id = ? AND target_scope = ? AND target_code = ?", deviceID, scope, code).
		Limit(1).Find(&state)
	if result.Error != nil {
		return deviceControlFact{}, result.Error
	}
	if result.RowsAffected == 0 {
		return deviceControlFact{Freshness: gbmodels.ControlStateUnknownFreshness}, nil
	}
	freshness := state.Freshness
	if state.ObservedAt.IsZero() {
		freshness = gbmodels.ControlStateUnknownFreshness
	} else if state.ObservedAt.Add(deviceStatusFreshFor).Before(dc.now()) {
		freshness = gbmodels.ControlStateStale
	}
	return deviceControlFact{Found: true, State: state, Freshness: freshness}, nil
}

func (dc *DeviceMgmtController) loadAlarmControlFacts(c *gin.Context, deviceID uint) ([]deviceControlFact, error) {
	var states []gbmodels.GbDeviceControlState
	if err := dc.db().WithContext(c.Request.Context()).
		Where("device_id = ? AND target_scope = ?", deviceID, gbmodels.ControlTargetScopeAlarm).
		Order("target_code").Find(&states).Error; err != nil {
		return nil, err
	}
	facts := make([]deviceControlFact, 0, len(states))
	for _, state := range states {
		freshness := state.Freshness
		if state.ObservedAt.IsZero() {
			freshness = gbmodels.ControlStateUnknownFreshness
		} else if state.ObservedAt.Add(deviceStatusFreshFor).Before(dc.now()) {
			freshness = gbmodels.ControlStateStale
		}
		facts = append(facts, deviceControlFact{Found: true, State: state, Freshness: freshness})
	}
	return facts, nil
}

func deviceStatusResponse(channelCode string, resolution catalog.AlarmTargetResolution, recordFact, alarmFact deviceControlFact, alarmFacts []deviceControlFact) gin.H {
	recordState := gbmodels.ControlStateUnknown
	if recordFact.Found {
		recordState = recordFact.State.RecordState
	}
	guardState := gbmodels.ControlStateUnknown
	if alarmFact.Found {
		guardState = alarmFact.State.GuardState
	}
	combinedFreshness := combineDeviceStatusFreshness(recordFact.Freshness, alarmFact.Freshness, resolution.Status == catalog.AlarmTargetResolved)
	candidates := make([]gin.H, 0, len(resolution.Candidates))
	for _, candidate := range resolution.Candidates {
		candidates = append(candidates, gin.H{"code": candidate.AlarmCode, "name": candidate.Name})
	}
	alarmTargetCode := ""
	if resolution.Target != nil {
		alarmTargetCode = resolution.Target.AlarmCode
	}
	alarmFactViews := make([]gin.H, 0, len(alarmFacts))
	for _, fact := range alarmFacts {
		alarmFactViews = append(alarmFactViews, gin.H{
			"targetCode": fact.State.TargetCode, "guardState": fact.State.GuardState,
			"freshness": fact.Freshness, "observedAt": fact.State.ObservedAt,
		})
	}
	state := gin.H{
		"recordState": recordState, "guardState": guardState, "freshness": combinedFreshness,
		"targetScope": gbmodels.ControlTargetScopeChannel, "targetCode": channelCode,
	}
	return gin.H{
		"state": state, "recordState": recordState, "guardState": guardState,
		"freshness": combinedFreshness, "completeness": deviceStatusCompleteness(recordFact, alarmFact, resolution),
		"record": gin.H{
			"state": recordState, "freshness": recordFact.Freshness,
			"targetScope": gbmodels.ControlTargetScopeChannel, "targetCode": channelCode,
		},
		"alarmResolution": gin.H{
			"status": resolution.Status, "source": resolution.Source, "targetCode": alarmTargetCode,
			"state": guardState, "freshness": alarmFact.Freshness, "candidates": candidates,
		},
		"alarmFacts":         alarmFactViews,
		"refreshOperationId": nil, "recordRefreshOperationId": nil, "alarmRefreshOperationId": nil,
	}
}

func combineDeviceStatusFreshness(record, alarm string, alarmResolved bool) string {
	if record == gbmodels.ControlStateStale || (alarmResolved && alarm == gbmodels.ControlStateStale) {
		return gbmodels.ControlStateStale
	}
	if alarmResolved && record == gbmodels.ControlStateFresh && alarm == gbmodels.ControlStateFresh {
		return gbmodels.ControlStateFresh
	}
	return gbmodels.ControlStateUnknownFreshness
}

func deviceStatusCompleteness(recordFact, alarmFact deviceControlFact, resolution catalog.AlarmTargetResolution) string {
	if recordFact.Found && resolution.Status == catalog.AlarmTargetResolved && alarmFact.Found {
		return "complete"
	}
	return "partial"
}

func scopedDeviceStatusKey(base, scope string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	suffix := ":" + scope
	if len(base)+len(suffix) <= 128 {
		return base + suffix
	}
	sum := sha256.Sum256([]byte(base))
	return fmt.Sprintf("%x%s", sum, suffix)
}

func deviceStatusRefreshError(recordErr, alarmErr error) string {
	parts := make([]string, 0, 2)
	if recordErr != nil {
		parts = append(parts, "录像状态查询: "+recordErr.Error())
	}
	if alarmErr != nil {
		parts = append(parts, "报警状态查询: "+alarmErr.Error())
	}
	return strings.Join(parts, "; ")
}

func (dc *DeviceMgmtController) now() time.Time {
	return time.Now()
}
