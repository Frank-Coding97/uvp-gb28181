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
	recorder       metrics.Recorder // 可选:埋点 SIP 事务
	catalogTrigger CatalogTrigger   // 可选:设备从离线恢复后重新拉 Catalog
	alarmProcessor AlarmMessageProcessor
}

type AlarmMessageProcessor interface {
	OnAlarmMessage(context.Context, string, string, string, []byte) error
}

// NewMessageHandler 创建消息处理器
func NewMessageHandler(cfg gbconfig.Config) *MessageHandler {
	return &MessageHandler{}
}

// SetRecorder 注入指标 Recorder(可选)
func (h *MessageHandler) SetRecorder(r metrics.Recorder) {
	h.recorder = r
}

// SetCatalogTrigger 注入设备重新上线后的 Catalog 触发器。
func (h *MessageHandler) SetCatalogTrigger(t CatalogTrigger) {
	h.catalogTrigger = t
}

func (h *MessageHandler) SetAlarmProcessor(processor AlarmMessageProcessor) {
	h.alarmProcessor = processor
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
			restored, err := device.Keepalive(ctx, head.DeviceID)
			if err != nil {
				app.ZapLog.Error("GB28181 心跳处理失败", zap.String("deviceId", head.DeviceID), zap.Error(err))
			} else if restored && h.catalogTrigger != nil && req.Source() != "" {
				// 离线时通道已统一置 OFF。设备恢复只证明 SIP 可达,
				// 通道必须等待新的 Catalog ON/OFF 后再恢复。
				h.catalogTrigger.Trigger(ctx, head.DeviceID, req.Source(), req.Transport())
			}
		case manscdp.CmdCatalog:
			// Catalog 应答(设备→平台),解析通道入库
			HandleCatalogResponse(ctx, req.Body())
		case manscdp.CmdDeviceInfo:
			// DeviceInfo 应答(设备→平台),回写 gb_device 本体元数据
			HandleDeviceInfoResponse(ctx, req.Body())
		case manscdp.CmdAlarm:
			if h.alarmProcessor != nil {
				if err := h.alarmProcessor.OnAlarmMessage(ctx, head.DeviceID, callID, cseq, req.Body()); err != nil {
					app.ZapLog.Warn("GB28181 MESSAGE 报警处理失败", zap.String("deviceId", head.DeviceID), zap.Error(err))
				}
			}
		}
	}
	// 其它 CmdType 本期不处理,统一回 200
	_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))

	if h.recorder != nil && kind != metrics.TxUnknown && callID != "" {
		h.recorder.End(callID, cseq, 200, true)
	}
}
