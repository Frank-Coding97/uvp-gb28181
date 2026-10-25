package catalog_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// TestPipelineIngestAcceptsEightDigitCivilCode 回归 2026-10-25 LiveGBS 验收缺陷:
// 下级上报 8 位行政区划码(2022 附录 J 允许 2/4/6/8 位),而
// `gb_catalog_node.civil_code` 只有 6 位宽,生产库还物化成
// `CHECK (length(civil_code) <= 6)`;原实现把 8 位原样写入 ⇒ 单条事务回滚 ⇒
// 整批 Catalog 一条都进不了库,页面通道全空,日志只剩"部分通道未入库"。
//
// ⚠️ 单测的 AutoMigrate 不会生成那条 CHECK(所以这个缺陷此前一路绿灯),
// 因此这里断言的是"落库值本身不超过 6 位",而不是"没有报错"。
func TestPipelineIngestAcceptsEightDigitCivilCode(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	const (
		platformID = "34020000002000000001"
		streetCode = "37010301" // 8 位:济南市历下区某街道
		channelID  = "37010301021320000006"
	)

	err := p.Ingest(context.Background(), catalog.Sender{OwnerDeptID: 1, SourceDeviceID: platformID}, []catalog.CatalogItem{
		{DeviceID: streetCode, Name: "某街道"},
		{DeviceID: channelID, Name: "球机", CivilCode: streetCode, ParentID: streetCode, StatusOn: true},
	})
	require.NoError(t, err, "8 位行政区划码不应让整批目录入库失败")

	var tooLong int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Where("length(civil_code) > 6").Count(&tooLong).Error)
	require.Zero(t, tooLong, "civil_code 落库值不得超过 6 位(否则生产库 CHECK 会拒收)")

	// 节点身份仍是完整的 8 位编码:被截断的只是冗余的 civil_code 列。
	var node gbmodels.GbCatalogNode
	require.NoError(t, db.Where("node_type = ? AND code = ?", gbmodels.NodeTypeCivilCode, streetCode).
		Limit(1).Find(&node).Error)
	require.NotZero(t, node.ID, "8 位行政区划节点必须建出来")
	require.Equal(t, "370103", node.CivilCode)

	// 逐级挂链:8 位节点必须挂在 6 位区县下,不能跳级挂到 4 位市下面。
	require.NotNil(t, node.ParentID)
	var parent gbmodels.GbCatalogNode
	require.NoError(t, db.Where("id = ?", *node.ParentID).Limit(1).Find(&parent).Error)
	require.Equal(t, "370103", parent.Code)

	// 通道本身也必须入库(原实现整个 item 事务都回滚)。
	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", channelID).Limit(1).Find(&channel).Error)
	require.NotZero(t, channel.ID, "通道必须落库")
}
