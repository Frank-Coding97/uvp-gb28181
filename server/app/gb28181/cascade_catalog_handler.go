package gb28181

import (
	"context"
	"encoding/xml"
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
			if app.ZapLog != nil {
				app.ZapLog.Warn("级联目录查询失败", zap.Int("status", code), zap.Error(err))
			}
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
				if app.ZapLog != nil {
					app.ZapLog.Warn("级联目录响应发送失败", zap.Uint64("platformId", matched.ID), zap.Int("status", result.StatusCode), zap.Error(result.TransportErr), zap.Error(result.BuildErr))
				}
				return true
			}
		}
		if app.ZapLog != nil {
			app.ZapLog.Info("级联目录响应完成", zap.Uint64("platformId", matched.ID), zap.Int("items", len(snapshot.Items)), zap.Int("batches", len(responses)))
		}
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
		if app.ZapLog != nil {
			app.ZapLog.Warn("级联云台命令转发失败", zap.Uint64("platformId", platform.ID), zap.String("channelId", head.DeviceID), zap.String("reason", "PTZ service unavailable"))
		}
		return
	}
	service := control.NewService(store, control.NewGormTargetLoader(app.DB()), ptzService)
	if _, err := service.Forward(ctx, control.ForwardRequest{PlatformID: platform.ID, CallID: callID, Body: body}); err != nil && app.ZapLog != nil {
		app.ZapLog.Warn("级联云台命令转发失败", zap.Uint64("platformId", platform.ID), zap.String("channelId", head.DeviceID), zap.Error(err))
	}
}
