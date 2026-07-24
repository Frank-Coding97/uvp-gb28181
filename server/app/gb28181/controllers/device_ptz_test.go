package controllers_test

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type fakePTZSender struct {
	deviceID  string
	dest      string
	transport string
	body      []byte
}

func mustPTZService(t *testing.T, db *gorm.DB, sender ptz.TrackedSender) *ptz.Service {
	t.Helper()
	service, err := ptz.NewService(db, sender, time.Now)
	require.NoError(t, err)
	return service
}

type fakeTrackedPTZSender struct{}

func (fakeTrackedPTZSender) SendMessageTracked(_ context.Context, _ string, _ string, _ string, _ []byte) (uac.TrackedMessageResult, error) {
	return uac.TrackedMessageResult{CallID: "call-ptz", CSeq: "1", StatusCode: 200}, nil
}

func (f *fakePTZSender) SendMessage(_ context.Context, deviceID, dest, transport string, body []byte) error {
	f.deviceID, f.dest, f.transport, f.body = deviceID, dest, transport, body
	return nil
}

func TestDeviceMgmt_ControlPTZ(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	device := &gbmodels.GbDevice{
		DeviceID: "34020000002000000001", IP: "192.0.2.10", Port: 5060,
		Transport: "TCP", Status: gbmodels.DeviceStatusOnline,
	}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{
		DeviceID: device.DeviceID, ChannelID: "37011200001310000001",
		Status: gbmodels.ChannelStatusOnline, PTZType: 1,
	}
	require.NoError(t, db.Create(channel).Error)

	sender := &fakePTZSender{}
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetPTZSender(sender)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/channel/:id/ptz", controller.ControlPTZ)

	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz", strings.NewReader(`{"action":"left_up","speed":8}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, device.DeviceID, sender.deviceID)
	require.Equal(t, "192.0.2.10:5060", sender.dest)
	require.Equal(t, "TCP", sender.transport)
	var body struct {
		CmdType  string `xml:"CmdType"`
		DeviceID string `xml:"DeviceID"`
		PTZCmd   string `xml:"PTZCmd"`
	}
	require.NoError(t, xml.Unmarshal(bytes.Replace(sender.body, []byte(`encoding="GB2312"`), []byte(`encoding="UTF-8"`), 1), &body))
	require.Equal(t, "DeviceControl", body.CmdType)
	require.Equal(t, channel.ChannelID, body.DeviceID)
	require.Equal(t, "A50F010A080800CF", body.PTZCmd)
}

func TestDeviceMgmt_ControlPTZRejectsOfflineChannel(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	device := &gbmodels.GbDevice{DeviceID: "D", IP: "192.0.2.10", Port: 5060, Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOffline, PTZType: 1}
	require.NoError(t, db.Create(channel).Error)
	sender := &fakePTZSender{}
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetPTZSender(sender)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/channel/:id/ptz", controller.ControlPTZ)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz", strings.NewReader(`{"action":"left","speed":8}`)))
	require.Empty(t, sender.body)
}

func TestDeviceMgmt_ControlPTZ_AllowsUnreportedPTZType(t *testing.T) {
	// 回归:海康这类未上报 PTZType 的 IPC(PTZType=0)以前被硬拦,现在应放行让设备自己回应
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZOperation{}))
	device := &gbmodels.GbDevice{DeviceID: "D", IP: "192.0.2.10", Port: 5060, Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline, PTZType: 0}
	require.NoError(t, db.Create(channel).Error)
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetPTZService(mustPTZService(t, db, fakeTrackedPTZSender{}))
	r := gin.New()
	r.POST("/channel/:id/ptz", controller.ControlPTZ)
	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz", strings.NewReader(`{"action":"left","speed":8}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "未上报")
}

func TestDeviceMgmt_ControlPTZ_AllowsReportedFixedCameraType(t *testing.T) {
	// PTZType 仅作为设备描述信息；厂商上报不可靠，控制指令仍应交给设备决定是否执行。
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZOperation{}))
	device := &gbmodels.GbDevice{DeviceID: "D", IP: "192.0.2.10", Port: 5060, Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline, PTZType: 3}
	require.NoError(t, db.Create(channel).Error)
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetPTZService(mustPTZService(t, db, fakeTrackedPTZSender{}))
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/channel/:id/ptz", controller.ControlPTZ)
	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz", strings.NewReader(`{"action":"left","speed":8}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "operationId")
}

func TestDeviceMgmt_ControlPTZ_ServiceReturnsOperation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZOperation{}))
	device := &gbmodels.GbDevice{DeviceID: "D", IP: "192.0.2.10", Port: 5060, Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline, PTZType: 1}
	require.NoError(t, db.Create(channel).Error)
	service := mustPTZService(t, db, fakeTrackedPTZSender{})
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetPTZService(service)
	r := gin.New()
	r.POST("/channel/:id/ptz", controller.ControlPTZ)
	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz", strings.NewReader(`{"action":"left","speed":8,"idempotencyKey":"op-key"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "operationId")
}

func TestDeviceMgmt_ControlPTZPrecise_ServiceReturnsOperation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZOperation{}))
	device := &gbmodels.GbDevice{DeviceID: "D", IP: "192.0.2.10", Port: 5060, Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "C", Status: gbmodels.ChannelStatusOnline, PTZType: 1}
	require.NoError(t, db.Create(channel).Error)
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetPTZService(mustPTZService(t, db, fakeTrackedPTZSender{}))
	r := gin.New()
	r.POST("/channel/:id/ptz/precise", controller.ControlPTZPrecise)
	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/precise", strings.NewReader(`{"pan":12.5,"tilt":-3.25,"speed":4}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "operationId")
}
