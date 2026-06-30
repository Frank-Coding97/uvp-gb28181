package catalog_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newPipelineTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbCatalogNode{},
		&gbmodels.GbChannelMount{},
		&gbmodels.GbAnomalyRecord{},
		&gbmodels.GbChannel{},
		&gbmodels.GbDevice{},
	))
	return db
}

const tenantID uint = 1

// TestIngest_FiveLevelCatalog A3.3 五层结构入库 + 物化路径
//
// 模拟器推一批 catalog item:
//   - 6 个 channel 挂在济南历城区 370112
//   - 期望 catalog_node 至少 6 个 + 行政区链 + 主挂载 6 个
func TestIngest_FiveLevelCatalog(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)

	items := []catalog.CatalogItem{
		{DeviceID: "37011200001310000001", Name: "通道 1", CivilCode: "370112", StatusOn: true},
		{DeviceID: "37011200001310000002", Name: "通道 2", CivilCode: "370112", StatusOn: true},
		{DeviceID: "37011200001310000003", Name: "通道 3", CivilCode: "370112", StatusOn: false},
		{DeviceID: "37011200001310000004", Name: "通道 4", CivilCode: "370112"},
		{DeviceID: "37011200001310000005", Name: "通道 5", CivilCode: "370112"},
		{DeviceID: "37011200001310000006", Name: "通道 6", CivilCode: "370112"},
	}
	require.NoError(t, p.Ingest(context.Background(), catalog.Sender{TenantID: tenantID, SourceDeviceID: "34020000002000000001"}, items))

	// catalog_node 应至少 6 个 channel 节点 + 3 级行政区链
	var totalNodes int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Count(&totalNodes).Error)
	assert.GreaterOrEqual(t, totalNodes, int64(6+3), "至少 6 channel + 3 级 civil_code 节点")

	// 通道节点物化路径非空 + 含 /
	var channelNodes []gbmodels.GbCatalogNode
	require.NoError(t, db.Where("node_type = ?", gbmodels.NodeTypeChannel).Find(&channelNodes).Error)
	assert.Len(t, channelNodes, 6)
	for _, n := range channelNodes {
		assert.NotEmpty(t, n.Path)
		assert.Contains(t, n.Path, "/")
	}

	// gb_channel_mount 应有 6 条主挂载
	var primaryCount int64
	require.NoError(t, db.Model(&gbmodels.GbChannelMount{}).Where("is_primary = ?", true).Count(&primaryCount).Error)
	assert.EqualValues(t, 6, primaryCount, "每个 channel 必有且仅有一个主挂载")

	// gb_channel 物理通道也建了
	var chCount int64
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Count(&chCount).Error)
	assert.EqualValues(t, 6, chCount)
}

// TestIngest_Idempotent A3.3 二次入库不重复创建
func TestIngest_Idempotent(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{TenantID: tenantID, SourceDeviceID: "34020000002000000001"}

	item := catalog.CatalogItem{DeviceID: "37011200001310000001", Name: "通道", CivilCode: "370112", StatusOn: true}
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{item}))

	var n1, m1 int64
	db.Model(&gbmodels.GbCatalogNode{}).Count(&n1)
	db.Model(&gbmodels.GbChannelMount{}).Count(&m1)

	// 二次入库
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{item}))
	var n2, m2 int64
	db.Model(&gbmodels.GbCatalogNode{}).Count(&n2)
	db.Model(&gbmodels.GbChannelMount{}).Count(&m2)

	assert.Equal(t, n1, n2, "catalog_node 数不变")
	assert.Equal(t, m1, m2, "channel_mount 数不变")
}

// TestIngest_AnomalyFallback A3.4 不规范编码 → virtual_org + anomaly_record
func TestIngest_AnomalyFallback(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)

	items := []catalog.CatalogItem{
		// 厂商私有非数字编码 → 兜底
		{DeviceID: "UVP-PRIVATE-X0001", Name: "私有通道", StatusOn: true},
		// 长度不符 → 兜底
		{DeviceID: "12345", Name: "短码节点"},
	}
	require.NoError(t, p.Ingest(context.Background(), catalog.Sender{TenantID: tenantID}, items))

	// 应建 2 个 anomaly 节点(virtual_org)+ 2 条 anomaly_record
	var anomalyNodes int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Where("anomaly = ?", true).Count(&anomalyNodes).Error)
	assert.EqualValues(t, 2, anomalyNodes)

	var records int64
	require.NoError(t, db.Model(&gbmodels.GbAnomalyRecord{}).Count(&records).Error)
	assert.EqualValues(t, 2, records, "每个 anomaly 节点对应一条审计")

	// raw_code 非空
	var sample gbmodels.GbCatalogNode
	require.NoError(t, db.Where("anomaly = ?", true).First(&sample).Error)
	assert.NotEmpty(t, sample.RawCode)
	assert.NotEmpty(t, sample.AnomalyReason)
}

// TestIngestDelta_AddUpdateDel A3.5 增量三态
func TestIngestDelta_AddUpdateDel(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	ctx := context.Background()
	sender := catalog.Sender{TenantID: tenantID, SourceDeviceID: "34020000002000000001"}

	it := catalog.CatalogItem{DeviceID: "37011200001310000001", Name: "原名", CivilCode: "370112", StatusOn: true}
	require.NoError(t, p.IngestDelta(ctx, sender, "ADD", it))

	var n1 gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", it.DeviceID).First(&n1).Error)
	assert.Equal(t, "原名", n1.Name)
	_ = n1

	// UPDATE 改名(通过 gb_channel 表观察)
	it.Name = "新名"
	require.NoError(t, p.IngestDelta(ctx, sender, "UPDATE", it))
	var ch gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", it.DeviceID).First(&ch).Error)
	assert.Equal(t, "新名", ch.Name)

	// DEL 软删
	require.NoError(t, p.IngestDelta(ctx, sender, "DEL", it))
	var leftover int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Where("code = ?", it.DeviceID).Count(&leftover).Error)
	assert.EqualValues(t, 0, leftover, "DEL 后软删,常规 Count 应过滤掉")
}

// TestBuildPath / TestDepthFromPath 路径工具单测
func TestBuildPath(t *testing.T) {
	assert.Equal(t, "/12/", catalog.BuildPath("/", 12))
	assert.Equal(t, "/12/47/", catalog.BuildPath("/12/", 47))
	assert.Equal(t, "/12/47/189/", catalog.BuildPath("/12/47/", 189))
	assert.Equal(t, "/5/", catalog.BuildPath("", 5)) // 空 parentPath 兜底成 "/"
}

// TestIngest_DepthChain 回归 bug §2:civil_code 链不应跳级(2026-06-30 接口手验时发现)
//
// 期望:行政区 6 位级走 2/4/6 拆 → root=depth 0 / 4 位级=depth 1 / 6 位级=depth 2 / channel 节点=depth 3
// 之前的 bug:DepthFromPath + 又 +1 → 0/2/3/4(每级多 1)
func TestIngest_DepthChain(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)

	require.NoError(t, p.Ingest(context.Background(),
		catalog.Sender{TenantID: tenantID, SourceDeviceID: "34020000002000000001"},
		[]catalog.CatalogItem{
			{DeviceID: "37011200001310000001", Name: "通道", CivilCode: "370112", StatusOn: true},
		}))

	type row struct {
		Code  string
		Depth uint8
	}
	var rows []row
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).
		Select("code, depth").
		Where("node_type = ? OR node_type = ?", gbmodels.NodeTypeCivilCode, gbmodels.NodeTypeChannel).
		Order("depth, code").Find(&rows).Error)

	want := map[string]uint8{
		"37":                   0, // 根行政区
		"3701":                 1,
		"370112":               2,
		"37011200001310000001": 3, // channel 挂在 370112 下
	}
	got := map[string]uint8{}
	for _, r := range rows {
		got[r.Code] = r.Depth
	}
	for code, exp := range want {
		assert.Equal(t, exp, got[code], "节点 %s 期望 depth=%d 实际 %d", code, exp, got[code])
	}
}

func TestDepthFromPath(t *testing.T) {
	assert.Equal(t, uint8(0), catalog.DepthFromPath("/"))
	assert.Equal(t, uint8(0), catalog.DepthFromPath(""))
	assert.Equal(t, uint8(1), catalog.DepthFromPath("/12/"))
	assert.Equal(t, uint8(3), catalog.DepthFromPath("/12/47/189/"))
}
