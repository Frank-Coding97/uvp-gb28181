package controllers_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 设备类型由用户手动维护（默认 IPC），接口侧要能改、能拒非法值、
// 且「只改别名」时不能把已维护的类型冲掉。
func TestDeviceController_UpdateDeviceType(t *testing.T) {
	r, db := newScopedDeviceRouter(t, 100)
	const deviceID = "34020000002000000010"

	patch := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPatch, "/api/gb28181/device/"+deviceID, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	loadType := func() string {
		var rows []gbmodels.GbDevice
		require.NoError(t, db.Where("device_id = ?", deviceID).Limit(1).Find(&rows).Error)
		require.Len(t, rows, 1)
		return rows[0].DeviceType
	}

	// 自动建档的设备默认是 IPC（hook 兜底，不依赖 DB 默认值）。
	require.Equal(t, gbmodels.DeviceTypeIPC, loadType())

	// 1. 改成 NVR 并落库
	require.Equal(t, http.StatusOK, patch(`{"deviceType":"nvr"}`).Code)
	require.Equal(t, gbmodels.DeviceTypeNVR, loadType())

	// 2. 大小写/空白归一
	require.Equal(t, http.StatusOK, patch(`{"deviceType":" Platform "}`).Code)
	require.Equal(t, gbmodels.DeviceTypePlatform, loadType())

	// 3. 只改别名：类型必须保持不变
	require.Equal(t, http.StatusOK, patch(`{"alias":"一号机"}`).Code)
	require.Equal(t, gbmodels.DeviceTypePlatform, loadType())

	// 4. 详情接口要把类型带出去（前端卡片靠它选图标）
	detail := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/gb28181/device/"+deviceID, nil)
	r.ServeHTTP(detail, req)
	require.Equal(t, http.StatusOK, detail.Code)
	require.Contains(t, detail.Body.String(), `"deviceType":"platform"`)

	// 5. 空串按 IPC 处理（清空选择时不该留下脏值）
	require.Equal(t, http.StatusOK, patch(`{"deviceType":""}`).Code)
	require.Equal(t, gbmodels.DeviceTypeIPC, loadType())
}

// 非法类型必须拒掉，而不是静默落一个永远匹配不上图标的字符串。
func TestDeviceController_UpdateDeviceTypeRejectsUnknownValue(t *testing.T) {
	r, db := newScopedDeviceRouter(t, 100)
	const deviceID = "34020000002000000010"

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPatch, "/api/gb28181/device/"+deviceID, bytes.NewBufferString(`{"deviceType":"camera"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	resp := unmarshal(t, w)
	require.EqualValues(t, 1, resp["code"])
	require.Contains(t, resp["message"], "ipc")

	var rows []gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", deviceID).Limit(1).Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, gbmodels.DeviceTypeIPC, rows[0].DeviceType, "非法值不得落库")
}

// 列表按设备类型筛选。
// ⛔ 重点锁「不传类型 ≠ 只看 IPC」：NormalizeDeviceType("") 会合法归一成 ipc，
// 若不做空串短路，默认列表会静默少掉全部 NVR 和下级平台。
func TestDeviceMgmt_ListDevices_FilterByDeviceType(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	devices := []gbmodels.GbDevice{
		{DeviceID: "type-ipc", Name: "摄像机", DeviceType: gbmodels.DeviceTypeIPC, Status: gbmodels.DeviceStatusOnline, SubscribeCapability: gbmodels.SubscribeUnknown},
		{DeviceID: "type-nvr-a", Name: "录像机 A", DeviceType: gbmodels.DeviceTypeNVR, Status: gbmodels.DeviceStatusOnline, SubscribeCapability: gbmodels.SubscribeUnknown},
		{DeviceID: "type-nvr-b", Name: "录像机 B", DeviceType: gbmodels.DeviceTypeNVR, Status: gbmodels.DeviceStatusOffline, SubscribeCapability: gbmodels.SubscribeUnknown},
		{DeviceID: "type-platform", Name: "下级平台", DeviceType: gbmodels.DeviceTypePlatform, Status: gbmodels.DeviceStatusOnline, SubscribeCapability: gbmodels.SubscribeUnknown},
	}
	require.NoError(t, db.Create(&devices).Error)

	listIDs := func(query string) []string {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/devices"+query, nil))
		require.Equal(t, http.StatusOK, w.Code, query)
		rows := unmarshal(t, w)["data"].(map[string]any)["list"].([]any)
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.(map[string]any)["deviceId"].(string))
		}
		return ids
	}

	// 1. 不传 = 不过滤，四种设备全在
	require.ElementsMatch(t, []string{"type-ipc", "type-nvr-a", "type-nvr-b", "type-platform"}, listIDs(""))

	// 2. 精确筛
	require.ElementsMatch(t, []string{"type-nvr-a", "type-nvr-b"}, listIDs("?deviceType=nvr"))
	require.ElementsMatch(t, []string{"type-platform"}, listIDs("?deviceType=platform"))

	// 3. 大小写/空白归一（前端下拉传小写，但手拼 URL/脚本调用不该被卡）
	require.ElementsMatch(t, []string{"type-ipc"}, listIDs("?deviceType=%20IPC%20"))

	// 4. 与状态筛选叠加：筛 NVR 且在线的只剩 A，环形图统计也按这个口径
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/devices?deviceType=nvr&status=online", nil))
	require.Equal(t, http.StatusOK, w.Code)
	data := unmarshal(t, w)["data"].(map[string]any)
	require.Len(t, data["list"].([]any), 1)
	require.EqualValues(t, 1, data["onlineTotal"])
	require.EqualValues(t, 0, data["offlineTotal"])

	// 5. 非法值明确报错，不静默返回"什么都筛不到"
	bad := httptest.NewRecorder()
	r.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/devices?deviceType=camera", nil))
	require.EqualValues(t, 1, unmarshal(t, bad)["code"])
}
