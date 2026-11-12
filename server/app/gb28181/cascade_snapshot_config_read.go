package gb28181

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

// A device retains a single snapshot configuration, potentially written by
// another upstream or locally. Never expose that configuration's upload route.
func restoreCascadeSnapshotConfig(ctx context.Context, db *gorm.DB, platform model.GbCascadePlatform, target cascadeDeviceStatusTarget, response *manscdp.CascadeConfigReadResponse) (*manscdp.CascadeConfigReadResponse, func(context.Context) error, error) {
	snapshot, ok := response.SnapshotConfig()
	if !ok {
		return response, nil, nil
	}
	if db == nil || snapshot.SessionID == nil || !platform.ConfigEnabled || cascadeWireProfile(platform).Version != protocol.Version2022 {
		return nil, nil, fmt.Errorf("snapshot configuration ownership unavailable")
	}
	var session model.GbCascadeSnapshotSession
	if err := db.WithContext(ctx).Where("platform_id = ? AND (downstream_session_id = ? OR (downstream_session_id IS NULL AND upstream_session_id = ?))", platform.ID, *snapshot.SessionID, *snapshot.SessionID).First(&session).Error; err != nil {
		return nil, nil, fmt.Errorf("snapshot configuration does not belong to this upstream")
	}
	authorize := func(checkCtx context.Context) error {
		var currentPlatform model.GbCascadePlatform
		if err := db.WithContext(checkCtx).First(&currentPlatform, platform.ID).Error; err != nil {
			return err
		}
		if !currentPlatform.Enabled || !currentPlatform.ConfigEnabled || currentPlatform.ConfigRevision != platform.ConfigRevision || currentPlatform.ProjectionRevision != platform.ProjectionRevision || cascadeWireProfile(currentPlatform).Version != protocol.Version2022 {
			return fmt.Errorf("snapshot configuration upstream authorization changed")
		}
		var current model.GbCascadeSnapshotSession
		if err := db.WithContext(checkCtx).First(&current, session.ID).Error; err != nil {
			return err
		}
		if !sameCascadeSnapshotSession(current, session) || !sameCascadeSnapshotRouting(current, session) || current.UpstreamUploadURL == "" ||
			current.PlatformID != platform.ID || current.PublishedChannelID != target.PublishedID || current.SourceDeviceID != target.SourceDeviceID || current.SourceChannelID != target.SourceChannelID ||
			current.ConfigRevision != platform.ConfigRevision || current.ProjectionRevision != platform.ProjectionRevision {
			return fmt.Errorf("snapshot configuration binding changed")
		}
		loaded, err := control.NewGormTargetLoader(db).LoadAuthorized(checkCtx, current.SourceDeviceID, current.SourceChannelID)
		if err != nil || current.SourceDeviceEpoch <= 0 || loaded.DeviceEpoch != current.SourceDeviceEpoch || loaded.Profile.Version != protocol.Version2022 {
			return fmt.Errorf("snapshot configuration source authorization changed")
		}
		return nil
	}
	if err := authorize(ctx); err != nil {
		return nil, nil, err
	}
	// Completed/expired captures may still be the device's current configuration.
	// This does not renew their upload capability or permit another capture.
	restored, err := response.WithSnapshotRouting(session.UpstreamSessionID, session.UpstreamUploadURL)
	return restored, authorize, err
}

func sameCascadeSnapshotRouting(a, b model.GbCascadeSnapshotSession) bool {
	same := func(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
	return same(a.DownstreamSessionID, b.DownstreamSessionID) && same(a.UploadToken, b.UploadToken)
}
