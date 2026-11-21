package controllers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gbcontrollers "uvplatform.com/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.com/uvp-gb28181/app/gb28181/grant"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/service"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"
	"uvplatform.com/uvp-gb28181/app/utils/response"
)

const deviceShareRevocationModel = `[request_definition]
r = sub, obj, act, dom
[policy_definition]
p = sub, obj, act, dom
[role_definition]
g = _, _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = g(r.sub,p.sub,r.dom) && keyMatch2(r.obj,p.obj) && regexMatch(r.act,p.act) && (r.dom == p.dom || p.dom == "*")`

// This external test package can call both the GB controller and the base
// authority service without creating their production package import cycle.
type deviceShareRevocationConfig struct{ app.YmlConfigInterf }

func (deviceShareRevocationConfig) GetUintSlice(string) []uint { return nil }
func (deviceShareRevocationConfig) GetString(key string) string {
	return map[string]string{"gormv2.usedbtype": "sqlite", "casbin.modelconfig": deviceShareRevocationModel,
		"casbin.tableprefix": "sys_", "casbin.tablename": "casbin_rule"}[key]
}

type deviceShareRevocationFixture struct {
	db      *gorm.DB
	router  *gin.Engine
	shares  *videoshare.Service
	device  uint
	channel uint
}

func newDeviceShareRevocationFixture(t *testing.T) *deviceShareRevocationFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	oldConfig, oldDB, oldCasbin := app.ConfigYml, app.GormDbSqlite, app.CasbinV2
	oldResponse, oldLog := app.Response, app.ZapLog
	app.ConfigYml, app.GormDbSqlite, app.CasbinV2 = deviceShareRevocationConfig{}, db, nil
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() {
		app.ConfigYml, app.GormDbSqlite, app.CasbinV2 = oldConfig, oldDB, oldCasbin
		app.Response, app.ZapLog = oldResponse, oldLog
	})
	require.NoError(t, db.AutoMigrate(&models.SysOperationLog{}, &authoritytx.Gate{}, &gbmodels.GbChannelVideoShare{},
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceGrant{}, &models.User{}, &models.SysRole{},
		&models.SysUserRole{}, &models.SysDepartment{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch INTEGER NOT NULL DEFAULT 1").Error)
	require.NoError(t, db.Table("sys_casbin_rule").AutoMigrate(&gormadapter.CasbinRule{}))
	require.NoError(t, db.Create(&authoritytx.Gate{ID: 1}).Error)
	active := int8(1)
	for _, department := range []models.SysDepartment{
		{BaseModel: models.BaseModel{ID: 10}, Name: "viewer-department", Status: &active},
		{BaseModel: models.BaseModel{ID: 11}, Name: "owner-department", Status: &active},
	} {
		require.NoError(t, db.Create(&department).Error)
	}
	for _, user := range []models.User{
		{BaseModel: models.BaseModel{ID: 7}, Username: "share-creator", Status: 1, DeptID: 10},
		{BaseModel: models.BaseModel{ID: 8}, Username: "grant-admin", Status: 1, DeptID: 11},
	} {
		require.NoError(t, db.Create(&user).Error)
	}
	for _, role := range []models.SysRole{
		{BaseModel: models.BaseModel{ID: 3}, Name: "own-department-only", Status: 1, DataScope: 3},
		{BaseModel: models.BaseModel{ID: 4}, Name: "grant-admin", Status: 1, DataScope: 1},
	} {
		require.NoError(t, db.Create(&role).Error)
	}
	require.NoError(t, db.Create(&models.SysUserRole{UserID: 7, RoleID: 3}).Error)
	require.NoError(t, db.Create(&models.SysUserRole{UserID: 8, RoleID: 4}).Error)
	device := gbmodels.GbDevice{DeviceID: "34020000002000000001", OwnerDeptID: 11}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000001", OwnerDeptID: 11}
	require.NoError(t, db.Create(&channel).Error)
	permission := service.NewPermissionService()
	require.NoError(t, permission.AddRoleForUser(context.Background(), 7, []uint{3}))
	require.NoError(t, permission.AddPoliciesForRole(context.Background(), 3, models.SysApiList{
		&models.SysApi{Path: "/api/gb28181/channel-shares/:id", Method: "PUT"},
		&models.SysApi{Path: "/api/gb28181/play/:device/:channel", Method: "POST"},
	}))
	signer, err := videoshare.NewSigner(make([]byte, 32))
	require.NoError(t, err)
	shares, err := videoshare.NewService(db, signer, func() time.Duration { return 5 * time.Minute }, videoshare.NewTransactionalAuthorizer())
	require.NoError(t, err)
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 8, Username: "grant-admin"}})
		c.Next()
	})
	router.POST("/api/gb28181/device-mgmt/permission-workbench/grants/apply", controller.ApplyPermissionWorkbenchGrants)
	return &deviceShareRevocationFixture{db: db, router: router, shares: shares, device: device.ID, channel: channel.ID}
}

func (f *deviceShareRevocationFixture) apply(t *testing.T, mode grant.ApplyMode, target grant.ApplyTarget) grant.ApplyResult {
	t.Helper()
	state, err := grant.NewService(f.db, nil).Query(context.Background(), []uint{f.device})
	require.NoError(t, err)
	require.Len(t, state.Devices, 1)
	body, err := json.Marshal(grant.ApplyRequest{Mode: mode, Targets: []grant.ApplyTarget{target},
		Items: []grant.ApplyDevice{{DeviceID: f.device, ExpectedRevision: state.Devices[0].Revision}}})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/gb28181/device-mgmt/permission-workbench/grants/apply", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var reply struct {
		Code int               `json:"code"`
		Data grant.ApplyResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &reply))
	require.Zero(t, reply.Code, recorder.Body.String())
	require.Len(t, reply.Data.Results, 1)
	return reply.Data
}

func (f *deviceShareRevocationFixture) issue(t *testing.T, target grant.ApplyTarget) videoshare.Link {
	t.Helper()
	added := f.apply(t, grant.ApplyModeAdd, target)
	require.Equal(t, 1, added.Summary.Changed)
	require.Equal(t, 1, added.Summary.Added)
	link, err := f.shares.SetEnabled(context.Background(), f.channel, 7, true, 0)
	require.NoError(t, err)
	return link
}

func TestVideoShareDeviceGrantControllerQuickRegrantKeepsOldLinkRevoked(t *testing.T) {
	for _, target := range []grant.ApplyTarget{{Type: "user", ID: 7}, {Type: "dept", ID: 10}} {
		t.Run(target.Type, func(t *testing.T) {
			f := newDeviceShareRevocationFixture(t)
			link := f.issue(t, target)
			removed := f.apply(t, grant.ApplyModeRemove, target)
			require.Equal(t, 1, removed.Summary.Changed)
			require.Equal(t, 1, removed.Summary.Removed)
			// Restore through the same real controller before any Resolve/status
			// call could observe the temporary loss and lazily retire the share.
			added := f.apply(t, grant.ApplyModeAdd, target)
			require.Equal(t, 1, added.Summary.Added)
			var retired gbmodels.GbChannelVideoShare
			require.NoError(t, f.db.Where("channel_pk = ?", f.channel).Take(&retired).Error)
			require.False(t, retired.Enabled)
			require.NotNil(t, retired.RevokedAt)
			require.Equal(t, "creator_device_access_removed", retired.RevokeReason)
			require.Greater(t, retired.Revision, link.Revision)
			_, err := f.shares.Resolve(context.Background(), link.PublicID, link.Signature)
			require.ErrorIs(t, err, videoshare.ErrExpired)
			var audits []models.SysOperationLog
			require.NoError(t, f.db.Where("operation = ?", "video_share.revoked").Find(&audits).Error)
			require.Len(t, audits, 1, "the real controller callback must write permanent revocation audit")
			next, err := f.shares.SetEnabled(context.Background(), f.channel, 7, true, retired.Revision)
			require.NoError(t, err)
			require.NotEqual(t, link.Generation, next.Generation)
			_, err = f.shares.Resolve(context.Background(), next.PublicID, next.Signature)
			require.NoError(t, err)
			_, err = f.shares.Resolve(context.Background(), link.PublicID, link.Signature)
			require.ErrorIs(t, err, videoshare.ErrInvalidCredential)
		})
	}
}

func TestVideoShareDeviceGrantControllerRevocationAuditFailureRollsBackRemoval(t *testing.T) {
	f := newDeviceShareRevocationFixture(t)
	target := grant.ApplyTarget{Type: "user", ID: 7}
	link := f.issue(t, target)
	injected := errors.New("revocation audit unavailable")
	attempts := 0
	require.NoError(t, f.db.Callback().Create().Before("gorm:create").Register("test:device_share_revocation_audit", func(tx *gorm.DB) {
		if tx.Statement.Table == "sys_operation_logs" {
			attempts++
			tx.AddError(injected)
		}
	}))
	t.Cleanup(func() { _ = f.db.Callback().Create().Remove("test:device_share_revocation_audit") })
	failed := f.apply(t, grant.ApplyModeRemove, target)
	require.Equal(t, 1, attempts, "the controller must reach the real permanent revocation audit")
	require.Equal(t, 1, failed.Summary.Failed)
	require.Zero(t, failed.Summary.Changed)
	require.Zero(t, failed.Summary.Removed)
	var grants int64
	require.NoError(t, f.db.Model(&gbmodels.GbDeviceGrant{}).Where("device_id = ? AND target_type = ? AND target_id = ?", f.device, target.Type, target.ID).Count(&grants).Error)
	require.EqualValues(t, 1, grants, "audit failure must roll back grant deletion")
	var share gbmodels.GbChannelVideoShare
	require.NoError(t, f.db.Where("channel_pk = ?", f.channel).Take(&share).Error)
	require.True(t, share.Enabled)
	require.Nil(t, share.RevokedAt)
	require.Equal(t, link.Revision, share.Revision)
	var auditCount int64
	require.NoError(t, f.db.Model(&models.SysOperationLog{}).Where("operation = ?", "video_share.revoked").Count(&auditCount).Error)
	require.Zero(t, auditCount)
	_, err := f.shares.Resolve(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err, "failed removal must retain the preexisting authorized share")
}
