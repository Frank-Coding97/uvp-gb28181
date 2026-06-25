package handler

import (
	"context"
	"time"

	"github.com/emiago/sipgo/sip"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// MessageHandler 处理 MESSAGE(MANSCDP):本期处理 Keepalive 心跳
type MessageHandler struct {
	recorder metrics.Recorder // 可选:埋点 SIP 事务
}

// NewMessageHandler 创建消息处理器
func NewMessageHandler(cfg gbconfig.Config) *MessageHandler {
	return &MessageHandler{}
}

// SetRecorder 注入指标 Recorder(可选)
func (h *MessageHandler) SetRecorder(r metrics.Recorder) {
	h.recorder = r
}

// txKindFromCmd 根据 MANSCDP CmdType 映射 metrics 事务类型
func txKindFromCmd(cmd string) metrics.TxKind {
	switch cmd {
	case manscdp.CmdKeepalive:
		return metrics.TxKeepalive
	case manscdp.CmdCatalog:
		return metrics.TxCatalog
	case "Alarm":
		return metrics.TxAlarm
	}
	return metrics.TxUnknown
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

	kind := txKindFromCmd(head.CmdType)
	callID, cseq := sipPairKey(req)
	// 已知 Kind 的入向事件:Begin + End 一起打(瞬时事务,server 端立刻应答)
	// Catalog Response 也走入向计数,即便 UAC 端没埋点也至少有一条
	if h.recorder != nil && kind != metrics.TxUnknown && callID != "" {
		h.recorder.Begin(metrics.Transaction{
			Kind:      kind,
			Direction: metrics.DirIn,
			CallID:    callID,
			CSeq:      cseq,
			DeviceID:  head.DeviceID,
			StartedAt: time.Now(),
		})
	}

	if head.DeviceID != "" {
		ctx := context.Background()
		switch head.CmdType {
		case manscdp.CmdKeepalive:
			if err := device.Keepalive(ctx, head.DeviceID); err != nil {
				app.ZapLog.Error("GB28181 心跳处理失败", zap.String("deviceId", head.DeviceID), zap.Error(err))
			}
		case manscdp.CmdCatalog:
			// Catalog 应答(设备→平台),解析通道入库
			HandleCatalogResponse(ctx, req.Body())
		}
	}
	// 其它 CmdType 本期不处理,统一回 200
	_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))

	if h.recorder != nil && kind != metrics.TxUnknown && callID != "" {
		h.recorder.End(callID, cseq, 200, true)
	}
}
