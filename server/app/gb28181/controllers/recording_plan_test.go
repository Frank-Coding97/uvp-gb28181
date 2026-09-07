package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	appcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
)

func newRecordingPlanControllerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&appmodels.User{}, &gbmodels.GbRecordingPlan{}, &gbmodels.GbRecordingPlanPeriod{},
		&gbmodels.GbRecordingPlanBinding{}, &gbmodels.GbRecordingPlanChannelState{},
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{},
	))
	return db
}

func TestRecordingPlanControllerRejectsMissingIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newRecordingPlanControllerDB(t)
	controller := appcontrollers.NewRecordingPlanController(func() *gorm.DB { return db })
	router := gin.New()
	router.GET("/plans", controller.Page)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/plans", nil))

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), "未登录")
}

func TestRecordingPlanControllerScopesPlansToCurrentDepartment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newRecordingPlanControllerDB(t)
	require.NoError(t, db.Create(&appmodels.User{BaseModel: appmodels.BaseModel{ID: 7}, Username: "owner", Password: "x", Description: "", DeptID: 10}).Error)
	require.NoError(t, db.Create(&gbmodels.GbRecordingPlan{Name: "本部门", Status: 1, Version: 1, OwnerDeptID: 10}).Error)
	require.NoError(t, db.Create(&gbmodels.GbRecordingPlan{Name: "其他部门", Status: 1, Version: 1, OwnerDeptID: 20}).Error)

	controller := appcontrollers.NewRecordingPlanController(func() *gorm.DB { return db })
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
	})
	router.GET("/plans", controller.Page)
	router.GET("/plans/:id", controller.Detail)

	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/plans?page=1&pageSize=500", nil))
	require.Equal(t, http.StatusOK, page.Code)
	require.Contains(t, page.Body.String(), "本部门")
	require.NotContains(t, page.Body.String(), "其他部门")
	require.Contains(t, page.Body.String(), `"pageSize":100`)

	detail := httptest.NewRecorder()
	router.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/plans/2", nil))
	require.Equal(t, http.StatusNotFound, detail.Code)
}
