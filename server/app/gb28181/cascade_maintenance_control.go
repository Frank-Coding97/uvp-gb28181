package gb28181

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadeMaintenanceAuthorization = ptz.CascadeMaintenanceAuthorization

type cascadeMaintenanceCommand struct {
	SN       int
	DeviceID string
	Action   string
	CardID   int
}

func newCascadeMaintenanceControlHandler(store *repository.GormRepository, clients *cascadePlatformClientFactory) func(*sip.Request, sip.ServerTransaction) bool {
	return func(req *sip.Request, tx sip.ServerTransaction) bool {
		var wire struct {
			XMLName      xml.Name `xml:"Control"`
			CmdType      string   `xml:"CmdType"`
			SN           int      `xml:"SN"`
			DeviceID     string   `xml:"DeviceID"`
			TeleBoot     *string  `xml:"TeleBoot"`
			FormatSDCard *int     `xml:"FormatSDCard"`
		}
		if manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), req.Body(), &wire) != nil || wire.XMLName.Local != "Control" || wire.CmdType != manscdp.CmdDeviceControl {
			return false
		}
		if wire.TeleBoot == nil && wire.FormatSDCard == nil {
			return false
		}
		reject := func(code int, reason string) bool {
			_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
			return true
		}
		if wire.SN <= 0 || !validCascadeGBID(wire.DeviceID) || (wire.TeleBoot != nil && wire.FormatSDCard != nil) {
			return reject(400, "Invalid maintenance control")
		}
		command := cascadeMaintenanceCommand{SN: wire.SN, DeviceID: strings.TrimSpace(wire.DeviceID)}
		if wire.TeleBoot != nil {
			if strings.TrimSpace(*wire.TeleBoot) != "Boot" {
				return reject(400, "Invalid TeleBoot command")
			}
			command.Action = "teleboot"
		} else {
			if *wire.FormatSDCard < 0 {
				return reject(400, "Invalid storage card number")
			}
			command.Action, command.CardID = "format_sd", *wire.FormatSDCard
		}
		toID := ""
		if req.To() != nil {
			toID = req.To().Address.User
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		platforms, err := store.ListPlatforms(ctx)
		if err != nil {
			return reject(503, "Maintenance control unavailable")
		}
		platform, ambiguous := findCascadeControlPeer(req, platforms)
		if platform == nil && !ambiguous {
			return false
		}
		if ambiguous || platform == nil || !platform.Enabled || !platform.MaintenanceEnabled {
			return reject(403, "Maintenance control not permitted")
		}
		if !cascadeDeviceStatusRequestTargetsMatch(platform.LocalDeviceID, wire.DeviceID, toID, req.Recipient.User) {
			return reject(403, "Maintenance control target mismatch")
		}
		projection, err := store.ProjectionSnapshot(ctx, platform.ID)
		if err != nil || projection == nil || !projection.Platform.MaintenanceEnabled || !cascadeDeviceStatusAuthorizationCurrent(ctx, store, *platform) {
			return reject(403, "Maintenance authorization changed")
		}
		publication, err := buildCascadeCatalogSnapshot(ctx, store, &projection.Platform)
		if err != nil || publication.Revision != projection.Revision {
			return reject(503, "Maintenance publication unavailable")
		}
		authorization := cascadeMaintenanceAuthorization{PlatformID: projection.Platform.ID, ConfigRevision: projection.Platform.ConfigRevision, ProjectionRevision: projection.Platform.ProjectionRevision, PublishedID: command.DeviceID, Action: command.Action}
		var target cascadeConfigWriteTarget
		if command.Action == "teleboot" {
			deviceID, ok := resolveCascadeMaintenanceDevice(*projection, publication, command.DeviceID)
			if !ok {
				return reject(403, "Maintenance device is not published")
			}
			target.SourceDeviceID = deviceID
		} else {
			resolved, ok := resolveCascadeDeviceStatusTarget(*projection, publication, command.DeviceID)
			if !ok || resolved.IsPlatform || resolved.TargetScope != gbmodels.ControlTargetScopeChannel {
				return reject(403, "Maintenance channel is not published")
			}
			loaded, loadErr := control.NewGormTargetLoader(app.DB()).LoadAuthorized(ctx, resolved.SourceDeviceID, resolved.SourceChannelID)
			if loadErr != nil {
				return reject(403, "Maintenance source is unavailable")
			}
			target.cascadeDeviceStatusTarget, target.source = resolved, loaded
		}
		if err = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)); err != nil {
			return true
		}
		key := fmt.Sprintf("cascade-maintenance:%d:%s:%d:%s", platform.ID, cascadeCallID(req), cascadeCSeq(req), command.DeviceID)
		snapshot := *platform
		go executeCascadeMaintenanceControl(snapshot, target, command, authorization, key)
		return true
	}
}

func resolveCascadeMaintenanceDevice(projection repository.ProjectionSnapshot, publication catalog.Snapshot, publishedID string) (uint64, bool) {
	for _, item := range publication.Items {
		if item.ID != publishedID {
			continue
		}
		switch item.Kind {
		case catalog.CatalogItemDevice:
			for _, device := range projection.Devices {
				if device.PlatformID == projection.Platform.ID && device.Active && device.PublishedDeviceID == publishedID && device.SourceDeviceID != 0 {
					return device.SourceDeviceID, true
				}
			}
		case catalog.CatalogItemChannel:
			for _, channel := range projection.Channels {
				if channel.PlatformID == projection.Platform.ID && channel.Active && channel.PublishedChannelID == publishedID {
					if sourceDeviceID := cascadeProjectionSourceDevice(projection, channel.DeviceProjectionID); sourceDeviceID != 0 {
						return sourceDeviceID, true
					}
				}
			}
		}
	}
	return 0, false
}

func executeCascadeMaintenanceControl(platform model.GbCascadePlatform, target cascadeConfigWriteTarget, command cascadeMaintenanceCommand, authorization cascadeMaintenanceAuthorization, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	service := ptzService
	if service == nil || !service.RequiresDeviceAuthorization() {
		return
	}
	var err error
	switch command.Action {
	case "format_sd":
		_, err = service.FormatStorageCardForCascade(ctx, target.source, command.CardID, authorization, key)
	case "teleboot":
		var device gbmodels.GbDevice
		err = app.DB().WithContext(ctx).Where("id = ? AND deleted_at IS NULL", target.SourceDeviceID).Take(&device).Error
		if err == nil {
			deviceProfile := protocol.ProfileFor(protocol.Version2016)
			if strings.TrimSpace(device.EffectiveVersion) == protocol.Version2022 {
				deviceProfile = protocol.ProfileFor(protocol.Version2022)
			}
			deviceTarget := ptz.DeviceRebootTarget{DeviceID: device.ID, DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port, Transport: device.Transport, DeviceOnline: device.Status == gbmodels.DeviceStatusOnline, Profile: deviceProfile, CascadeAuthorization: &authorization}
			_, _, err = service.ExecuteDeviceRebootDetailed(ctx, deviceTarget, key, 0, 0)
		}
	default:
		err = fmt.Errorf("unsupported maintenance action %q", command.Action)
	}
	if err != nil {
		app.Log(ctx).Warn("级联维护控制未能下发", zap.String("event", "cascade.maintenance.control_failed"), zap.Uint64("platform_id", platform.ID), zap.String("published_target_id", command.DeviceID), zap.String("action", command.Action), zap.Error(err))
	}
}
