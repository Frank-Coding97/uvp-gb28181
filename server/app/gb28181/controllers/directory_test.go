package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

func TestDirectoryControllerViews(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbCatalogNode{}, &gbmodels.GbCustomGroup{}, &gbmodels.GbCustomGroupDevice{}, &civilcode.SysCivilCode{}))
	require.NoError(t, db.Create(&civilcode.SysCivilCode{Code: "370000", ShortName: "山东省", Level: 1}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "37000000001180000001", OwnerDeptID: 10}).Error)
	ctrl := NewDirectoryController(func() *gorm.DB { return db })
	r := gin.New()
	r.GET("/tree", ctrl.Tree)
	for _, view := range []string{"national", "custom"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tree?view="+view, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"list"`)
	}
	for _, query := range []string{"", "?view=foo"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tree"+query, nil))
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
}
