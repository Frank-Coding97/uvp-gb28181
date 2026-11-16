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
