package service

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
)

type userProfileTestConfig struct {
	skipUsers []uint
}

func (c userProfileTestConfig) ConfigFileChangeListen(...func()) {}
func (c userProfileTestConfig) Get(string) interface{}           { return nil }
func (c userProfileTestConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return "sqlite"
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
	db := newSQLiteSystemTestDB(t)
	oldConfig := app.ConfigYml
	app.ConfigYml = config
	t.Cleanup(func() { app.ConfigYml = oldConfig })
	return db
}

func userProfileTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(nil)
	return c
}

func TestGetUserProfileReturnsGrantedType2AndType3Permissions(t *testing.T) {
	db := newUserProfileTestDB(t, userProfileTestConfig{})
	require.NoError(t, db.Create(&models.User{
		BaseModel: models.BaseModel{ID: 10010}, Username: "viewer", Password: "x", Status: 1,
	}).Error)
	require.NoError(t, db.Create([]models.SysRole{
		{BaseModel: models.BaseModel{ID: 10001}, Name: "parent", ParentID: 0},
		{BaseModel: models.BaseModel{ID: 10002}, Name: "child", ParentID: 10001},
		{BaseModel: models.BaseModel{ID: 10003}, Name: "second", ParentID: 0},
	}).Error)
	require.NoError(t, db.Create([]models.SysMenu{
		{BaseModel: models.BaseModel{ID: 10101}, Type: 2, Permission: "gb28181:recording:view", Path: "/recording", Name: "Recording"},
		{BaseModel: models.BaseModel{ID: 10102}, Type: 3, Permission: "gb28181:alarm:view", Name: "AlarmView"},
		{BaseModel: models.BaseModel{ID: 10103}, Type: 2, Permission: "gb28181:secret:view", Name: "Secret"},
		{BaseModel: models.BaseModel{ID: 10104}, Type: 1, Permission: "gb28181:catalog:view", Path: "/catalog", Name: "Catalog"},
		{BaseModel: models.BaseModel{ID: 10105}, Type: 3, Permission: "", Name: "NoPermission"},
		{BaseModel: models.BaseModel{ID: 10106}, Type: 2, Permission: "gb28181:home:view", Path: "/home", Name: "Home"},
		{BaseModel: models.BaseModel{ID: 10107}, Type: 2, Permission: "", Path: "/empty", Name: "EmptyPagePermission"},
	}).Error)
	require.NoError(t, db.Create([]models.SysUserRole{
		{UserID: 10010, RoleID: 10002},
		{UserID: 10010, RoleID: 10003},
	}).Error)
	require.NoError(t, db.Create([]models.SysRoleMenu{
		{RoleID: 10001, MenuID: 10101},
		{RoleID: 10002, MenuID: 10102},
		{RoleID: 10002, MenuID: 10104},
		{RoleID: 10002, MenuID: 10105},
		{RoleID: 10002, MenuID: 10107},
		{RoleID: 10003, MenuID: 10106},
	}).Error)

	profile, err := NewUserService().GetUserProfile(userProfileTestContext(), 10010)
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
		BaseModel: models.BaseModel{ID: 10020}, Username: "viewer", Password: "x", Status: 1,
	}).Error)
	require.NoError(t, db.Create(&models.SysRole{BaseModel: models.BaseModel{ID: 10020}, Name: "guest"}).Error)
	require.NoError(t, db.Create(&models.SysMenu{
		BaseModel: models.BaseModel{ID: 10201}, Type: 2, Permission: "gb28181:home:view", Path: "/home", Name: "Home",
	}).Error)
	require.NoError(t, db.Create(&models.SysUserRole{UserID: 10020, RoleID: 10020}).Error)
	require.NoError(t, db.Create(&models.SysRoleMenu{RoleID: 10020, MenuID: 10201}).Error)

	profile, err := NewUserService().GetUserProfile(userProfileTestContext(), 10020)
	require.NoError(t, err)
	require.Equal(t, []string{"gb28181:home:view"}, profile.Permissions)

	require.NoError(t, db.Where("role_id = ? AND menu_id = ?", 10020, 10201).Delete(&models.SysRoleMenu{}).Error)
	profile, err = NewUserService().GetUserProfile(userProfileTestContext(), 10020)
	require.NoError(t, err)
	require.Empty(t, profile.Permissions)
}

func TestGetUserProfileKeepsAdministratorWildcard(t *testing.T) {
	db := newUserProfileTestDB(t, userProfileTestConfig{skipUsers: []uint{10099}})
	require.NoError(t, db.Create(&models.User{
		BaseModel: models.BaseModel{ID: 10099}, Username: "administrator", Password: "x", Status: 1,
	}).Error)

	profile, err := NewUserService().GetUserProfile(userProfileTestContext(), 10099)
	require.NoError(t, err)
	require.Equal(t, []string{"*:*:*"}, profile.Permissions)
}
