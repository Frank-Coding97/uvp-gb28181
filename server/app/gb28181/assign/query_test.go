package assign

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

func newQueryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &basemodels.SysDepartment{}))
	return db
}

func activeStatus() *int8 {
	value := int8(1)
	return &value
}

func uintPtr(value uint) *uint { return &value }

func TestQueryServiceSummaryUsesVisibleDevicesAndBuildsSubtreeCounts(t *testing.T) {
	db := newQueryTestDB(t)
	require.NoError(t, db.Create(&[]basemodels.SysDepartment{
		{BaseModel: basemodels.BaseModel{ID: 10}, Name: "父部门", Status: activeStatus()},
		{BaseModel: basemodels.BaseModel{ID: 11}, ParentID: uintPtr(10), Name: "子部门", Status: activeStatus()},
		{BaseModel: basemodels.BaseModel{ID: 12}, ParentID: uintPtr(10), Name: "空部门", Status: activeStatus()},
		{BaseModel: basemodels.BaseModel{ID: 30}, Name: "不可见部门", Status: activeStatus()},
	}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{
		{DeviceID: "summary-zero-a", OwnerDeptID: 0},
		{DeviceID: "summary-zero-b", OwnerDeptID: 0},
		{DeviceID: "summary-parent", OwnerDeptID: 10},
		{DeviceID: "summary-child-a", OwnerDeptID: 11},
		{DeviceID: "summary-child-b", OwnerDeptID: 11},
		{DeviceID: "summary-hidden", OwnerDeptID: 30},
	}).Error)

	scope := func(db *gorm.DB) *gorm.DB { return db.Where("owner_dept_id <> ?", 30) }
	summary, err := NewQueryService(db, scope).Summary(context.Background(), []uint{10, 11, 12}, true)
	require.NoError(t, err)
	require.EqualValues(t, 5, summary.AllCount)
	require.EqualValues(t, 3, summary.AssignedCount)
	require.EqualValues(t, 2, summary.UnassignedCount)
	require.Equal(t, []DepartmentCount{
		{DeptID: 10, DirectCount: 1, SubtreeCount: 3},
		{DeptID: 11, DirectCount: 2, SubtreeCount: 2},
		{DeptID: 12, DirectCount: 0, SubtreeCount: 0},
	}, summary.Departments)
}

func TestQueryServiceResolveDeduplicatesInInputOrder(t *testing.T) {
	db := newQueryTestDB(t)
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 10}, Name: "安保部", Status: activeStatus()}).Error)
	devices := []gbmodels.GbDevice{
		{DeviceID: "resolve-a", Name: "A", OwnerDeptID: 10, Status: gbmodels.DeviceStatusOnline},
		{DeviceID: "resolve-b", Name: "B", OwnerDeptID: 0},
		{DeviceID: "resolve-hidden", Name: "H", OwnerDeptID: 30},
		{DeviceID: "resolve-deleted", Name: "D", OwnerDeptID: 10},
	}
	require.NoError(t, db.Create(&devices).Error)
	require.NoError(t, db.Delete(&devices[3]).Error)

	scope := func(db *gorm.DB) *gorm.DB { return db.Where("owner_dept_id <> ?", 30) }
	result, err := NewQueryService(db, scope).Resolve(context.Background(), []uint{
		devices[1].ID, devices[0].ID, devices[1].ID, devices[2].ID, devices[3].ID, 999,
	})
	require.NoError(t, err)
	require.Equal(t, []uint{devices[1].ID, devices[0].ID}, []uint{result.Devices[0].ID, result.Devices[1].ID})
	require.Equal(t, "未分配", result.Devices[0].OwnerDeptName)
	require.Equal(t, "安保部", result.Devices[1].OwnerDeptName)
	require.True(t, result.Devices[1].Online)
	require.Equal(t, []uint{devices[2].ID, devices[3].ID, 999}, result.UnavailableIDs)
}
