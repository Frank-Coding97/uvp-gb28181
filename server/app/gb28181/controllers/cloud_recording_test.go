package controllers_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type fakeCloudRecordingManager struct {
	enableCalls  int
	disableCalls int
}

func (m *fakeCloudRecordingManager) Enable(_ context.Context, channelID uint) (*gbmodels.GbChannel, error) {
	m.enableCalls++
	return &gbmodels.GbChannel{ID: channelID, CloudRecordingEnabled: true, CloudRecordingState: gbmodels.CloudRecordingStateRecording}, nil
}

func (m *fakeCloudRecordingManager) Disable(_ context.Context, channelID uint) (*gbmodels.GbChannel, error) {
	m.disableCalls++
	return &gbmodels.GbChannel{ID: channelID, CloudRecordingState: gbmodels.CloudRecordingStateDisabled}, nil
}

func newCloudRecordingRouter(t *testing.T, db *gorm.DB, manager gbcontrollers.CloudRecordingManager, userID uint) *gin.Engine {
	t.Helper()
	controller := gbcontrollers.NewCloudRecordingController(manager)
	controller.SetDB(func() *gorm.DB { return db })
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(userID))
	router.PATCH("/channel/:id/cloud-recording", controller.Update)
	return router
}

func TestCloudRecordingControllerEnablesVisibleChannel(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	channel := &gbmodels.GbChannel{DeviceID: "device", ChannelID: "channel", OwnerDeptID: 10}
	require.NoError(t, db.Create(channel).Error)
	manager := &fakeCloudRecordingManager{}
	router := newCloudRecordingRouter(t, db, manager, 100)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/channel/"+uintStr(channel.ID)+"/cloud-recording", bytes.NewBufferString(`{"enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, manager.enableCalls)
	body := unmarshal(t, response)
	data := body["data"].(map[string]any)
	require.Equal(t, true, data["cloudRecordingEnabled"])
	require.Equal(t, gbmodels.CloudRecordingStateRecording, data["cloudRecordingState"])
}

func TestCloudRecordingControllerRejectsMissingEnabled(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	channel := &gbmodels.GbChannel{DeviceID: "device", ChannelID: "channel", OwnerDeptID: 10}
	require.NoError(t, db.Create(channel).Error)
	manager := &fakeCloudRecordingManager{}
	router := newCloudRecordingRouter(t, db, manager, 100)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/channel/"+uintStr(channel.ID)+"/cloud-recording", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Equal(t, 0, manager.enableCalls+manager.disableCalls)
}

func TestCloudRecordingControllerHidesOtherDepartmentChannel(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	channel := &gbmodels.GbChannel{DeviceID: "device", ChannelID: "channel", OwnerDeptID: 20}
	require.NoError(t, db.Create(channel).Error)
	manager := &fakeCloudRecordingManager{}
	router := newCloudRecordingRouter(t, db, manager, 100)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/channel/"+uintStr(channel.ID)+"/cloud-recording", bytes.NewBufferString(`{"enabled":false}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Equal(t, 0, manager.enableCalls+manager.disableCalls)
	require.Equal(t, "通道不存在", unmarshal(t, response)["message"])
}

func TestCloudRecordingControllerReturns503WithoutManager(t *testing.T) {
	db := newScopedDeviceDB(t)
	router := newCloudRecordingRouter(t, db, nil, 100)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/channel/1/cloud-recording", bytes.NewBufferString(`{"enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
}

func TestGenericChannelPatchCannotChangeCloudRecording(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	channel := &gbmodels.GbChannel{DeviceID: "device", ChannelID: "channel", OwnerDeptID: 10}
	require.NoError(t, db.Create(channel).Error)
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.PATCH("/channel/:id", controller.UpdateChannel)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/channel/"+uintStr(channel.ID), bytes.NewBufferString(`{"cloudRecordingEnabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	var stored gbmodels.GbChannel
	require.NoError(t, db.First(&stored, channel.ID).Error)
	require.False(t, stored.CloudRecordingEnabled)
}
