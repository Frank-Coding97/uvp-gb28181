package handler

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// PlayStopper 由点播 service 实现:停掉一路流(BYE + closeRtpServer)
// 接口化便于 hook 端点接入,且让 handler 包不依赖 play 包(避免循环导入)
type PlayStopper interface {
	Stop(ctx context.Context, streamID string) error
}

// HookController 接收 ZLMediaKit 的 Hook 回调
// ZLM 以 POST JSON 调用,响应需返回 {"code":0,"msg":"success"}
type HookController struct {
	notifier *stream.Notifier // 流就绪事件分发(由点播 service 订阅,T6 创新3)
	stopper  PlayStopper      // 无人观看/超时时调用,可为 nil(降级:仅返回 close=true,不发 BYE)
}

func NewHookController(notifier *stream.Notifier) *HookController {
	return &HookController{notifier: notifier}
}

// SetPlayStopper 注入点播停止器(bootstrap 装配 play service 后调用)
func (h *HookController) SetPlayStopper(s PlayStopper) {
	h.stopper = s
}

// hookOK ZLM 期望的标准成功响应
func hookOK(c *gin.Context) {
	c.JSON(200, gin.H{"code": 0, "msg": "success"})
}

// onStreamChangedBody on_stream_changed 回调载荷(只取我们需要的字段)
type onStreamChangedBody struct {
	App    string `json:"app"`
	Stream string `json:"stream"`
	Regist bool   `json:"regist"`
	Schema string `json:"schema"`
}

// OnStreamChanged 流注册/注销事件(regist=true 流就绪,false 流消失)
func (h *HookController) OnStreamChanged(c *gin.Context) {
	var body onStreamChangedBody
	_ = c.ShouldBindJSON(&body)
	app.ZapLog.Info("ZLM Hook on_stream_changed",
		zap.String("app", body.App),
		zap.String("stream", body.Stream),
		zap.String("schema", body.Schema),
		zap.Bool("regist", body.Regist))

	// 流就绪 → 通知正在 WaitReady 的点播 service
	if body.Regist && h.notifier != nil && body.Stream != "" {
		h.notifier.Publish(body.Stream)
	}
	hookOK(c)
}

// onStreamNoneReaderBody on_stream_none_reader 回调载荷
type onStreamNoneReaderBody struct {
	App    string `json:"app"`
	Stream string `json:"stream"`
	Schema string `json:"schema"`
}

// OnStreamNoneReader 无人观看 → ZLM 询问是否关流
// 返回 close=true 让 ZLM 立即关流;同时异步向设备发 BYE 释放上行
func (h *HookController) OnStreamNoneReader(c *gin.Context) {
	var body onStreamNoneReaderBody
	_ = c.ShouldBindJSON(&body)
	app.ZapLog.Info("ZLM Hook on_stream_none_reader",
		zap.String("app", body.App), zap.String("stream", body.Stream))

	// 异步停播:发 BYE + closeRtpServer。即便失败也告知 ZLM 关流,避免端口悬挂
	if h.stopper != nil && body.Stream != "" {
		go func(streamID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.stopper.Stop(ctx, streamID); err != nil {
				app.ZapLog.Warn("无人观看自动断流失败",
					zap.String("stream", streamID), zap.Error(err))
			} else {
				app.ZapLog.Info("无人观看自动断流", zap.String("stream", streamID))
			}
		}(body.Stream)
	}

	c.JSON(200, gin.H{"code": 0, "close": true})
}

// onRtpServerTimeoutBody on_rtp_server_timeout 回调载荷
type onRtpServerTimeoutBody struct {
	StreamID string `json:"stream_id"`
	App      string `json:"app"`
	SSRC     string `json:"ssrc"`
}

// OnRtpServerTimeout RTP 收流超时 → 设备实际没推流,清理会话
func (h *HookController) OnRtpServerTimeout(c *gin.Context) {
	var body onRtpServerTimeoutBody
	_ = c.ShouldBindJSON(&body)
	app.ZapLog.Info("ZLM Hook on_rtp_server_timeout",
		zap.String("stream_id", body.StreamID), zap.String("ssrc", body.SSRC))

	if h.stopper != nil && body.StreamID != "" {
		go func(streamID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.stopper.Stop(ctx, streamID); err != nil {
				app.ZapLog.Warn("RTP 超时清理会话失败",
					zap.String("stream", streamID), zap.Error(err))
			}
		}(body.StreamID)
	}
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
