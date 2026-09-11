package assign

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

func newAssignTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbRecordingFile{},
		&gbmodels.GbAlarmResource{}, &gbmodels.GbAnomalyRecord{},
		&gbmodels.GbCatalogNode{}, &gbmodels.GbChannelMount{},
		&gbmodels.GbCustomGroupDevice{}, &basemodels.SysDepartment{},
	))
	require.NoError(t, db.Exec("CREATE TABLE IF NOT EXISTS gb_cascade_device_projection (id INTEGER PRIMARY KEY AUTOINCREMENT, source_device_id INTEGER)").Error)
	// 目标部门(id=2,启用)+ 源部门(id=1)
	require.NoError(t, db.Create(&basemodels.SysDepartment{Name: "源部门", Status: int8Ptr(1)}).Error)
	require.NoError(t, db.Create(&basemodels.SysDepartment{Name: "目标部门", Status: int8Ptr(1)}).Error)
	return db
}

// 校验器:操作者可见部门 = {1,2}(源部门 1 + 目标部门 2 均在可见范围)
func int8Ptr(v int8) *int8 { return &v }

func validatorVisibleDept1(_ context.Context, _ *gorm.DB) ([]uint, bool, error) {
	return []uint{1, 2}, true, nil
}

func seedAssignedDeviceWithCode(t *testing.T, db *gorm.DB, code string) (device gbmodels.GbDevice) {
	t.Helper()
	device = gbmodels.GbDevice{DeviceID: code, OwnerDeptID: 1}
	require.NoError(t, db.Create(&device).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "c1", OwnerDeptID: 1}).Error)
	require.NoError(t, db.Create(&gbmodels.GbRecordingFile{DeviceID: device.DeviceID, OwnerDeptID: 1, FileKey: "fk-" + code}).Error)
	require.NoError(t, db.Create(&gbmodels.GbAlarmResource{DeviceID: device.ID, DeviceCode: device.DeviceID, OwnerDeptID: 1}).Error)
	require.NoError(t, db.Create(&gbmodels.GbAnomalyRecord{OwnerDeptID: 1, SourceDeviceID: &device.ID}).Error)
	require.NoError(t, db.Create(&gbmodels.GbCatalogNode{Name: "设备节点", DeviceID: &device.ID, OwnerDeptID: 1}).Error)
	var channelID uint
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("device_id = ?", device.DeviceID).Pluck("id", &channelID).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannelMount{ChannelID: channelID, OwnerDeptID: 1}).Error)
	require.NoError(t, db.Create(&gbmodels.GbCustomGroupDevice{DeviceID: device.ID}).Error)
	require.NoError(t, db.Exec("INSERT INTO gb_cascade_device_projection (source_device_id) VALUES (?)", device.ID).Error)
	return device
}

// T5 RED-1: 成功流转,八类对象全部跟随
func TestAssignOne_AllCascades(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000000001")
	svc := NewService(db, validatorVisibleDept1)

	err := svc.AssignOne(context.Background(), device.ID, 2, []uint{1}, true)
	require.NoError(t, err)

	var dev gbmodels.GbDevice
	require.NoError(t, db.First(&dev, device.ID).Error)
	assert.EqualValues(t, 2, dev.OwnerDeptID)

	var ch gbmodels.GbChannel
	require.NoError(t, db.Where("device_id = ?", device.DeviceID).First(&ch).Error)
	assert.EqualValues(t, 2, ch.OwnerDeptID)

	var f gbmodels.GbRecordingFile
	require.NoError(t, db.Where("device_id = ?", device.DeviceID).First(&f).Error)
	assert.EqualValues(t, 2, f.OwnerDeptID)

	var ar gbmodels.GbAlarmResource
	require.NoError(t, db.Where("device_id = ?", device.ID).First(&ar).Error)
	assert.EqualValues(t, 2, ar.OwnerDeptID)

	var an gbmodels.GbAnomalyRecord
	require.NoError(t, db.Where("source_device_id = ?", device.ID).First(&an).Error)
	assert.EqualValues(t, 2, an.OwnerDeptID)

	// 清理类:目录节点/挂载/分组/级联投影应删除
	var nodeCount, mountCount, groupCount, projCount int64
	var channelID uint
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("device_id = ?", device.DeviceID).Pluck("id", &channelID).Error)
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Where("device_id = ?", device.ID).Count(&nodeCount).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannelMount{}).Where("channel_id = ?", channelID).Count(&mountCount).Error)
	require.NoError(t, db.Model(&gbmodels.GbCustomGroupDevice{}).Where("device_id = ?", device.ID).Count(&groupCount).Error)
	require.NoError(t, db.Raw("SELECT count(*) FROM gb_cascade_device_projection WHERE source_device_id = ?", device.ID).Scan(&projCount).Error)
	assert.Zero(t, nodeCount)
	assert.Zero(t, mountCount)
	assert.Zero(t, groupCount)
	assert.Zero(t, projCount)
}

// T5 RED-2: 设备不在可见部门 → ErrDeviceNotVisible,且无任何数据变化
func TestAssignOne_DeviceNotVisible(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000000009")

	err := NewService(db, validatorVisibleDept1).AssignOne(context.Background(), device.ID, 2, []uint{9}, true)
	assert.ErrorIs(t, err, ErrDeviceNotVisible)

	var dev gbmodels.GbDevice
	require.NoError(t, db.First(&dev, device.ID).Error)
	assert.EqualValues(t, 1, dev.OwnerDeptID, "归属不应变化")
}

// T5 RED-3: 批量分配部分失败不影响其他(第二台设备不在可见部门)
func TestAssignBatch_PartialFailure(t *testing.T) {
	db := newAssignTestDB(t)
	d1 := seedAssignedDeviceWithCode(t, db, "34020000002000000011")
	d2 := seedAssignedDeviceWithCode(t, db, "34020000002000000012")
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("id = ?", d2.ID).Update("owner_dept_id", 9).Error)

	result, err := NewService(db, validatorVisibleDept1).AssignBatch(context.Background(), []uint{d1.ID, d2.ID}, 2)
	require.NoError(t, err)
	require.Len(t, result.Results, 2)

	assert.True(t, result.Results[0].Success)
	assert.False(t, result.Results[1].Success)

	var dev gbmodels.GbDevice
	require.NoError(t, db.First(&dev, d1.ID).Error)
	assert.EqualValues(t, 2, dev.OwnerDeptID, "第一台应流转成功")
}

// T5 RED-4: 目标部门不在可见范围 → ErrTargetDeptInvalid
func TestAssignBatch_TargetDeptNotVisible(t *testing.T) {
	db := newAssignTestDB(t)
	seedAssignedDeviceWithCode(t, db, "34020000002000000013")

	_, err := NewService(db, validatorVisibleDept1).AssignBatch(context.Background(), []uint{1}, 99)
	assert.ErrorIs(t, err, ErrTargetDeptInvalid)
}

func TestAssignBatchV2_SameTargetIsSkippedWithoutCleanup(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000000021")

	result, err := NewService(db, validatorVisibleDept1).AssignBatchV2(context.Background(), []AssignmentInput{
		{DeviceID: device.ID, ExpectedOwnerDeptID: 1},
	}, 1)
	require.NoError(t, err)
	require.Equal(t, BatchSummary{Requested: 1, Skipped: 1}, result.Summary)
	require.Len(t, result.Results, 1)
	require.Equal(t, AssignmentSkipped, result.Results[0].Status)

	var nodes int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Where("device_id = ?", device.ID).Count(&nodes).Error)
	require.EqualValues(t, 1, nodes, "跳过项不能触发关联资源清理")
}

func TestAssignBatchV2_StaleOwnerFailsClosed(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000000022")

	result, err := NewService(db, validatorVisibleDept1).AssignBatchV2(context.Background(), []AssignmentInput{
		{DeviceID: device.ID, ExpectedOwnerDeptID: 9},
	}, 2)
	require.NoError(t, err)
	require.Equal(t, BatchSummary{Requested: 1, Failed: 1}, result.Summary)
	require.Len(t, result.Results, 1)
	require.Equal(t, AssignmentFailed, result.Results[0].Status)
	require.Contains(t, result.Results[0].Message, "其他管理员")

	var persisted gbmodels.GbDevice
	require.NoError(t, db.First(&persisted, device.ID).Error)
	require.EqualValues(t, 1, persisted.OwnerDeptID)
}
