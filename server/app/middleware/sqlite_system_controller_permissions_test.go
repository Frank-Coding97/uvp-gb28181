package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

type controllerSQLiteConfig struct {
	app.YmlConfigInterf
	strings map[string]string
	uints   map[string][]uint
}

func (c controllerSQLiteConfig) GetString(key string) string { return c.strings[key] }
func (c controllerSQLiteConfig) GetUintSlice(key string) []uint {
	return c.uints[key]
}

func newControllerSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB, oldConfig, oldLog, oldResponse := app.GormDbSQLite, app.ConfigYml, app.ZapLog, app.Response
	app.ConfigYml = controllerSQLiteConfig{
		strings: map[string]string{"gormv2.usedbtype": "sqlite"},
		uints:   map[string][]uint{"server.notcheckuser": nil},
	}
	app.ZapLog = nil
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "system.db"))
	require.NoError(t, err)
	app.ZapLog = zap.NewNop()
	app.Response = response.NewResponseHandler()
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
		app.GormDbSQLite, app.ConfigYml, app.ZapLog, app.Response = oldDB, oldConfig, oldLog, oldResponse
	})
	return db
}

func TestSQLiteSystemExistingAffixListControllerEnforcesDataScope(t *testing.T) {
	db := newControllerSQLiteDB(t)
	status := int8(1)
	rootID, childID, otherID := uint(13001), uint(13002), uint(13003)
	require.NoError(t, db.Create([]models.SysDepartment{
		{BaseModel: models.BaseModel{ID: rootID}, Name: "controller scope root", Status: &status},
		{BaseModel: models.BaseModel{ID: childID}, ParentID: &rootID, Name: "controller scope child", Status: &status},
		{BaseModel: models.BaseModel{ID: otherID}, Name: "controller scope other", Status: &status},
	}).Error)
	currentID, childUserID, otherUserID := uint(13001), uint(13002), uint(13003)
	require.NoError(t, db.Create([]models.User{
		{BaseModel: models.BaseModel{ID: currentID}, Username: "controller-scope-current", Password: "x", Status: 1, DeptID: rootID},
		{BaseModel: models.BaseModel{ID: childUserID}, Username: "controller-scope-child", Password: "x", Status: 1, DeptID: childID},
		{BaseModel: models.BaseModel{ID: otherUserID}, Username: "controller-scope-other", Password: "x", Status: 1, DeptID: otherID},
	}).Error)
	role := models.SysRole{BaseModel: models.BaseModel{ID: 13001}, Name: "controller scope role", Status: 1, DataScope: 4}
	require.NoError(t, db.Create(&role).Error)
	require.NoError(t, db.Create(&models.SysUserRole{UserID: currentID, RoleID: role.ID}).Error)
	require.NoError(t, db.Create([]models.SysAffix{
		{BaseModel: models.BaseModel{ID: 13001}, Name: "controller-current.txt", CreatedBy: currentID},
		{BaseModel: models.BaseModel{ID: 13002}, Name: "controller-child.txt", CreatedBy: childUserID},
		{BaseModel: models.BaseModel{ID: 13003}, Name: "controller-other.txt", CreatedBy: otherUserID},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/sysAffix/list?pageNum=1&pageSize=20", nil)
	ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: currentID}})
	require.NotPanics(t, func() { controllers.NewSysAffixController().List(ctx) })
	require.Equal(t, http.StatusOK, recorder.Code)
	var body struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Name string `json:"name"`
			} `json:"list"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Zero(t, body.Code)
	require.EqualValues(t, 2, body.Data.Total)
	names := make([]string, 0, len(body.Data.List))
	for _, item := range body.Data.List {
		names = append(names, item.Name)
	}
	require.ElementsMatch(t, []string{"controller-current.txt", "controller-child.txt"}, names)
	require.NotContains(t, names, "controller-other.txt")
}
