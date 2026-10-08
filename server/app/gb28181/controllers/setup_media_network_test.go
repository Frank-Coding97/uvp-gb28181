package controllers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	gbsetup "uvplatform.com/uvp-gb28181/app/gb28181/setup"
)

func TestSetupController_MediaNetworkDefaultsReachReloadAndStatus(t *testing.T) {
	db := newSetupControllerDB(t)
	calls := 0
	reload := func() error {
		calls++
		row, err := gbsetup.NewSIPConfigRepository(db).Get(t.Context())
		require.NoError(t, err)
		require.Equal(t, "192.168.1.20", row.HookIP)
		require.Equal(t, "stream.example.com", row.StreamIP)
		return nil
	}
	router := newSetupControllerRouter(NewSetupController(db, gbsetup.NewRuntimeStatus(), nil, reload))
	body := `{"deploymentMode":"lan","listenIp":"0.0.0.0","advertiseIp":"","port":5061,"domain":"3402000000","serverId":"34020000002000000001","password":"K9#nT2xQ","hookIp":"192.168.1.20","streamIp":"stream.example.com"}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(body)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, calls)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	config := decodeSetupResponse(t, recorder)["data"].(map[string]any)["config"].(map[string]any)
	require.Equal(t, "192.168.1.20", config["hookIp"])
	require.Equal(t, "stream.example.com", config["streamIp"])
}
