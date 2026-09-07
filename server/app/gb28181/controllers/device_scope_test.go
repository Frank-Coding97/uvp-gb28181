package controllers_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	zlmrepo "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

func newScopedDeviceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{},
		&gbmodels.GbChannel{},
		&gbmodels.GbDeviceGrant{},
		&zlmrepo.MetaNode{},
		&basemodels.SysDepartment{},
		&basemodels.SysRole{},
		&basemodels.SysUserRole{},
		&basemodels.User{},
	))

	prevDB := app.GormDbMysql
	prevConfig := app.ConfigYml
	prevResponse := app.Response
	prevZapLog := app.ZapLog
	t.Cleanup(func() {
		app.GormDbMysql = prevDB
		app.ConfigYml = prevConfig
		app.Response = prevResponse
		app.ZapLog = prevZapLog
	})
	app.GormDbMysql = db
	app.ConfigYml = scopedTestConfig{}
	app.Response = mockResponse{}
	app.ZapLog = zap.NewNop()
	return db
}

func newScopedDeviceRouter(t *testing.T, userID uint) (*gin.Engine, *gorm.DB) {
	t.Helper()
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, userID, 10)
	seedScopedDeviceRows(t, db)

	dc := gbcontrollers.NewDeviceController()
	pc := gbcontrollers.NewPlayController(&play.Service{})
	r := gin.New()
	r.Use(gin.Recovery(), withClaims(userID))
	r.GET("/api/gb28181/device/:deviceId", dc.GetByDeviceID)
	r.GET("/api/gb28181/device/:deviceId/channels", dc.ListChannels)
	r.PATCH("/api/gb28181/device/:deviceId", dc.Update)
	r.POST("/api/gb28181/play/:deviceId/:channelId", pc.Start)
	r.DELETE("/api/gb28181/play/:streamId", pc.Stop)
	return r, db
}

func seedScopedDeviceRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID:            "34020000002000000010",
		Name:                "本部门 NVR",
		Status:              gbmodels.DeviceStatusOnline,
		OwnerDeptID:         10,
		SubscribeCapability: gbmodels.SubscribeUnknown,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID:            "34020000002000000020",
		Name:                "外部门 NVR",
		Status:              gbmodels.DeviceStatusOnline,
		OwnerDeptID:         20,
		SubscribeCapability: gbmodels.SubscribeUnknown,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{
		DeviceID:    "34020000002000000010",
		ChannelID:   "37011200001310000010",
		Name:        "本部门通道",
		Status:      gbmodels.ChannelStatusOnline,
		StreamID:    "dept10-stream",
		OwnerDeptID: 10,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{
		DeviceID:    "34020000002000000020",
		ChannelID:   "37011200001310000020",
		Name:        "外部门通道",
		Status:      gbmodels.ChannelStatusOnline,
		StreamID:    "dept20-stream",
		OwnerDeptID: 20,
	}).Error)
}

func TestDeviceController_GetByDeviceID_FiltersOwnerDept(t *testing.T) {
	r, _ := newScopedDeviceRouter(t, 100)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/gb28181/device/34020000002000000020", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := unmarshal(t, w)
	assert.EqualValues(t, 1, resp["code"])
	assert.Equal(t, "设备不存在", resp["message"])
}

func TestDeviceController_ListChannels_FiltersOwnerDept(t *testing.T) {
	r, _ := newScopedDeviceRouter(t, 100)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/gb28181/device/34020000002000000020/channels", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := unmarshal(t, w)
	assert.EqualValues(t, 1, resp["code"])
	assert.Equal(t, "设备不存在", resp["message"])
}

func TestDeviceController_UpdateAutoDefaultsTo2016AfterOverrideWithoutReportedVersion(t *testing.T) {
	r, db := newScopedDeviceRouter(t, 100)
	const deviceID = "34020000002000000010"
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", deviceID).Updates(map[string]interface{}{
		"reported_version": "", "protocol_override": gbmodels.ProtocolVersion2022,
		"effective_version": gbmodels.ProtocolVersion2022, "effective_version_source": gbmodels.ProtocolVersionSourceOverride,
	}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPatch, "/api/gb28181/device/"+deviceID, bytes.NewBufferString(`{"protocolOverride":"auto"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var stored gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", deviceID).First(&stored).Error)
	require.Equal(t, gbmodels.ProtocolOverrideAuto, stored.ProtocolOverride)
	require.Equal(t, gbmodels.ProtocolVersion2016, stored.EffectiveVersion)
	require.Equal(t, gbmodels.ProtocolVersionSourceDefault, stored.EffectiveVersionSource)
}

func TestDeviceController_UpdateZLMNodeBinding(t *testing.T) {
	r, db := newScopedDeviceRouter(t, 100)
	require.NoError(t, db.Create(&zlmrepo.MetaNode{ID: 7, Name: "主媒体节点", State: "active"}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPatch, "/api/gb28181/device/34020000002000000010", bytes.NewBufferString(`{"zlmNodeId":7}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var stored gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", "34020000002000000010").First(&stored).Error)
	require.Equal(t, int64(7), stored.ZLMNodeID)
}

func TestDeviceController_UpdateRejectsUnknownZLMNode(t *testing.T) {
	r, _ := newScopedDeviceRouter(t, 100)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPatch, "/api/gb28181/device/34020000002000000010", bytes.NewBufferString(`{"zlmNodeId":999}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	resp := unmarshal(t, w)
	require.EqualValues(t, 1, resp["code"])
	require.Equal(t, "ZLM 节点不存在", resp["message"])
}

func TestPlayController_FiltersOwnerDept(t *testing.T) {
	r, _ := newScopedDeviceRouter(t, 100)

	start := httptest.NewRecorder()
	startReq, _ := http.NewRequest(http.MethodPost, "/api/gb28181/play/34020000002000000020/37011200001310000020", nil)
	r.ServeHTTP(start, startReq)
	startResp := unmarshal(t, start)
	assert.EqualValues(t, 1, startResp["code"])
	assert.Equal(t, "通道不存在", startResp["message"])

	stop := httptest.NewRecorder()
	stopReq, _ := http.NewRequest(http.MethodDelete, "/api/gb28181/play/dept20-stream", nil)
	r.ServeHTTP(stop, stopReq)
	stopResp := unmarshal(t, stop)
	assert.EqualValues(t, 1, stopResp["code"])
	assert.Equal(t, "流不存在或无权停播", stopResp["message"])
}

type scopedTestConfig struct{}

func (scopedTestConfig) ConfigFileChangeListen(...func()) {}
func (scopedTestConfig) Get(string) interface{}           { return nil }
func (scopedTestConfig) GetBool(string) bool              { return false }
func (scopedTestConfig) GetInt(string) int                { return 0 }
func (scopedTestConfig) GetInt32(string) int32            { return 0 }
func (scopedTestConfig) GetInt64(string) int64            { return 0 }
func (scopedTestConfig) GetFloat64(string) float64        { return 0 }
func (scopedTestConfig) GetDuration(string) time.Duration { return 0 }
func (scopedTestConfig) GetStringSlice(string) []string   { return nil }
func (scopedTestConfig) GetUintSlice(string) []uint       { return nil }
func (scopedTestConfig) Set(string, interface{})          {}
func (scopedTestConfig) SaveConfig() error                { return nil }
func (scopedTestConfig) GetString(keyName string) string {
	if keyName == "gormv2.usedbtype" {
		return "mysql"
	}
	return ""
}
