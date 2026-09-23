package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

func TestOpenAPIPlayAuthControllerRejectsUnlock(t *testing.T) {
	// The production policy deliberately has no unlock API. Exercise its HTTP
	// boundary in a child test process instead of exposing a reset for tests.
	if os.Getenv("UVP_TEST_MUST_AUTH_CONTROLLER") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOpenAPIPlayAuthControllerRejectsUnlock$", "-test.count=1")
		command.Env = append(os.Environ(), "UVP_TEST_MUST_AUTH_CONTROLLER=1")
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}
	config := &serviceConfigTestYAML{values: map[string]interface{}{
		gbconfig.PlayAuthEnabledConfigKey: false,
		"openapi.enabled":                 false, "openapi.play_enabled": false,
	}}
	app.ConfigYml = config
	gbconfig.RequirePlayAuth()
	controller := NewServiceConfigController()
	updates := 0
	controller.SetPlayAuthTTLUpdater(func(time.Duration) error { updates++; return nil })
	router := newServiceConfigRouter(controller)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/play-auth", jsonBody(t, map[string]interface{}{
		"authEnabled": false, "authBindClientIP": false, "authTTLSeconds": 120,
	})))
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "停用接入不会解除该保护")
	require.Zero(t, config.saveNum)
	require.Zero(t, updates)
	read := httptest.NewRecorder()
	router.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/play-auth", nil))
	require.Equal(t, http.StatusOK, read.Code)
	require.Equal(t, true, serviceConfigData(t, read)["authEnabled"])
	require.Equal(t, true, serviceConfigData(t, read)["authRequiredByOpenAPI"])
	require.Equal(t, true, serviceConfigData(t, read)["authConfigConflict"])
	require.False(t, config.GetBool(gbconfig.PlayAuthEnabledConfigKey), "GET reports effective policy without rewriting YAML")
	SetPlayAuthRuntimeReady(true)
	saved := httptest.NewRecorder()
	router.ServeHTTP(saved, httptest.NewRequest(http.MethodPut, "/play-auth", jsonBody(t, map[string]interface{}{
		"authEnabled": true, "authBindClientIP": false, "authTTLSeconds": 120,
		"authRequiredByOpenAPI": false, "authConfigConflict": true,
	})))
	require.Equal(t, http.StatusOK, saved.Code)
	require.Equal(t, true, serviceConfigData(t, saved)["authRequiredByOpenAPI"], "request cannot overwrite server-owned protection metadata")
	require.Equal(t, false, serviceConfigData(t, saved)["authConfigConflict"])
}
