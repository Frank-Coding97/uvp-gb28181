package controllers

import (
	"bytes"
	"context"
	"encoding/json"
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
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/service"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"
	"uvplatform.com/uvp-gb28181/app/utils/response"
)

const shareRevocationPolicyModel = `[request_definition]
r = sub, obj, act, dom
[policy_definition]
p = sub, obj, act, dom
[role_definition]
g = _, _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = g(r.sub,p.sub,r.dom) && keyMatch2(r.obj,p.obj) && regexMatch(r.act,p.act) && (r.dom == p.dom || p.dom == "*")`

type shareRevocationAcceptance struct {
	db      *gorm.DB
	share   *videoshare.Service
	channel uint
	role    *SysRoleController
	menu    *SysMenuController
	api     *SysApiController
}

// Reuses the controller configuration fixture, but real DB-backed policy,
// authorizer, authority transaction and handlers; no mocked revoker is involved.
func newShareRevocationAcceptance(t *testing.T, inherited bool, scope int8) *shareRevocationAcceptance {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	oldConfig, oldDB, oldCasbin := app.ConfigYml, app.GormDbSqlite, app.CasbinV2
	oldResponse, oldLog := app.Response, app.ZapLog
	app.ConfigYml = &loginTestConfig{values: map[string]any{
		"gormv2.usedbtype": "sqlite", "casbin.modelconfig": shareRevocationPolicyModel,
		"casbin.tableprefix": "sys_", "casbin.tablename": "casbin_rule",
	}}
	app.GormDbSqlite, app.CasbinV2 = db, nil
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() {
		app.ConfigYml, app.GormDbSqlite, app.CasbinV2 = oldConfig, oldDB, oldCasbin
		app.Response, app.ZapLog = oldResponse, oldLog
	})
	require.NoError(t, db.AutoMigrate(&models.SysOperationLog{}, &authoritytx.Gate{}, &gbmodels.GbChannelVideoShare{},
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceGrant{}, &models.User{}, &models.SysRole{},
		&models.SysUserRole{}, &models.SysDepartment{}, &models.SysMenu{}, &models.SysApi{}, &models.SysRoleMenu{}, &models.SysMenuApi{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch INTEGER NOT NULL DEFAULT 1").Error)
	require.NoError(t, db.Table("sys_casbin_rule").AutoMigrate(&gormadapter.CasbinRule{}))
	require.NoError(t, db.Create(&authoritytx.Gate{ID: 1}).Error)
	root, active := uint(10), int8(1)
	for _, dept := range []models.SysDepartment{
		{BaseModel: models.BaseModel{ID: 10}, Name: "creator-department", Status: &active},
		{BaseModel: models.BaseModel{ID: 11}, Name: "camera-department", ParentID: &root, Status: &active},
		{BaseModel: models.BaseModel{ID: 12}, Name: "outside-department", Status: &active},
	} {
		require.NoError(t, db.Create(&dept).Error)
	}
	require.NoError(t, db.Create(&models.User{BaseModel: models.BaseModel{ID: 7}, Username: "share-creator", Status: 1, DeptID: 10}).Error)
	parent := uint(0)
	if inherited {
		parent = 4
	}
	require.NoError(t, db.Create(&models.SysRole{BaseModel: models.BaseModel{ID: 3}, Name: "creator-role", Status: 1, DataScope: scope, ParentID: parent}).Error)
	require.NoError(t, db.Create(&models.SysRole{BaseModel: models.BaseModel{ID: 4}, Name: "parent-role", Status: 1, DataScope: 1}).Error)
	require.NoError(t, db.Create(&models.SysUserRole{UserID: 7, RoleID: 3}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "device", OwnerDeptID: 11}).Error)
	channel := gbmodels.GbChannel{DeviceID: "device", ChannelID: "camera", OwnerDeptID: 11}
	require.NoError(t, db.Create(&channel).Error)
	for _, menu := range []models.SysMenu{
		{BaseModel: models.BaseModel{ID: 20}, Name: "share-menu", Path: "/share", Type: 2},
		{BaseModel: models.BaseModel{ID: 21}, Name: "unrelated-menu", Path: "/unrelated", Type: 2},
	} {
		require.NoError(t, db.Create(&menu).Error)
	}
	for _, api := range []models.SysApi{
		{BaseModel: models.BaseModel{ID: 50}, Title: "manage-share", Path: "/api/gb28181/channel-shares/:id", Method: "PUT", ApiGroup: "设备管理"},
		{BaseModel: models.BaseModel{ID: 51}, Title: "live-play", Path: "/api/gb28181/play/:device/:channel", Method: "POST", ApiGroup: "多屏播放"},
	} {
		require.NoError(t, db.Create(&api).Error)
		require.NoError(t, db.Create(&models.SysMenuApi{MenuID: 20, ApiID: api.ID}).Error)
	}
	permission := service.NewPermissionService()
	require.NoError(t, permission.AddRoleForUser(context.Background(), 7, []uint{3}))
	if inherited {
		require.NoError(t, permission.AddRoleInheritance(context.Background(), 3, 4))
	}
	signer, err := videoshare.NewSigner(make([]byte, 32))
	require.NoError(t, err)
	share, err := videoshare.NewService(db, signer, func() time.Duration { return 5 * time.Minute }, videoshare.NewTransactionalAuthorizer())
	require.NoError(t, err)
	fixture := &shareRevocationAcceptance{db: db, share: share, channel: channel.ID,
		role: NewSysRoleController(), menu: NewSysMenuController(), api: NewSysApiController()}
	policyRole := uint(3)
	if inherited {
		policyRole = 4
	}
	fixture.call(t, fixture.role.AddRoleMenu, map[string]any{"roleId": policyRole, "menuId": []uint{20}})
	return fixture
}

func (f *shareRevocationAcceptance) call(t *testing.T, handler gin.HandlerFunc, payload any) {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/authority-acceptance", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c)
	require.Equal(t, 200, recorder.Code, recorder.Body.String())
	var reply struct{ Code int }
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &reply))
	require.Zero(t, reply.Code, recorder.Body.String())
}

func (f *shareRevocationAcceptance) assertPermanentRetirement(t *testing.T, withdraw, restore func()) {
	t.Helper()
	ctx := context.Background()
	link, err := f.share.SetEnabled(ctx, f.channel, 7, true, 0)
	require.NoError(t, err)
	_, err = f.share.Resolve(ctx, link.PublicID, link.Signature)
	require.NoError(t, err)
	withdraw()
	// No Resolve, background sweep, sleep or lazy revalidation between writes.
	restore()
	var retired gbmodels.GbChannelVideoShare
	require.NoError(t, f.db.Where("channel_pk = ?", f.channel).Take(&retired).Error)
	require.False(t, retired.Enabled, "the business write must persist revocation before any public request")
	require.NotNil(t, retired.RevokedAt)
	require.Greater(t, retired.Revision, link.Revision)
	_, err = f.share.Resolve(ctx, link.PublicID, link.Signature)
	require.ErrorIs(t, err, videoshare.ErrExpired, "immediate regrant must not revive the old link")
	next, err := f.share.SetEnabled(ctx, f.channel, 7, true, retired.Revision)
	require.NoError(t, err, "restored business authority must allow an explicitly new share")
	require.NotEqual(t, link.Generation, next.Generation)
	_, err = f.share.Resolve(ctx, next.PublicID, next.Signature)
	require.NoError(t, err)
	_, err = f.share.Resolve(ctx, link.PublicID, link.Signature)
	require.ErrorIs(t, err, videoshare.ErrInvalidCredential, "issuing a new generation must keep the old credential invalid")
}

func TestVideoShareBusinessWriteRevocationAcceptance(t *testing.T) {
	t.Run("role-menu-reassignment", func(t *testing.T) {
		f := newShareRevocationAcceptance(t, false, 1)
		assign := func(menus []uint) {
			f.call(t, f.role.AddRoleMenu, map[string]any{"roleId": 3, "menuId": menus})
		}
		f.assertPermanentRetirement(t, func() { assign([]uint{21}) }, func() { assign([]uint{20}) })
	})
	t.Run("menu-api-reassignment", func(t *testing.T) {
		f := newShareRevocationAcceptance(t, false, 1)
		assign := func(apis []uint) {
			f.call(t, f.menu.SetMenuApis, map[string]any{"menuId": 20, "apiIds": apis})
		}
		f.assertPermanentRetirement(t, func() { assign([]uint{50}) }, func() { assign([]uint{50, 51}) })
	})
	t.Run("api-path-update", func(t *testing.T) {
		f := newShareRevocationAcceptance(t, false, 1)
		update := func(path string) {
			f.call(t, f.api.Update, map[string]any{"id": 51, "title": "live-play", "path": path,
				"method": "POST", "apiGroup": "多屏播放"})
		}
		f.assertPermanentRetirement(t, func() { update("/unrelated-live") },
			func() { update("/api/gb28181/play/:device/:channel") })
	})
	t.Run("creator-role-disable", func(t *testing.T) {
		f := newShareRevocationAcceptance(t, false, 1)
		update := func(status int8) {
			f.call(t, f.role.Update, map[string]any{"id": 3, "name": "creator-role", "status": status, "parentId": 0})
		}
		f.assertPermanentRetirement(t, func() { update(0) }, func() { update(1) })
	})
	t.Run("inherited-parent-role-disable", func(t *testing.T) {
		f := newShareRevocationAcceptance(t, true, 1)
		update := func(status int8) {
			f.call(t, f.role.Update, map[string]any{"id": 4, "name": "parent-role", "status": status, "parentId": 0})
		}
		f.assertPermanentRetirement(t, func() { update(0) }, func() { update(1) })
	})
	t.Run("role-inheritance-update", func(t *testing.T) {
		f := newShareRevocationAcceptance(t, true, 1)
		update := func(parent uint) {
			f.call(t, f.role.Update, map[string]any{"id": 3, "name": "creator-role", "status": 1, "parentId": parent})
		}
		f.assertPermanentRetirement(t, func() { update(0) }, func() { update(4) })
	})
	t.Run("role-data-scope-update", func(t *testing.T) {
		f := newShareRevocationAcceptance(t, false, 1)
		update := func(scope int8) {
			f.call(t, f.role.UpdateDataScope, map[string]any{"id": 3, "dataScope": scope, "checkedDepts": ""})
		}
		// The creator owns department 10; the camera belongs to department 11.
		f.assertPermanentRetirement(t, func() { update(3) }, func() { update(1) })
	})
	t.Run("department-tree-update", func(t *testing.T) {
		f := newShareRevocationAcceptance(t, false, 4)
		update := func(parent uint) {
			active := int8(1)
			_, err := service.NewSysDepartmentService().Update(context.Background(), &models.SysDepartmentUpdateRequest{
				ID: 11, Name: "camera-department", ParentID: &parent, Status: &active,
			})
			require.NoError(t, err)
		}
		f.assertPermanentRetirement(t, func() { update(12) }, func() { update(10) })
	})
}
