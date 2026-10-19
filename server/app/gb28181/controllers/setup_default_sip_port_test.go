package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	gbsetup "uvplatform.com/uvp-gb28181/app/gb28181/setup"
)

// 引导页默认 SIP 端口来自部署层：install.sh 按段基址算出后写进 .env，
// compose 注入成环境变量，后端原样下发给前端。
//
// ⛔ 这条用例锁的是「部署层说了算」这个方向：别把默认值改回**只在构建期**注入，
// 那样 `--https-port 53000` 平移整段后，引导页仍会预填段外的 51064。
func TestSetupController_StatusCarriesDeploymentDefaultSIPPort(t *testing.T) {
	t.Setenv(gbsetup.DefaultSIPPortEnv, "53064")
	db := newSetupControllerDB(t)
	router := newSetupControllerRouter(NewSetupController(db, gbsetup.NewRuntimeStatus(), nil, nil))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.EqualValues(t, 53064, decodeSetupResponse(t, recorder)["data"].(map[string]any)["defaultSipPort"])
}

// 部署层没告知 / 给了非法值时必须**字段缺席**，而不是带一个 0 或某个编出来的默认端口。
// 原因：0 传到前端会被当成"后端建议端口 0"，比让前端走自己的回退链更糟。
func TestSetupController_StatusOmitsDefaultSIPPortWhenUnknown(t *testing.T) {
	for _, raw := range []string{"", "not-a-number", "0", "70000"} {
		t.Run("env="+raw, func(t *testing.T) {
			t.Setenv(gbsetup.DefaultSIPPortEnv, raw)
			db := newSetupControllerDB(t)
			router := newSetupControllerRouter(NewSetupController(db, gbsetup.NewRuntimeStatus(), nil, nil))

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			data := decodeSetupResponse(t, recorder)["data"].(map[string]any)
			_, present := data["defaultSipPort"]
			require.False(t, present, "非法或缺失时应省略 defaultSipPort，实际:%v", data["defaultSipPort"])
		})
	}
}

// 已有配置时前端应该显示库里的端口；但接口本身仍把部署默认值带上（前端自行忽略），
// 这样升级到「已配置但端口在段外」的存量现场，前端也能拿到正确段值做提示。
func TestSetupController_StatusKeepsDefaultSIPPortWhenConfigured(t *testing.T) {
	t.Setenv(gbsetup.DefaultSIPPortEnv, "53064")
	db := newSetupControllerDB(t)
	password := "Sec12345Aa!!"
	require.NoError(t, db.Create(&gbsetup.SIPConfig{
		ID: gbsetup.SingletonID, DeploymentMode: gbsetup.DeploymentLAN,
		ListenIP: "0.0.0.0", AdvertiseIP: "192.168.10.220", Port: 51064,
		Domain: "3402000000", ServerID: "34020000002000000001", Password: password,
	}).Error)
	router := newSetupControllerRouter(NewSetupController(db, gbsetup.NewRuntimeStatus(), nil, nil))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	data := decodeSetupResponse(t, recorder)["data"].(map[string]any)
	require.EqualValues(t, 53064, data["defaultSipPort"])
	require.EqualValues(t, 51064, data["config"].(map[string]any)["port"])
}
