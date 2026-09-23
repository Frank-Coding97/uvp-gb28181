package controllers

import (
	"crypto/sha256"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/utils/response"

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
			response.SetBusinessResult(c, http.StatusServiceUnavailable, false)
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
		"deviceReport":       deviceReportView(recordFact),
		"alarmFacts":         alarmFactViews,
		"refreshOperationId": nil, "recordRefreshOperationId": nil, "alarmRefreshOperationId": nil,
	}
}

// deviceReportView 暴露设备在 DeviceStatus 应答里自报的事实。
//
// 每一项都可能是 null —— 那表示"设备这次没有上报这一项"，与 false / 0 不是一回事：
// alarmInputCount 为 0 是**设备明确声明自己没有报警输入**，null 才是"设备没提"。
// 前端必须照着 null 显示"未上报"，不要兜底成关闭。
func deviceReportView(fact deviceControlFact) gin.H {
	view := gin.H{
		"online": nil, "selfTest": nil, "encode": nil,
		"deviceTime": nil, "clockSkewSeconds": nil, "alarmInputCount": nil,
		"observedAt": nil,
	}
	if !fact.Found {
		return view
	}
	state := fact.State
	if state.OnlineState != nil {
		view["online"] = *state.OnlineState
	}
	if state.SelfTestState != nil {
		view["selfTest"] = *state.SelfTestState
	}
	if state.EncodeState != nil {
		view["encode"] = *state.EncodeState
	}
	if state.DeviceTime != nil {
		view["deviceTime"] = state.DeviceTime
		view["clockSkewSeconds"] = deviceClockSkewSeconds(state)
	}
	if state.AlarmInputCount != nil {
		view["alarmInputCount"] = *state.AlarmInputCount
	}
	if !state.ObservedAt.IsZero() {
		view["observedAt"] = state.ObservedAt
	}
	return view
}

// deviceClockSkewSeconds = 平台观测时刻 − 设备自报时刻，四舍五入到秒。
// 设备自报时间只精确到秒，所以这个差值自带约 1 秒的量化误差，只适合回答
// "设备时间是不是差了几分钟/几小时"，别拿去当毫秒级结论。
func deviceClockSkewSeconds(state gbmodels.GbDeviceControlState) *int64 {
	if state.DeviceTime == nil || state.ObservedAt.IsZero() {
		return nil
	}
	skew := int64(math.Round(state.ObservedAt.Sub(*state.DeviceTime).Seconds()))
	return &skew
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
	// 设备明确回了 Alarmstatus Num="0"（自己没有报警输入），目录里也确实解析不到报警目标 ——
	// 这种情况下「报警事实」本来就是个空集，再报 partial 等于把"设备没有这个能力"
	// 说成"平台没收到"，用户会一直以为还有东西没查到。
	if recordFact.Found && resolution.Status == catalog.AlarmTargetUnavailable && declaresNoAlarmInputs(recordFact) {
		return "complete"
	}
	return "partial"
}

func declaresNoAlarmInputs(fact deviceControlFact) bool {
	return fact.State.AlarmInputCount != nil && *fact.State.AlarmInputCount == 0
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
