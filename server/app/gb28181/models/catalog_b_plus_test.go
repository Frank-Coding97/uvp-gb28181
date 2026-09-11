package models_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// newCatalogTestDB sqlite in-memory + AutoMigrate 4 张目录新表
// 复用 zlm/repo 范式:跑测试不依赖 MySQL 真库,CI 友好
func newCatalogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbCatalogNode{},
		&gbmodels.GbChannelMount{},
		&gbmodels.GbAnomalyRecord{},
		&gbmodels.GbChannel{},
		&gbmodels.GbDevice{},
		&gbmodels.GbAlarmResource{},
		&gbmodels.GbAlarmResourceParent{},
		&gbmodels.GbAlarmBinding{},
	))
	return db
}

func TestAlarmResource_AutoMigrateAndRelations(t *testing.T) {
	db := newCatalogTestDB(t)
	mig := db.Migrator()

	require.True(t, mig.HasTable(&gbmodels.GbAlarmResource{}))
	require.True(t, mig.HasTable(&gbmodels.GbAlarmResourceParent{}))
	require.True(t, mig.HasTable(&gbmodels.GbAlarmBinding{}))
	assert.True(t, mig.HasColumn(&gbmodels.GbCatalogNode{}, "alarm_resource_id"))
	assert.True(t, mig.HasIndex(&gbmodels.GbAlarmResource{}, "uk_alarm_resource_code"))
	assert.True(t, mig.HasIndex(&gbmodels.GbAlarmResource{}, "idx_alarm_resource_device_id"))
	assert.True(t, mig.HasIndex(&gbmodels.GbAlarmResource{}, "idx_alarm_resource_alarm_code"))
	assert.True(t, mig.HasIndex(&gbmodels.GbAlarmResource{}, "idx_alarm_resource_type"))
	assert.True(t, mig.HasIndex(&gbmodels.GbAlarmResource{}, "idx_alarm_resource_deleted_at"))
	assert.True(t, mig.HasIndex(&gbmodels.GbAlarmResourceParent{}, "uk_alarm_resource_parent"))
	assert.True(t, mig.HasIndex(&gbmodels.GbAlarmResourceParent{}, "idx_alarm_parent_resource"))
	assert.True(t, mig.HasIndex(&gbmodels.GbAlarmResourceParent{}, "idx_alarm_parent_code"))
	assert.True(t, mig.HasIndex(&gbmodels.GbAlarmBinding{}, "uk_alarm_binding_channel"))
	assert.True(t, mig.HasIndex(&gbmodels.GbAlarmBinding{}, "idx_alarm_binding_device"))
	assert.True(t, mig.HasIndex(&gbmodels.GbAlarmBinding{}, "idx_alarm_binding_resource"))
	assert.True(t, mig.HasIndex(&gbmodels.GbCatalogNode{}, "idx_catalog_alarm_resource"))

	resource := &gbmodels.GbAlarmResource{
		OwnerDeptID: 1, DeviceID: 7, DeviceCode: "34020000001180000001",
		AlarmCode: "34020000001340000001", ResourceType: gbmodels.AlarmResourceInput,
	}
	require.NoError(t, db.Create(resource).Error)
	require.NoError(t, db.Create(&gbmodels.GbAlarmResourceParent{AlarmResourceID: resource.ID, ParentCode: "34020000001310000001"}).Error)
	err := db.Create(&gbmodels.GbAlarmResourceParent{AlarmResourceID: resource.ID, ParentCode: "34020000001310000001"}).Error
	assert.Error(t, err, "同一报警资源的父编码必须去重")
}

// TestCatalogNode_AutoMigrate A1.1 RED-验证 1+3:GbCatalogNode 字段/索引就位
func TestCatalogNode_AutoMigrate(t *testing.T) {
	db := newCatalogTestDB(t)
	mig := db.Migrator()

	require.True(t, mig.HasTable(&gbmodels.GbCatalogNode{}), "gb_catalog_node 表应存在")
	assert.True(t, mig.HasColumn(&gbmodels.GbCatalogNode{}, "anomaly"))
	assert.True(t, mig.HasColumn(&gbmodels.GbCatalogNode{}, "raw_code"))
	assert.True(t, mig.HasColumn(&gbmodels.GbCatalogNode{}, "path"))
	assert.True(t, mig.HasColumn(&gbmodels.GbCatalogNode{}, "depth"))
	assert.True(t, mig.HasColumn(&gbmodels.GbCatalogNode{}, "node_type"))

	// 索引存在(plan §3.4)
	assert.True(t, mig.HasIndex(&gbmodels.GbCatalogNode{}, "idx_owner_dept_anomaly"))
	assert.True(t, mig.HasIndex(&gbmodels.GbCatalogNode{}, "idx_owner_dept_path"))
	assert.True(t, mig.HasIndex(&gbmodels.GbCatalogNode{}, "idx_owner_dept_type"))
}

// TestChannelMount_UniqueConstraint A1.1 RED-验证 2:多挂载唯一约束 (channel_id, parent_node_id)
func TestChannelMount_UniqueConstraint(t *testing.T) {
	db := newCatalogTestDB(t)

	m1 := &gbmodels.GbChannelMount{ChannelID: 1, ParentNodeID: 10, IsPrimary: true}
	require.NoError(t, db.Create(m1).Error)

	// 同一 channel 挂同一 parent 第二次应失败(唯一约束)
	m2 := &gbmodels.GbChannelMount{ChannelID: 1, ParentNodeID: 10, IsPrimary: false}
	err := db.Create(m2).Error
	assert.Error(t, err, "重复挂载应被唯一约束拦截")

	// 同一 channel 挂不同 parent 应允许(多挂载)
	m3 := &gbmodels.GbChannelMount{ChannelID: 1, ParentNodeID: 20, IsPrimary: false}
	require.NoError(t, db.Create(m3).Error, "挂不同节点应允许")
}

// TestAnomalyRecord_AutoMigrate A1.1 RED-验证:anomaly 表字段就位
func TestAnomalyRecord_AutoMigrate(t *testing.T) {
	db := newCatalogTestDB(t)
	mig := db.Migrator()

	require.True(t, mig.HasTable(&gbmodels.GbAnomalyRecord{}))
	assert.True(t, mig.HasColumn(&gbmodels.GbAnomalyRecord{}, "raw_code"))
	assert.True(t, mig.HasColumn(&gbmodels.GbAnomalyRecord{}, "fallback_type"))
	assert.True(t, mig.HasColumn(&gbmodels.GbAnomalyRecord{}, "resolved"))
	assert.True(t, mig.HasIndex(&gbmodels.GbAnomalyRecord{}, "idx_owner_dept_resolved"))
}

// TestDevice_SubscribeFields A1.1 RED-验证 4:gb_device 加 subscribe_* 字段
func TestDevice_SubscribeFields(t *testing.T) {
	db := newCatalogTestDB(t)
	mig := db.Migrator()

	assert.True(t, mig.HasColumn(&gbmodels.GbDevice{}, "subscribe_capability"))
	assert.True(t, mig.HasColumn(&gbmodels.GbDevice{}, "subscribe_last_test"))
	assert.True(t, mig.HasColumn(&gbmodels.GbDevice{}, "subscribe_expires_at"))

	// 默认值应为 'unknown'(insert 不带该字段,读出来应是 unknown)
	d := &gbmodels.GbDevice{DeviceID: "34020000001320000001", Name: "test"}
	require.NoError(t, db.Create(d).Error)
	var got gbmodels.GbDevice
	require.NoError(t, db.First(&got, d.ID).Error)
	assert.Equal(t, gbmodels.SubscribeUnknown, got.SubscribeCapability,
		"默认 subscribe_capability 应为 unknown")
}

// TestChannel_CapabilitiesField A1.1 RED-验证:gb_channel 加 capabilities JSON
func TestChannel_CapabilitiesField(t *testing.T) {
	db := newCatalogTestDB(t)
	mig := db.Migrator()
	assert.True(t, mig.HasColumn(&gbmodels.GbChannel{}, "capabilities"))

	caps := `{"audio":true,"h265":false}`
	ch := &gbmodels.GbChannel{
		DeviceID:     "34020000001320000001",
		ChannelID:    "34020000001310000001",
		Name:         "test ch",
		Capabilities: &caps,
	}
	require.NoError(t, db.Create(ch).Error)

	var got gbmodels.GbChannel
	require.NoError(t, db.First(&got, ch.ID).Error)
	require.NotNil(t, got.Capabilities, "Capabilities 应回读非 nil")
	assert.Contains(t, *got.Capabilities, "audio")
}

// TestCatalogNode_PathSubtreeQuery A1.1 RED-验证:物化路径子树 LIKE 查询
func TestCatalogNode_PathSubtreeQuery(t *testing.T) {
	db := newCatalogTestDB(t)

	nodes := []*gbmodels.GbCatalogNode{
		{NodeType: gbmodels.NodeTypeCivilCode, Path: "/", Depth: 0, Name: "root", Code: "37"},
		{NodeType: gbmodels.NodeTypeCivilCode, Path: "/1/", Depth: 1, Name: "济南", Code: "370100"},
		{NodeType: gbmodels.NodeTypeCivilCode, Path: "/1/2/", Depth: 2, Name: "历城区", Code: "370105"},
		{NodeType: gbmodels.NodeTypeChannel, Path: "/1/2/3/", Depth: 3, Name: "通道 1"},
	}
	for _, n := range nodes {
		require.NoError(t, db.Create(n).Error)
	}

	// 子树查询:济南节点下所有子孙
	var sub []gbmodels.GbCatalogNode
	require.NoError(t, db.Where("path LIKE ?", "/1/%").Find(&sub).Error)
	assert.GreaterOrEqual(t, len(sub), 2, "济南下应至少 2 个子孙(历城区 + 通道 1)")
}
