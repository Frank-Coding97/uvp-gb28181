package service

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

type userProfileTestConfig struct {
	skipUsers []uint
}

func (c userProfileTestConfig) ConfigFileChangeListen(...func()) {}
func (c userProfileTestConfig) Get(string) interface{}           { return nil }
func (c userProfileTestConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return "mysql"
	}
	return ""
}
func (c userProfileTestConfig) GetBool(string) bool              { return false }
func (c userProfileTestConfig) GetInt(string) int                { return 0 }
func (c userProfileTestConfig) GetInt32(string) int32            { return 0 }
func (c userProfileTestConfig) GetInt64(string) int64            { return 0 }
func (c userProfileTestConfig) GetFloat64(string) float64        { return 0 }
func (c userProfileTestConfig) GetDuration(string) time.Duration { return 0 }
func (c userProfileTestConfig) GetStringSlice(string) []string   { return nil }
func (c userProfileTestConfig) GetUintSlice(string) []uint       { return c.skipUsers }
func (c userProfileTestConfig) Set(string, interface{})          {}
func (c userProfileTestConfig) SaveConfig() error                { return nil }

func newUserProfileTestDB(t *testing.T, config userProfileTestConfig) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.SysDepartment{},
		&models.SysRole{},
		&models.SysMenu{},
		&models.SysUserRole{},
		&models.SysRoleMenu{},
	))

	oldDB, oldConfig := app.GormDbMysql, app.ConfigYml
	app.GormDbMysql, app.ConfigYml = db, config
	t.Cleanup(func() {
		app.GormDbMysql, app.ConfigYml = oldDB, oldConfig
	})
	return db
}

func userProfileTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(nil)
	return c
}

func TestGetUserProfileReturnsGrantedType2AndType3Permissions(t *testing.T) {
	db := newUserProfileTestDB(t, userProfileTestConfig{})
	require.NoError(t, db.Create(&models.User{
		BaseModel: models.BaseModel{ID: 10}, Username: "viewer", Password: "x", Status: 1,
	}).Error)
	require.NoError(t, db.Create([]models.SysRole{
		{BaseModel: models.BaseModel{ID: 1}, Name: "parent", ParentID: 0},
		{BaseModel: models.BaseModel{ID: 2}, Name: "child", ParentID: 1},
		{BaseModel: models.BaseModel{ID: 3}, Name: "second", ParentID: 0},
	}).Error)
	require.NoError(t, db.Create([]models.SysMenu{
		{BaseModel: models.BaseModel{ID: 101}, Type: 2, Permission: "gb28181:recording:view", Path: "/recording", Name: "Recording"},
		{BaseModel: models.BaseModel{ID: 102}, Type: 3, Permission: "gb28181:alarm:view", Name: "AlarmView"},
		{BaseModel: models.BaseModel{ID: 103}, Type: 2, Permission: "gb28181:secret:view", Name: "Secret"},
		{BaseModel: models.BaseModel{ID: 104}, Type: 1, Permission: "gb28181:catalog:view", Path: "/catalog", Name: "Catalog"},
		{BaseModel: models.BaseModel{ID: 105}, Type: 3, Permission: "", Name: "NoPermission"},
		{BaseModel: models.BaseModel{ID: 106}, Type: 2, Permission: "gb28181:home:view", Path: "/home", Name: "Home"},
		{BaseModel: models.BaseModel{ID: 107}, Type: 2, Permission: "", Path: "/empty", Name: "EmptyPagePermission"},
	}).Error)
	require.NoError(t, db.Create([]models.SysUserRole{
		{UserID: 10, RoleID: 2},
		{UserID: 10, RoleID: 3},
	}).Error)
	require.NoError(t, db.Create([]models.SysRoleMenu{
		{RoleID: 1, MenuID: 101},
		{RoleID: 2, MenuID: 102},
		{RoleID: 2, MenuID: 104},
		{RoleID: 2, MenuID: 105},
		{RoleID: 2, MenuID: 107},
		{RoleID: 3, MenuID: 106},
	}).Error)

	profile, err := NewUserService().GetUserProfile(userProfileTestContext(), 10)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"gb28181:recording:view",
		"gb28181:alarm:view",
		"gb28181:home:view",
	}, profile.Permissions)
	require.NotContains(t, profile.Permissions, "gb28181:secret:view")
	require.NotContains(t, profile.Permissions, "gb28181:catalog:view")
}

func TestGetUserProfileReflectsRoleMenuRevocation(t *testing.T) {
	db := newUserProfileTestDB(t, userProfileTestConfig{})
	require.NoError(t, db.Create(&models.User{
		BaseModel: models.BaseModel{ID: 20}, Username: "viewer", Password: "x", Status: 1,
	}).Error)
	require.NoError(t, db.Create(&models.SysRole{BaseModel: models.BaseModel{ID: 20}, Name: "guest"}).Error)
	require.NoError(t, db.Create(&models.SysMenu{
		BaseModel: models.BaseModel{ID: 201}, Type: 2, Permission: "gb28181:home:view", Path: "/home", Name: "Home",
	}).Error)
	require.NoError(t, db.Create(&models.SysUserRole{UserID: 20, RoleID: 20}).Error)
	require.NoError(t, db.Create(&models.SysRoleMenu{RoleID: 20, MenuID: 201}).Error)

	profile, err := NewUserService().GetUserProfile(userProfileTestContext(), 20)
	require.NoError(t, err)
	require.Equal(t, []string{"gb28181:home:view"}, profile.Permissions)

	require.NoError(t, db.Where("role_id = ? AND menu_id = ?", 20, 201).Delete(&models.SysRoleMenu{}).Error)
	profile, err = NewUserService().GetUserProfile(userProfileTestContext(), 20)
	require.NoError(t, err)
	require.Empty(t, profile.Permissions)
}

func TestGetUserProfileKeepsAdministratorWildcard(t *testing.T) {
	db := newUserProfileTestDB(t, userProfileTestConfig{skipUsers: []uint{99}})
	require.NoError(t, db.Create(&models.User{
		BaseModel: models.BaseModel{ID: 99}, Username: "administrator", Password: "x", Status: 1,
	}).Error)

	profile, err := NewUserService().GetUserProfile(userProfileTestContext(), 99)
	require.NoError(t, err)
	require.Equal(t, []string{"*:*:*"}, profile.Permissions)
}
