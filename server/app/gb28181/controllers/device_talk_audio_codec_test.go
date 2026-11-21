package controllers_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 对讲音频编码由用户按设备维护：接口要能改、能归一、能拒非法值，
// 且「只改别名」时不能把已选编码冲回默认值（设备重新注册同理）。
func TestDeviceController_UpdateTalkAudioCodec(t *testing.T) {
	r, db := newScopedDeviceRouter(t, 100)
	const deviceID = "34020000002000000010"

	patch := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPatch, "/api/gb28181/device/"+deviceID, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	loadCodec := func() string {
		var rows []gbmodels.GbDevice
		require.NoError(t, db.Where("device_id = ?", deviceID).Limit(1).Find(&rows).Error)
		require.Len(t, rows, 1)
		return rows[0].TalkAudioCodec
	}

	// 1. 改成 G.711U 并落库
	require.Equal(t, http.StatusOK, patch(`{"talkAudioCodec":"pcmu"}`).Code)
	require.Equal(t, gbmodels.TalkAudioCodecPCMU, loadCodec())

	// 2. 大小写/别名写法归一
	require.Equal(t, http.StatusOK, patch(`{"talkAudioCodec":" G711U "}`).Code)
	require.Equal(t, gbmodels.TalkAudioCodecPCMU, loadCodec())

	// 3. 只改别名：编码保持不变
	require.Equal(t, http.StatusOK, patch(`{"alias":"门口机"}`).Code)
	require.Equal(t, gbmodels.TalkAudioCodecPCMU, loadCodec())

	// 4. 详情接口要把编码带出去（前端编辑弹窗靠它回填）
	detail := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/gb28181/device/"+deviceID, nil)
	r.ServeHTTP(detail, req)
	require.Equal(t, http.StatusOK, detail.Code)
	require.Contains(t, detail.Body.String(), `"talkAudioCodec":"pcmu"`)

	// 5. 切回 G.711A（空串也按默认处理）
	require.Equal(t, http.StatusOK, patch(`{"talkAudioCodec":"g711a"}`).Code)
	require.Equal(t, gbmodels.TalkAudioCodecPCMA, loadCodec())
	require.Equal(t, http.StatusOK, patch(`{"talkAudioCodec":""}`).Code)
	require.Equal(t, gbmodels.TalkAudioCodecPCMA, loadCodec())
}

// 非法编码必须拒掉：静默落一个设备不认识的字符串，会让对讲一直没声音。
func TestDeviceController_UpdateTalkAudioCodecRejectsUnknownValue(t *testing.T) {
	r, db := newScopedDeviceRouter(t, 100)
	const deviceID = "34020000002000000010"

	seed := httptest.NewRecorder()
	seedReq, _ := http.NewRequest(http.MethodPatch, "/api/gb28181/device/"+deviceID, bytes.NewBufferString(`{"talkAudioCodec":"pcmu"}`))
	seedReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(seed, seedReq)
	require.Equal(t, http.StatusOK, seed.Code)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPatch, "/api/gb28181/device/"+deviceID, bytes.NewBufferString(`{"talkAudioCodec":"aac"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	resp := unmarshal(t, w)
	require.EqualValues(t, 1, resp["code"])
	require.Contains(t, resp["message"], "pcma")

	var rows []gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", deviceID).Limit(1).Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, gbmodels.TalkAudioCodecPCMU, rows[0].TalkAudioCodec, "非法值不得落库，也不能冲掉原值")
}
