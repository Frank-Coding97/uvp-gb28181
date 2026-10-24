package gb28181

import (
	"context"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

func newCascadeTargetTrackHandler(store *repository.GormRepository) func(*sip.Request, sip.ServerTransaction) bool {
	return func(req *sip.Request, tx sip.ServerTransaction) bool {
		body := string(req.Body())
		if !strings.Contains(body, "TargetTrack") || !strings.Contains(body, "DeviceControl") {
			return false
		}
		reject := func(code int, reason string) bool {
			_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		platforms, err := store.ListPlatforms(ctx)
		if err != nil {
			return reject(503, "TargetTrack unavailable")
		}
		platform, ambiguous := findCascadeControlPeer(req, platforms)
		if platform == nil && !ambiguous {
			return false
		}
		if ambiguous || platform == nil || !platform.Enabled || !platform.PTZEnabled {
			return reject(403, "TargetTrack not permitted")
		}
		parsed, err := manscdp.ParseTargetTrackControlWithProfile(cascadeWireProfile(*platform), req.Body())
		if err != nil {
			return reject(400, "Invalid TargetTrack")
		}
		toID := ""
		if req.To() != nil {
			toID = req.To().Address.User
		}
		if !cascadeDeviceStatusRequestTargetsMatch(platform.LocalDeviceID, parsed.DeviceID, toID, req.Recipient.User) {
			return reject(403, "TargetTrack target mismatch")
		}
		if protocol.Version(strings.TrimSpace(platform.EffectiveVersion)) != protocol.Version2022 {
			return reject(400, "TargetTrack requires GB/T 28181-2022 upstream")
		}
		projection, err := store.ProjectionSnapshot(ctx, platform.ID)
		if err != nil || projection == nil || !projection.Platform.PTZEnabled || !cascadeDeviceStatusAuthorizationCurrent(ctx, store, *platform) {
			return reject(403, "TargetTrack authorization changed")
		}
		publication, err := buildCascadeCatalogSnapshot(ctx, store, &projection.Platform)
		if err != nil || publication.Revision != projection.Revision {
			return reject(503, "TargetTrack publication unavailable")
		}
		primary, ok := resolveCascadeDeviceStatusTarget(*projection, publication, parsed.DeviceID)
		if !ok || primary.IsPlatform || primary.TargetScope != gbmodels.ControlTargetScopeChannel || !cascadeTargetTrackPTZAllowed(*projection, primary.SourceChannelID) {
			return reject(403, "TargetTrack channel not published for PTZ")
		}
		loader := control.NewGormTargetLoader(app.DB())
		primaryTarget, err := loader.LoadAuthorized(ctx, primary.SourceDeviceID, primary.SourceChannelID)
		if err != nil {
			return reject(403, "TargetTrack source unavailable")
		}
		if primaryTarget.Profile.Version != protocol.Version2022 {
			return reject(400, "TargetTrack requires GB/T 28181-2022 source")
		}
		authorization := ptz.CascadeTargetTrackAuthorization{
			PlatformID: platform.ID, ConfigRevision: platform.ConfigRevision, ProjectionRevision: platform.ProjectionRevision, PublishedID: parsed.DeviceID,
		}
		var primaryOwner struct{ OwnerDeptID uint }
		if err := app.DB().WithContext(ctx).Table("gb_device").Select("owner_dept_id").Where("id=? AND deleted_at IS NULL", primaryTarget.DeviceID).Take(&primaryOwner).Error; err != nil {
			return reject(403, "TargetTrack source unavailable")
		}
		authorization.PrimaryOwnerDeptID = primaryOwner.OwnerDeptID
		if parsed.Command.DeviceID2 != "" {
			panorama, found := resolveCascadeDeviceStatusTarget(*projection, publication, parsed.Command.DeviceID2)
			if !found || panorama.IsPlatform || panorama.TargetScope != gbmodels.ControlTargetScopeChannel {
				return reject(403, "TargetTrack panorama channel is not published")
			}
			panoramaTarget, loadErr := loader.LoadAuthorized(ctx, panorama.SourceDeviceID, panorama.SourceChannelID)
			if loadErr != nil {
				return reject(403, "TargetTrack panorama source unavailable")
			}
			var owner struct{ OwnerDeptID uint }
			if err := app.DB().WithContext(ctx).Table("gb_device").Select("owner_dept_id").Where("id=? AND deleted_at IS NULL", panoramaTarget.DeviceID).Take(&owner).Error; err != nil {
				return reject(403, "TargetTrack panorama source unavailable")
			}
			authorization.PanoramaPublishedID = parsed.Command.DeviceID2
			authorization.PanoramaDeviceID = panoramaTarget.DeviceID
			authorization.PanoramaDeviceCode = panoramaTarget.DeviceCode
			authorization.PanoramaChannelID = panoramaTarget.ChannelID
			authorization.PanoramaChannelCode = panoramaTarget.ChannelCode
			authorization.PanoramaOwnerDeptID = owner.OwnerDeptID
			authorization.PanoramaDeviceEpoch = panoramaTarget.DeviceEpoch
		}
		service := ptzService
		if service == nil || !service.RequiresDeviceAuthorization() {
			return reject(503, "TargetTrack service unavailable")
		}
		if err := tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)); err != nil {
			return true
		}
		key := cascadeDeviceStatusIdempotencyKey(platform.ID, cascadeCallID(req), cascadeCSeq(req), parsed.DeviceID+"|target-track")
		// Keep the one-way send inside the admitted SIP handler so shutdown
		// waits for intent persistence as well as transport completion.
		executeCascadeTargetTrack(service, *platform, primaryTarget, parsed.Command, authorization, key)
		return true
	}
}

func cascadeTargetTrackPTZAllowed(projection repository.ProjectionSnapshot, sourceChannelID uint64) bool {
	for _, channel := range projection.Channels {
		if channel.PlatformID == projection.Platform.ID && channel.Active && channel.SourceChannelID == sourceChannelID && channel.PTZAllowed {
			return true
		}
	}
	return false
}

func executeCascadeTargetTrack(service *ptz.Service, platform model.GbCascadePlatform, target ptz.Target, command manscdp.TargetTrackCommand, authorization ptz.CascadeTargetTrackAuthorization, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := service.TrackTargetForCascade(ctx, target, ptz.TargetTrackRequest{Mode: command.Mode, DeviceID2: authorization.PanoramaChannelCode, Area: command.Area}, authorization, key)
	if err != nil {
		app.Log(ctx).Warn("级联目标跟踪未能下发", zap.String("event", "cascade.target_track.forward_failed"), zap.Uint64("platform_id", platform.ID), zap.String("published_channel_id", authorization.PublishedID), zap.Error(err))
	}
}
