package controllers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type fakeSubscriptionManager struct{ calls []string }

func (f *fakeSubscriptionManager) Enable(_ context.Context, device *gbmodels.GbDevice, kind gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error) {
	f.calls = append(f.calls, "enable:"+string(kind))
	return &gbmodels.GbDeviceSubscription{DeviceID: device.ID, Kind: kind, Enabled: true, Status: gbmodels.SubscriptionStatusActive, ExpiresSeconds: 3600}, nil
}

func (f *fakeSubscriptionManager) Disable(_ context.Context, device *gbmodels.GbDevice, kind gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error) {
	f.calls = append(f.calls, "disable:"+string(kind))
	return &gbmodels.GbDeviceSubscription{DeviceID: device.ID, Kind: kind, Status: gbmodels.SubscriptionStatusDisabled, ExpiresSeconds: 3600}, nil
}

func (f *fakeSubscriptionManager) Configure(_ context.Context, device *gbmodels.GbDevice, kind gbmodels.SubscriptionKind, enabled *bool, expiresSeconds *int, intervalSeconds *int) (*gbmodels.GbDeviceSubscription, error) {
	f.calls = append(f.calls, "configure:"+string(kind))
	sub := &gbmodels.GbDeviceSubscription{DeviceID: device.ID, Kind: kind, Status: gbmodels.SubscriptionStatusDisabled, ExpiresSeconds: 3600}
	if enabled != nil {
		sub.Enabled = *enabled
		if *enabled {
			sub.Status = gbmodels.SubscriptionStatusActive
		}
	}
	if expiresSeconds != nil {
		sub.ExpiresSeconds = *expiresSeconds
	}
	if intervalSeconds != nil {
		sub.IntervalSeconds = *intervalSeconds
	}
	return sub, nil
}

func (f *fakeSubscriptionManager) Renew(_ context.Context, device *gbmodels.GbDevice, kind gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error) {
	f.calls = append(f.calls, "renew:"+string(kind))
	return &gbmodels.GbDeviceSubscription{DeviceID: device.ID, Kind: kind, Enabled: true, Status: gbmodels.SubscriptionStatusActive, ExpiresSeconds: 3600}, nil
}

func newDeviceMgmtRouter(t *testing.T, middlewares ...gin.HandlerFunc) (*gin.Engine, *gorm.DB) {
	t.Helper()
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbCatalogNode{},
		&gbmodels.GbChannelMount{},
		&gbmodels.GbAnomalyRecord{},
		&gbmodels.GbChannel{},
		&gbmodels.GbDevice{},
		&gbmodels.GbMobilePositionLatest{},
		&gbmodels.GbDeviceSubscription{},
		&gbmodels.GbAlarmEvent{},
		&gbmodels.GbDeviceStatusEvent{},
		&gbmodels.GbCustomGroup{},
		&gbmodels.GbCustomGroupDevice{},
		&basemodels.SysDepartment{},
		&basemodels.SysRole{},
		&basemodels.SysUserRole{},
		&basemodels.User{},
	 &gbmodels.GbDeviceGrant{}))

	dmgmt := gbcontrollers.NewDeviceMgmtController()
	dmgmt.SetDB(func() *gorm.DB { return db })
	mc := gbcontrollers.NewMapController()
	mc.SetDB(func() *gorm.DB { return db })
	ac := gbcontrollers.NewAnomalyController()
	ac.SetDB(func() *gorm.DB { return db })

	r := gin.New()
	r.Use(gin.Recovery())
	if len(middlewares) > 0 {
		r.Use(middlewares...)
	}
	gr := r.Group("/api/gb28181/device-mgmt")
	{
		gr.GET("/devices", dmgmt.ListDevices)
		gr.POST("/device", dmgmt.CreateDevice)
		gr.GET("/device/:id", dmgmt.GetDevice)
		gr.GET("/device/:id/status-events", dmgmt.ListDeviceStatusEvents)
		gr.GET("/device/:id/subscriptions", dmgmt.ListSubscriptions)
		gr.PATCH("/device/:id/subscriptions/:kind", dmgmt.UpdateSubscription)
		gr.POST("/device/:id/subscriptions/:kind/renew", dmgmt.RenewSubscription)
		gr.GET("/device/:id/alarms", dmgmt.ListAlarms)
		gr.DELETE("/device/:id", dmgmt.DeleteDevice)
		gr.POST("/device/batch-delete", dmgmt.BatchDeleteDevices)
		gr.GET("/channels", dmgmt.ListChannels)
		gr.GET("/channel/:id", dmgmt.GetChannel)
		gr.PATCH("/channel/:id", dmgmt.UpdateChannel)
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

func TestDeviceMgmt_SubscriptionUpdateAndRenew(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}))
	device := &gbmodels.GbDevice{DeviceID: "D", Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	manager := &fakeSubscriptionManager{}
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetSubscriptionManager(manager)
	r := gin.New()
	r.Use(gin.Recovery())
	r.PATCH("/device/:id/subscriptions/:kind", controller.UpdateSubscription)
	r.POST("/device/:id/subscriptions/:kind/renew", controller.RenewSubscription)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PATCH", "/device/"+uintStr(device.ID)+"/subscriptions/catalog", bytes.NewBufferString(`{"enabled":true,"expiresSeconds":7200}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/device/"+uintStr(device.ID)+"/subscriptions/catalog/renew", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"configure:catalog", "renew:catalog"}, manager.calls)
}

func TestDeviceMgmt_SubscriptionsReturnsAllKinds(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	deviceID, _, _ := seedDevicesAndChannels(t, db)
	require.NoError(t, db.Create(&gbmodels.GbDeviceSubscription{DeviceID: deviceID, Kind: gbmodels.SubscriptionKindCatalog, Enabled: true, Status: gbmodels.SubscriptionStatusActive, Event: "Catalog", ExpiresSeconds: 3600}).Error)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/device/"+uintStr(deviceID)+"/subscriptions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	list := unmarshal(t, w)["data"].(map[string]any)["list"].([]any)
	require.Len(t, list, 3)
}

func TestDeviceMgmt_DeleteDeviceRemovesSubscriptionData(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	deviceID, channelID, _ := seedDevicesAndChannels(t, db)
	other := &gbmodels.GbDevice{DeviceID: "other-delete", SubscribeCapability: gbmodels.SubscribeUnknown}
	require.NoError(t, db.Create(other).Error)
	group := &gbmodels.GbCustomGroup{Path: "/1/", Name: "delete-group"}
	require.NoError(t, db.Create(group).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbCustomGroupDevice{{GroupID: group.ID, DeviceID: deviceID}, {GroupID: group.ID, DeviceID: other.ID}}).Error)
	now := time.Now()
	require.NoError(t, db.Create(&gbmodels.GbDeviceSubscription{DeviceID: deviceID, Kind: gbmodels.SubscriptionKindAlarm, Event: "presence"}).Error)
	require.NoError(t, db.Create(&gbmodels.GbMobilePositionLatest{DeviceID: deviceID, SourceCode: "C", ChannelID: &channelID, EventTime: now, ReceivedAt: now, Latitude: 1, Longitude: 1}).Error)
	require.NoError(t, db.Create(&gbmodels.GbAlarmEvent{DeviceID: deviceID, ChannelID: &channelID, SourceCode: "C", DedupeKey: "delete-device", ReceivedAt: now}).Error)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/gb28181/device-mgmt/device/"+uintStr(deviceID), nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	for _, model := range []any{&gbmodels.GbDeviceSubscription{}, &gbmodels.GbMobilePositionLatest{}, &gbmodels.GbAlarmEvent{}} {
		var count int64
		require.NoError(t, db.Model(model).Where("device_id = ?", deviceID).Count(&count).Error)
		require.Zero(t, count)
	}
	var targetMembership, otherMembership int64
	require.NoError(t, db.Model(&gbmodels.GbCustomGroupDevice{}).Where("device_id = ?", deviceID).Count(&targetMembership).Error)
	require.NoError(t, db.Model(&gbmodels.GbCustomGroupDevice{}).Where("device_id = ?", other.ID).Count(&otherMembership).Error)
	require.Zero(t, targetMembership)
	require.EqualValues(t, 1, otherMembership)
}

func seedDeptScopedUser(t *testing.T, db *gorm.DB, userID, deptID uint) {
	t.Helper()
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: deptID}, Name: "dept"}).Error)
	user := &basemodels.User{BaseModel: basemodels.BaseModel{ID: userID}, Username: "dept-user", Password: "x", DeptID: deptID}
	require.NoError(t, db.Create(user).Error)
	role := &basemodels.SysRole{Name: "dept-role", DataScope: 3}
	require.NoError(t, db.Create(role).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: user.ID, RoleID: role.ID}).Error)
}

func withClaims(userID uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: userID}})
		c.Next()
	}
}

func seedDevicesAndChannels(t *testing.T, db *gorm.DB) (devID uint, chOnlineID, chOfflineID uint) {
	t.Helper()
	d := &gbmodels.GbDevice{DeviceID: "34020000002000000001", Name: "测试 NVR", Manufacturer: "Hikvision", Transport: "UDP", Status: gbmodels.DeviceStatusOnline, SubscribeCapability: gbmodels.SubscribeUnknown}
	require.NoError(t, db.Create(d).Error)
	ch1 := &gbmodels.GbChannel{DeviceID: "34020000002000000001", ChannelID: "37011200001310000001", Name: "通道 在线", Status: gbmodels.ChannelStatusOnline, Latitude: 36.685, Longitude: 117.05, PTZType: 1}
	ch2 := &gbmodels.GbChannel{DeviceID: "34020000002000000001", ChannelID: "37011200001310000002", Name: "通道 离线", Status: gbmodels.ChannelStatusOffline}
	require.NoError(t, db.Create(ch1).Error)
	require.NoError(t, db.Create(ch2).Error)
	return d.ID, ch1.ID, ch2.ID
}

func TestMapMarkers_UsesViewportAndChannelFilters(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, _, _ = seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/map/markers?status=online&q=%E5%9C%A8%E7%BA%BF&minLat=36&maxLat=37&minLng=117&maxLng=118", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	data := unmarshal(t, w)["data"].(map[string]any)
	list := data["list"].([]any)
	assert.Len(t, list, 1)
	assert.Equal(t, "37011200001310000001", list[0].(map[string]any)["channelId"])
}

func TestMapMarkers_PrefersLatestPositionAndReportsStaleness(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	deviceID, channelID, _ := seedDevicesAndChannels(t, db)
	old := time.Now().Add(-91 * time.Second)
	require.NoError(t, db.Create(&gbmodels.GbMobilePositionLatest{DeviceID: deviceID, SourceCode: "37011200001310000001", ChannelID: &channelID, EventTime: old, ReceivedAt: old, Latitude: 35.1, Longitude: 118.2}).Error)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/map/markers", nil)
	r.ServeHTTP(w, req)
	data := unmarshal(t, w)["data"].(map[string]any)
	marker := data["list"].([]any)[0].(map[string]any)
	assert.Equal(t, 35.1, marker["latitude"])
	assert.Equal(t, 118.2, marker["longitude"])
	assert.Equal(t, true, marker["positionStale"])
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
	assert.EqualValues(t, 1, data["onlineTotal"])
	assert.EqualValues(t, 0, data["offlineTotal"])
}

func TestDeviceMgmt_ListDevices_ThousandDevicesUseFixedQueryCount(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	devices := make([]gbmodels.GbDevice, 1000)
	channels := make([]gbmodels.GbChannel, 1000)
	for i := range devices {
		deviceID := fmt.Sprintf("batch-device-%02d", i)
		devices[i] = gbmodels.GbDevice{DeviceID: deviceID, Status: gbmodels.DeviceStatusOnline, SubscribeCapability: gbmodels.SubscribeUnknown}
		channels[i] = gbmodels.GbChannel{DeviceID: deviceID, ChannelID: fmt.Sprintf("batch-channel-%02d", i), Status: gbmodels.ChannelStatusOnline}
	}
	require.NoError(t, db.CreateInBatches(&devices, 100).Error)
	require.NoError(t, db.CreateInBatches(&channels, 100).Error)

	queryCount := 0
	countQuery := func(*gorm.DB) {
		queryCount++
	}
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("count_device_list_queries", countQuery))
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("count_device_list_rows", countQuery))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/devices?page=1&pageSize=50", nil))

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, unmarshal(t, w)["data"].(map[string]any)["list"].([]any), 50)
	assert.Equal(t, 4, queryCount, "设备列表查询数不应随设备数线性增长")
}

func TestDeviceMgmt_ListDevices_DefaultSort(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	now := time.Now()
	latestOnline := now.Add(-time.Minute)
	olderOnline := now.Add(-time.Hour)
	devices := []gbmodels.GbDevice{
		{DeviceID: "online-bravo", Name: "Bravo", Status: gbmodels.DeviceStatusOnline, RegisterTime: &latestOnline, SubscribeCapability: gbmodels.SubscribeUnknown},
		{DeviceID: "online-alpha-old-id", Name: "Alpha", Status: gbmodels.DeviceStatusOnline, RegisterTime: &latestOnline, SubscribeCapability: gbmodels.SubscribeUnknown},
		{DeviceID: "online-alpha-new-id", Name: "Alpha", Status: gbmodels.DeviceStatusOnline, RegisterTime: &latestOnline, SubscribeCapability: gbmodels.SubscribeUnknown},
		{DeviceID: "online-old", Name: "Zulu", Status: gbmodels.DeviceStatusOnline, RegisterTime: &olderOnline, SubscribeCapability: gbmodels.SubscribeUnknown},
		{DeviceID: "offline-newest", Name: "Zulu", Status: gbmodels.DeviceStatusOffline, RegisterTime: &now, SubscribeCapability: gbmodels.SubscribeUnknown},
	}
	require.NoError(t, db.Create(&devices).Error)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/devices", nil))

	require.Equal(t, http.StatusOK, w.Code)
	list := unmarshal(t, w)["data"].(map[string]any)["list"].([]any)
	require.Len(t, list, 5)
	assert.Equal(t, "online-bravo", list[0].(map[string]any)["deviceId"])
	assert.Equal(t, "online-alpha-new-id", list[1].(map[string]any)["deviceId"])
	assert.Equal(t, "online-alpha-old-id", list[2].(map[string]any)["deviceId"])
	assert.Equal(t, "online-old", list[3].(map[string]any)["deviceId"])
	assert.Equal(t, "offline-newest", list[4].(map[string]any)["deviceId"])
}

func TestDeviceMgmt_ListDevices_FiltersByOwnerDept(t *testing.T) {
	const userID = 100
	r, db := newDeviceMgmtRouter(t, withClaims(userID))
	seedDeptScopedUser(t, db, userID, 10)
	require.NoError(t, db.Create(&gbmodels.GbDevice{OwnerDeptID: 10, DeviceID: "34020000002000000010", Name: "本部门 NVR", SubscribeCapability: gbmodels.SubscribeUnknown}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{OwnerDeptID: 20, DeviceID: "34020000002000000020", Name: "外部门 NVR", SubscribeCapability: gbmodels.SubscribeUnknown}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/devices", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	list := data["list"].([]any)
	require.Len(t, list, 1)
	d := list[0].(map[string]any)
	assert.Equal(t, "34020000002000000010", d["deviceId"])
}

func TestDeviceMgmt_ListDevices_FilterByCatalogNode(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, chOnID, _ := seedDevicesAndChannels(t, db)
	other := &gbmodels.GbDevice{DeviceID: "34020000002000000002", Name: "其他 NVR", Status: gbmodels.DeviceStatusOnline, SubscribeCapability: gbmodels.SubscribeUnknown}
	require.NoError(t, db.Create(other).Error)

	root := &gbmodels.GbCatalogNode{NodeType: gbmodels.NodeTypeCivilCode, Path: "/1/", Name: "历城区", Code: "370112"}
	require.NoError(t, db.Create(root).Error)
	chNode := &gbmodels.GbCatalogNode{
		NodeType:  gbmodels.NodeTypeChannel,
		ParentID:  &root.ID,
		Path:      "/1/2/",
		Name:      "通道 在线",
		Code:      "37011200001310000001",
		ChannelID: &chOnID,
	}
	require.NoError(t, db.Create(chNode).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/devices?nodeId="+uintStr(root.ID), nil)
	r.ServeHTTP(w, req)

	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	list := data["list"].([]any)
	require.Len(t, list, 1)
	d := list[0].(map[string]any)
	assert.Equal(t, "34020000002000000001", d["deviceId"])
}

func TestDeviceMgmt_CustomDirectoryFiltersDevicesChannelsAndMap(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	deviceID, _, _ := seedDevicesAndChannels(t, db)
	other := &gbmodels.GbDevice{DeviceID: "34020000002000000002", Name: "other", SubscribeCapability: gbmodels.SubscribeUnknown}
	require.NoError(t, db.Create(other).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: other.DeviceID, ChannelID: "C-other", Latitude: 35, Longitude: 116}).Error)
	root := &gbmodels.GbCustomGroup{OwnerDeptID: 0, ParentID: 0, Path: "/", Name: "A"}
	require.NoError(t, db.Create(root).Error)
	root.Path = "/" + uintStr(root.ID) + "/"
	require.NoError(t, db.Model(root).Update("path", root.Path).Error)
	child := &gbmodels.GbCustomGroup{OwnerDeptID: 0, ParentID: root.ID, Path: root.Path, Depth: 1, Name: "B"}
	require.NoError(t, db.Create(child).Error)
	child.Path += uintStr(child.ID) + "/"
	require.NoError(t, db.Model(child).Update("path", child.Path).Error)
	require.NoError(t, db.Create(&gbmodels.GbCustomGroupDevice{GroupID: child.ID, DeviceID: deviceID}).Error)
	query := "?directoryView=custom&directoryKey=custom:group:" + uintStr(root.ID)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/devices"+query, nil))
	require.EqualValues(t, 1, unmarshal(t, w)["data"].(map[string]any)["total"])
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/channels"+query, nil))
	require.EqualValues(t, 2, unmarshal(t, w)["data"].(map[string]any)["total"])
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/markers"+query, nil))
	require.EqualValues(t, 1, unmarshal(t, w)["data"].(map[string]any)["total"])
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/no-coord-count"+query, nil))
	require.EqualValues(t, 1, unmarshal(t, w)["data"].(map[string]any)["count"])
}

func TestDeviceMgmt_CustomUngroupedFilterIsDeptScoped(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{
		{DeviceID: "D10", OwnerDeptID: 10, SubscribeCapability: gbmodels.SubscribeUnknown},
		{DeviceID: "D20", OwnerDeptID: 20, SubscribeCapability: gbmodels.SubscribeUnknown},
	}).Error)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/devices?directoryView=custom&directoryKey=custom:ungrouped:10", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data := unmarshal(t, w)["data"].(map[string]any)
	require.EqualValues(t, 1, data["total"])
	require.Equal(t, "D10", data["list"].([]any)[0].(map[string]any)["deviceId"])
}

func TestDeviceMgmt_RejectsMixedDirectoryParameters(t *testing.T) {
	r, _ := newDeviceMgmtRouter(t)
	for _, path := range []string{"devices", "channels", "map/markers", "map/clusters", "map/no-coord-count"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/"+path+"?nodeId=1&directoryView=custom&directoryKey=custom:ungrouped", nil))
		require.Equal(t, http.StatusBadRequest, w.Code, path+": "+w.Body.String())
	}
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

func TestDeviceMgmt_ListChannels_FilterByDeviceID(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	seedDevicesAndChannels(t, db)
	require.NoError(t, db.Create(&gbmodels.GbChannel{
		DeviceID:  "34020000002000000002",
		ChannelID: "37011200001310000003",
		Name:      "其他设备通道",
	}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/channels?deviceId=34020000002000000001", nil)
	r.ServeHTTP(w, req)

	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	assert.EqualValues(t, 2, data["total"])
	for _, item := range data["list"].([]any) {
		channel := item.(map[string]any)
		assert.Equal(t, "34020000002000000001", channel["deviceId"])
	}
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
	assert.Equal(t, "UDP", data["transport"])
}

func TestDeviceMgmt_UpdateChannelAlias(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, chOnID, _ := seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PATCH", "/api/gb28181/device-mgmt/channel/"+uintStr(chOnID), bytes.NewBufferString(`{"alias":"园区西门"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var channel gbmodels.GbChannel
	require.NoError(t, db.First(&channel, chOnID).Error)
	assert.Equal(t, "园区西门", channel.Alias)
	assert.Equal(t, "通道 在线", channel.Name)
}

func TestDeviceMgmt_UpdateChannelAudio(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, chOnID, _ := seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PATCH", "/api/gb28181/device-mgmt/channel/"+uintStr(chOnID), bytes.NewBufferString(`{"audioEnabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var channel gbmodels.GbChannel
	require.NoError(t, db.First(&channel, chOnID).Error)
	assert.True(t, channel.AudioEnabled)
}

func TestDeviceMgmt_ChannelMounts(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, chOnID, _ := seedDevicesAndChannels(t, db)
	// 加一个主挂载
	node := &gbmodels.GbCatalogNode{NodeType: gbmodels.NodeTypeCivilCode, Path: "/1/", Name: "山东"}
	require.NoError(t, db.Create(node).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannelMount{ChannelID: chOnID, ParentNodeID: node.ID, IsPrimary: true, MountSource: gbmodels.MountSourceCatalog}).Error)

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

func TestMap_Markers_FiltersByOwnerDept(t *testing.T) {
	const userID = 100
	r, db := newDeviceMgmtRouter(t, withClaims(userID))
	seedDeptScopedUser(t, db, userID, 10)
	require.NoError(t, db.Create(&gbmodels.GbChannel{OwnerDeptID: 10, DeviceID: "34020000002000000010", ChannelID: "37011200001310000010", Name: "本部门通道", Latitude: 36.1, Longitude: 117.1}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{OwnerDeptID: 20, DeviceID: "34020000002000000020", ChannelID: "37011200001310000020", Name: "外部门通道", Latitude: 36.2, Longitude: 117.2}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/map/markers", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	list := data["list"].([]any)
	require.Len(t, list, 1)
	marker := list[0].(map[string]any)
	assert.Equal(t, "37011200001310000010", marker["channelId"])
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
	require.NoError(t, db.Create(&gbmodels.GbAnomalyRecord{CatalogNodeID: 1, RawCode: "X", FallbackType: gbmodels.FallbackTypeVirtualOrg, Resolved: false}).Error)
	require.NoError(t, db.Create(&gbmodels.GbAnomalyRecord{CatalogNodeID: 2, RawCode: "Y", FallbackType: gbmodels.FallbackTypeVirtualOrg, Resolved: true}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/anomaly?resolved=0", nil)
	r.ServeHTTP(w, req)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	assert.EqualValues(t, 1, data["total"], "默认只列未处理")
}

func TestAnomaly_Resolve_ChangeType(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	node := &gbmodels.GbCatalogNode{NodeType: gbmodels.NodeTypeVirtualOrg, Path: "/1/", Name: "异常节点", Anomaly: true}
	require.NoError(t, db.Create(node).Error)
	rec := &gbmodels.GbAnomalyRecord{CatalogNodeID: node.ID, RawCode: "XYZ", FallbackType: gbmodels.FallbackTypeVirtualOrg, Resolved: false}
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

func TestAnomaly_Resolve_RejectsOtherOwnerDept(t *testing.T) {
	const userID = 100
	r, db := newDeviceMgmtRouter(t, withClaims(userID))
	seedDeptScopedUser(t, db, userID, 10)
	node := &gbmodels.GbCatalogNode{OwnerDeptID: 20, NodeType: gbmodels.NodeTypeVirtualOrg, Path: "/1/", Name: "外部门异常节点", Anomaly: true}
	require.NoError(t, db.Create(node).Error)
	rec := &gbmodels.GbAnomalyRecord{OwnerDeptID: 20, CatalogNodeID: node.ID, RawCode: "XYZ", FallbackType: gbmodels.FallbackTypeVirtualOrg, Resolved: false}
	require.NoError(t, db.Create(rec).Error)

	body, _ := json.Marshal(map[string]any{
		"action":     "change-type",
		"targetType": "biz_group",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/gb28181/device-mgmt/anomaly/"+uintStr(rec.ID)+"/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	resp := unmarshal(t, w)
	assert.NotEqual(t, "success", resp["status"])
	var ur gbmodels.GbAnomalyRecord
	require.NoError(t, db.First(&ur, rec.ID).Error)
	assert.False(t, ur.Resolved)
}

func TestAnomaly_BatchResolve(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	ids := []uint{}
	for i := 0; i < 3; i++ {
		rec := &gbmodels.GbAnomalyRecord{CatalogNodeID: uint(i + 1), RawCode: "X", FallbackType: gbmodels.FallbackTypeVirtualOrg, Resolved: false}
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
