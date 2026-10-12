package catalog

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestReconcileCatalogIdentity_MergesDescendantsAndMounts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbCatalogNode{}, &gbmodels.GbChannelMount{}, &gbmodels.GbAnomalyRecord{}))
	dev := gbmodels.GbDevice{DeviceID: "source", OwnerDeptID: 10}
	require.NoError(t, db.Create(&dev).Error)
	target := gbmodels.GbCatalogNode{OwnerDeptID: 10, Code: "folder", NodeType: gbmodels.NodeTypeDevice, DeviceID: &dev.ID, Source: gbmodels.NodeSourceCatalog, Path: "/"}
	require.NoError(t, db.Create(&target).Error)
	target.Path = BuildPath("/", target.ID)
	require.NoError(t, db.Save(&target).Error)
	old := gbmodels.GbCatalogNode{OwnerDeptID: 10, Code: "folder", NodeType: gbmodels.NodeTypeVirtualOrg, Source: gbmodels.NodeSourceCatalog, Path: "/", Anomaly: true}
	require.NoError(t, db.Create(&old).Error)
	old.Path = BuildPath("/", old.ID)
	require.NoError(t, db.Save(&old).Error)
	require.NoError(t, db.Create(&gbmodels.GbAnomalyRecord{OwnerDeptID: 10, CatalogNodeID: old.ID, SourceDeviceID: &dev.ID}).Error)
	ch := gbmodels.GbChannel{DeviceID: dev.DeviceID, ChannelID: "camera", OwnerDeptID: 10}
	require.NoError(t, db.Create(&ch).Error)
	child := gbmodels.GbCatalogNode{OwnerDeptID: 10, Code: ch.ChannelID, NodeType: gbmodels.NodeTypeChannel, ChannelID: &ch.ID, ParentID: &old.ID, Path: BuildPath(old.Path, 99), Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&child).Error)
	mount := gbmodels.GbChannelMount{OwnerDeptID: 10, ChannelID: ch.ID, ParentNodeID: old.ID, MountSource: gbmodels.MountSourceCatalog, IsPrimary: true}
	require.NoError(t, db.Create(&mount).Error)
	require.NoError(t, db.WithContext(context.Background()).Transaction(func(tx *gorm.DB) error {
		return reconcileCatalogIdentity(tx, Sender{OwnerDeptID: 10, SourceDeviceID: dev.DeviceID}, CatalogItem{DeviceID: "folder"}, Classification{NodeType: gbmodels.NodeTypeDevice}, nil)
	}))
	require.NoError(t, db.First(&child, child.ID).Error)
	require.Equal(t, target.ID, *child.ParentID)
	require.Contains(t, child.Path, target.Path)
	require.NoError(t, db.First(&mount, mount.ID).Error)
	require.Equal(t, target.ID, mount.ParentNodeID)
	require.True(t, mount.IsPrimary)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Where("code = ?", "folder").Count(&count).Error)
	require.EqualValues(t, 1, count)
}
