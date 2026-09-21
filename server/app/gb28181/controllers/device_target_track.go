package controllers

import (
	"strings"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
)

// TargetTrackWindowHint 是前端算框选坐标时必须遵守的口径，随读接口一起下发。
//
// ⛔ 标准原文（A.2.3.1.14）：「由于平台与设备画面比例大小不同，需要进行比例关系转化。
// 因此，平台应提供画面大小：**播放窗口**长度像素值和播放窗口宽度像素值。」
// ⇒ 送出去的 `Length`/`Width` 必须是**用户实际看到的那块视频画面**的像素尺寸，
// 而框选坐标必须落在同一坐标系里。把这两件事写在服务端常量里（而不是只写在前端注释里），
// 是为了让"换个前端实现"时不会各自猜一套。
const TargetTrackWindowHint = "area 六项必须同一坐标系：length/width 是视频画面在页面上实际渲染的像素尺寸（不是视频原始分辨率、不是元素外框），midPointX/midPointY/lengthX/lengthY 是相对该画面左上角的框选坐标。"

// GbDeviceTargetTrackReadModel 是目标跟踪读接口的返回体。
//
// ⛔ `DeviceAcknowledged` 恒为 false，且**不是**"暂时还没收到应答"：
// 目标跟踪在 GB/T 28181-2022 里是无应答命令（9.3.1 d) + 表 1 序号 13），
// 而且全文没有任何"目标跟踪状态查询/上报"的命令 ⇒ 设备**永远不会**回执，
// 平台也**永远**无法知道设备实际在跟踪什么。界面上必须照这个字段措辞。
type GbDeviceTargetTrackReadModel struct {
	// Intent 是平台最近一次下发的指令；nil 表示这台设备还没被下发过。
	Intent *gbmodels.GbDeviceTargetTrack `json:"intent"`
	// DeviceAcknowledged 恒 false，理由见类型注释。
	DeviceAcknowledged bool `json:"deviceAcknowledged"`
	// ResponseRequired 恒 false（无应答命令），前端据此决定措辞与是否需要轮询。
	ResponseRequired bool `json:"responseRequired"`
	// WindowHint 见 [TargetTrackWindowHint]。
	WindowHint string `json:"windowHint"`
	// Capability 是设备自报的目标跟踪能力（默认 unknown，不臆断）。
	Capability manscdp.ControlCapability `json:"capability"`
	// TargetCode 是这次读的"球机通道"编码（= 报文里 SN 之后的 DeviceID）。
	TargetCode string `json:"targetCode"`
}

// GetChannelTargetTrack 读取该通道**最近一次下发的**目标跟踪指令（A.2.3.1.14）。
//
// ⛔ 它读的是 gb_device_target_track（平台意图），**不是**设备状态 ——
// 标准里没有可查询设备跟踪状态的命令，这一点见
// GbDeviceTargetTrackReadModel.DeviceAcknowledged 的注释。
//
// 交互形态与既有读接口一致：纯本地读，不产生任何 SIP 报文。
func (dc *DeviceMgmtController) GetChannelTargetTrack(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	// 与 GetChannelStorageCards 同形：读接口也先解析 target —— 它顺带做了
	// 设备可见性（visibleScope）与 PTZ 目标授权，读别人的通道必须在这里被挡住。
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return
	}
	data := GbDeviceTargetTrackReadModel{
		DeviceAcknowledged: false,
		ResponseRequired:   false,
		WindowHint:         TargetTrackWindowHint,
		Capability:         manscdp.ParseControlCapabilities(channel.Capabilities, channel.PTZType).TargetTrack,
		TargetCode:         target.ChannelCode,
	}
	var intent gbmodels.GbDeviceTargetTrack
	result := dc.db().WithContext(c.Request.Context()).
		Where("device_id = ? AND target_code = ?", target.DeviceID, target.ChannelCode).
		Limit(1).Find(&intent)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询目标跟踪指令失败", result.Error)
		return
	}
	if result.RowsAffected == 1 {
		data.Intent = &intent
	}
	dc.Success(c, data)
}

// targetTrackAreaRequest 是框选坐标的请求体。
//
// ⛔ 六个字段都用值类型而不是指针，与 storageCardFormatRequest 的 `*int` 刻意不同：
// 那边用指针是因为**0 是合法值且含义相反**（0 = 格式化全部卡）。这里 0 不合法
// （窗口尺寸为 0 无法做比例换算），所以"没传"与"传 0"都会在同一个地方被拒，
// 不需要靠指针去区分。
type targetTrackAreaRequest struct {
	Length    int `json:"length"`
	Width     int `json:"width"`
	MidPointX int `json:"midPointX"`
	MidPointY int `json:"midPointY"`
	LengthX   int `json:"lengthX"`
	LengthY   int `json:"lengthY"`
}

type targetTrackRequest struct {
	Mode           string                  `json:"mode" binding:"required"`
	DeviceID2      string                  `json:"deviceId2"`
	Area           *targetTrackAreaRequest `json:"area"`
	IdempotencyKey string                  `json:"idempotencyKey"`
}

// SetChannelTargetTrack 下发目标跟踪（GB/T 28181-2022 A.2.3.1.14）。
//
// ⛔ 门禁顺序：登录（路由中间件）→ 权限（路由 + Casbin）→ 参数。
// 与存储卡格式化的"确认在权限之前"不同，这里**没有二次确认** ——
// 目标跟踪是可逆的普通控制（Stop 一条就回来），把它做成破坏性动作的门禁形态
// 只会让操作员多按一次确认框而没有任何风险收益。
//
// ⛔ 也不能塞进 `/channel/:id/device-control` 的 action：那条路由整条绑
// `gb28181:device:control`，而本能力需要自己的读接口与返回体（含意图快照）。
// 权限分档照 video-params 先例：读 ptz:view / 写 ptz:control。
//
// ⛔ 登录检查不能省（与 `/channel/:id/device-control` 的现状刻意不同）：
// 那边匿名调用会一路走到 `loadHomePositionActor` 才失败，报出来的是
// 500「无法识别看守位操作者」—— 结论虽然是"拒绝了"，但错误码与措辞都在误导排障的人。
// 这里是会**真的操作设备**的写接口，匿名就该在第一步拿到明确的 401 语义。
func (dc *DeviceMgmtController) SetChannelTargetTrack(c *gin.Context) {
	if !dc.requireMaintenanceAuthentication(c) {
		return
	}
	service := dc.ptzServiceSnapshot()
	if service == nil {
		dc.FailAndAbort(c, "PTZ Service 未就绪", nil)
		return
	}
	var request targetTrackRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "目标跟踪参数不合法", err)
		return
	}
	mode, err := manscdp.ParseTargetTrackMode(request.Mode)
	if err != nil {
		// 直接把 manscdp 的措辞当消息透出（同 app/controllers/user.go 的做法）：
		// 这些文案是协议层手写的中文，不含内部细节，而"到底哪一项不合法"
		// 正是前端排障唯一需要的信息 —— 统一回一句"参数不合法"会让前端只能靠猜。
		dc.FailAndAbort(c, err.Error(), err)
		return
	}
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	area := targetTrackArea(request.Area)
	// 参数先自检一遍再落库：`TrackTarget` 里还有一次（那是协议层的最终防线），
	// 这里提前挡住能让"手动跟踪没带框"这类调用方 bug 不进 operation 表。
	// 同样把具体原因透出（见上文模式解析处的说明）。
	if err := manscdp.ValidateTargetTrackCommand(manscdp.TargetTrackCommand{Mode: mode, DeviceID2: request.DeviceID2, Area: area}); err != nil {
		dc.FailAndAbort(c, err.Error(), err)
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
	operation, err := service.TrackTarget(c.Request.Context(), target, ptz.TargetTrackRequest{
		Mode: mode, DeviceID2: request.DeviceID2, Area: area,
	}, actorID, actorDeptID, key)
	if err != nil {
		if failure := homePositionOperationFailure(err); failure != nil {
			writeHomePositionFailure(c, failure)
			return
		}
		dc.FailAndAbort(c, "下发目标跟踪失败", err)
		return
	}

	// 带上落库后的意图一起返回：前端不必再打一次读接口，
	// 也就不会出现"下发成功但界面还显示上一条"的中间态。
	payload := deviceControlPayload(operation, operation.Action, false)
	var intent gbmodels.GbDeviceTargetTrack
	read := dc.db().WithContext(c.Request.Context()).
		Where("device_id = ? AND target_code = ?", operation.DeviceID, operation.TargetCode).Limit(1).Find(&intent)
	if read.Error == nil && read.RowsAffected == 1 {
		payload["intent"] = &intent
	}
	payload["deviceAcknowledged"] = false
	payload["windowHint"] = TargetTrackWindowHint
	dc.Success(c, payload)
}

// targetTrackArea 把请求体里的框选区域转成协议层类型；没传就返回 nil。
//
// ⛔ 返回 nil 而不是"六项全 0 的结构体"：全 0 的 TargetArea 会被序列化成
// `<TargetArea><Length>0</Length>…`，设备那边是"平台给了一个原点上的框"，
// 与"平台没给过框"完全不是一回事。
func targetTrackArea(request *targetTrackAreaRequest) *manscdp.TargetTrackArea {
	if request == nil {
		return nil
	}
	return &manscdp.TargetTrackArea{
		Length:    request.Length,
		Width:     request.Width,
		MidPointX: request.MidPointX,
		MidPointY: request.MidPointY,
		LengthX:   request.LengthX,
		LengthY:   request.LengthY,
	}
}
