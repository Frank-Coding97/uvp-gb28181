package controllers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

func TestChannelFavoriteControllerCreateAndIsolation(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbChannelFavoriteGroup{}, &models.GbChannelFavoriteItem{}, &models.GbChannel{}, &basemodels.User{}, &basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}, &models.GbDevice{}, &models.GbDeviceGrant{}))
	require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: 7}, Username: "u", DeptID: 1}).Error)
	require.NoError(t, db.Create(&models.GbChannel{DeviceID: "D1", ChannelID: "C1", Name: "东门", OwnerDeptID: 1}).Error)
	ctrl := NewChannelFavoriteController(func() *gorm.DB { return db })
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
		c.Next()
	})
	r.POST("/groups", ctrl.Create)
	r.GET("/groups", ctrl.List)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/groups", bytes.NewBufferString(`{"name":"重点","channels":[{"deviceCode":"D1","channelCode":"C1"}]}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/groups", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "重点")
	require.Contains(t, w.Body.String(), "东门")
}
