package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

// Exercise the aggregate JSON boundary so RED proves missing product behavior,
// rather than a missing Go symbol or an uncreated package.
func TestServiceConfigVideoShareDefaults(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = &serviceConfigTestYAML{values: map[string]interface{}{}}
	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/service-config", nil))
	require.Equal(t, 200, recorder.Code)
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.JSONEq(t, `{"defaultEnabled":false,"ttlSeconds":300}`, string(envelope.Data["videoShare"]))
}

func TestServiceConfigVideoShareSave(t *testing.T) {
	for _, ttl := range []int{180, 300, 1800, 3600, 420} {
		t.Run(fmt.Sprint(ttl), func(t *testing.T) {
			previous := app.ConfigYml
			t.Cleanup(func() { app.ConfigYml = previous })
			config := &serviceConfigTestYAML{values: map[string]interface{}{}}
			app.ConfigYml = config
			controller := NewServiceConfigController()
			request := videoShareConfigPayload(t, controller)
			request["videoShare"] = json.RawMessage(fmt.Sprintf(`{"defaultEnabled":true,"ttlSeconds":%d}`, ttl))
			recorder := httptest.NewRecorder()
			newServiceConfigRouter(controller).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, request)))
			require.Equal(t, 200, recorder.Code, recorder.Body.String())
			// A fresh controller reads persisted configuration, not per-controller memory.
			data, err := json.Marshal(NewServiceConfigController().currentServiceConfig())
			require.NoError(t, err)
			var loaded map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(data, &loaded))
			require.JSONEq(t, fmt.Sprintf(`{"defaultEnabled":true,"ttlSeconds":%d}`, ttl), string(loaded["videoShare"]))
			require.Equal(t, 1, config.saveNum)
		})
	}
}

func TestServiceConfigVideoShareInvalidDoesNotSave(t *testing.T) {
	for _, value := range []string{`null`, `{}`, `{"defaultEnabled":true}`, `{"ttlSeconds":300}`, `{"defaultEnabled":null,"ttlSeconds":300}`, `{"defaultEnabled":true,"ttlSeconds":null}`, `{"defaultEnabled":true,"ttlSeconds":0}`, `{"defaultEnabled":true,"ttlSeconds":-60}`, `{"defaultEnabled":true,"ttlSeconds":90}`, `{"defaultEnabled":true,"ttlSeconds":1.5}`, `{"defaultEnabled":true,"ttlSeconds":"300"}`, `{"defaultEnabled":true,"ttlSeconds":9223372036854775807}`} {
		t.Run(value, func(t *testing.T) {
			previous := app.ConfigYml
			t.Cleanup(func() { app.ConfigYml = previous })
			config := &serviceConfigTestYAML{values: map[string]interface{}{}}
			app.ConfigYml = config
			request := videoShareConfigPayload(t, NewServiceConfigController())
			request["videoShare"] = json.RawMessage(value)
			recorder := httptest.NewRecorder()
			newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, request)))
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			require.Zero(t, config.saveNum)
		})
	}
}

func TestServiceConfigVideoShareLegacyAndRollback(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			previous := app.ConfigYml
			t.Cleanup(func() { app.ConfigYml = previous })
			config := &serviceConfigTestYAML{values: map[string]interface{}{
				"gb28181.video_share.default_enabled": true, "gb28181.video_share.ttl_seconds": 420,
			}}
			app.ConfigYml = config
			controller := NewServiceConfigController()
			request := videoShareConfigPayload(t, controller)
			if failure {
				config.saveErr = errors.New("disk full")
				request["videoShare"] = json.RawMessage(`{"defaultEnabled":false,"ttlSeconds":180}`)
			} else {
				delete(request, "videoShare")
			}
			recorder := httptest.NewRecorder()
			newServiceConfigRouter(controller).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, request)))
			if failure {
				require.Equal(t, 500, recorder.Code)
			} else {
				require.Equal(t, 200, recorder.Code, recorder.Body.String())
			}
			data, err := json.Marshal(NewServiceConfigController().currentServiceConfig())
			require.NoError(t, err)
			var loaded map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(data, &loaded))
			require.JSONEq(t, `{"defaultEnabled":true,"ttlSeconds":420}`, string(loaded["videoShare"]))
			require.NotContains(t, strings.ToLower(recorder.Body.String()), "secret")
		})
	}
}

func videoShareConfigPayload(t *testing.T, controller *ServiceConfigController) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(controller.currentServiceConfig())
	require.NoError(t, err)
	var request map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &request))
	return request
}
