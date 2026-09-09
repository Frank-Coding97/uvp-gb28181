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
		projection, err := store.ProjectionSnapshot(ctx, matched.ID)
		if err != nil {
			return fail(500, err)
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
				return fail(500, err)
			}
			for _, r := range rows {
				facts.DeviceOnline[r.ID] = r.Status == 1
			}
		}
		rows = nil
		if len(channelIDs) > 0 {
			if err = app.DB().WithContext(ctx).Table("gb_channel").Select("id,status").Where("id IN ?", channelIDs).Find(&rows).Error; err != nil {
				return fail(500, err)
			}
			for _, r := range rows {
				facts.ChannelOnline[r.ID] = r.Status == 1
			}
		}
		snapshot, err := catalog.Build(*projection, facts)
		if err != nil {
			return fail(500, err)
		}
		snapshot = snapshot.ChannelsOnly()
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
