package catalog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

type catalogShareFixture struct {
	db       *gorm.DB
	pipeline *catalog.Pipeline
	sender   catalog.Sender
	channels []gbmodels.GbChannel
	links    []videoshare.Link
	shares   *videoshare.Service
}

func newCatalogShareFixture(t *testing.T) *catalogShareFixture {
	t.Helper()
	db := newPipelineTestDB(t)
	require.NoError(t, db.AutoMigrate(&basemodels.SysOperationLog{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch INTEGER NOT NULL DEFAULT 1").Error)
	device := gbmodels.GbDevice{DeviceID: "37011200001170000001", OwnerDeptID: 10}
	require.NoError(t, db.Create(&device).Error)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: device.DeviceID}
	pipeline := catalog.New(db)
	require.NoError(t, pipeline.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: "37011200001320000001", ParentID: device.DeviceID, CivilCode: "370112", Name: "first"},
		{DeviceID: "37011200001320000002", ParentID: device.DeviceID, CivilCode: "370112", Name: "second"},
	}))
	var channels []gbmodels.GbChannel
	require.NoError(t, db.Order("id").Find(&channels).Error)
	require.Len(t, channels, 2)
	signer, err := videoshare.NewSigner(make([]byte, 32))
	require.NoError(t, err)
	authorizer := func(ctx context.Context, tx *gorm.DB, _, channelID uint) (videoshare.AuthorizationSnapshot, error) {
		var channel gbmodels.GbChannel
		result := tx.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", channelID).Limit(1).Find(&channel)
		if result.Error != nil {
			return videoshare.AuthorizationSnapshot{}, result.Error
		}
		if result.RowsAffected != 1 {
			return videoshare.AuthorizationSnapshot{}, videoshare.ErrExpired
		}
		return videoshare.AuthorizationSnapshot{DevicePK: device.ID, DeviceID: device.DeviceID, DeviceEpoch: 1}, nil
	}
	shares, err := videoshare.NewService(db, signer, func() time.Duration { return 5 * time.Minute }, authorizer)
	require.NoError(t, err)
	links := make([]videoshare.Link, len(channels))
	for i, channel := range channels {
		links[i], err = shares.SetEnabled(context.Background(), channel.ID, 7, true, 0)
		require.NoError(t, err)
	}
	return &catalogShareFixture{db: db, pipeline: pipeline, sender: sender, channels: channels, links: links, shares: shares}
}

func (f *catalogShareFixture) assertUnaffected(t *testing.T) {
	t.Helper()
	_, err := f.shares.Resolve(context.Background(), f.links[1].PublicID, f.links[1].Signature)
	require.NoError(t, err, "unrelated channel must remain shared")
}

func (f *catalogShareFixture) assertRetiredAfterReaddAndPKReuse(t *testing.T) {
	t.Helper()
	var retired gbmodels.GbChannelVideoShare
	require.NoError(t, f.db.Where("channel_pk = ?", f.channels[0].ID).Take(&retired).Error)
	require.False(t, retired.Enabled)
	require.NotNil(t, retired.RevokedAt)
	require.Equal(t, f.links[0].Revision+1, retired.Revision)
	f.assertUnaffected(t)

	// A later ADD and even restoration of the original database identity must not
	// revive a credential which existed before the removal/type correction.
	require.NoError(t, f.pipeline.IngestDelta(context.Background(), f.sender, "ADD", catalog.CatalogItem{
		DeviceID: f.channels[0].ChannelID, Name: "restored", CivilCode: "370112", ParentID: f.sender.SourceDeviceID, ParentalPresent: true,
	}))
	require.NoError(t, f.db.Where("device_id = ? AND channel_id = ?", f.sender.SourceDeviceID, f.channels[0].ChannelID).Delete(&gbmodels.GbChannel{}).Error)
	restored := f.channels[0]
	restored.DeletedAt = nil
	require.NoError(t, f.db.Create(&restored).Error)
	_, err := f.shares.Resolve(context.Background(), f.links[0].PublicID, f.links[0].Signature)
	require.ErrorIs(t, err, videoshare.ErrExpired)
	f.assertUnaffected(t)
}

func TestCatalogDeletionPermanentlyRetiresOnlyRemovedChannelShare(t *testing.T) {
	for _, prune := range []bool{false, true} {
		name := "DEL"
		if prune {
			name = "prune"
		}
		t.Run(name, func(t *testing.T) {
			f := newCatalogShareFixture(t)
			if prune {
				count, err := f.pipeline.PruneChannels(context.Background(), f.sender, []string{f.channels[1].ChannelID})
				require.NoError(t, err)
				require.Equal(t, 1, count)
			} else {
				require.NoError(t, f.pipeline.IngestDelta(context.Background(), f.sender, "DEL", catalog.CatalogItem{DeviceID: f.channels[0].ChannelID}))
			}
			f.assertRetiredAfterReaddAndPKReuse(t)
		})
	}
}

func TestCatalogTypeCorrectionPermanentlyRetiresPreviousVideoShare(t *testing.T) {
	f := newCatalogShareFixture(t)
	require.NoError(t, f.pipeline.IngestDelta(context.Background(), f.sender, "ADD", catalog.CatalogItem{
		DeviceID: f.channels[0].ChannelID, Name: "corrected device", CivilCode: "370112", ParentID: f.sender.SourceDeviceID, Parental: 1, ParentalPresent: true,
	}))
	f.assertRetiredAfterReaddAndPKReuse(t)
}

func TestCatalogRemovalPreservesShareWhenManualMountKeepsChannel(t *testing.T) {
	f := newCatalogShareFixture(t)
	parent := gbmodels.GbCatalogNode{OwnerDeptID: 10, Code: "manual-folder", NodeType: gbmodels.NodeTypeVirtualOrg, Source: gbmodels.NodeSourceManual, Path: "/", Name: "manual"}
	require.NoError(t, f.db.Create(&parent).Error)
	require.NoError(t, f.db.Create(&gbmodels.GbChannelMount{OwnerDeptID: 10, ChannelID: f.channels[0].ID, ParentNodeID: parent.ID, MountSource: gbmodels.MountSourceManual}).Error)
	// Identity correction is rejected while the channel remains manually mounted.
	require.Error(t, f.pipeline.IngestDelta(context.Background(), f.sender, "ADD", catalog.CatalogItem{
		DeviceID: f.channels[0].ChannelID, CivilCode: "370112", Parental: 1, ParentalPresent: true,
	}))
	require.NoError(t, f.pipeline.IngestDelta(context.Background(), f.sender, "DEL", catalog.CatalogItem{DeviceID: f.channels[0].ChannelID}))
	_, err := f.shares.Resolve(context.Background(), f.links[0].PublicID, f.links[0].Signature)
	require.NoError(t, err, "remaining manual mount retains the physical channel and its share")
	f.assertUnaffected(t)
}

func TestCatalogShareRetirementFailureRollsBackResourceRemoval(t *testing.T) {
	for _, correction := range []bool{false, true} {
		name := "DEL"
		if correction {
			name = "type correction"
		}
		t.Run(name, func(t *testing.T) {
			f := newCatalogShareFixture(t)
			injected := errors.New("share retirement unavailable")
			require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register("test:share_retirement_failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "gb_channel_video_share" {
					tx.AddError(injected)
				}
			}))
			item := catalog.CatalogItem{DeviceID: f.channels[0].ChannelID}
			action := "DEL"
			if correction {
				action = "ADD"
				item.Parental = 1
				item.ParentalPresent = true
				item.CivilCode = "370112"
			}
			err := f.pipeline.IngestDelta(context.Background(), f.sender, action, item)
			require.ErrorIs(t, err, injected)
			var node gbmodels.GbCatalogNode
			require.NoError(t, f.db.Where("channel_id = ?", f.channels[0].ID).Take(&node).Error)
			require.Equal(t, gbmodels.NodeTypeChannel, node.NodeType)
			var mounts int64
			require.NoError(t, f.db.Model(&gbmodels.GbChannelMount{}).Where("channel_id = ?", f.channels[0].ID).Count(&mounts).Error)
			require.Positive(t, mounts)
			_, err = f.shares.Resolve(context.Background(), f.links[0].PublicID, f.links[0].Signature)
			require.NoError(t, err, "failed revocation must preserve the original channel transactionally")
			f.assertUnaffected(t)
		})
	}
}
