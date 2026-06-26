package controllers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newDeviceMgmtRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbCatalogNode{},
		&gbmodels.GbChannelMount{},
		&gbmodels.GbAnomalyRecord{},
		&gbmodels.GbChannel{},
		&gbmodels.GbDevice{},
	))

	dmgmt := gbcontrollers.NewDeviceMgmtController()
	dmgmt.SetDB(func() *gorm.DB { return db })
	mc := gbcontrollers.NewMapController()
	mc.SetDB(func() *gorm.DB { return db })
	ac := gbcontrollers.NewAnomalyController()
	ac.SetDB(func() *gorm.DB { return db })

	r := gin.New()
	r.Use(gin.Recovery())
	gr := r.Group("/api/gb28181/device-mgmt")
	{
		gr.GET("/devices", dmgmt.ListDevices)
		gr.GET("/device/:id", dmgmt.GetDevice)
		gr.GET("/channels", dmgmt.ListChannels)
		gr.GET("/channel/:id", dmgmt.GetChannel)
		gr.GET("/channel/:id/mounts", dmgmt.ListChannelMounts)
		gr.GET("/channel/:id/timeline", dmgmt.ChannelTimeline)
		gr.GET("/map/markers", mc.Markers)
		gr.GET("/map/clusters", mc.Clusters)
		gr.GET("/map/no-coord-count", mc.NoCoordCount)
		gr.GET("/anomaly", ac.List)
		gr.POST("/anomaly/:id/resolve", ac.Resolve)
		gr.POST("/anomaly/batch-resolve", ac.BatchResolve)
	}
	return r, db
}

func seedDevicesAndChannels(t *testing.T, db *gorm.DB) (devID uint, chOnlineID, chOfflineID uint) {
	t.Helper()
	d := &gbmodels.GbDevice{TenantID: 1, DeviceID: "34020000002000000001", Name: "测试 NVR", Manufacturer: "Hikvision", Status: gbmodels.DeviceStatusOnline, SubscribeCapability: gbmodels.SubscribeUnknown}
	require.NoError(t, db.Create(d).Error)
	ch1 := &gbmodels.GbChannel{TenantID: 1, DeviceID: "34020000002000000001", ChannelID: "37011200001310000001", Name: "通道 在线", Status: gbmodels.ChannelStatusOnline, Latitude: 36.685, Longitude: 117.05, PTZType: 1}
	ch2 := &gbmodels.GbChannel{TenantID: 1, DeviceID: "34020000002000000001", ChannelID: "37011200001310000002", Name: "通道 离线", Status: gbmodels.ChannelStatusOffline}
	require.NoError(t, db.Create(ch1).Error)
	require.NoError(t, db.Create(ch2).Error)
	return d.ID, ch1.ID, ch2.ID
}

// ---------- B2 devicemgmt ----------

func TestDeviceMgmt_ListDevices(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/devices", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	list := data["list"].([]any)
	assert.Len(t, list, 1)
	d := list[0].(map[string]any)
	assert.Equal(t, "测试 NVR", d["name"])
	assert.EqualValues(t, 2, d["channelCount"])
	assert.EqualValues(t, 1, d["channelOnlineCount"])
}

func TestDeviceMgmt_ListChannels_FilterStatus(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/channels?status=online", nil)
	r.ServeHTTP(w, req)

	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	assert.EqualValues(t, 1, data["total"])
}

func TestDeviceMgmt_GetChannel(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, chOnID, _ := seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/channel/"+uintStr(chOnID), nil)
	r.ServeHTTP(w, req)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	assert.Equal(t, "通道 在线", data["name"])
}

func TestDeviceMgmt_ChannelMounts(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, chOnID, _ := seedDevicesAndChannels(t, db)
	// 加一个主挂载
	node := &gbmodels.GbCatalogNode{TenantID: 1, NodeType: gbmodels.NodeTypeCivilCode, Path: "/1/", Name: "山东"}
	require.NoError(t, db.Create(node).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannelMount{TenantID: 1, ChannelID: chOnID, ParentNodeID: node.ID, IsPrimary: true, MountSource: gbmodels.MountSourceCatalog}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/channel/"+uintStr(chOnID)+"/mounts", nil)
	r.ServeHTTP(w, req)

	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	list := data["list"].([]any)
	require.Len(t, list, 1)
	m := list[0].(map[string]any)
	assert.True(t, m["isPrimary"].(bool))
	assert.Equal(t, "山东", m["parentName"])
}

func TestDeviceMgmt_ChannelTimeline(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, chOnID, _ := seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/channel/"+uintStr(chOnID)+"/timeline?range=24h", nil)
	r.ServeHTTP(w, req)

	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	slots := data["slots"].([]any)
	assert.Len(t, slots, 48, "24h 应有 48 个 30min slot")
}

// ---------- B3 map ----------

func TestMap_Markers(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/map/markers", nil)
	r.ServeHTTP(w, req)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	assert.EqualValues(t, 1, data["total"], "只有 1 个通道带坐标")
}

func TestMap_NoCoordCount(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/map/no-coord-count", nil)
	r.ServeHTTP(w, req)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	assert.EqualValues(t, 1, data["count"], "1 个通道无坐标")
}

func TestMap_Clusters(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/map/clusters?zoom=10", nil)
	r.ServeHTTP(w, req)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	assert.NotNil(t, data["clusters"])
}

// ---------- B4 anomaly ----------

func TestAnomaly_List(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	// 加 2 条 anomaly:1 未处理 + 1 已处理
	require.NoError(t, db.Create(&gbmodels.GbAnomalyRecord{TenantID: 1, CatalogNodeID: 1, RawCode: "X", FallbackType: gbmodels.FallbackTypeVirtualOrg, Resolved: false}).Error)
	require.NoError(t, db.Create(&gbmodels.GbAnomalyRecord{TenantID: 1, CatalogNodeID: 2, RawCode: "Y", FallbackType: gbmodels.FallbackTypeVirtualOrg, Resolved: true}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/anomaly?resolved=0", nil)
	r.ServeHTTP(w, req)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	assert.EqualValues(t, 1, data["total"], "默认只列未处理")
}

func TestAnomaly_Resolve_ChangeType(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	node := &gbmodels.GbCatalogNode{TenantID: 1, NodeType: gbmodels.NodeTypeVirtualOrg, Path: "/1/", Name: "异常节点", Anomaly: true}
	require.NoError(t, db.Create(node).Error)
	rec := &gbmodels.GbAnomalyRecord{TenantID: 1, CatalogNodeID: node.ID, RawCode: "XYZ", FallbackType: gbmodels.FallbackTypeVirtualOrg, Resolved: false}
	require.NoError(t, db.Create(rec).Error)

	body, _ := json.Marshal(map[string]any{
		"action":     "change-type",
		"targetType": "biz_group",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/gb28181/device-mgmt/anomaly/"+uintStr(rec.ID)+"/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 验证节点类型改了 + anomaly 清了
	var updated gbmodels.GbCatalogNode
	require.NoError(t, db.First(&updated, node.ID).Error)
	assert.Equal(t, gbmodels.NodeTypeBizGroup, updated.NodeType)
	assert.False(t, updated.Anomaly)

	// 验证 anomaly resolved
	var ur gbmodels.GbAnomalyRecord
	require.NoError(t, db.First(&ur, rec.ID).Error)
	assert.True(t, ur.Resolved)
	assert.Equal(t, "change-type", ur.ResolvedAction)
}

func TestAnomaly_BatchResolve(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	ids := []uint{}
	for i := 0; i < 3; i++ {
		rec := &gbmodels.GbAnomalyRecord{TenantID: 1, CatalogNodeID: uint(i + 1), RawCode: "X", FallbackType: gbmodels.FallbackTypeVirtualOrg, Resolved: false}
		require.NoError(t, db.Create(rec).Error)
		ids = append(ids, rec.ID)
	}
	body, _ := json.Marshal(map[string]any{
		"ids":    ids,
		"action": "mark-resolved",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/gb28181/device-mgmt/anomaly/batch-resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	succeeded := data["succeeded"].([]any)
	assert.Len(t, succeeded, 3)
}
