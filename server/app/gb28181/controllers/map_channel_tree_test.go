package controllers_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 地图视角左栏的「可定位通道树」：只列有坐标的通道，按设备分组。
//
// ⛔ 头号契约：**没有坐标的通道不进树**（它在图上没有点位，点了也无处可飞），
// 但要通过 noCoordCount 让前端把它显式提示出来 —— 静默隐藏等于"设备丢了"。
func TestMapChannelTree_ListsOnlyLocatableChannels(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	seedDevicesAndChannels(t, db) // ch1 有坐标(在线)、ch2 无坐标(离线)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/channel-tree", nil))
	require.Equal(t, http.StatusOK, w.Code)
	data := unmarshal(t, w)["data"].(map[string]any)

	assert.EqualValues(t, 1, data["total"])
	assert.Equal(t, false, data["truncated"])
	assert.EqualValues(t, 1, data["noCoordCount"], "无坐标的通道要单独报数，供左栏提示")

	devices := data["devices"].([]any)
	require.Len(t, devices, 1)
	device := devices[0].(map[string]any)
	assert.Equal(t, "34020000002000000001", device["deviceId"])
	assert.Equal(t, true, device["online"])

	channels := device["channels"].([]any)
	require.Len(t, channels, 1)
	channel := channels[0].(map[string]any)
	assert.Equal(t, "37011200001310000001", channel["channelId"])
	assert.Equal(t, 36.685, channel["latitude"], "坐标要带给前端，否则点了飞不过去")
	assert.Equal(t, 117.05, channel["longitude"])
}

// 分组按**展示名**排序，不是按 device_id 的字符串序 —— 后者对用户等于没排序。
// 展示名口径与列表页一致：alias 优先，其次 name，最后才是国标编码。
func TestMapChannelTree_SortsGroupsByDisplayName(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	seedDevicesAndChannels(t, db) // name=测试 NVR

	other := &gbmodels.GbDevice{DeviceID: "34020000002000000002", Name: "原始名", Alias: "A 组执法仪",
		Status: gbmodels.DeviceStatusOffline, SubscribeCapability: gbmodels.SubscribeUnknown}
	require.NoError(t, db.Create(other).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: other.DeviceID, ChannelID: "37011200001310000009",
		Name: "执法仪通道", Status: gbmodels.ChannelStatusOffline, Latitude: 36.7, Longitude: 117.1}).Error)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/channel-tree", nil))
	require.Equal(t, http.StatusOK, w.Code)
	devices := unmarshal(t, w)["data"].(map[string]any)["devices"].([]any)

	require.Len(t, devices, 2)
	assert.Equal(t, "A 组执法仪", devices[0].(map[string]any)["alias"], "alias 要原样回传，展示名由前端统一兜底")
	// "A 组执法仪" < "测试 NVR"：按展示名排，alias 生效
	assert.Equal(t, "34020000002000000002", devices[0].(map[string]any)["deviceId"])
	assert.Equal(t, "34020000002000000001", devices[1].(map[string]any)["deviceId"])
}

// 状态筛选必须同时作用在**树**和**未定位计数**上：两边若各筛各的，
// 用户会看到"树里 0 个，却提示还有 1 个没定位"这种自相矛盾的左栏。
func TestMapChannelTree_StatusFilterAppliesToTreeAndNoCoordCount(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/channel-tree?status=offline", nil))
	require.Equal(t, http.StatusOK, w.Code)
	data := unmarshal(t, w)["data"].(map[string]any)
	assert.EqualValues(t, 0, data["total"], "离线且无坐标的通道不该出现在可定位树里")
	assert.Empty(t, data["devices"])
	assert.EqualValues(t, 1, data["noCoordCount"])

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/channel-tree?status=online", nil))
	require.Equal(t, http.StatusOK, w.Code)
	data = unmarshal(t, w)["data"].(map[string]any)
	assert.EqualValues(t, 1, data["total"])
	assert.EqualValues(t, 0, data["noCoordCount"])
}

// 超过上限必须回 truncated，且只给前 N 个 —— 静默截断会让"某台设备在地图上永远找不到"。
func TestMapChannelTree_TruncatesAndFlags(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	seedDevicesAndChannels(t, db)
	// 直接用种子里的国标编码，不做 First 存在性判断（本仓禁止用 First + ErrRecordNotFound 判存在）。
	const deviceCode = "34020000002000000001"

	batch := make([]gbmodels.GbChannel, 0, 1001)
	for i := 0; i < 1001; i++ {
		batch = append(batch, gbmodels.GbChannel{
			DeviceID:  deviceCode,
			ChannelID: fmt.Sprintf("3701120000131%05d", i),
			Name:      fmt.Sprintf("批量通道 %04d", i),
			Status:    gbmodels.ChannelStatusOnline,
			Latitude:  36.6,
			Longitude: 117.0,
		})
	}
	require.NoError(t, db.CreateInBatches(batch, 200).Error)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/channel-tree", nil))
	require.Equal(t, http.StatusOK, w.Code)
	data := unmarshal(t, w)["data"].(map[string]any)

	assert.EqualValues(t, 1000, data["total"])
	assert.Equal(t, true, data["truncated"], "超上限必须显式告知，不能静默截断")
}
