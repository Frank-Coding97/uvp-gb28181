package ptz

import (
	"context"
	"encoding/json"
	"fmt"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

type CascadeTargetTrackAuthorization struct {
	PlatformID          uint64 `json:"platformId"`
	ConfigRevision      uint64 `json:"configRevision"`
	ProjectionRevision  uint64 `json:"projectionRevision"`
	PublishedID         string `json:"publishedId"`
	PrimaryOwnerDeptID  uint   `json:"primaryOwnerDeptId"`
	PanoramaPublishedID string `json:"panoramaPublishedId,omitempty"`
	PanoramaDeviceID    uint   `json:"panoramaDeviceId,omitempty"`
	PanoramaDeviceCode  string `json:"panoramaDeviceCode,omitempty"`
	PanoramaChannelID   uint   `json:"panoramaChannelId,omitempty"`
	PanoramaChannelCode string `json:"panoramaChannelCode,omitempty"`
	PanoramaOwnerDeptID uint   `json:"panoramaOwnerDeptId,omitempty"`
	PanoramaDeviceEpoch int64  `json:"panoramaDeviceEpoch,omitempty"`
}

type cascadeTargetTrackPayload struct {
	Authorization CascadeTargetTrackAuthorization `json:"cascadeTargetTrackAuthorization"`
}

func (s *Service) TrackTargetForCascade(ctx context.Context, target Target, request TargetTrackRequest, authorization CascadeTargetTrackAuthorization, key string) (gbmodels.GbPTZOperation, error) {
	if target.Profile.Version != protocol.Version2022 || target.DeviceEpoch <= 0 || key == "" {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("cascade TargetTrack requires an authorized 2022 target")
	}
	if authorization.PlatformID == 0 || authorization.ConfigRevision == 0 || authorization.ProjectionRevision == 0 || authorization.PublishedID == "" {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("cascade TargetTrack authorization missing")
	}
	if request.DeviceID2 == "" && authorization.PanoramaPublishedID != "" || request.DeviceID2 != "" && request.DeviceID2 != authorization.PanoramaChannelCode {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("cascade TargetTrack panorama binding mismatch")
	}
	return s.trackTarget(ctx, target, request, 0, 0, key, &authorization)
}

func isCascadeTargetTrackOperation(operation gbmodels.GbPTZOperation) bool {
	if operation.Action != actionTargetTrack {
		return false
	}
	var payload cascadeTargetTrackPayload
	return json.Unmarshal([]byte(operation.PayloadJSON), &payload) == nil && payload.Authorization.PlatformID != 0
}

func buildCascadeTargetTrackOperation(operation gbmodels.GbPTZOperation) ([]byte, error) {
	var payload struct {
		Mode      string                   `json:"mode"`
		DeviceID2 string                   `json:"deviceId2"`
		Area      *manscdp.TargetTrackArea `json:"targetArea"`
	}
	if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
		return nil, err
	}
	return manscdp.BuildTargetTrackControlWithProfile(operationProtocolProfile(operation), operationTargetCode(operation), operation.SN, manscdp.TargetTrackCommand{
		Mode: manscdp.TargetTrackMode(payload.Mode), DeviceID2: payload.DeviceID2, Area: payload.Area,
	})
}

func (s *Service) validateCascadeTargetTrackAuthorization(ctx context.Context, operation gbmodels.GbPTZOperation) error {
	var payload cascadeTargetTrackPayload
	if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
		return err
	}
	a := payload.Authorization
	if a.PlatformID == 0 || a.ConfigRevision == 0 || a.ProjectionRevision == 0 || a.PublishedID == "" || operation.ProfileVersion != string(protocol.Version2022) {
		return fmt.Errorf("cascade TargetTrack authorization invalid")
	}
	var primary int64
	err := ptzWriter(s.db).WithContext(ctx).Table("gb_cascade_platform AS p").
		Joins("JOIN gb_cascade_channel_projection AS c ON c.platform_id=p.id").
		Joins("JOIN gb_cascade_device_projection AS d ON d.id=c.device_projection_id AND d.platform_id=p.id").
		Joins("JOIN gb_device AS sd ON sd.id=d.source_device_id").
		Joins("JOIN gb_channel AS sc ON sc.id=c.source_channel_id AND sc.device_id=sd.device_id AND sc.owner_dept_id=sd.owner_dept_id").
		Where("p.id=? AND p.enabled=? AND p.ptz_enabled=? AND p.deleted_at IS NULL AND p.config_revision=? AND p.projection_revision=? AND p.effective_version=?", a.PlatformID, true, true, a.ConfigRevision, a.ProjectionRevision, protocol.Version2022).
		Where("c.deleted_at IS NULL AND d.deleted_at IS NULL AND sd.owner_dept_id=? AND sc.owner_dept_id=?", a.PrimaryOwnerDeptID, a.PrimaryOwnerDeptID).
		Where("c.active=? AND c.ptz_allowed=? AND d.active=? AND c.published_channel_id=? AND d.source_device_id=? AND c.source_channel_id=?", true, true, true, a.PublishedID, operation.DeviceID, operation.ChannelID).
		Where("sd.deleted_at IS NULL AND sc.deleted_at IS NULL AND sd.device_id=? AND sc.channel_id=? AND sd.effective_version=? AND sd.access_epoch=? AND sd.cleanup_completed_epoch=?", operation.DeviceCode, operation.ChannelCode, protocol.Version2022, operation.DeviceEpoch, operation.DeviceEpoch).
		Count(&primary).Error
	if err != nil {
		return fmt.Errorf("check cascade TargetTrack source: %w", err)
	}
	if primary != 1 {
		return fmt.Errorf("cascade TargetTrack authorization revoked")
	}
	if a.PanoramaPublishedID == "" {
		if a.PanoramaDeviceID != 0 || a.PanoramaChannelID != 0 || a.PanoramaDeviceEpoch != 0 {
			return fmt.Errorf("cascade TargetTrack panorama binding invalid")
		}
		return nil
	}
	if a.PanoramaDeviceID == 0 || a.PanoramaChannelID == 0 || a.PanoramaDeviceEpoch <= 0 || a.PanoramaDeviceCode == "" || a.PanoramaChannelCode == "" {
		return fmt.Errorf("cascade TargetTrack panorama binding incomplete")
	}
	var panorama int64
	err = ptzWriter(s.db).WithContext(ctx).Table("gb_cascade_platform AS p").
		Joins("JOIN gb_cascade_channel_projection AS c ON c.platform_id=p.id").
		Joins("JOIN gb_cascade_device_projection AS d ON d.id=c.device_projection_id AND d.platform_id=p.id").
		Joins("JOIN gb_device AS sd ON sd.id=d.source_device_id").
		Joins("JOIN gb_channel AS sc ON sc.id=c.source_channel_id AND sc.device_id=sd.device_id AND sc.owner_dept_id=sd.owner_dept_id").
		Where("p.id=? AND p.enabled=? AND p.ptz_enabled=? AND p.deleted_at IS NULL AND p.config_revision=? AND p.projection_revision=?", a.PlatformID, true, true, a.ConfigRevision, a.ProjectionRevision).
		Where("c.deleted_at IS NULL AND d.deleted_at IS NULL").
		Where("c.active=? AND d.active=? AND c.published_channel_id=? AND d.source_device_id=? AND c.source_channel_id=?", true, true, a.PanoramaPublishedID, a.PanoramaDeviceID, a.PanoramaChannelID).
		Where("sd.deleted_at IS NULL AND sc.deleted_at IS NULL AND sd.device_id=? AND sc.channel_id=? AND sd.owner_dept_id=? AND sc.owner_dept_id=? AND sd.access_epoch=? AND sd.cleanup_completed_epoch=?", a.PanoramaDeviceCode, a.PanoramaChannelCode, a.PanoramaOwnerDeptID, a.PanoramaOwnerDeptID, a.PanoramaDeviceEpoch, a.PanoramaDeviceEpoch).
		Count(&panorama).Error
	if err != nil {
		return fmt.Errorf("check cascade TargetTrack panorama: %w", err)
	}
	if panorama != 1 {
		return fmt.Errorf("cascade TargetTrack panorama authorization revoked")
	}
	return nil
}
