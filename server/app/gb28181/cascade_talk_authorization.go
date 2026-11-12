package gb28181

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
)

// cascadeTalkAuthorization fixes publication and source identity for one audio
// session. Callers must revalidate before signaling and throughout the media lease.
type cascadeTalkAuthorization struct {
	Platform    model.GbCascadePlatform
	PublishedID string
	Source      ptz.Target
	OwnerDeptID uint
}

func resolveCascadeTalkAuthorization(ctx context.Context, db *gorm.DB, store *repository.GormRepository, platform model.GbCascadePlatform, publishedID string) (cascadeTalkAuthorization, error) {
	if db == nil || store == nil || !cascadeDeviceStatusAuthorizationCurrent(ctx, store, platform) {
		return cascadeTalkAuthorization{}, fmt.Errorf("cascade audio authorization changed")
	}
	projection, err := store.ProjectionSnapshot(ctx, platform.ID)
	if err != nil {
		return cascadeTalkAuthorization{}, err
	}
	if projection == nil || !projection.Platform.Enabled || projection.Platform.ConfigRevision != platform.ConfigRevision || projection.Revision != platform.ProjectionRevision {
		return cascadeTalkAuthorization{}, fmt.Errorf("cascade audio publication changed")
	}
	publication, err := buildCascadeCatalogSnapshotWithDB(ctx, db, store, &projection.Platform)
	if err != nil {
		return cascadeTalkAuthorization{}, err
	}
	if publication.Revision != projection.Revision {
		return cascadeTalkAuthorization{}, fmt.Errorf("cascade audio publication changed")
	}
	target, ok := resolveCascadeDeviceStatusTarget(*projection, publication, publishedID)
	if !ok || target.IsPlatform || target.TargetScope != gbmodels.ControlTargetScopeChannel {
		return cascadeTalkAuthorization{}, fmt.Errorf("cascade audio channel is not published")
	}
	source, err := control.NewGormTargetLoader(db).LoadAuthorized(ctx, target.SourceDeviceID, target.SourceChannelID)
	if err != nil {
		return cascadeTalkAuthorization{}, err
	}
	if !source.DeviceOnline || !source.ChannelOnline {
		return cascadeTalkAuthorization{}, fmt.Errorf("cascade audio source is offline")
	}
	var owner struct{ OwnerDeptID uint }
	err = db.WithContext(ctx).Table("gb_device").Select("owner_dept_id").Where("id=? AND device_id=? AND access_epoch=? AND cleanup_completed_epoch=? AND deleted_at IS NULL", source.DeviceID, source.DeviceCode, source.DeviceEpoch, source.DeviceEpoch).Take(&owner).Error
	if err != nil {
		return cascadeTalkAuthorization{}, err
	}
	if !cascadeDeviceStatusAuthorizationCurrent(ctx, store, platform) {
		return cascadeTalkAuthorization{}, fmt.Errorf("cascade audio authorization changed")
	}
	return cascadeTalkAuthorization{Platform: platform, PublishedID: publishedID, Source: source, OwnerDeptID: owner.OwnerDeptID}, nil
}

func (a cascadeTalkAuthorization) Validate(ctx context.Context, db *gorm.DB, store *repository.GormRepository) error {
	current, err := resolveCascadeTalkAuthorization(ctx, db, store, a.Platform, a.PublishedID)
	if err != nil {
		return err
	}
	if current.OwnerDeptID != a.OwnerDeptID || current.Source.DeviceID != a.Source.DeviceID || current.Source.ChannelID != a.Source.ChannelID || current.Source.DeviceCode != a.Source.DeviceCode || current.Source.ChannelCode != a.Source.ChannelCode || current.Source.DeviceEpoch != a.Source.DeviceEpoch || current.Source.Profile != a.Source.Profile || current.Source.IP != a.Source.IP || current.Source.Port != a.Source.Port || current.Source.Transport != a.Source.Transport {
		return fmt.Errorf("cascade audio source identity changed")
	}
	return nil
}
