package controllers

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
)

// videoParamMaxStreams 一次下发最多带几个码流。
//
// ⛔ 标准里 `Item` 是 `maxOccurs="unbounded"`（与存储卡的 `maxOccurs=8` 不同），
// 本仓**不设协议上限**，这里只是一个工程护栏：单帧 SIP 报文过大要分片，
// 而"码流分段超过 8 段"的设备不存在。真正的取值范围校验在
// manscdp.ValidateVideoParamItems（附录 G），那才是唯一出处。
const videoParamMaxStreams = 8

// videoParamItemRequest 是下发请求里的一个码流。
//
// ⛔ StreamNumber 用指针：0 号是**合法**的主码流编号，普通 int 无法区分
// "没填"与"填了 0"，而这两者的处理完全不同（前者报错、后者照发）。
type videoParamItemRequest struct {
	StreamNumber *int    `json:"streamNumber"`
	VideoFormat  string  `json:"videoFormat"`
	Resolution   string  `json:"resolution"`
	FrameRate    string  `json:"frameRate"`
	BitRateType  string  `json:"bitRateType"`
	VideoBitRate *string `json:"videoBitRate"`
}

type applyVideoParamsRequest struct {
	Items          []videoParamItemRequest `json:"items"`
	IdempotencyKey string                  `json:"idempotencyKey"`
}

// GetChannelVideoParams 读取通道所属设备的「视频参数属性」(GB/T 28181-2022 A.2.4.7)。
//
// 交互形态与 device-storage-cards / device-status 一致：
//   - 不带 refresh：只读缓存表里"上一次回读得到的配置"，不产生 SIP 报文；
//   - 带 refresh=true：先发起一次 ConfigDownload，再把**当前缓存**返回，
//     同时给出 refreshOperationId 让前端去轮询。
//
// ⭐ 与存储卡接口最大的不同是这里多透出一个 `reconcile` 块。原因：本面板的值
// 是"设备最近一次怎么说的"，而**写入的 ack 不构成终态**（A.2.6.8 没有任何回显）。
// 所以面板必须能区分下面几种"看起来都是空/旧"的情形，否则会把能力问题说成操作问题：
//
//	never_read  从未回读过 —— 提示"点读取设备参数"
//	pending     回读还在飞 —— 保持 loading
//	read_ok     回读成功且设备带了该配置类型 —— 正常展示
//	type_absent 设备回了 OK 但没带该元素 —— 提示"设备未返回此配置类型"（2016 的典型形态）
//	mismatch    下发被接受但回读值不一致 —— 展示逐格差异（≠ 失败，是能力边界）
//	failed      被拒/超时/报文不合法 —— 提示"没拿到答案"
//
// 判据本身由 ptz.DeriveVideoParamReconcileState 单点定义（纯函数、有单测），
// 控制器只负责把"最近一次回读 operation"查出来交给它。
func (dc *DeviceMgmtController) GetChannelVideoParams(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return
	}

	data := gin.H{
		"list":       []gbmodels.GbDeviceVideoParam{},
		"freshness":  gbmodels.PTZFreshnessUnknown,
		"targetCode": target.ChannelCode,
		// streamNumberList 是"面板按几段码流渲染"的出处（2022 独有目录属性）。
		// 空串 = 设备本次未上报目录属性，前端应退化成"按已回读到的行渲染"。
		"streamNumberList": channel.StreamNumberList,
		// registeredVersion 是设备**当前生效**的协议版本
		// （gb_device.effective_version，来源可能是注册声明 / 手工 override / 历史 / 默认 2016）。
		// ⛔ 它**只用来选提示措辞**（"为什么这个类型是空的"），绝不参与任何门禁判断：
		// 登记成 2022 的也可能没实现，登记成 2016 的也可能提前实现 ——
		// 唯一可靠判据是回读结果本身（规格 §十③/§十④）。
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
		operation, err := service.ReadVideoParams(c.Request.Context(), target, actorID, actorDeptID, c.GetHeader("Idempotency-Key"))
		if err != nil {
			// 发送失败不阻断读：前端仍应看到上一次回读的配置，只是带上错误说明。
			data["refreshError"] = "视频参数读取请求未发送成功"
		} else {
			data["refreshOperationId"] = operation.OperationID
		}
	}

	var list []gbmodels.GbDeviceVideoParam
	result := dc.db().WithContext(c.Request.Context()).
		Where("device_id = ? AND target_code = ?", target.DeviceID, target.ChannelCode).
		Order("stream_number").Find(&list)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询视频参数失败", result.Error)
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

	data["reconcile"] = dc.videoParamReconcileState(c, channel)
	dc.Success(c, data)
}

// videoParamReconcileState 取"最近一次回读 operation"并交给纯函数推导面板状态。
//
// ⛔ 查不到（从没读过 / 读失败）时一律返回 never_read，不报错：
// 面板左栏是"事实展示"，没有事实不等于接口坏了 —— 与存储卡"设备可以不装卡"同一条口径。
func (dc *DeviceMgmtController) videoParamReconcileState(c *gin.Context, channel *gbmodels.GbChannel) ptz.VideoParamReconcileState {
	var latest gbmodels.GbPTZOperation
	result := dc.db().WithContext(c.Request.Context()).
		Where("channel_id = ? AND action = ?", channel.ID, ptz.ActionRefreshVideoParams).
		Order("id DESC").Limit(1).Find(&latest)
	if result.Error != nil || result.RowsAffected == 0 {
		return ptz.DeriveVideoParamReconcileState(nil)
	}
	return ptz.DeriveVideoParamReconcileState(&latest)
}

// ApplyChannelVideoParams 下发通道的视频参数（GB/T 28181-2022 A.2.3.2.5 DeviceConfig）。
//
// ⛔ 这个接口**不承诺**"下发成功 = 配置生效"。写入应答只有 Result，没有回显，
// 所以本控制器在返回里显式给出 `reconcilePending: true`：真正的结论由紧随其后的
// 自动回读给出（服务层在收到 ack 的同一事务里就排好了那条 ConfigDownload）。
// 前端应据 GET 的 reconcile 块收敛，而不是拿这个 200 当成功终态。
//
// ⛔ 这里**不做版本门禁**（与 RefreshStorageCards 的既有口径一致）：
// 被误登记成 2016 的真 2022 设备，不试一次就永远用不了这功能；而 2016 设备
// 收到不认识配置类型的真实结果会由回读的 type_absent/mismatch 如实暴露出来。
// 版本只用于选择提示措辞，绝不拦操作员点出来的动作。
func (dc *DeviceMgmtController) ApplyChannelVideoParams(c *gin.Context) {
	service := dc.ptzServiceSnapshot()
	if service == nil {
		dc.FailAndAbort(c, "PTZ Service 未就绪", nil)
		return
	}
	var request applyVideoParamsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "视频参数下发参数不合法", err)
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

	items, failureMessage := buildVideoParamItems(request.Items)
	if failureMessage != "" {
		dc.FailAndAbort(c, failureMessage, nil)
		return
	}
	// 严格发：附录 G 的取值范围在这里收口，违规**拒发**而不是静默夹取 ——
	// 平台自己发出的值乱来，对端会静默当 0 处理，这种错在回读对账里只表现为
	// "设备没照做"，归因成本极高。服务层会再校验一次（同一条规则，双重保险）。
	if err := manscdp.ValidateVideoParamItems(items); err != nil {
		dc.FailAndAbort(c, "视频参数不合法: "+err.Error(), nil)
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

	op, err := service.ApplyVideoParams(c.Request.Context(), target, items, actorID, actorDeptID, key)
	if err != nil {
		dc.FailAndAbort(c, "下发视频参数失败", err)
		return
	}
	dc.Success(c, gin.H{
		"operationId": op.OperationID,
		"channelId":   channel.ChannelID,
		"action":      "apply_video_params",
		"sn":          op.SN,
		"status":      op.Status,
		"streamCount": len(items),
		// ack 不是终态：回读对账已排队，界面须轮询 GET 的 reconcile 收敛。
		"reconcilePending": true,
	})
}

// buildVideoParamItems 把请求体折成协议层 DTO，并做**结构性**校验（不看取值）。
// 返回的第二个值非空即为错误文案。
//
// ⛔ 取值范围的校验一律交给 manscdp.ValidateVideoParamItems：控制器里再写一套
// "1-5 / 0-99 / 0-100000" 就是第二个真源，两边迟早会漂。
func buildVideoParamItems(raw []videoParamItemRequest) ([]manscdp.VideoParamItem, string) {
	if len(raw) == 0 {
		return nil, "至少需要一个码流"
	}
	if len(raw) > videoParamMaxStreams {
		return nil, "码流数量超出上限"
	}
	items := make([]manscdp.VideoParamItem, 0, len(raw))
	for _, item := range raw {
		if item.StreamNumber == nil {
			return nil, "码流编号缺失（0 号表示主码流，不能省略）"
		}
		items = append(items, manscdp.VideoParamItem{
			StreamNumber: *item.StreamNumber,
			VideoFormat:  strings.TrimSpace(item.VideoFormat),
			Resolution:   strings.TrimSpace(item.Resolution),
			FrameRate:    strings.TrimSpace(item.FrameRate),
			BitRateType:  strings.TrimSpace(item.BitRateType),
			VideoBitRate: trimOptionalString(item.VideoBitRate),
		})
	}
	return items, ""
}

// trimOptionalString 修剪可选字符串，空串归一成 nil。
// ⛔ 空串与 nil 在协议层是**两件事**：nil = 不发这个元素（VBR 下的正确形态），
// 空串 = 发了个空元素（设备侧的解析结果不可预期）。前端传 "" 时按"没填"处理。
func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
