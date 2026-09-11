package datascope

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/models"
)

func newVisibilityTestDB(t *testing.T) (*gorm.DB, *gin.Context) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{}, &models.SysRole{}, &models.SysUserRole{}, &models.SysDepartment{},
		&gbmodels.GbDevice{}, &gbmodels.GbDeviceGrant{},
	))

	// 部门:1 总部 / 2 子部门
	require.NoError(t, db.Create(&models.SysDepartment{Name: "总部", ParentID: nil}).Error)
	require.NoError(t, db.Create(&models.SysDepartment{Name: "子部门"}).Error)

	// 用户 1 属于部门 1,角色 DataScope=3(本部门)
	require.NoError(t, db.Create(&models.User{Username: "u1", DeptID: 1}).Error)
	require.NoError(t, db.Create(&models.SysRole{DataScope: 3}).Error)
	require.NoError(t, db.Create(&models.SysUserRole{UserID: 1, RoleID: 1}).Error)

	// 设备:1/2 归属部门 1,3 归属部门 2,4 归属部门 9
	codes := []string{"d1", "d2", "d3", "d4"}
	for i, dept := range []uint{1, 1, 2, 9} {
		require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: codes[i], OwnerDeptID: dept}).Error)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 1, Username: "u1"}})
	return db, c
}

func visibleDeviceIDs(t *testing.T, db *gorm.DB, c *gin.Context) []uint {
	t.Helper()
	var ids []uint
	err := db.Model(&gbmodels.GbDevice{}).Scopes(VisibilityScope(c, "owner_dept_id", "device_id")).
		Order("id ASC").Pluck("id", &ids).Error
	require.NoError(t, err)
	return ids
}

// T2 RED-1: 归属本部门可见,其他部门不可见
func TestVisibilityScope_OwnerDept(t *testing.T) {
	db, c := newVisibilityTestDB(t)
	assert.Equal(t, []uint{1, 2}, visibleDeviceIDs(t, db, c))
}

// T2 RED-2: 共享给本用户 → 可见
func TestVisibilityScope_GrantToUser(t *testing.T) {
	db, c := newVisibilityTestDB(t)
	require.NoError(t, db.Create(&gbmodels.GbDeviceGrant{
		DeviceID: 4, TargetType: gbmodels.GrantTargetTypeUser, TargetID: 1,
	}).Error)
	assert.Equal(t, []uint{1, 2, 4}, visibleDeviceIDs(t, db, c))
}

// T2 RED-3: 共享给本部门 → 可见
func TestVisibilityScope_GrantToDept(t *testing.T) {
	db, c := newVisibilityTestDB(t)
	require.NoError(t, db.Create(&gbmodels.GbDeviceGrant{
		DeviceID: 4, TargetType: gbmodels.GrantTargetTypeDept, TargetID: 1,
	}).Error)
	assert.Equal(t, []uint{1, 2, 4}, visibleDeviceIDs(t, db, c))
}

// T2 RED-4: 共享给子部门(2)不继承 → 部门 1 用户不可见设备 4
func TestVisibilityScope_GrantToChildDept_NotInherited(t *testing.T) {
	db, c := newVisibilityTestDB(t)
	require.NoError(t, db.Create(&gbmodels.GbDeviceGrant{
		DeviceID: 4, TargetType: gbmodels.GrantTargetTypeDept, TargetID: 2,
	}).Error)
	assert.Equal(t, []uint{1, 2}, visibleDeviceIDs(t, db, c))
}

// T2 RED-5: 软删的共享授权不生效
func TestVisibilityScope_SoftDeletedGrant_Ignored(t *testing.T) {
	db, c := newVisibilityTestDB(t)
	require.NoError(t, db.Create(&gbmodels.GbDeviceGrant{
		DeviceID: 4, TargetType: gbmodels.GrantTargetTypeDept, TargetID: 1,
	}).Error)
	require.NoError(t, db.Where("device_id = ?", 4).Delete(&gbmodels.GbDeviceGrant{}).Error)
	assert.Equal(t, []uint{1, 2}, visibleDeviceIDs(t, db, c))
}

// T2 RED-6: 通道表用法(device_id 为设备编码列)
func TestVisibilityScope_ChannelTable(t *testing.T) {
	db, c := newVisibilityTestDB(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannel{}))
	require.NoError(t, db.Create(&gbmodels.GbChannel{
		DeviceID: "d4", ChannelID: "c1", OwnerDeptID: 9,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{
		DeviceID: "d1", ChannelID: "c2", OwnerDeptID: 1,
	}).Error)
	// grant 引用 gb_device.id=4(编码 d4)
	require.NoError(t, db.Create(&gbmodels.GbDeviceGrant{
		DeviceID: 4, TargetType: gbmodels.GrantTargetTypeDept, TargetID: 1,
	}).Error)

	var ids []uint
	err := db.Model(&gbmodels.GbChannel{}).Scopes(VisibilityScope(c, "owner_dept_id", "device_id")).
		Order("id ASC").Pluck("id", &ids).Error
	require.NoError(t, err)
	assert.Equal(t, []uint{1, 2}, ids, "通道 c1(设备 d4 共享给本部门)与 c2(归属本部门)均可见")
}
