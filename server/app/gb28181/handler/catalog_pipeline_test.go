package handler

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

func init() {
	// 单测兜底:给 app.ZapLog 一个 nop logger,防 Handle* 路径 nil 解引用
	if app.ZapLog == nil {
		app.ZapLog = zap.NewNop()
	}
}

// TestHandleCatalogResponse_PipelineIntegration A4 改造回归测试
//
// 准备:sqlite + AutoMigrate + 注入 Pipeline
// 推一条带 2 个通道的 Catalog 应答
// 期望:gb_channel + gb_catalog_node + gb_channel_mount 都有记录
func TestHandleCatalogResponse_PipelineIntegration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbCatalogNode{},
		&gbmodels.GbChannelMount{},
		&gbmodels.GbAnomalyRecord{},
		&gbmodels.GbChannel{},
		&gbmodels.GbDevice{},
	))

	SetCatalogPipeline(catalog.New(db))
	t.Cleanup(func() { SetCatalogPipeline(nil) })

	body := []byte(`<?xml version="1.0" encoding="GB2312"?>
<Response>
<CmdType>Catalog</CmdType>
<SN>1</SN>
<DeviceID>34020000002000000001</DeviceID>
<SumNum>2</SumNum>
<DeviceList Num="2">
<Item>
<DeviceID>37011200001310000001</DeviceID>
<Name>通道 1</Name>
<CivilCode>370112</CivilCode>
<Status>ON</Status>
</Item>
<Item>
<DeviceID>37011200001310000002</DeviceID>
<Name>通道 2</Name>
<CivilCode>370112</CivilCode>
<Status>OFF</Status>
</Item>
</DeviceList>
</Response>`)

	HandleCatalogResponse(context.Background(), body)

	// gb_channel 2 条
	var chCount int64
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Count(&chCount).Error)
	assert.EqualValues(t, 2, chCount, "2 个通道应入 gb_channel")

	// gb_catalog_node 至少 2 个 channel 节点
	var nodes []gbmodels.GbCatalogNode
	require.NoError(t, db.Where("node_type = ?", gbmodels.NodeTypeChannel).Find(&nodes).Error)
	assert.Len(t, nodes, 2, "2 个 channel 节点")

	// gb_channel_mount 2 条主挂载
	var mounts int64
	require.NoError(t, db.Model(&gbmodels.GbChannelMount{}).Where("is_primary = ?", true).Count(&mounts).Error)
	assert.EqualValues(t, 2, mounts)

	// 状态正确
	var on int64
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("status = ?", gbmodels.ChannelStatusOnline).Count(&on).Error)
	assert.EqualValues(t, 1, on)
}

// TestHandleCatalogResponse_NoPipelineSafe Pipeline 未注入时不崩
func TestHandleCatalogResponse_NoPipelineSafe(t *testing.T) {
	SetCatalogPipeline(nil)
	// app.DB() 在单测 nil,getCatalogPipeline 返回 nil,handler 仅打日志不崩
	body := []byte(`<?xml version="1.0"?><Response><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>X</DeviceID><SumNum>0</SumNum><DeviceList Num="0"></DeviceList></Response>`)
	// 不 panic 即通过
	require.NotPanics(t, func() {
		HandleCatalogResponse(context.Background(), body)
	})
}
