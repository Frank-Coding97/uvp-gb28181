package catalog_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// TestPipelineIngestIsIdempotent 守住"同一批目录重复上报不制造重复节点"。
//
// 级联场景下下级会周期性重发 Catalog(以及应答平台主动查询),
// 如果每批都新插一遍节点,目录树会指数级膨胀,页面出现同名同码多条记录。
// 2026-10-25 在 220 上对 LiveGBS 做了两次目录刷新(08:28 / 08:49),
// 行政区链与通道都没有新增行,这里把它固化成用例。
func TestPipelineIngestIsIdempotent(t *testing.T) {
	const (
		platformID = "34020000002000000001"
		streetCode = "37010301"
		channelID  = "37010301021320000006"
	)
	sender := catalog.Sender{OwnerDeptID: 1, SourceDeviceID: platformID}
	items := []catalog.CatalogItem{
		{DeviceID: streetCode, Name: "某街道"},
		{DeviceID: channelID, Name: "球机", CivilCode: streetCode, ParentID: streetCode, StatusOn: true},
	}

	db := newPipelineTestDB(t)
	p := catalog.New(db)

	require.NoError(t, p.Ingest(context.Background(), sender, items))

	var nodesAfterFirst, channelsAfterFirst int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Count(&nodesAfterFirst).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", channelID).Count(&channelsAfterFirst).Error)
	require.NotZero(t, nodesAfterFirst)
	require.Equal(t, int64(1), channelsAfterFirst)

	// 第二、三次上报必须完全复用既有节点。
	for round := 2; round <= 3; round++ {
		require.NoError(t, p.Ingest(context.Background(), sender, items), "第 %d 次上报不该失败", round)

		var nodes, channels int64
		require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Count(&nodes).Error)
		require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", channelID).Count(&channels).Error)
		require.Equal(t, nodesAfterFirst, nodes, "第 %d 次上报不应新增目录节点", round)
		require.Equal(t, int64(1), channels, "第 %d 次上报不应新增通道", round)
	}

	// 行政区链必须仍是一条 37 -> 3701 -> 370103 -> 37010301。
	var chain []gbmodels.GbCatalogNode
	require.NoError(t, db.Where("node_type = ? AND code IN ?", gbmodels.NodeTypeCivilCode,
		[]string{"37", "3701", "370103", "37010301"}).Order("length(code)").Find(&chain).Error)
	require.Len(t, chain, 4, "行政区链不应出现重复分支")
}
