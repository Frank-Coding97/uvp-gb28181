package catalog_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestPipeline_CompatibleLeafCodes(t *testing.T) {
	for _, code := range []string{"34020000001410000020", "vendor-camera-1", "12345"} {
		t.Run(code, func(t *testing.T) {
			db := newPipelineTestDB(t)
			p := catalog.New(db)
			sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "34020000001411684864"}
			require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: sender.SourceDeviceID, OwnerDeptID: 10}).Error)
			items := []catalog.CatalogItem{{DeviceID: code, ParentID: sender.SourceDeviceID, StatusOn: true}, {DeviceID: sender.SourceDeviceID, Parental: 1}}
			for i := 0; i < 2; i++ {
				require.NoError(t, p.Ingest(context.Background(), sender, items))
			}
			var channels []gbmodels.GbChannel
			require.NoError(t, db.Find(&channels).Error)
			require.Len(t, channels, 1)
			require.Equal(t, code, channels[0].ChannelID)
			require.Equal(t, sender.SourceDeviceID, channels[0].DeviceID)
			var root, leaf gbmodels.GbCatalogNode
			require.NoError(t, db.Where("code = ?", sender.SourceDeviceID).First(&root).Error)
			require.NoError(t, db.Where("code = ?", code).First(&leaf).Error)
			require.Equal(t, gbmodels.NodeTypeDevice, root.NodeType)
			require.Equal(t, gbmodels.NodeTypeChannel, leaf.NodeType)
			require.Equal(t, root.ID, *leaf.ParentID)
			require.True(t, leaf.Anomaly)
			var audit gbmodels.GbAnomalyRecord
			require.NoError(t, db.Where("catalog_node_id = ?", leaf.ID).First(&audit).Error)
			require.Equal(t, gbmodels.FallbackTypeChannel, audit.FallbackType)
		})
	}
}

func TestPipeline_DeviceRootAndSameCodeIPC(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "37010301021320000021"}
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: sender.SourceDeviceID, OwnerDeptID: 10}).Error)
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{{DeviceID: sender.SourceDeviceID}}))
	var old gbmodels.GbChannel
	require.NoError(t, db.First(&old).Error)
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{{DeviceID: sender.SourceDeviceID, Parental: 1}}))
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Count(&count).Error)
	require.Zero(t, count)
	var node gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", sender.SourceDeviceID).First(&node).Error)
	require.Equal(t, gbmodels.NodeTypeDevice, node.NodeType)
	require.Nil(t, node.ChannelID)
	require.NoError(t, p.IngestDelta(context.Background(), sender, "UPDATE", catalog.CatalogItem{DeviceID: sender.SourceDeviceID, StatusOn: true}))
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestPipeline_RecoversOldAnomalyNode(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	dev := gbmodels.GbDevice{DeviceID: "34020000001411684864", OwnerDeptID: 10}
	require.NoError(t, db.Create(&dev).Error)
	old := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeVirtualOrg, Code: "34020000001410000020", Path: "/", Anomaly: true, Source: gbmodels.NodeSourceCatalog}
	require.NoError(t, db.Create(&old).Error)
	require.NoError(t, db.Create(&gbmodels.GbAnomalyRecord{OwnerDeptID: 10, CatalogNodeID: old.ID, SourceDeviceID: &dev.ID, RawCode: old.Code, FallbackType: gbmodels.FallbackTypeVirtualOrg}).Error)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: dev.DeviceID}
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{{DeviceID: old.Code}}))
	var nodes []gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", old.Code).Find(&nodes).Error)
	require.Len(t, nodes, 1)
	require.Equal(t, old.ID, nodes[0].ID)
	require.Equal(t, gbmodels.NodeTypeChannel, nodes[0].NodeType)
	require.NotNil(t, nodes[0].ChannelID)
}

func TestPipeline_ExplicitSameCodeLeafRestoresIPC(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "37010301021320000021"}
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: sender.SourceDeviceID, OwnerDeptID: 10}).Error)
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{{DeviceID: sender.SourceDeviceID, Parental: 1}}))
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{{DeviceID: sender.SourceDeviceID, ParentalPresent: true, StatusOn: true}}))
	var ch gbmodels.GbChannel
	require.NoError(t, db.First(&ch).Error)
	require.Equal(t, sender.SourceDeviceID, ch.ChannelID)
}

func TestPipeline_IdentityRecoveryPreservesOtherSourcesAndManualMounts(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "37010301021320000021"}
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: sender.SourceDeviceID, OwnerDeptID: 10}).Error)
	other := gbmodels.GbChannel{DeviceID: "other-device", ChannelID: sender.SourceDeviceID, OwnerDeptID: 10}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&gbmodels.GbCatalogNode{OwnerDeptID: 10, Code: sender.SourceDeviceID, NodeType: gbmodels.NodeTypeChannel, ChannelID: &other.ID, Source: gbmodels.NodeSourceCatalog, Path: "/"}).Error)
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{{DeviceID: sender.SourceDeviceID}}))
	var ch gbmodels.GbChannel
	require.NoError(t, db.Where("device_id = ?", sender.SourceDeviceID).First(&ch).Error)
	mount := gbmodels.GbChannelMount{OwnerDeptID: 10, ChannelID: ch.ID, ParentNodeID: 999, MountSource: gbmodels.MountSourceManual}
	require.NoError(t, db.Create(&mount).Error)
	require.ErrorContains(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{{DeviceID: sender.SourceDeviceID, Parental: 1}}), "stop or unmount")
	require.NoError(t, db.First(&mount, mount.ID).Error)
	require.NoError(t, db.First(&ch, ch.ID).Error)
	require.NoError(t, db.First(&other, other.ID).Error)
}

func TestPipeline_IdentityRecoveryDoesNotDeleteActiveSession(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "37010301021320000021"}
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: sender.SourceDeviceID, OwnerDeptID: 10}).Error)
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{{DeviceID: sender.SourceDeviceID}}))
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("device_id = ?", sender.SourceDeviceID).Update("stream_id", "active-stream").Error)
	require.ErrorContains(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{{DeviceID: sender.SourceDeviceID, Parental: 1}}), "stop or unmount")
	var ch gbmodels.GbChannel
	require.NoError(t, db.First(&ch).Error)
	require.Equal(t, "active-stream", ch.StreamID)
}

func TestPipeline_UnknownParentByRelationship(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "registered-device"}
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: sender.SourceDeviceID, OwnerDeptID: 10}).Error)
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: "camera", ParentID: "private-folder"}, {DeviceID: "private-folder", ParentID: sender.SourceDeviceID},
		{DeviceID: sender.SourceDeviceID, Parental: 1},
	}))
	var channels []gbmodels.GbChannel
	require.NoError(t, db.Find(&channels).Error)
	require.Len(t, channels, 1)
	require.Equal(t, "camera", channels[0].ChannelID)
	require.NoError(t, p.IngestDelta(context.Background(), sender, "UPDATE", catalog.CatalogItem{DeviceID: "private-folder", StatusOn: true}))
	require.NoError(t, db.Find(&channels).Error)
	require.Len(t, channels, 1)
}

func TestPipeline_RecoveryKeepsOneNodePerMount(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	dev := gbmodels.GbDevice{DeviceID: "registered-device", OwnerDeptID: 10}
	require.NoError(t, db.Create(&dev).Error)
	code := "34020000001410000020"
	for i := 0; i < 2; i++ {
		old := gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeVirtualOrg, Code: code, Path: "/", Anomaly: true, Source: gbmodels.NodeSourceCatalog}
		require.NoError(t, db.Create(&old).Error)
		require.NoError(t, db.Create(&gbmodels.GbAnomalyRecord{OwnerDeptID: 10, CatalogNodeID: old.ID, SourceDeviceID: &dev.ID}).Error)
	}
	require.NoError(t, p.Ingest(context.Background(), catalog.Sender{OwnerDeptID: 10, SourceDeviceID: dev.DeviceID}, []catalog.CatalogItem{{DeviceID: code}}))
	var nodes []gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", code).Find(&nodes).Error)
	require.Len(t, nodes, 1)
}

func TestPipeline_RecoveryPreservesOtherDepartments(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	const code = "37010301021320000021"
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: code, OwnerDeptID: 10}).Error)
	other := gbmodels.GbChannel{DeviceID: code, ChannelID: code, OwnerDeptID: 20}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&gbmodels.GbCatalogNode{OwnerDeptID: 20, Code: code, NodeType: gbmodels.NodeTypeChannel, ChannelID: &other.ID, Source: gbmodels.NodeSourceCatalog, Path: "/"}).Error)
	require.NoError(t, p.Ingest(context.Background(), catalog.Sender{OwnerDeptID: 10, SourceDeviceID: code}, []catalog.CatalogItem{{DeviceID: code, Parental: 1}}))
	require.NoError(t, db.First(&other, other.ID).Error)
	require.Equal(t, uint(20), other.OwnerDeptID)
}
