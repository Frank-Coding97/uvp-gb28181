package handler

import (
	"context"
	"time"

	"github.com/emiago/sipgo/sip"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// MessageHandler 处理 MESSAGE(MANSCDP):本期处理 Keepalive 心跳
type MessageHandler struct {
	onlineTTL time.Duration
}

// NewMessageHandler 创建消息处理器
func NewMessageHandler(cfg gbconfig.Config) *MessageHandler {
	ttl := time.Duration(cfg.Device.KeepaliveInterval*cfg.Device.KeepaliveTimeoutCount) * time.Second
	if ttl <= 0 {
		ttl = 180 * time.Second
	}
	return &MessageHandler{onlineTTL: ttl}
}

// Handle 处理 MESSAGE 请求
func (h *MessageHandler) Handle(req *sip.Request, tx sip.ServerTransaction) {
	// 解析 MANSCDP body(兼容 GB2312/GB18030 编码)
	head, err := manscdp.ParseHead(req.Body())
	if err != nil {
		// 非法/畸形 XML 不报错,回 200 避免设备重发风暴(不更新状态)
		app.ZapLog.Warn("GB28181 MESSAGE 解析失败,忽略", zap.Error(err))
		_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
		return
	}

	if head.IsKeepalive() && head.DeviceID != "" {
		ctx := context.Background()
		if err := device.Keepalive(ctx, head.DeviceID, h.onlineTTL); err != nil {
			app.ZapLog.Error("GB28181 心跳处理失败", zap.String("deviceId", head.DeviceID), zap.Error(err))
		}
	}
	// 其它 CmdType(Catalog/DeviceInfo 等)本期不处理,统一回 200
	_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
}
