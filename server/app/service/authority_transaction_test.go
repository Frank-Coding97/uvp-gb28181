package service

import (
	"context"
	"errors"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
	"time"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	gbvideoshare "uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"
)

const authorityTestModel = `[request_definition]
r = sub, obj, act, dom
[policy_definition]
p = sub, obj, act, dom
[role_definition]
g = _, _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = g(r.sub,p.sub,r.dom) && keyMatch2(r.obj,p.obj) && regexMatch(r.act,p.act) && (r.dom == p.dom || p.dom == "*")`

type authorityTestConfig struct{ app.YmlConfigInterf }

func (authorityTestConfig) GetUintSlice(string) []uint { return nil }

func (authorityTestConfig) GetString(key string) string {
	switch key {
	case "gormv2.usedbtype":
		return "sqlite"
	case "casbin.modelconfig":
		return authorityTestModel
	case "casbin.tableprefix":
		return "sys_"
	case "casbin.tablename":
		return "casbin_rule"
	}
	return ""
}

func TestAuthorityTransactionRollsBackBusinessAndPolicyTogether(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	defer raw.Close()
	oldConfig, oldDB := app.ConfigYml, app.GormDbSqlite
	app.ConfigYml, app.GormDbSqlite = authorityTestConfig{}, db
	defer func() { app.ConfigYml, app.GormDbSqlite = oldConfig, oldDB }()
	require.NoError(t, db.AutoMigrate(&models.SysOperationLog{}, &authoritytx.Gate{}, &gbmodels.GbChannelVideoShare{}, &models.User{}, &models.SysRole{}))
	require.NoError(t, db.Table("sys_casbin_rule").AutoMigrate(&gormadapter.CasbinRule{}))
	require.NoError(t, db.Create(&authoritytx.Gate{ID: 1}).Error)
	require.NoError(t, db.Create(&models.User{Username: "operator", Status: 1}).Error)
	fail := errors.New("injected after policy writes")
	ps := NewPermissionService()
	err = ps.Transaction(context.Background(), func(tx *gorm.DB, permission *PermissionService) error {
		require.NoError(t, tx.Model(&models.User{}).Where("id = 1").Update("status", 0).Error)
		require.NoError(t, permission.AddPoliciesForRole(context.Background(), 3, models.SysApiList{&models.SysApi{Path: "/resource", Method: "GET"}}))
		var count int64
		require.NoError(t, tx.Table("sys_casbin_rule").Count(&count).Error)
		require.EqualValues(t, 1, count)
		return fail
	})
	require.ErrorIs(t, err, fail)
	var user models.User
	require.NoError(t, db.First(&user, 1).Error)
	require.EqualValues(t, 1, user.Status)
	var count int64
	require.NoError(t, db.Table("sys_casbin_rule").Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, ps.AddPoliciesForRole(context.Background(), 3, models.SysApiList{&models.SysApi{Path: "/resource", Method: "GET"}}))
	require.NoError(t, db.Table("sys_casbin_rule").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestAuthorityRevocationAndQuickRegrantCannotReviveLink(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	defer raw.Close()
	previousConfig, previousDB := app.ConfigYml, app.GormDbSqlite
	app.ConfigYml, app.GormDbSqlite = authorityTestConfig{}, db
	defer func() { app.ConfigYml, app.GormDbSqlite = previousConfig, previousDB }()
	require.NoError(t, db.AutoMigrate(&models.SysOperationLog{}, &authoritytx.Gate{}, &gbmodels.GbChannelVideoShare{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceGrant{}, &models.User{}, &models.SysRole{}, &models.SysUserRole{}, &models.SysDepartment{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch INTEGER NOT NULL DEFAULT 1").Error)
	require.NoError(t, db.Table("sys_casbin_rule").AutoMigrate(&gormadapter.CasbinRule{}))
	require.NoError(t, db.Create(&authoritytx.Gate{ID: 1}).Error)
	require.NoError(t, db.Create(&models.User{BaseModel: models.BaseModel{ID: 7}, Username: "creator", Status: 1, DeptID: 10}).Error)
	require.NoError(t, db.Create(&models.SysRole{BaseModel: models.BaseModel{ID: 3}, Name: "sharing", Status: 1, DataScope: 1}).Error)
	require.NoError(t, db.Create(&models.SysUserRole{UserID: 7, RoleID: 3}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "device", OwnerDeptID: 10}).Error)
	channel := gbmodels.GbChannel{DeviceID: "device", ChannelID: "camera", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	ps := NewPermissionService()
	policies := models.SysApiList{&models.SysApi{Path: "/api/gb28181/channel-shares/:id", Method: "PUT"}, &models.SysApi{Path: "/api/gb28181/play/:device/:channel", Method: "POST"}}
	require.NoError(t, ps.Transaction(context.Background(), func(tx *gorm.DB, p *PermissionService) error {
		if err := p.AddRoleForUser(context.Background(), 7, []uint{3}); err != nil {
			return err
		}
		return p.AddPoliciesForRole(context.Background(), 3, policies)
	}))
	signer, err := gbvideoshare.NewSigner(make([]byte, 32))
	require.NoError(t, err)
	share, err := gbvideoshare.NewService(db, signer, func() time.Duration { return 5 * time.Minute }, gbvideoshare.NewTransactionalAuthorizer())
	require.NoError(t, err)
	first, err := share.SetEnabled(context.Background(), channel.ID, 7, true, 0)
	require.NoError(t, err)
	require.NoError(t, ps.DeleteRoleApis(context.Background(), 3))
	require.NoError(t, ps.AddPoliciesForRole(context.Background(), 3, policies))
	_, err = share.Resolve(context.Background(), first.PublicID, first.Signature)
	require.ErrorIs(t, err, gbvideoshare.ErrExpired)
	var retired gbmodels.GbChannelVideoShare
	require.NoError(t, db.First(&retired).Error)
	require.NotNil(t, retired.RevokedAt)
	next, err := share.SetEnabled(context.Background(), channel.ID, 7, true, retired.Revision)
	require.NoError(t, err)
	require.NotEqual(t, first.Generation, next.Generation)
}
