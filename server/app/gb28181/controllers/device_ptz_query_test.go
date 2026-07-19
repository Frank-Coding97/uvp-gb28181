package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestDeviceMgmt_GetPTZStateAndOperation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZState{}, &gbmodels.GbPTZOperation{}))
	device := &gbmodels.GbDevice{DeviceID: "D", Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline, PTZType: 1}
	require.NoError(t, db.Create(channel).Error)
	pan := 12.5
	require.NoError(t, db.Create(&gbmodels.GbPTZState{DeviceID: device.ID, ChannelID: channel.ID, ChannelCode: "C", Pan: &pan, Freshness: gbmodels.PTZFreshnessFresh}).Error)
	require.NoError(t, db.Create(&gbmodels.GbPTZOperation{OperationID: "op-1", IdempotencyKey: "k", DeviceID: device.ID, DeviceCode: "D", ChannelID: channel.ID, ChannelCode: "C", CmdType: "DeviceControl", Status: gbmodels.PTZOperationSent}).Error)
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	r := gin.New()
	r.GET("/channel/:id/ptz/precise-status", controller.GetPTZState)
	r.GET("/channel/:id/ptz/operations/:operationId", controller.GetPTZOperation)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/ptz/precise-status", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "12.5")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/ptz/operations/op-1", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, strings.Contains(w.Body.String(), "op-1"))
}
