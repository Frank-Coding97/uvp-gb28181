package controllers

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

func newCustomGroupRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbCustomGroup{}, &gbmodels.GbCustomGroupDevice{}, &gbmodels.GbDevice{}, &basemodels.User{}, &basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}))
	require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: 7}, Username: "actor", Password: "x", DeptID: 10}).Error)
	ctrl := NewCustomGroupController(func() *gorm.DB { return db })
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
		c.Next()
	})
	r.POST("/groups", ctrl.Create)
	r.PATCH("/groups/:id", ctrl.Rename)
	r.POST("/groups/:id/move", ctrl.Move)
	r.DELETE("/groups/:id", ctrl.Delete)
	r.POST("/groups/:id/devices", ctrl.AddDevices)
	r.POST("/groups/:id/devices/remove", ctrl.RemoveDevices)
	return r, db
}

func TestCustomGroupControllerContract(t *testing.T) {
	r, db := newCustomGroupRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/groups", bytes.NewBufferString(`{"name":" 东区 "}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var root gbmodels.GbCustomGroup
	require.NoError(t, db.Where("name = ?", "东区").First(&root).Error)
	require.Equal(t, uint(10), root.OwnerDeptID)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/groups", bytes.NewBufferString(`{"name":"东区"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "GROUP_NAME_CONFLICT")

	device := gbmodels.GbDevice{DeviceID: "D1", OwnerDeptID: 10}
	require.NoError(t, db.Create(&device).Error)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/groups/"+strconv.Itoa(int(root.ID))+"/devices", bytes.NewBufferString(fmt.Sprintf(`{"deviceIds":[%d]}`, device.ID)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"addedCount":1`)

	child := gbmodels.GbCustomGroup{OwnerDeptID: 10, ParentID: root.ID, Path: root.Path + "99/", Depth: 1, Name: "入口"}
	require.NoError(t, db.Create(&child).Error)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/groups/"+strconv.Itoa(int(root.ID)), nil))
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "GROUP_HAS_CHILDREN")
	require.Contains(t, w.Body.String(), "childCount")
}
