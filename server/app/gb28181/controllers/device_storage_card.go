package controllers

import (
	"time"

	"github.com/gin-gonic/gin"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// GetChannelStorageCards 读取某通道所属设备的存储卡状态（GB/T 28181-2022 A.2.4.14/A.2.6.16）。
//
// 交互形态与既有的 device-status / cruise-tracks 读接口一致：
//   - 不带 refresh：只读缓存表里"上一次查询得到的卡事实"，不产生任何 SIP 报文；
//   - 带 refresh=true：先发起一次 SDCardStatus 查询，再把**当前缓存**返回，
//     同时给出 refreshOperationId 让前端去轮询 /ptz/operations/:operationId。
//
// ⭐ 为什么返回的列表可能是空的但不算错误：设备可以不装卡。空列表 + SumNum=0
// 是标准里完全合法的结果，前端应展示"无存储卡"而不是报错。
func (dc *DeviceMgmtController) GetChannelStorageCards(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return
	}

	data := gin.H{"list": []gbmodels.GbDeviceStorageCard{}, "freshness": gbmodels.PTZFreshnessUnknown, "targetCode": target.ChannelCode}

	if c.Query("refresh") == "true" {
		service := dc.ptzServiceSnapshot()
		if service == nil {
			dc.FailAndAbort(c, "PTZ Service 未就绪", nil)
			return
		}
		// 复用看守位那套"取当前操作者 + 所属部门"的读法：动作本身与看守位无关，
		// 但记录审计是谁发起的这件事是同一条契约（operation.actor_id）。
		actorID, actorDeptID, actorFailure := dc.loadHomePositionActor(c)
		if actorFailure != nil {
			writeHomePositionFailure(c, actorFailure)
			return
		}
		operation, err := service.RefreshStorageCards(c.Request.Context(), target, actorID, actorDeptID, c.GetHeader("Idempotency-Key"))
		if err != nil {
			// 发送失败不阻断读：前端仍应看到上一次的卡事实，只是带上错误说明。
			data["refreshError"] = "存储卡状态查询未发送成功"
		} else {
			data["refreshOperationId"] = operation.OperationID
			// 与查询同批返回的既有事实，用操作序号说明"这份数据是不是刚问出来的"。
			data["pendingOperationId"] = operation.OperationID
		}
	}

	var list []gbmodels.GbDeviceStorageCard
	result := dc.db().WithContext(c.Request.Context()).
		Where("device_id = ? AND target_code = ?", target.DeviceID, target.ChannelCode).
		Order("card_id").Find(&list)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询存储卡状态失败", result.Error)
		return
	}
	if len(list) > 0 {
		data["list"] = list
		latest := time.Time{}
		for _, item := range list {
			if item.ObservedAt.After(latest) {
				latest = item.ObservedAt
			}
		}
		data["freshness"] = ptzFreshness(latest, true)
		data["observedAt"] = latest
	}
	dc.Success(c, data)
}
