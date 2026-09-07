package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/casbinhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

const sqliteSystemCasbinModel = `
[request_definition]
r = sub, obj, act, dom

[policy_definition]
p = sub, obj, act, dom

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.dom) && keyMatch2(r.obj, p.obj) && regexMatch(r.act, p.act) && (r.dom == p.dom || p.dom == "*")
`

type sqliteMiddlewareTestConfig struct {
	strings map[string]string
	uints   map[string][]uint
}

func (c sqliteMiddlewareTestConfig) ConfigFileChangeListen(...func()) {}
func (c sqliteMiddlewareTestConfig) Get(key string) interface{} {
	return c.strings[key]
}
func (c sqliteMiddlewareTestConfig) GetString(key string) string { return c.strings[key] }
func (sqliteMiddlewareTestConfig) GetBool(string) bool           { return false }
func (sqliteMiddlewareTestConfig) GetInt(string) int             { return 0 }
func (sqliteMiddlewareTestConfig) GetInt32(string) int32         { return 0 }
func (sqliteMiddlewareTestConfig) GetInt64(string) int64         { return 0 }
func (sqliteMiddlewareTestConfig) GetFloat64(string) float64     { return 0 }
func (sqliteMiddlewareTestConfig) GetDuration(string) time.Duration {
	return 0
}
func (sqliteMiddlewareTestConfig) GetStringSlice(string) []string { return nil }
func (c sqliteMiddlewareTestConfig) GetUintSlice(key string) []uint {
	return c.uints[key]
}
func (sqliteMiddlewareTestConfig) Set(string, interface{}) {}
func (sqliteMiddlewareTestConfig) SaveConfig() error       { return nil }

func newSQLiteMiddlewareTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB, oldConfig, oldLog := app.GormDbSQLite, app.ConfigYml, app.ZapLog
	app.ConfigYml = sqliteMiddlewareTestConfig{
		strings: map[string]string{
			"gormv2.usedbtype":   "sqlite",
			"casbin.tableprefix": "",
			"casbin.tablename":   "sys_casbin_rule",
		},
		uints: map[string][]uint{"server.notcheckuser": nil},
	}
	app.ZapLog = zap.NewNop()
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "system.db"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	_, err = sqlitebootstrap.Initialize(ctx, db)
	require.NoError(t, err)
	app.GormDbSQLite = db
	t.Cleanup(func() {
		raw, dbErr := db.DB()
		if dbErr == nil {
			_ = raw.Close()
		}
		app.GormDbSQLite, app.ConfigYml, app.ZapLog = oldDB, oldConfig, oldLog
	})
	return db
}

func TestSQLiteSystemCasbinMiddlewareDeniesUnassignedAPIAndPersistsGrant(t *testing.T) {
	db := newSQLiteMiddlewareTestDB(t)
	helper := casbinhelper.NewCasbinHelper()
	require.NoError(t, helper.InitCasbin(db, sqliteSystemCasbinModel))
	t.Cleanup(helper.Close)
	require.NoError(t, helper.AddRolesForUserByID(12001, []uint{12001}))

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 12001}})
		c.Next()
	}, helper.CasbinMiddleware())
	engine.GET("/api/sqlite-denied", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/api/sqlite-denied", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)

	require.NoError(t, helper.AddPolicyForRole(12001, "/api/sqlite-denied", http.MethodGet))
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code)

	var policyCount int64
	require.NoError(t, db.Table("sys_casbin_rule").Where("v0 = ? AND v1 = ? AND v2 = ?", "role_12001", "/api/sqlite-denied", http.MethodGet).Count(&policyCount).Error)
	require.EqualValues(t, 1, policyCount)
}

func TestSQLiteSystemDataScopeRejectsOtherDepartmentRecords(t *testing.T) {
	db := newSQLiteMiddlewareTestDB(t)
	status := int8(1)
	rootID, childID, otherID := uint(12001), uint(12002), uint(12003)
	require.NoError(t, db.Create([]models.SysDepartment{
		{BaseModel: models.BaseModel{ID: rootID}, Name: "scope root", Status: &status},
		{BaseModel: models.BaseModel{ID: childID}, ParentID: &rootID, Name: "scope child", Status: &status},
		{BaseModel: models.BaseModel{ID: otherID}, Name: "scope other", Status: &status},
	}).Error)
	currentID, childUserID, otherUserID := uint(12001), uint(12002), uint(12003)
	require.NoError(t, db.Create([]models.User{
		{BaseModel: models.BaseModel{ID: currentID}, Username: "scope-current", Password: "x", Status: 1, DeptID: rootID},
		{BaseModel: models.BaseModel{ID: childUserID}, Username: "scope-child", Password: "x", Status: 1, DeptID: childID},
		{BaseModel: models.BaseModel{ID: otherUserID}, Username: "scope-other", Password: "x", Status: 1, DeptID: otherID},
	}).Error)
	role := models.SysRole{BaseModel: models.BaseModel{ID: 12001}, Name: "scope role", Status: 1, DataScope: 4}
	require.NoError(t, db.Create(&role).Error)
	require.NoError(t, db.Create(&models.SysUserRole{UserID: currentID, RoleID: role.ID}).Error)
	require.NoError(t, db.Create([]models.SysAffix{
		{BaseModel: models.BaseModel{ID: 12001}, Name: "current.txt", CreatedBy: currentID},
		{BaseModel: models.BaseModel{ID: 12002}, Name: "child.txt", CreatedBy: childUserID},
		{BaseModel: models.BaseModel{ID: 12003}, Name: "other.txt", CreatedBy: otherUserID},
	}).Error)

	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: currentID}})
	var visible []models.SysAffix
	require.NoError(t, db.Scopes(datascope.GetDataScope(ginContext)).Find(&visible).Error)
	visibleNames := make([]string, 0, len(visible))
	for _, affix := range visible {
		visibleNames = append(visibleNames, affix.Name)
	}
	require.ElementsMatch(t, []string{"current.txt", "child.txt"}, visibleNames)
	require.NotContains(t, visibleNames, "other.txt")
}
