package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestDeviceMgmt_DeleteDevice_RemovesStatusEvents(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	deviceID, _, _ := seedDevicesAndChannels(t, db)
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Create(&gbmodels.GbDeviceStatusEvent{
			DeviceID: deviceID, DeviceCode: "34020000002000000001",
			EventType: gbmodels.DeviceEventRegisterRenewed, ToStatus: gbmodels.DeviceStatusOnline,
			OccurredAt: time.Now().Add(time.Duration(i) * time.Second), Source: gbmodels.DeviceEventSourceRegister,
		}).Error)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/gb28181/device-mgmt/device/"+uintStr(deviceID), nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var count int64
	require.NoError(t, db.Unscoped().Model(&gbmodels.GbDeviceStatusEvent{}).Where("device_id = ?", deviceID).Count(&count).Error)
	assert.Zero(t, count)
}

func TestDeviceMgmt_ListDeviceStatusEvents_PaginatesAndOrders(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	deviceID, _, _ := seedDevicesAndChannels(t, db)
	base := time.Date(2026, 7, 19, 10, 0, 0, 0, time.Local)
	for i, eventType := range []gbmodels.DeviceStatusEventType{
		gbmodels.DeviceEventRegisterOnline, gbmodels.DeviceEventRegisterRenewed, gbmodels.DeviceEventHeartbeatTimeout,
	} {
		require.NoError(t, db.Create(&gbmodels.GbDeviceStatusEvent{
			DeviceID: deviceID, DeviceCode: "34020000002000000001", EventType: eventType,
			ToStatus: gbmodels.DeviceStatusOnline, OccurredAt: base.Add(time.Duration(i) * time.Minute),
			Source: gbmodels.DeviceEventSourceRegister,
		}).Error)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/device/"+uintStr(deviceID)+"/status-events?page=1&pageSize=2", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	assert.EqualValues(t, 3, data["total"])
	assert.EqualValues(t, 2, data["pageSize"])
	list := data["list"].([]any)
	require.Len(t, list, 2)
	assert.Equal(t, string(gbmodels.DeviceEventHeartbeatTimeout), list[0].(map[string]any)["eventType"])
	assert.Equal(t, "心跳超时", list[0].(map[string]any)["eventName"])
}

func TestDeviceMgmt_ListDeviceStatusEvents_RejectsOtherOwnerDept(t *testing.T) {
	const userID = 301
	r, db := newDeviceMgmtRouter(t, withClaims(userID))
	seedDeptScopedUser(t, db, userID, 10)
	other := &gbmodels.GbDevice{OwnerDeptID: 20, DeviceID: "34020000002000000020", SubscribeCapability: gbmodels.SubscribeUnknown}
	require.NoError(t, db.Create(other).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceStatusEvent{
		DeviceID: other.ID, DeviceCode: other.DeviceID, EventType: gbmodels.DeviceEventRegisterOnline,
		ToStatus: gbmodels.DeviceStatusOnline, OccurredAt: time.Now(), Source: gbmodels.DeviceEventSourceRegister,
	}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/device/"+uintStr(other.ID)+"/status-events", nil)
	r.ServeHTTP(w, req)

	resp := unmarshal(t, w)
	assert.EqualValues(t, 1, resp["code"])
	assert.Equal(t, "设备不存在", resp["message"])
}
