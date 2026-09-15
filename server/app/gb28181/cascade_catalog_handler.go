package gb28181

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// `cascade.control.forward_failed` 的 reason_code —— 受控短码，不是自由文本。
// 语义：失败发生在"命令还没到通道之前"还是"转发被拒"。
const (
	// PTZ 服务未就绪（本进程自身状态），级联侧无需重试同样的命令。
	cascadeControlReasonPTZUnavailable = "ptz_service_unavailable"
	// 已交给 control.Service.Forward 但被拒（通道未共享给该平台、PTZ 权限关闭、
	// 目标设备离线等）。具体原因看 error 字段。
	cascadeControlReasonForwardRejected = "forward_rejected"
)

// cascadeCallID 取本次 SIP 事务的 Call-ID。级联是纯 SIP 链路，没有 HTTP
// request id 可以继承，能把它和 SIP 报文对上的关联键只有 Call-ID
// （`gb_sip_trace_message` 也是按它串的）。
//
// 这里刻意返回 string 由调用点内联进 `zap.String`，而不是包成
// `func(...) zap.Field`：字段构造函数会让**扫描脚本**看不见这个字段名
// （它只认字面量形式的 `zap.String("call_id", ...)`），于是这条日志在
// 「定位字段覆盖」指标里被算成无定位 —— 治理工具的假阴性比字段本身更麻烦。
func cascadeCallID(req *sip.Request) string {
	if req == nil {
		return ""
	}
	if header := req.CallID(); header != nil {
		return header.Value()
	}
	return ""
}

// cascadePeerLabel 是本次报文实际观测到的对端身份
// （源地址:端口/传输层 + From 用户）。认定平台靠的正是它，所以"认不出来"时
// 它更是唯一的身份线索。`cascadePeer.String()` 对残缺报文返回 "unparsed"，
// 不会留下空值冒充"已带定位字段"。
func cascadePeerLabel(req *sip.Request) string {
	peer, _ := cascadePeerFromRequest(req)
	return peer.String()
}

func matchCascadeCatalogPeer(req *sip.Request, p model.GbCascadePlatform) bool {
	host, port, err := net.SplitHostPort(req.Source())
	if err != nil {
		return false
	}
	return req.From() != nil && req.To() != nil && req.From().Address.User == p.UpstreamServerID && req.To().Address.User == p.LocalDeviceID &&
		strings.EqualFold(host, p.Host) && port == strconv.Itoa(p.Port) && strings.EqualFold(req.Transport(), p.Transport)
}

func matchCascadeControlPeer(req *sip.Request, p model.GbCascadePlatform) bool {
	host, port, err := net.SplitHostPort(req.Source())
	if err != nil {
		return false
	}
	return req.From() != nil && req.From().Address.User == p.UpstreamServerID &&
		strings.EqualFold(host, p.Host) && port == strconv.Itoa(p.Port) && strings.EqualFold(req.Transport(), p.Transport)
}

func findCascadeControlPeer(req *sip.Request, platforms []model.GbCascadePlatform) (*model.GbCascadePlatform, bool) {
	var matched *model.GbCascadePlatform
	for i := range platforms {
		p := &platforms[i]
		if !matchCascadeControlPeer(req, *p) {
			continue
		}
		if matched != nil {
			return nil, true
		}
		matched = p
	}
	return matched, false
}

// buildCascadeCatalogSnapshot builds the channel-only catalog snapshot for one
// upstream platform, restricted to the resources explicitly shared with it.
// Used by the query responder and the proactive manual push.
func buildCascadeCatalogSnapshot(ctx context.Context, store *repository.GormRepository, platform *model.GbCascadePlatform) (catalog.Snapshot, error) {
	projection, err := store.ProjectionSnapshot(ctx, platform.ID)
	if err != nil {
		return catalog.Snapshot{}, err
	}
	facts := catalog.SourceFacts{DeviceOnline: map[uint64]bool{}, ChannelOnline: map[uint64]bool{}}
	// Read only the resources selected for this upstream, never the entire device catalog.
	type statusRow struct {
		ID     uint64
		Status int
	}
	var deviceIDs, channelIDs []uint64
	for _, d := range projection.Devices {
		deviceIDs = append(deviceIDs, d.SourceDeviceID)
	}
	for _, c := range projection.Channels {
		channelIDs = append(channelIDs, c.SourceChannelID)
	}
	var rows []statusRow
	if len(deviceIDs) > 0 {
		if err = app.DB().WithContext(ctx).Table("gb_device").Select("id,status").Where("id IN ?", deviceIDs).Find(&rows).Error; err != nil {
			return catalog.Snapshot{}, err
		}
		for _, r := range rows {
			facts.DeviceOnline[r.ID] = r.Status == 1
		}
	}
	rows = nil
	if len(channelIDs) > 0 {
		if err = app.DB().WithContext(ctx).Table("gb_channel").Select("id,status").Where("id IN ?", channelIDs).Find(&rows).Error; err != nil {
			return catalog.Snapshot{}, err
		}
		for _, r := range rows {
			facts.ChannelOnline[r.ID] = r.Status == 1
		}
	}
	snapshot, err := catalog.Build(*projection, facts)
	if err != nil {
		return catalog.Snapshot{}, err
	}
	return snapshot.ChannelsOnly(), nil
}

func newCascadeCatalogHandler(store *repository.GormRepository, clients *cascadePlatformClientFactory) func(*sip.Request, sip.ServerTransaction) bool {
	return func(req *sip.Request, tx sip.ServerTransaction) bool {
		var head struct {
			XMLName xml.Name
			CmdType string `xml:"CmdType"`
		}
		if manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), req.Body(), &head) != nil || head.XMLName.Local != "Query" || head.CmdType != "Catalog" {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		fail := func(code int, err error) bool {
			_ = tx.Respond(sip.NewResponseFromRequest(req, code, "Catalog query rejected", nil))
			// 这里拿不到 platform_id：多数失败（多个候选 / 认不出上游 / 平台被禁用）
			// 恰恰发生在认定平台之前。所以身份靠 call_id + peer 承担，
			// 不要为了"看起来有定位"而塞一个 platform_id=0 进去。
			app.Log(ctx).Warn("级联目录查询失败",
				zap.String("event", "cascade.catalog.query_failed"),
				zap.String("call_id", cascadeCallID(req)),
				zap.String("peer", cascadePeerLabel(req)),
				zap.Int("status", code),
				zap.Error(err))
			return true
		}
		platforms, err := store.ListPlatforms(ctx)
		if err != nil {
			return fail(500, err)
		}
		var matched *model.GbCascadePlatform
		for i := range platforms {
			p := &platforms[i]
			if matchCascadeCatalogPeer(req, *p) {
				if matched != nil {
					return fail(403, fmt.Errorf("ambiguous upstream"))
				}
				matched = p
			}
		}
		if matched == nil || !matched.Enabled {
			return fail(403, fmt.Errorf("unrecognized or disabled upstream"))
		}
		snapshot, err := buildCascadeCatalogSnapshot(ctx, store, matched)
		if err != nil {
			return fail(500, err)
		}
		client, err := clients.NewClient(*matched)
		if err != nil {
			return fail(503, err)
		}
		outbound := client.(*cascadePlatformClient)
		var fits func([]byte) bool
		if strings.EqualFold(matched.Transport, "UDP") {
			fits = outbound.transactions.MessageFitsUDP
		}
		responses, err := catalog.PlanCatalogResponsesWithinLimit(protocol.ProfileFor(effectiveCascadeProfile(*matched)), matched.LocalDeviceID, snapshot, matched.CatalogBatchSize, req.Body(), fits)
		if err != nil {
			return fail(400, err)
		}
		if err = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)); err != nil {
			return true
		}
		for _, response := range responses {
			result := outbound.transactions.SendMessage(ctx, response.Body, fmt.Sprintf("catalog-%d-%d", matched.ID, time.Now().UnixNano()))
			if !result.Success {
				app.Log(ctx).Warn("级联目录响应发送失败",
					zap.String("event", "cascade.catalog.response_send_failed"),
					zap.Uint64("platform_id", matched.ID),
					zap.Int("status", result.StatusCode),
					// TransportErr 与 BuildErr 互斥（SendMessage 在 build 失败时直接返回），
					// 合成一个 error 字段 —— 两个 zap.Error 会写出两个同名 "error" 键。
					zap.Error(errors.Join(result.TransportErr, result.BuildErr)))
				return true
				return true
			}
		}
		app.Log(ctx).Info("级联目录响应完成",
			zap.String("event", "cascade.catalog.response_completed"),
			zap.Uint64("platform_id", matched.ID),
			zap.Int("items", len(snapshot.Items)),
			zap.Int("batches", len(responses)))
		return true
	}
}

// newCascadeMessageHandler handles inbound catalog queries and cascaded PTZ
// controls before the ordinary device-response handler. A Control message is
// an instruction from the upstream platform, not a response to a local PTZ
// operation; sending it through ptz.OnPTZMessage would therefore always log
// an unmatched response.
func newCascadeMessageHandler(store *repository.GormRepository, clients *cascadePlatformClientFactory) func(*sip.Request, sip.ServerTransaction) bool {
	catalogHandler := newCascadeCatalogHandler(store, clients)
	return func(req *sip.Request, tx sip.ServerTransaction) bool {
		if catalogHandler(req, tx) {
			return true
		}
		head, err := manscdp.ParseHead(req.Body())
		if err != nil || head.CmdType != manscdp.CmdDeviceControl || !strings.Contains(string(req.Body()), "<Control") {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		platforms, err := store.ListPlatforms(ctx)
		if err != nil {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 500, "Cascade Control Failed", nil))
			return true
		}
		matched, ambiguous := findCascadeControlPeer(req, platforms)
		if matched == nil && !ambiguous {
			return false
		}
		if ambiguous {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 403, "Ambiguous Cascade Platform", nil))
			return true
		}
		if matched == nil || !matched.Enabled {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 403, "Cascade Platform Not Available", nil))
			return true
		}
		// PTZ is a no-response device control in GB/T 28181-2016. Acknowledge the
		// upstream MESSAGE immediately, then forward it to the shared channel.
		if err := tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)); err != nil {
			return true
		}
		callID := ""
		if header := req.CallID(); header != nil {
			callID = header.Value()
		}
		platform := *matched
		body := append([]byte(nil), req.Body()...)
		go forwardCascadeControl(store, platform, callID, body, *head)
		return true
	}
}

func forwardCascadeControl(store *repository.GormRepository, platform model.GbCascadePlatform, callID string, body []byte, head manscdp.MessageHead) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if ptzService == nil {
		logCascadeControlForwardFailed(ctx, platform.ID, head.DeviceID, cascadeControlReasonPTZUnavailable, nil)
		return
	}
	service := control.NewService(store, control.NewGormTargetLoader(app.DB()), ptzService)
	if _, err := service.Forward(ctx, control.ForwardRequest{PlatformID: platform.ID, CallID: callID, Body: body}); err != nil {
		logCascadeControlForwardFailed(ctx, platform.ID, head.DeviceID, cascadeControlReasonForwardRejected, err)
	}
}

// logCascadeControlForwardFailed 是 `cascade.control.forward_failed` 的唯一出口。
//
// 这条事件原先有两个写法（一处 `reason="PTZ service unavailable"`、一处 `error=<err>`）。
// **同名事件两种字段集**会让聚合统计与排障都不确定该读哪个键，所以合并成一条：
// `reason_code` 说"哪一类失败"（受控短码），`error` 说"具体错在哪"（失败在转发时才有）。
func logCascadeControlForwardFailed(ctx context.Context, platformID uint64, channelID, reasonCode string, err error) {
	app.Log(ctx).Warn("级联云台命令转发失败",
		zap.String("event", "cascade.control.forward_failed"),
		zap.Uint64("platform_id", platformID),
		zap.String("channel_id", channelID),
		zap.String("reason_code", reasonCode),
		zap.Error(err))
}
