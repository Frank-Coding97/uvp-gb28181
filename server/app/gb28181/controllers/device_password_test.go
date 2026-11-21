package controllers_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 设备独立密码三态：设置 / 不传保持 / 空串清除。
// 这条能力是「一设备一密码」在平台侧的入口 —— 只写不读的话（历史实现）等于摆设。
func TestDeviceController_UpdateDevicePassword(t *testing.T) {
	r, db := newScopedDeviceRouter(t, 100)
	const deviceID = "34020000002000000010"

	patch := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPatch, "/api/gb28181/device/"+deviceID, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	loadPassword := func() string {
		var rows []gbmodels.GbDevice
		require.NoError(t, db.Where("device_id = ?", deviceID).Limit(1).Find(&rows).Error)
		require.Len(t, rows, 1)
		return rows[0].Password
	}

	// 1. 设置独立密码
	require.Equal(t, http.StatusOK, patch(`{"password":"dev-pass-1"}`).Code)
	require.Equal(t, "dev-pass-1", loadPassword())

	// 2. 详情接口只回 hasPassword，不回明文
	detail := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/gb28181/device/"+deviceID, nil)
	r.ServeHTTP(detail, req)
	require.Equal(t, http.StatusOK, detail.Code)
	require.Contains(t, detail.Body.String(), `"hasPassword":true`)
	require.NotContains(t, detail.Body.String(), "dev-pass-1")

	// 3. 不传 password → 保持不变（避免每次改别名都把密码冲掉）
	require.Equal(t, http.StatusOK, patch(`{"alias":"新别名"}`).Code)
	require.Equal(t, "dev-pass-1", loadPassword())

	// 4. 传空串 → 清除独立密码，设备回落统一接入密码
	require.Equal(t, http.StatusOK, patch(`{"password":""}`).Code)
	require.Equal(t, "", loadPassword())
}

// 带空白字符的设备密码在设备端根本录不进去，digest 会原样计算 ⇒ 必然注册失败。
// 落库前拒掉，而不是让它变成一个查不出来的"密码错误"。
func TestDeviceController_UpdateDevicePasswordRejectsWhitespace(t *testing.T) {
	r, db := newScopedDeviceRouter(t, 100)
	const deviceID = "34020000002000000010"

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPatch, "/api/gb28181/device/"+deviceID, bytes.NewBufferString(`{"password":"dev pass"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	resp := unmarshal(t, w)
	require.EqualValues(t, 1, resp["code"])
	require.Contains(t, resp["message"], "空格")

	var rows []gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", deviceID).Limit(1).Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "", rows[0].Password)
}
