package controllers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
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

// storageCardFormatRequest 是存储卡格式化请求体。
//
// ⛔ CardIndex 是 `*int` 而不是 `int`：`0` 在标准里是**合法且有意义**的取值
// （A.2.3.1.13 注释「SD 卡编号，从1开始编号。该值0时，对所有存储卡进行格式化」）。
// 用值类型会让"没传这个字段"与"要格式化全部卡"共用零值，而后者是更危险的那个解释 ——
// 破坏性动作绝不能靠默认值兜底。没传就是要报错。
type storageCardFormatRequest struct {
	CardIndex      *int   `json:"cardIndex"`
	Confirmed      bool   `json:"confirmed"`
	IdempotencyKey string `json:"idempotencyKey"`
}

// FormatStorageCard 下发存储卡格式化（GB/T 28181-2022 A.2.3.1.13）。
//
// 门禁顺序（与 RebootDevice 一致，别调换）：登录 → 显式确认 → 权限 → 参数。
// 把"确认"放在"权限"之前是刻意的：一个没有权限、又没带确认的请求，
// 先报缺确认不会泄露"这个账号有没有格式化权限"。
func (dc *DeviceMgmtController) FormatStorageCard(c *gin.Context) {
	service := dc.ptzServiceSnapshot()
	if service == nil {
		response.SetBusinessResult(c, http.StatusServiceUnavailable, false)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "设备控制服务未就绪"})
		return
	}
	var request storageCardFormatRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "存储卡格式化参数不合法", err)
		return
	}
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	if !request.Confirmed {
		dc.FailAndAbort(c, "存储卡格式化会清空卡上录像，需要显式确认", nil)
		return
	}
	if !dc.checkDeviceStorageCardFormatPermission(c, channel.ID) {
		return
	}
	if request.CardIndex == nil {
		dc.FailAndAbort(c, "缺少存储卡编号", nil)
		return
	}
	cardIndex := *request.CardIndex
	if cardIndex < 0 {
		dc.FailAndAbort(c, "存储卡编号不合法", nil)
		return
	}
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return
	}
	actorID, actorDeptID, actorFailure := dc.loadHomePositionActor(c)
	if actorFailure != nil {
		writeHomePositionFailure(c, actorFailure)
		return
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	operation, err := service.FormatStorageCard(c.Request.Context(), target, cardIndex, actorID, actorDeptID, key)
	if err != nil {
		if failure := homePositionOperationFailure(err); failure != nil {
			writeHomePositionFailure(c, failure)
			return
		}
		dc.FailAndAbort(c, "下发存储卡格式化失败", err)
		return
	}
	// 复用设备控制的响应形态（operationId/status/deadlineAt/targetCode…），
	// 前端可以照 `device-control` 那套去轮询 /ptz/operations/:operationId，不必新写一套。
	dc.deviceControlSuccess(c, operation, operation.Action, false)
}

// checkDeviceStorageCardFormatPermission 是存储卡格式化的 defense-in-depth 权限复检。
//
// 独立路由已经挂了 JWT/Casbin（`gb28181:device:format_sd`），这里**再查一遍**：
// 直接调用（单测、将来的内部编排）也必须满足同一授权，不能因为绕过路由就拿到破坏性动作。
// 写法与 checkDeviceRebootPermission 同族。
func (dc *DeviceMgmtController) checkDeviceStorageCardFormatPermission(c *gin.Context, channelID uint) bool {
	userID := dc.GetCurrentUserID(c)
	if userID == 0 {
		dc.FailAndAbort(c, "未登录", nil)
		return false
	}
	if app.ConfigYml != nil {
		for _, skipped := range app.ConfigYml.GetUintSlice("server.notcheckuser") {
			if skipped == userID {
				return true
			}
		}
	}
	if app.CasbinV2 == nil {
		dc.FailAndAbort(c, "存储卡格式化权限服务未就绪", nil)
		return false
	}
	allowed, err := app.CasbinV2.Enforce(fmt.Sprintf("user_%d", userID), storageCardFormatAPIPath(channelID), http.MethodPost, "*")
	if err != nil {
		dc.FailAndAbort(c, "校验存储卡格式化权限失败", err)
		return false
	}
	if !allowed {
		dc.FailAndAbort(c, "无存储卡格式化权限", nil)
		return false
	}
	return true
}

// storageCardFormatAPIPath 把路由常量里的 `:id` 换成真实通道 id，得到鉴权中间件**实际看到**的路径。
//
// ⛔ 从这里派生，而不是在权限检查里再手写一遍全路径：手写那份与迁移里登记的 sys_api
// 一旦不一致，表现是"接口通但恒 403"（Casbin 里没有这条规则），而且两侧都不报错。
func storageCardFormatAPIPath(channelID uint) string {
	return strings.Replace(gbmodels.StorageCardFormatAPIPath, ":id", strconv.FormatUint(uint64(channelID), 10), 1)
}
