package controllers_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func jsonBody(payload string) io.Reader {
	return strings.NewReader(payload)
}

/** urlEscape 用于把带空格的时间串塞进 query（httptest.NewRequest 不会自动转义）。 */
func urlEscape(value string) string {
	return url.QueryEscape(value)
}

// TestMapMarkers_CarriesDeviceCode 地图卡片要用 (设备编码, 通道编码) 去点播，
// marker 必须带 deviceId；聚合里落单的通道同样要带，否则低层级下点卡片播不了。
func TestMapMarkers_CarriesDeviceCode(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, channelID, _ := seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/markers", nil))
	require.Equal(t, http.StatusOK, w.Code)
	marker := unmarshal(t, w)["data"].(map[string]any)["list"].([]any)[0].(map[string]any)
	assert.Equal(t, "34020000002000000001", marker["deviceId"])

	// 聚合路径：同一批数据在低缩放级别走 clusters，落单点也必须能播。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/clusters?zoom=10", nil))
	require.Equal(t, http.StatusOK, w.Code)
	clusters := unmarshal(t, w)["data"].(map[string]any)["clusters"].([]any)
	require.Len(t, clusters, 1)
	single, ok := clusters[0].(map[string]any)["single"].(map[string]any)
	require.True(t, ok, "只有一路通道时应回传落单身份")
	assert.Equal(t, "34020000002000000001", single["deviceId"])
	assert.EqualValues(t, channelID, single["id"])
}

func TestMapTrajectory_ReturnsPointsAscendingByTime(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	deviceID, channelID, _ := seedDevicesAndChannels(t, db)
	now := time.Now()
	// 故意乱序写入：升序必须由接口保证，不能依赖插入顺序。
	for _, offset := range []time.Duration{-10 * time.Minute, -30 * time.Minute, -20 * time.Minute} {
		require.NoError(t, db.Create(&gbmodels.GbMobilePositionHistory{
			DeviceID: deviceID, SourceCode: "37011200001310000001", ChannelID: &channelID,
			EventTime: now.Add(offset), ReceivedAt: now, Latitude: 36.6 + offset.Minutes()/10000, Longitude: 117.0,
		}).Error)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/gb28181/device-mgmt/map/trajectory?channelId=%d", channelID), nil))
	require.Equal(t, http.StatusOK, w.Code)
	data := unmarshal(t, w)["data"].(map[string]any)

	list := data["list"].([]any)
	require.Len(t, list, 3)
	assert.EqualValues(t, 3, data["total"])
	assert.Equal(t, false, data["truncated"])
	assert.Equal(t, "37011200001310000001", data["channelId"])
	assert.Equal(t, "34020000002000000001", data["deviceId"])

	first := list[0].(map[string]any)["eventTime"].(string)
	last := list[2].(map[string]any)["eventTime"].(string)
	assert.Less(t, first, last, "轨迹点必须按时间升序返回")
}

// 超过 maxPoints 时必须抽稀并如实上报 truncated，而不是静默截断前半段。
func TestMapTrajectory_ThinsAndFlagsTruncation(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	deviceID, channelID, _ := seedDevicesAndChannels(t, db)
	now := time.Now()
	total := 300
	for i := 0; i < total; i++ {
		require.NoError(t, db.Create(&gbmodels.GbMobilePositionHistory{
			DeviceID: deviceID, SourceCode: "37011200001310000001", ChannelID: &channelID,
			EventTime:  now.Add(-time.Duration(total-i) * time.Second),
			ReceivedAt: now, Latitude: 36.6, Longitude: 117.0 + float64(i)/10000,
		}).Error)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/gb28181/device-mgmt/map/trajectory?channelId=%d&maxPoints=100", channelID), nil))
	require.Equal(t, http.StatusOK, w.Code)
	data := unmarshal(t, w)["data"].(map[string]any)

	list := data["list"].([]any)
	assert.LessOrEqual(t, len(list), 100)
	assert.Equal(t, true, data["truncated"])
	// 首尾必须保留：截断成前 100 个会让路线只画到三分之一就断掉。
	assert.EqualValues(t, 117.0, list[0].(map[string]any)["longitude"])
	assert.InDelta(t, 117.0+299.0/10000, list[len(list)-1].(map[string]any)["longitude"], 1e-6)
}

func TestMapTrajectory_RejectsWindowOver24Hours(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, channelID, _ := seedDevicesAndChannels(t, db)
	now := time.Now()
	start := now.Add(-25 * time.Hour).Format("2006-01-02 15:04:05")
	end := now.Format("2006-01-02 15:04:05")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/gb28181/device-mgmt/map/trajectory?channelId=%d&startTime=%s&endTime=%s",
			channelID, urlEscape(start), urlEscape(end)), nil))

	body := unmarshal(t, w)
	assert.NotEqualValues(t, 0, body["code"], "超过 24 小时的查询必须被拒")
	assert.Contains(t, fmt.Sprint(body["message"]), "24")
}

func TestMapTrajectory_RejectsReversedRange(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, channelID, _ := seedDevicesAndChannels(t, db)
	now := time.Now()
	start := now.Format("2006-01-02 15:04:05")
	end := now.Add(-time.Hour).Format("2006-01-02 15:04:05")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/gb28181/device-mgmt/map/trajectory?channelId=%d&startTime=%s&endTime=%s",
			channelID, urlEscape(start), urlEscape(end)), nil))
	assert.NotEqualValues(t, 0, unmarshal(t, w)["code"])
}

func TestMapTrajectory_UnknownChannelIsRejected(t *testing.T) {
	r, _ := newDeviceMgmtRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/trajectory?channelId=999999", nil))
	assert.NotEqualValues(t, 0, unmarshal(t, w)["code"])
}

// 空轨迹是正常业务结果（固定枪机本来就不上报），接口必须照常 200 并把
// historyEnabled 一并回传 —— 前端靠它区分"没开记录"与"设备不上报"。
func TestMapTrajectory_EmptyResultStillReportsHistorySwitch(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, channelID, _ := seedDevicesAndChannels(t, db)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/gb28181/device-mgmt/map/trajectory?channelId=%d", channelID), nil))
	require.Equal(t, http.StatusOK, w.Code)
	data := unmarshal(t, w)["data"].(map[string]any)
	assert.Empty(t, data["list"])
	assert.EqualValues(t, 0, data["total"])
	assert.Contains(t, data, "historyEnabled")
}

func TestMapCenter_NullUntilSavedThenRoundTrips(t *testing.T) {
	r, _ := newDeviceMgmtRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/center", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, unmarshal(t, w)["data"].(map[string]any)["center"], "从未设置时必须回 null")

	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/gb28181/device-mgmt/map/center",
		jsonBody(`{"longitude":116.995051,"latitude":36.662371,"zoom":12.5}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/center", nil))
	center := unmarshal(t, w)["data"].(map[string]any)["center"].(map[string]any)
	assert.InDelta(t, 116.995051, center["longitude"], 1e-9)
	assert.InDelta(t, 36.662371, center["latitude"], 1e-9)
	assert.InDelta(t, 12.5, center["zoom"], 1e-9, "zoom 是小数，不能被取整")
}

// 重复保存必须覆盖而不是插出第二行 —— 否则"默认中心点"会变成"最后一条随机生效"。
func TestMapCenter_SaveIsUpsertNotAppend(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	for _, payload := range []string{
		`{"longitude":116.4,"latitude":39.9,"zoom":10}`,
		`{"longitude":120.5,"latitude":30.5,"zoom":14}`,
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/gb28181/device-mgmt/map/center", jsonBody(payload))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	}
	var count int64
	require.NoError(t, db.Table("sys_param").Where("code = ?", "gb28181.map.center").Count(&count).Error)
	assert.EqualValues(t, 1, count)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/center", nil))
	center := unmarshal(t, w)["data"].(map[string]any)["center"].(map[string]any)
	assert.InDelta(t, 120.5, center["longitude"], 1e-9)
}

func TestMapCenter_RejectsInvalidPayload(t *testing.T) {
	r, _ := newDeviceMgmtRouter(t)
	cases := []string{
		`{"longitude":0,"latitude":0,"zoom":10}`,    // 0/0 = 无坐标，不是真实中心
		`{"longitude":181,"latitude":30,"zoom":10}`, // 经度越界
		`{"longitude":116,"latitude":91,"zoom":10}`, // 纬度越界
		`{"longitude":116,"latitude":30,"zoom":1}`,  // 低于地图最小缩放
		`{"longitude":116,"latitude":30,"zoom":30}`, // 高于地图最大缩放
	}
	for _, payload := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/gb28181/device-mgmt/map/center", jsonBody(payload))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.NotEqualValues(t, 0, unmarshal(t, w)["code"], "应拒绝: %s", payload)
	}
}

// 库里被手改成脏 JSON 时，读接口必须回 null（前端回退默认视野），不能 500。
func TestMapCenter_DirtyValueFallsBackToNull(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	require.NoError(t, db.Exec(
		"INSERT INTO sys_param (name, code, value, status) VALUES ('地图默认中心点','gb28181.map.center','{not-json',1)",
	).Error)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/map/center", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, unmarshal(t, w)["data"].(map[string]any)["center"])
}
