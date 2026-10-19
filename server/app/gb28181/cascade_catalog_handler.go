package gb28181

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/civilcode"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/global/app"
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
	if !matchCascadeCatalogPeerIdentity(req, p) {
		return false
	}
	return req.To() != nil &&
		(req.To().Address.User == p.LocalDeviceID || (p.PublishCivil && cascadeCivilTarget(req.To().Address.User)))
}

func matchCascadeCatalogPeerIdentity(req *sip.Request, p model.GbCascadePlatform) bool {
	peer, ok := cascadePeerFromRequest(req)
	return ok && peer.match(p)
}

func cascadeCivilTarget(id string) bool {
	if len(id) != 2 && len(id) != 4 && len(id) != 6 && len(id) != 8 {
		return false
	}
	for _, digit := range id {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return !strings.HasPrefix(id, "00")
}

func matchCascadeControlPeer(req *sip.Request, p model.GbCascadePlatform) bool {
	peer, ok := cascadePeerFromRequest(req)
	return ok && peer.match(p)
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

// buildCascadeCatalogSnapshot builds the publication snapshot for one
// upstream platform, restricted to the resources explicitly shared with it.
// Used by the query responder and the proactive manual push.
func buildCascadeCatalogSnapshot(ctx context.Context, store *repository.GormRepository, platform *model.GbCascadePlatform) (catalog.Snapshot, error) {
	projection, err := store.ProjectionSnapshot(ctx, platform.ID)
	if err != nil {
		return catalog.Snapshot{}, err
	}
	facts := catalog.SourceFacts{DeviceOnline: map[uint64]bool{}, ChannelOnline: map[uint64]bool{}, ChannelMetadata: map[uint64]catalog.ChannelMetadata{}}
	// Read only the resources selected for this upstream, never the entire device catalog.
	type statusRow struct {
		ID     uint64
		Status int
	}
	type channelRow struct {
		ID           uint64
		Status       int
		Manufacturer string
		Model        string
		Owner        string
		CivilCode    string
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
	if len(channelIDs) > 0 {
		var channels []channelRow
		if err = app.DB().WithContext(ctx).Table("gb_channel").Select("id,status,manufacturer,model,owner,civil_code").Where("id IN ? AND deleted_at IS NULL", channelIDs).Find(&channels).Error; err != nil {
			return catalog.Snapshot{}, err
		}
		for _, r := range channels {
			facts.ChannelOnline[r.ID] = r.Status == 1
			facts.ChannelMetadata[r.ID] = catalog.ChannelMetadata{Manufacturer: r.Manufacturer, Model: r.Model, Owner: r.Owner, CivilCode: r.CivilCode}
		}
	}
	snapshot, err := catalog.Build(*projection, facts)
	if err != nil {
		return catalog.Snapshot{}, err
	}
	settings := gbconfig.CurrentCascadeSettings()
	names := make(map[string]string)
	lookup := func(code string) string {
		if name, ok := names[code]; ok {
			return name
		}
		lookupCode := code
		if len(code) < 6 {
			lookupCode += strings.Repeat("0", 6-len(code))
		}
		var row civilcode.SysCivilCode
		if err := app.DB().WithContext(ctx).Where("code = ?", lookupCode).First(&row).Error; err == nil {
			names[code] = row.Name
		} else {
			names[code] = ""
		}
		return names[code]
	}
	return snapshot.Publish(*platform, catalog.PublicationSettings{
		PlatformName: settings.PlatformName, Manufacturer: settings.Manufacturer, Model: settings.Model,
		CivilCode: settings.CivilCode, Address: settings.Address, CivilName: lookup,
	}), nil
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
		accepted := false
		fail := func(code int, err error) bool {
			if !accepted {
				_ = tx.Respond(sip.NewResponseFromRequest(req, code, "Catalog query rejected", nil))
			}
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
			if matchCascadeCatalogPeerIdentity(req, *p) {
				if matched != nil {
					return fail(403, fmt.Errorf("ambiguous upstream"))
				}
				matched = p
			}
		}
		if matched == nil || !matched.Enabled {
			return fail(403, fmt.Errorf("unrecognized or disabled upstream"))
		}
		query, err := catalog.ParseCatalogQuery(cascadeWireProfile(*matched), req.Body())
		if err != nil {
			return fail(400, err)
		}
		// Confirm receipt before database reads and XML generation can delay the SIP transaction.
		if err := tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)); err != nil {
			return true
		}
		accepted = true
		snapshot, err := buildCascadeCatalogSnapshot(ctx, store, matched)
		if err != nil {
			return fail(500, err)
		}
		toID := ""
		if req.To() != nil {
			toID = req.To().Address.User
		}
		if err := catalog.ValidateCatalogRequestTargets(snapshot, matched.LocalDeviceID, toID, req.Recipient.User, query.DeviceID); err != nil {
			return fail(403, err)
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
		responses, err := catalog.PlanCatalogResponsesWithinLimit(cascadeWireProfile(*matched), matched.LocalDeviceID, snapshot, matched.CatalogBatchSize, req.Body(), fits)
		if err != nil {
			return fail(400, err)
		}
		for _, response := range responses {
			current, checkErr := store.FindPlatform(ctx, matched.ID)
			if checkErr != nil || !current.Enabled || current.ConfigRevision != matched.ConfigRevision || current.ProjectionRevision != snapshot.Revision {
				return true
			}
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
	infoHandler := newCascadeInfoHandler(store, clients)
	deviceStatusHandler := newCascadeDeviceStatusHandler(store, clients)
	recordInfoHandler := newCascadeRecordInfoHandler(store, clients)
	return func(req *sip.Request, tx sip.ServerTransaction) bool {
		if recordInfoHandler(req, tx) {
			return true
		}
		if deviceStatusHandler(req, tx) {
			return true
		}
		if infoHandler(req, tx) {
			return true
		}
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
