package handler

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"

	"go.uber.org/zap"
)

// MessageHandler 处理 MESSAGE(MANSCDP):本期处理 Keepalive 心跳
type MessageHandler struct {
	recorder           metrics.Recorder // 可选:埋点 SIP 事务
	catalogTrigger     CatalogTrigger   // 可选:设备从离线恢复后重新拉 Catalog
	subscriptionWaker  SubscriptionWaker
	alarmProcessor     AlarmMessageProcessor
	ptzProcessor       PTZMessageProcessor
	upgradeMu          sync.RWMutex
	upgradeProcessor   UpgradeMessageProcessor
	recordInfoMu       sync.RWMutex
	recordInfoSink     RecordInfoSink
	snapshotMu         sync.RWMutex
	snapshotSink       SnapshotSink
	playbackEndMu      sync.RWMutex
	playbackEndSink    PlaybackEndSink
	broadcastMu        sync.RWMutex
	broadcastProcessor BroadcastMessageProcessor
}

type AlarmMessageProcessor interface {
	OnAlarmMessage(context.Context, string, string, string, []byte) error
}

type PTZMessageProcessor interface {
	OnPTZMessage(context.Context, string, string, string, []byte) error
}

// UpgradeMessageProcessor gets first routing priority for DeviceControl
// responses and DeviceUpgradeResult notifications. The bool tells the
// handler whether a DeviceControl SN belonged to an upgrade operation and
// therefore must not be handed to PTZ.
type UpgradeMessageProcessor interface {
	OnUpgradeMessage(context.Context, string, string, string, []byte) (bool, error)
}

// UpgradeResultMessageProcessor is an optional stronger boundary for the
// final DeviceUpgradeResult notification. The response status is selected by
// the durable processor: 200 after persistence, 400 for a protocol error, or
// 503 when the platform could not persist the result.
type UpgradeResultMessageProcessor interface {
	OnUpgradeResultMessage(context.Context, string, string, string, []byte) (bool, int, error)
}

// RecordInfoSink receives the original payload after the SIP transaction has
// already been acknowledged. Implementations must keep their own work bounded.
type RecordInfoSink interface {
	OnRecordInfoMessage(context.Context, string, []byte) error
}

type PlaybackEndSink interface {
	OnPlaybackFileToEnd(context.Context, string, string, []byte) error
}

type SnapshotSink interface {
	OnSnapshotNotify(context.Context, string, []byte) error
}

type BroadcastMessageProcessor interface {
	OnBroadcastMessage(context.Context, string, []byte) error
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

// SetSubscriptionWaker 注入设备恢复在线后的订阅唤醒器。
func (h *MessageHandler) SetSubscriptionWaker(w SubscriptionWaker) {
	h.subscriptionWaker = w
}

func (h *MessageHandler) SetAlarmProcessor(processor AlarmMessageProcessor) {
	h.alarmProcessor = processor
}

func (h *MessageHandler) SetPTZProcessor(processor PTZMessageProcessor) {
	h.ptzProcessor = processor
}

// SetUpgradeProcessor installs the upgrade response router. It is safe to
// swap during SIP runtime reloads and is intentionally named after the
// processor role used by the integration boundary.
func (h *MessageHandler) SetUpgradeProcessor(processor UpgradeMessageProcessor) {
	h.upgradeMu.Lock()
	h.upgradeProcessor = processor
	h.upgradeMu.Unlock()
}

// SetUpgradeMessageProcessor is the descriptive alias used by SIP bootstrap.
func (h *MessageHandler) SetUpgradeMessageProcessor(processor UpgradeMessageProcessor) {
	h.SetUpgradeProcessor(processor)
}

func (h *MessageHandler) getUpgradeProcessor() UpgradeMessageProcessor {
	h.upgradeMu.RLock()
	defer h.upgradeMu.RUnlock()
	return h.upgradeProcessor
}

func (h *MessageHandler) SetBroadcastProcessor(processor BroadcastMessageProcessor) {
	h.broadcastMu.Lock()
	h.broadcastProcessor = processor
	h.broadcastMu.Unlock()
}

func (h *MessageHandler) getBroadcastProcessor() BroadcastMessageProcessor {
	h.broadcastMu.RLock()
	defer h.broadcastMu.RUnlock()
	return h.broadcastProcessor
}

func (h *MessageHandler) SetRecordInfoSink(sink RecordInfoSink) {
	h.recordInfoMu.Lock()
	h.recordInfoSink = sink
	h.recordInfoMu.Unlock()
}

func (h *MessageHandler) getRecordInfoSink() RecordInfoSink {
	h.recordInfoMu.RLock()
	defer h.recordInfoMu.RUnlock()
	return h.recordInfoSink
}

func (h *MessageHandler) SetSnapshotSink(sink SnapshotSink) {
	h.snapshotMu.Lock()
	h.snapshotSink = sink
	h.snapshotMu.Unlock()
}

func (h *MessageHandler) getSnapshotSink() SnapshotSink {
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	return h.snapshotSink
}

func (h *MessageHandler) SetPlaybackEndSink(sink PlaybackEndSink) {
	h.playbackEndMu.Lock()
	h.playbackEndSink = sink
	h.playbackEndMu.Unlock()
}

func (h *MessageHandler) getPlaybackEndSink() PlaybackEndSink {
	h.playbackEndMu.RLock()
	defer h.playbackEndMu.RUnlock()
	return h.playbackEndSink
}

func isPlaybackFileToEnd(body []byte) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(string(body)), ""))
	return strings.Contains(normalized, "filetoend") ||
		strings.Contains(normalized, "file-end") ||
		strings.Contains(normalized, "file_end")
}

// txKindFromCmd 根据 MANSCDP CmdType 映射 metrics 事务类型
func txKindFromCmd(cmd string) metrics.TxKind {
	switch cmd {
	case manscdp.CmdKeepalive:
		return metrics.TxKeepalive
	case manscdp.CmdCatalog:
		return metrics.TxCatalog
	case manscdp.CmdRecordInfo:
		return metrics.TxRecord
	case manscdp.CmdDeviceControl:
		return metrics.TxPTZ
	case manscdp.CmdDeviceStatus, manscdp.CmdPTZPreciseCtrl, manscdp.CmdPTZPosition, manscdp.CmdPresetQuery, manscdp.CmdHomePositionQuery, manscdp.CmdCruiseTrackListQuery, manscdp.CmdCruiseTrackQuery, manscdp.CmdPTZPreciseStatusQuery:
		return metrics.TxPTZ
	case "Alarm":
		return metrics.TxAlarm
	}
	return metrics.TxUnknown
}

// Handle 处理 MESSAGE 请求
func (h *MessageHandler) Handle(req *sip.Request, tx sip.ServerTransaction) {
	callID, cseq := sipPairKey(req)
	ctx := context.Background()
	logger := app.Log(ctx).Named("gb28181.message")
	// 解析 MANSCDP body(兼容 GB2312/GB18030 编码)
	head, err := manscdp.ParseHead(req.Body())
	if err != nil {
		// A malformed final upgrade notification must be retried/fixed by the
		// device and therefore gets 400. Other legacy malformed MESSAGE bodies
		// retain the historical 200 response to avoid a retry storm.
		normalizedBody := strings.ToLower(strings.Join(strings.Fields(string(req.Body())), ""))
		status := 200
		reason := "OK"
		if strings.Contains(normalizedBody, "<cmdtype>deviceupgraderesult</cmdtype>") {
			status = http.StatusBadRequest
			reason = http.StatusText(status)
		}
		logger.Warn("GB28181 MESSAGE 解析失败,忽略",
			zap.String("event", "gb28181.message.parse_failed"),
			zap.String("call_id", callID), zap.String("cseq", cseq), logging.Error(err))
		_ = tx.Respond(sip.NewResponseFromRequest(req, status, reason, nil))
		return
	}

	kind := txKindFromCmd(head.CmdType)
	responseSent := false
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
	if head.CmdType == manscdp.CmdDeviceControl || head.CmdType == manscdp.CmdDeviceUpgradeResult {
		processor := h.getUpgradeProcessor()
		if head.CmdType == manscdp.CmdDeviceUpgradeResult && processor == nil {
			// During a SIP runtime detach the final result must be retried by
			// the device; a generic 200 would acknowledge and lose it.
			_ = tx.Respond(sip.NewResponseFromRequest(req, http.StatusServiceUnavailable, http.StatusText(http.StatusServiceUnavailable), nil))
			responseSent = true
			if h.recorder != nil && kind != metrics.TxUnknown && callID != "" {
				h.recorder.End(callID, cseq, http.StatusServiceUnavailable, false)
			}
			return
		}
		if processor != nil {
			if head.CmdType == manscdp.CmdDeviceUpgradeResult {
				if resultProcessor, ok := processor.(UpgradeResultMessageProcessor); ok {
					// A final result is acknowledged only after its durable
					// state transition has succeeded. The processor maps syntax
					// errors to 400 and persistence failures to 503.
					consumed, status, processErr := resultProcessor.OnUpgradeResultMessage(ctx, ptzDeviceCode(req, head.DeviceID), callID, cseq, req.Body())
					if status < 100 {
						status = http.StatusOK
					}
					reason := http.StatusText(status)
					if reason == "" {
						reason = "OK"
					}
					_ = tx.Respond(sip.NewResponseFromRequest(req, status, reason, nil))
					responseSent = true
					if processErr != nil {
						logger.Warn("GB28181 设备升级最终结果处理失败",
							zap.String("event", "gb28181.message.upgrade_result_failed"),
							zap.String("device_id", head.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
							zap.Int("sip_status", status), logging.Error(processErr))
					}
					if h.recorder != nil && kind != metrics.TxUnknown && callID != "" {
						h.recorder.End(callID, cseq, status, processErr == nil)
					}
					// DeviceUpgradeResult has no PTZ fallback. An unmatched
					// result is still fully handled by the generic SIP 200.
					_ = consumed
					return
				}
			}
			// Ordinary DeviceControl keeps the fast transport ACK before
			// durable work; this also remains the compatibility path for a
			// processor that has not implemented the stronger final-result
			// boundary.
			_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
			responseSent = true
			consumed, processErr := processor.OnUpgradeMessage(ctx, ptzDeviceCode(req, head.DeviceID), callID, cseq, req.Body())
			if consumed || processErr != nil {
				if processErr != nil {
					logger.Warn("GB28181 设备升级响应处理失败",
						zap.String("event", "gb28181.message.upgrade_failed"),
						zap.String("device_id", head.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
						logging.Error(processErr))
				}
				if h.recorder != nil && kind != metrics.TxUnknown && callID != "" {
					h.recorder.End(callID, cseq, 200, processErr == nil)
				}
				return
			}
		}
	}

	if head.DeviceID != "" {
		if head.CmdType == manscdp.CmdBroadcast {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
			if processor := h.getBroadcastProcessor(); processor != nil {
				if err := processor.OnBroadcastMessage(ctx, ptzDeviceCode(req, head.DeviceID), req.Body()); err != nil {
					logger.Warn("GB28181 Broadcast Response 处理失败",
						zap.String("event", "gb28181.message.broadcast_failed"),
						zap.String("device_id", head.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
						logging.Error(err))
				}
			}
			return
		}
		if head.CmdType == manscdp.CmdNotify && manscdp.IsSnapshotNotify(req.Body()) {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
			if sink := h.getSnapshotSink(); sink != nil {
				if err := sink.OnSnapshotNotify(ctx, ptzDeviceCode(req, head.DeviceID), req.Body()); err != nil {
					logger.Warn("GB28181 SnapShot Notify 处理失败",
						zap.String("event", "gb28181.message.snapshot_failed"),
						zap.String("device_id", head.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
						logging.Error(err))
				}
			}
			return
		}
		if isPlaybackFileToEnd(req.Body()) {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
			if h.recorder != nil && kind != metrics.TxUnknown && callID != "" {
				h.recorder.End(callID, cseq, 200, true)
			}
			if sink := h.getPlaybackEndSink(); sink != nil {
				if err := sink.OnPlaybackFileToEnd(ctx, callID, ptzDeviceCode(req, head.DeviceID), req.Body()); err != nil {
					logger.Warn("GB28181 回放自然结束处理失败",
						zap.String("event", "gb28181.message.playback_end_failed"),
						zap.String("device_id", head.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
						logging.Error(err))
				}
			}
			return
		}
		if head.CmdType == manscdp.CmdRecordInfo {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
			if h.recorder != nil && kind != metrics.TxUnknown && callID != "" {
				h.recorder.End(callID, cseq, 200, true)
			}
			sink := h.getRecordInfoSink()
			if sink == nil {
				logger.Warn("GB28181 RecordInfo sink 未装配,忽略响应",
					zap.String("event", "gb28181.message.record_info_sink_unavailable"),
					zap.String("device_id", head.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq))
				return
			}
			if err := sink.OnRecordInfoMessage(ctx, ptzDeviceCode(req, ""), req.Body()); err != nil {
				logger.Warn("GB28181 RecordInfo 响应处理失败",
					zap.String("event", "gb28181.message.record_info_failed"),
					zap.String("device_id", head.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
					logging.Error(err))
			}
			return
		}
		switch head.CmdType {
		case manscdp.CmdKeepalive:
			restored, err := device.Keepalive(ctx, head.DeviceID)
			if err != nil {
				logger.Error("GB28181 心跳处理失败",
					zap.String("event", "gb28181.message.keepalive_failed"),
					zap.String("device_id", head.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
					logging.Error(err))
			} else if restored {
				if h.subscriptionWaker != nil {
					if err := h.subscriptionWaker.WakeDeviceByCode(ctx, head.DeviceID); err != nil {
						logger.Warn("GB28181 设备恢复订阅失败",
							zap.String("event", "gb28181.message.subscription_wake_failed"),
							zap.String("device_id", head.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
							logging.Error(err))
					}
				}
				if h.catalogTrigger != nil && req.Source() != "" && gbconfig.SyncChannelsOnOnline() {
					// 离线时通道已统一置 OFF。设备恢复只证明 SIP 可达,
					// 通道必须等待新的 Catalog ON/OFF 后再恢复。
					h.catalogTrigger.Trigger(ctx, head.DeviceID, req.Source(), req.Transport())
				}
			}
		case manscdp.CmdCatalog:
			// Catalog 应答(设备→平台),解析通道入库
			HandleCatalogResponse(ctx, req.Body())
		case manscdp.CmdDeviceInfo:
			// DeviceInfo 应答(设备→平台),回写 gb_device 本体元数据
			HandleDeviceInfoResponse(ctx, req.Body())
		case manscdp.CmdDeviceControl:
			if h.ptzProcessor != nil {
				if err := h.ptzProcessor.OnPTZMessage(ctx, ptzDeviceCode(req, head.DeviceID), callID, cseq, req.Body()); err != nil {
					logger.Warn("GB28181 PTZ DeviceControl 应答处理失败",
						zap.String("event", "gb28181.message.ptz_failed"),
						zap.String("operation", "device_control"), zap.String("device_id", head.DeviceID),
						zap.String("call_id", callID), zap.String("cseq", cseq), logging.Error(err))
				}
			}
		case manscdp.CmdDeviceStatus, manscdp.CmdPTZPreciseCtrl, manscdp.CmdPTZPosition, manscdp.CmdPresetQuery, manscdp.CmdHomePositionQuery, manscdp.CmdCruiseTrackListQuery, manscdp.CmdCruiseTrackQuery, manscdp.CmdPTZPreciseStatusQuery:
			if h.ptzProcessor != nil {
				if err := h.ptzProcessor.OnPTZMessage(ctx, ptzDeviceCode(req, head.DeviceID), callID, cseq, req.Body()); err != nil {
					logger.Warn("GB28181 PTZ 查询应答处理失败",
						zap.String("event", "gb28181.message.ptz_failed"),
						zap.String("operation", "status_query"), zap.String("device_id", head.DeviceID),
						zap.String("call_id", callID), zap.String("cseq", cseq), logging.Error(err))
				}
			}
		case manscdp.CmdAlarm:
			if h.alarmProcessor != nil {
				if err := h.alarmProcessor.OnAlarmMessage(ctx, head.DeviceID, callID, cseq, req.Body()); err != nil {
					logger.Warn("GB28181 MESSAGE 报警处理失败",
						zap.String("event", "gb28181.message.alarm_failed"),
						zap.String("device_id", head.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
						logging.Error(err))
				}
			}
		}
	}
	// 其它 CmdType 本期不处理,统一回 200
	if !responseSent {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	}

	if h.recorder != nil && kind != metrics.TxUnknown && callID != "" {
		h.recorder.End(callID, cseq, 200, true)
	}
}

func ptzDeviceCode(req *sip.Request, fallback string) string {
	if req != nil {
		if from := req.From(); from != nil && from.Address.User != "" {
			return from.Address.User
		}
	}
	return fallback
}
