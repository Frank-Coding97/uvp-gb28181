package handler

import (
	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// HookController 接收 ZLMediaKit 的 Hook 回调
// ZLM 以 POST JSON 调用,响应需返回 {"code":0,"msg":"success"}
type HookController struct{}

func NewHookController() *HookController {
	return &HookController{}
}

// hookOK ZLM 期望的标准成功响应
func hookOK(c *gin.Context) {
	c.JSON(200, gin.H{"code": 0, "msg": "success"})
}

// OnStreamChanged 流注册/注销事件(regist=true 流就绪,false 流消失)
func (h *HookController) OnStreamChanged(c *gin.Context) {
	var body map[string]interface{}
	_ = c.ShouldBindJSON(&body)
	app.ZapLog.Info("ZLM Hook on_stream_changed",
		zap.Any("app", body["app"]), zap.Any("stream", body["stream"]), zap.Any("regist", body["regist"]))
	// 业务(流就绪通知点播会话)留 T6
	hookOK(c)
}

// OnStreamNoneReader 无人观看 → 应断流(业务留 T7,本期先收)
func (h *HookController) OnStreamNoneReader(c *gin.Context) {
	var body map[string]interface{}
	_ = c.ShouldBindJSON(&body)
	app.ZapLog.Info("ZLM Hook on_stream_none_reader", zap.Any("stream", body["stream"]))
	// 默认不立即关闭(close=false),T7 接入 BYE 逻辑后改 true
	c.JSON(200, gin.H{"code": 0, "close": false})
}

// OnRtpServerTimeout RTP 收流超时
func (h *HookController) OnRtpServerTimeout(c *gin.Context) {
	var body map[string]interface{}
	_ = c.ShouldBindJSON(&body)
	app.ZapLog.Info("ZLM Hook on_rtp_server_timeout", zap.Any("stream_id", body["stream_id"]))
	hookOK(c)
}

// OnPublish 推流鉴权(本期放行)
func (h *HookController) OnPublish(c *gin.Context) {
	hookOK(c)
}

// OnPlay 播放鉴权(本期放行)
func (h *HookController) OnPlay(c *gin.Context) {
	hookOK(c)
}

// OnServerStarted ZLM 启动事件
func (h *HookController) OnServerStarted(c *gin.Context) {
	app.ZapLog.Info("ZLM Hook on_server_started")
	hookOK(c)
}
