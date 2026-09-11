package handler

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

func catalogResponseBody(sn, sumNum int, deviceID string, itemIDs ...string) []byte {
	items := ""
	for _, itemID := range itemIDs {
		items += fmt.Sprintf(`<Item><DeviceID>%s</DeviceID><Name>%s</Name><CivilCode>370112</CivilCode><Status>ON</Status></Item>`, itemID, itemID)
	}
	return []byte(fmt.Sprintf(`<Response><CmdType>Catalog</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><SumNum>%d</SumNum><DeviceList Num="%d">%s</DeviceList></Response>`, sn, deviceID, sumNum, len(itemIDs), items))
}

type catalogSQLiteConfig struct{ app.YmlConfigInterf }

func (catalogSQLiteConfig) GetString(string) string { return "sqlite" }

func TestCatalogPipelineUsesConfiguredSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldConfig, oldSQLite, oldMySQL := app.ConfigYml, app.GormDbSQLite, app.GormDbMysql
	SetCatalogPipeline(nil)
	t.Cleanup(func() {
		SetCatalogPipeline(nil)
		app.ConfigYml, app.GormDbSQLite, app.GormDbMysql = oldConfig, oldSQLite, oldMySQL
		raw, _ := db.DB()
		_ = raw.Close()
	})
	app.ConfigYml = catalogSQLiteConfig{}
	app.GormDbSQLite, app.GormDbMysql = db, nil
	require.NotNil(t, getCatalogPipeline())
}

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

	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID:            "34020000002000000001",
		OwnerDeptID:         1,
		SubscribeCapability: gbmodels.SubscribeUnknown,
	}).Error)
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

func TestHandleCatalogResponse_AggregatesByDeviceAndSNBeforeIngest(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbCatalogNode{}, &gbmodels.GbChannelMount{}, &gbmodels.GbAnomalyRecord{},
		&gbmodels.GbChannel{}, &gbmodels.GbDevice{},
	))
	deviceID := "34020000002000000001"
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: deviceID, OwnerDeptID: 1}).Error)
	SetCatalogPipeline(catalog.New(db))
	catalogAgg.reset()
	t.Cleanup(func() {
		SetCatalogPipeline(nil)
		catalogAgg.reset()
	})

	// SN=10 的第一包未收齐，不能提前把部分目录写入数据库。
	HandleCatalogResponse(context.Background(), catalogResponseBody(10, 2, deviceID, "37011200001310000001"))
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Count(&count).Error)
	require.Zero(t, count)

	// 重复包不能增加聚合进度。
	HandleCatalogResponse(context.Background(), catalogResponseBody(10, 2, deviceID, "37011200001310000001"))
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Count(&count).Error)
	require.Zero(t, count)

	// 同设备另一个 SN 应独立聚合并可以先完成。
	HandleCatalogResponse(context.Background(), catalogResponseBody(
		11, 2, deviceID,
		"37011200001310000002", "37011200001310000003",
	))
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Count(&count).Error)
	require.EqualValues(t, 2, count)

	// SN=10 补齐后，再一次性落入它自己的两条结果。
	HandleCatalogResponse(context.Background(), catalogResponseBody(10, 2, deviceID, "37011200001310000004"))
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Count(&count).Error)
	require.EqualValues(t, 4, count)
}

func TestHandleCatalogResponse_EmptyResponseDoesNotLeaveBucket(t *testing.T) {
	SetCatalogPipeline(nil)
	catalogAgg.reset()
	t.Cleanup(func() { catalogAgg.reset() })

	HandleCatalogResponse(context.Background(), catalogResponseBody(7, 0, "empty-device"))
	require.Equal(t, 0, catalogAgg.active())
}
