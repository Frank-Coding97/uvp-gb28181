package controllers

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
)

// 配置家族（A.2.4.7 查询 / A.2.3.2.5 下发）的 HTTP 面。
//
// ## 请求/响应的字段词汇表
//
// 请求体里的 `blocks` **就是协议层的容器结构本身**（`manscdp.DeviceConfigBlocks`），
// 键名 = 标准元素名的小驼峰（`frameMirror` / `osdConfig` / `alarmReport`…）。
//
// ⛔ 为什么不让前端传 `deviceConfigGroups.ts` 里那套表单键（`mask1` / `beginTime`）：
// 那是**展示层**的表单字段 id，与协议字段不是一对一（4 个 `maskN` 对应一个 `RegionList`）。
// 在接口上再翻译一道 = 造第二套词汇表，然后它和协议的定义迟早对不上。
// 表单键 → 协议块的映射是**展示**问题，留在前端。
//
// ⛔ 由此得到一个必须守住的性质：**这三个地方的键名是同一套** ——
// 请求/响应体、库里的 `gb_device_config.payload_json`、对账差异里的字段路径。
// 它们都来自 `manscdp` 结构上的 json tag，改一处即同时改三处，不存在漂移。

// deviceConfigEntry 是读取接口返回的一条配置快照。
type deviceConfigEntry struct {
	ConfigType        string          `json:"configType"`
	Payload           json.RawMessage `json:"payload"`
	ObservedAt        time.Time       `json:"observedAt"`
	SourceOperationID *string         `json:"sourceOperationId,omitempty"`
}

type applyDeviceConfigRequest struct {
	Blocks         manscdp.DeviceConfigBlocks `json:"blocks"`
	IdempotencyKey string                     `json:"idempotencyKey"`
}

// GetChannelDeviceConfigs 读取通道所属设备的配置家族快照。
//
// 交互形态与 device-storage-cards / video-params 完全一致：
//   - 不带 refresh：只读缓存表，不产生 SIP 报文；
//   - 带 refresh=true：先发起一次 ConfigDownload，再把**当前缓存**返回，
//     并给出 refreshOperationId 让前端轮询。
//   - 可选 configTypes=OSDConfig,FrameMirror 收窄范围，缺省为全部 8 组。
//
// `reconcile` 块与视频参数面板同源（同一个纯函数）：`Result=OK` 只表示"收到并接受"，
// 所以界面必须能区分 never_read / pending / read_ok / type_absent / mismatch / failed，
// 否则会把能力问题说成操作问题。
func (dc *DeviceMgmtController) GetChannelDeviceConfigs(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return
	}

	requested, failureMessage := requestedDeviceConfigTypes(c.Query("configTypes"))
	if failureMessage != "" {
		dc.FailAndAbort(c, failureMessage, nil)
		return
	}

	data := gin.H{
		"list":              []deviceConfigEntry{},
		"freshness":         gbmodels.PTZFreshnessUnknown,
		"targetCode":        target.ChannelCode,
		"requestedTypes":    requested,
		"registeredVersion": target.Profile.Version,
	}

	if c.Query("refresh") == "true" {
		service := dc.ptzServiceSnapshot()
		if service == nil {
			dc.FailAndAbort(c, "PTZ Service 未就绪", nil)
			return
		}
		actorID, actorDeptID, actorFailure := dc.loadHomePositionActor(c)
		if actorFailure != nil {
			writeHomePositionFailure(c, actorFailure)
			return
		}
		operation, err := service.ReadDeviceConfigs(c.Request.Context(), target, requested,
			actorID, actorDeptID, c.GetHeader("Idempotency-Key"))
		if err != nil {
			// 发送失败不阻断读：前端仍应看到上一次回读到的配置，只是带上错误说明。
			data["refreshError"] = "设备配置读取请求未发送成功"
		} else {
			data["refreshOperationId"] = operation.OperationID
		}
	}

	var rows []gbmodels.GbDeviceConfig
	result := dc.db().WithContext(c.Request.Context()).
		Where("device_id = ? AND target_code = ?", target.DeviceID, target.ChannelCode).
		Order("config_type").Find(&rows)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询设备配置失败", result.Error)
		return
	}
	if len(rows) > 0 {
		entries, latest, absent := dc.buildDeviceConfigEntries(rows, requested)
		data["list"] = entries
		// absentTypes 是**按"这次问过的类型"算出来的差集**，不是"所有没数据的类型"。
		// 没问过的类型当然没数据，把它列成 absent 就是在报假问题。
		data["absentTypes"] = absent
		data["freshness"] = ptzFreshness(latest, true)
		data["observedAt"] = latest
	} else {
		data["absentTypes"] = requested
	}

	data["reconcile"] = dc.deviceConfigReconcileState(c, channel)
	dc.Success(c, data)
}

// buildDeviceConfigEntries 把落库行折成响应条目，并算出"问过但库里没有"的类型。
//
// ⛔ `requested` 必须传进来，不能拿 [manscdp.ConfigTypeOrder] 当分母（2026-09-19 修）：
// 库里的行是**历次读取累积**的，与"这一次问了什么"无关。用全部 8 组减已落库，
// 会在 `configTypes=PictureMask` 这种收窄查询里把另外 7 个**没问过**的类型报成 absent，
// 而前端拿到 absent 就显示「设备未返回该类型」并禁用编辑 ⇒ **凭空造出 7 个假问题**。
func (dc *DeviceMgmtController) buildDeviceConfigEntries(
	rows []gbmodels.GbDeviceConfig, requested []string,
) ([]deviceConfigEntry, time.Time, []string) {
	entries := make([]deviceConfigEntry, 0, len(rows))
	latest := time.Time{}
	stored := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		// ⛔ payload_json 直接透出成对象而不是字符串：前端拿到的就是协议结构本身，
		// 不需要再 parse 一次；字符串形式还会诱使前端自己写一套反序列化。
		if !json.Valid([]byte(row.PayloadJSON)) {
			continue
		}
		entries = append(entries, deviceConfigEntry{
			ConfigType:        row.ConfigType,
			Payload:           json.RawMessage(row.PayloadJSON),
			ObservedAt:        row.ObservedAt,
			SourceOperationID: row.SourceOperationID,
		})
		stored[row.ConfigType] = struct{}{}
		if row.ObservedAt.After(latest) {
			latest = row.ObservedAt
		}
	}
	// 差集在 **requested** 上算，与下面 `len(rows) == 0` 那条分支同口径。
	// `requested` 已按 ConfigTypeOrder 排序（normalizeDeviceConfigTypes），顺序稳定。
	absent := make([]string, 0, len(requested))
	for _, configType := range requested {
		if _, ok := stored[configType]; !ok {
			absent = append(absent, configType)
		}
	}
	return entries, latest, absent
}

// deviceConfigReconcileState 取"最近一次回读 operation"交给纯函数推导面板状态。
//
// ⛔ 查不到（从没读过 / 读失败）时一律返回 never_read，不报错：
// 面板左栏是"事实展示"，没有事实不等于接口坏了。
func (dc *DeviceMgmtController) deviceConfigReconcileState(c *gin.Context, channel *gbmodels.GbChannel) ptz.DeviceConfigReconcileState {
	var latest gbmodels.GbPTZOperation
	result := dc.db().WithContext(c.Request.Context()).
		Where("channel_id = ? AND action = ?", channel.ID, ptz.ActionRefreshDeviceConfigs).
		Order("id DESC").Limit(1).Find(&latest)
	if result.Error != nil || result.RowsAffected == 0 {
		return ptz.DeriveDeviceConfigReconcileState(nil)
	}
	return ptz.DeriveDeviceConfigReconcileState(&latest)
}

// ApplyChannelDeviceConfigs 下发通道的设备配置（A.2.3.2.5 DeviceConfig）。
//
// ⛔ 这个接口**不承诺**"下发成功 = 配置生效"。写入应答只有 Result、没有回显，
// 所以返回里显式给出 `reconcilePending: true`：真正的结论由紧随其后的自动回读给出
// （服务层在收到 ack 的同一事务里就排好了那条 ConfigDownload）。
// 前端应据 GET 的 reconcile 块收敛，而不是拿这个 200 当成功终态。
//
// ⛔ 这里**不做版本门禁**（与 ApplyChannelVideoParams 一致）：被误登记成 2016 的真 2022
// 设备，不试一次就永远用不了这功能；而 2016 设备收到不认识配置类型的真实结果，
// 会由回读的 type_absent / mismatch 如实暴露出来。版本只用于选择提示措辞。
func (dc *DeviceMgmtController) ApplyChannelDeviceConfigs(c *gin.Context) {
	service := dc.ptzServiceSnapshot()
	if service == nil {
		dc.FailAndAbort(c, "PTZ Service 未就绪", nil)
		return
	}
	var request applyDeviceConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "设备配置下发参数不合法", err)
		return
	}
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return
	}

	// ⛔ 严格发：取值范围在协议层收口，违规**拒发**而不是静默夹取 ——
	// 平台自己发出的值乱来，对端会静默当 0 处理，这种错在回读对账里只表现为
	// "设备没照做"，归因成本极高。服务层会再校验一次（同一条规则，双重保险）。
	if request.Blocks.IsEmpty() {
		dc.FailAndAbort(c, "至少需要一组配置", nil)
		return
	}
	if err := manscdp.ValidateDeviceConfigBlocks(request.Blocks); err != nil {
		dc.FailAndAbort(c, "设备配置不合法: "+err.Error(), nil)
		return
	}

	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	actorID, actorDeptID, actorFailure := dc.loadHomePositionActor(c)
	if actorFailure != nil {
		writeHomePositionFailure(c, actorFailure)
		return
	}

	// 与 PTZ / 资源配置写共用一个通道级互斥：同一通道的设备命令不交错发送。
	lock := dc.deviceControlLock(channel.ID)
	lock.Lock()
	defer lock.Unlock()

	op, err := service.ApplyDeviceConfig(c.Request.Context(), target, request.Blocks,
		actorID, actorDeptID, key)
	if err != nil {
		dc.FailAndAbort(c, "下发设备配置失败", err)
		return
	}
	dc.Success(c, gin.H{
		"operationId": op.OperationID,
		"channelId":   channel.ChannelID,
		"action":      ptz.ActionApplyDeviceConfig,
		"sn":          op.SN,
		"status":      op.Status,
		// configTypes 回显本次真的会发的类型集合（按标准顺序去重后的结果），
		// 让调用方不必自己推导"我传的键到底被认下来几个"。
		"configTypes": request.Blocks.PresentConfigTypes(),
		// ack 不是终态：回读对账已排队，界面须轮询 GET 的 reconcile 收敛。
		"reconcilePending": true,
	})
}

// requestedDeviceConfigTypes 解析 `configTypes` 查询参数（逗号分隔），缺省为全部 8 组。
//
// ⛔ 缺省必须是"全部"而不是"空"：空集合会让 ReadDeviceConfigs 拒发，
// 于是 GET ?refresh=true 在没给参数时会**静默不发报文**，前端看起来像"设备没响应"。
func requestedDeviceConfigTypes(raw string) ([]string, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return append([]string(nil), manscdp.ConfigTypeOrder...), ""
	}
	parts := strings.Split(trimmed, ",")
	requested := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			requested = append(requested, value)
		}
	}
	if len(requested) == 0 {
		return nil, "configTypes 不能为空"
	}
	// 复用协议层的顺序/去重/未知类型判定，不在这里另写一套。
	for _, value := range requested {
		known := false
		for _, candidate := range manscdp.ConfigTypeOrder {
			if candidate == value {
				known = true
				break
			}
		}
		if !known {
			return nil, "未知的配置类型: " + value
		}
	}
	ordered := make([]string, 0, len(requested))
	for _, configType := range manscdp.ConfigTypeOrder {
		for _, value := range requested {
			if value == configType {
				ordered = append(ordered, configType)
				break
			}
		}
	}
	return ordered, ""
}
