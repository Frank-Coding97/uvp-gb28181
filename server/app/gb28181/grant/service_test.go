package grant

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

func newGrantServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{},
		&gbmodels.GbDeviceGrant{},
		&basemodels.SysDepartment{},
		&basemodels.User{},
	))
	return db
}

func TestServiceQueryReturnsNamedGrantsAndStableRevision(t *testing.T) {
	db := newGrantServiceTestDB(t)
	active := int8(1)
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 10}, Name: "安保部", Status: &active}).Error)
	require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: 7}, Username: "guard", NickName: "值班员", Password: "x", Status: 1, DeptID: 10}).Error)
	devices := []gbmodels.GbDevice{
		{DeviceID: "query-a", Name: "设备 A", OwnerDeptID: 10},
		{DeviceID: "query-b", Name: "设备 B", OwnerDeptID: 20},
	}
	require.NoError(t, db.Create(&devices).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbDeviceGrant{
		{DeviceID: devices[0].ID, TargetType: gbmodels.GrantTargetTypeUser, TargetID: 7},
		{DeviceID: devices[0].ID, TargetType: gbmodels.GrantTargetTypeDept, TargetID: 10},
		{DeviceID: devices[1].ID, TargetType: gbmodels.GrantTargetTypeDept, TargetID: 10},
	}).Error)

	service := NewService(db, func(query *gorm.DB) *gorm.DB {
		return query.Where("owner_dept_id = ?", 10)
	})
	first, err := service.Query(context.Background(), []uint{devices[0].ID, devices[1].ID, 999, devices[0].ID})
	require.NoError(t, err)
	require.Len(t, first.Devices, 1)
	require.Equal(t, []uint{devices[1].ID, 999}, first.UnavailableIDs)
	require.Len(t, first.Devices[0].Grants, 2)
	require.Equal(t, "安保部", first.Devices[0].Grants[0].TargetName)
	require.Equal(t, "值班员", first.Devices[0].Grants[1].TargetName)
	require.NotEmpty(t, first.Devices[0].Revision)

	second, err := service.Query(context.Background(), []uint{devices[0].ID})
	require.NoError(t, err)
	require.Equal(t, first.Devices[0].Revision, second.Devices[0].Revision)

	require.NoError(t, db.Where("device_id = ? AND target_type = ?", devices[0].ID, gbmodels.GrantTargetTypeDept).Delete(&gbmodels.GbDeviceGrant{}).Error)
	third, err := service.Query(context.Background(), []uint{devices[0].ID})
	require.NoError(t, err)
	require.NotEqual(t, first.Devices[0].Revision, third.Devices[0].Revision)
}

func TestServiceQueryMarksDeletedOrDisabledTargetsInvalid(t *testing.T) {
	db := newGrantServiceTestDB(t)
	disabled := int8(0)
	department := basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 10}, Name: "停用部门", Status: &disabled}
	user := basemodels.User{BaseModel: basemodels.BaseModel{ID: 7}, Username: "deleted-user", Password: "x", Status: 1, DeptID: 10}
	require.NoError(t, db.Create(&department).Error)
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Delete(&user).Error)
	device := gbmodels.GbDevice{DeviceID: "invalid-target", OwnerDeptID: 10}
	require.NoError(t, db.Create(&device).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbDeviceGrant{
		{DeviceID: device.ID, TargetType: gbmodels.GrantTargetTypeDept, TargetID: department.ID},
		{DeviceID: device.ID, TargetType: gbmodels.GrantTargetTypeUser, TargetID: user.ID},
	}).Error)

	result, err := NewService(db, nil).Query(context.Background(), []uint{device.ID})
	require.NoError(t, err)
	require.Len(t, result.Devices, 1)
	require.Len(t, result.Devices[0].Grants, 2)
	require.True(t, result.Devices[0].Grants[0].Invalid)
	require.True(t, result.Devices[0].Grants[1].Invalid)
}

func TestServiceSearchTargetsFiltersScopeAndPaginates(t *testing.T) {
	db := newGrantServiceTestDB(t)
	active, disabled := int8(1), int8(0)
	require.NoError(t, db.Create(&[]basemodels.SysDepartment{
		{BaseModel: basemodels.BaseModel{ID: 10}, Name: "安保一部", Status: &active},
		{BaseModel: basemodels.BaseModel{ID: 20}, Name: "安保二部", Status: &active},
		{BaseModel: basemodels.BaseModel{ID: 30}, Name: "安保停用", Status: &disabled},
	}).Error)
	users := make([]basemodels.User, 0, 251)
	for index := 1; index <= 250; index++ {
		users = append(users, basemodels.User{Username: "guard-" + threeDigits(index), NickName: "值班员", Password: "x", Status: 1, DeptID: 10})
	}
	users = append(users, basemodels.User{Username: "outside", Password: "x", Status: 1, DeptID: 20})
	require.NoError(t, db.Create(&users).Error)
	access := datascope.OwnerDeptAccess{DeptIDs: []uint{10}}
	service := NewService(db, nil)

	departments, err := service.SearchTargets(context.Background(), gbmodels.GrantTargetTypeDept, "安保", 1, 50, access)
	require.NoError(t, err)
	require.EqualValues(t, 1, departments.Total)
	require.EqualValues(t, 10, departments.List[0].ID)

	page, err := service.SearchTargets(context.Background(), gbmodels.GrantTargetTypeUser, "guard", 5, 50, access)
	require.NoError(t, err)
	require.EqualValues(t, 250, page.Total)
	require.Len(t, page.List, 50)
	require.Equal(t, "guard-201", page.List[0].Name)
	for _, option := range page.List {
		require.EqualValues(t, 10, option.DeptID)
	}
}

func threeDigits(value int) string {
	return string([]byte{'0' + byte(value/100), '0' + byte(value/10%10), '0' + byte(value%10)})
}
