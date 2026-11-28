package gb28181

import (
	"context"
	"encoding/xml"
	"fmt"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

func newCascadeInfoHandler(store *repository.GormRepository, clients *cascadePlatformClientFactory) func(*sip.Request, sip.ServerTransaction) bool {
	return func(req *sip.Request, tx sip.ServerTransaction) bool {
		var head struct {
			XMLName xml.Name
			CmdType string `xml:"CmdType"`
		}
		if manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), req.Body(), &head) != nil || head.XMLName.Local != "Query" || head.CmdType != manscdp.CmdDeviceInfo {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		reject := func(code int) bool {
			_ = tx.Respond(sip.NewResponseFromRequest(req, code, "DeviceInfo query rejected", nil))
			return true
		}
		platforms, err := store.ListPlatforms(ctx)
		if err != nil {
			return reject(500)
		}
		platform, ambiguous := findCascadeControlPeer(req, platforms)
		if platform == nil && !ambiguous {
			return false
		}
		if ambiguous || !platform.Enabled {
			return reject(403)
		}
		snapshot, err := buildCascadeCatalogSnapshot(ctx, store, platform)
		if err != nil {
			return reject(500)
		}
		settings := gbconfig.CurrentCascadeSettings()
		count := 0
		for _, item := range snapshot.Items {
			if item.Kind == catalog.CatalogItemChannel {
				count++
			}
		}
		response, err := catalog.PlanDeviceInfoResponse(cascadeWireProfile(*platform), catalog.PlatformDeviceInfo{
			DeviceID: platform.LocalDeviceID, DeviceName: settings.PlatformName, Manufacturer: settings.Manufacturer, Model: settings.Model, Channel: &count,
		}, snapshot, req.Body())
		if err != nil {
			return reject(403)
		}
		client, err := clients.NewClient(*platform)
		if err != nil {
			return reject(503)
		}
		current, err := store.FindPlatform(ctx, platform.ID)
		if err != nil || !current.Enabled || current.ConfigRevision != platform.ConfigRevision || current.ProjectionRevision != snapshot.Revision {
			return reject(403)
		}
		if err := tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)); err != nil {
			return true
		}
		result := client.(*cascadePlatformClient).transactions.SendMessage(ctx, response.Body, fmt.Sprintf("device-info-%d-%d", platform.ID, time.Now().UnixNano()))
		if !result.Success {
			app.Log(ctx).Warn("级联平台信息响应发送失败", zap.String("event", "cascade.device_info.response_failed"), zap.Uint64("platform_id", platform.ID), zap.Int("status", result.StatusCode))
		}
		return true
	}
}
