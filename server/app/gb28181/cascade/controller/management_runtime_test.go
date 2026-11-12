package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/service"
)

func TestPlatformMutationHTTPReportsRuntimeFailureAfterCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{}, &model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}, &model.GbCascadeMediaSession{}))

	repo := repository.NewGormRepository(db)
	runtime := &publishGroupRuntimeFake{err: errors.New("runtime unavailable")}
	controller := NewManagementController(service.NewManagementService(repo, nil, runtime, nil), nil)
	router := gin.New()
	router.POST("/platforms", controller.Create)
	router.PUT("/platforms/:id", controller.Update)
	router.PUT("/platforms/:id/enabled", controller.SetEnabled)

	requestBody := func(enabled bool) map[string]any {
		return map[string]any{
			"name": "upstream", "upstreamServerId": "34020000002000000002", "upstreamDomain": "3402000000",
			"host": "192.0.2.1", "port": 5060, "localDeviceId": "34020000002000000001", "localDomain": "3402000000",
			"localSipIp": "192.0.2.2", "localSipPort": 5060, "authUsername": "34020000002000000001",
			"profileOverride": "auto", "transport": "UDP", "registerExpires": 3600, "keepaliveInterval": 60,
			"catalogBatchSize": 1, "maxStreams": 4, "enabled": enabled,
		}
	}
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		payload, marshalErr := json.Marshal(body)
		require.NoError(t, marshalErr)
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		return recorder
	}
	assertCommittedRuntimeFailure := func(recorder *httptest.ResponseRecorder) uint64 {
		t.Helper()
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), `"runtimeSynced":false`)
		require.Contains(t, recorder.Body.String(), `"platform":{"id":`)
		require.Contains(t, recorder.Body.String(), "运行时同步失败")
		require.NotContains(t, recorder.Body.String(), "runtime unavailable")
		var result struct {
			Platform service.PlatformView `json:"platform"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
		require.NotNil(t, result.Platform.RuntimeSynced)
		require.False(t, *result.Platform.RuntimeSynced)
		return result.Platform.ID
	}

	createdID := assertCommittedRuntimeFailure(request(http.MethodPost, "/platforms", requestBody(true)))
	created, err := repo.FindPlatform(context.Background(), createdID)
	require.NoError(t, err)
	require.True(t, created.Enabled, "reload failure must not roll back the committed create")
	require.Equal(t, 1, runtime.reloads)

	runtime.err = nil
	disabledBody := requestBody(false)
	disabledBody["name"] = "upstream-disabled"
	disabledBody["upstreamServerId"] = "34020000002000000003"
	require.Equal(t, uint64(1), created.ConfigRevision)
	disabled := request(http.MethodPost, "/platforms", disabledBody)
	require.Equal(t, http.StatusOK, disabled.Code, disabled.Body.String())
	runtime.err = errors.New("runtime unavailable")

	updatedID := assertCommittedRuntimeFailure(request(http.MethodPut, fmt.Sprintf("/platforms/%d", createdID), func() map[string]any {
		body := requestBody(true)
		body["expectedRevision"] = created.ConfigRevision
		return body
	}()))
	require.Equal(t, createdID, updatedID)
	updated, err := repo.FindPlatform(context.Background(), createdID)
	require.NoError(t, err)
	require.True(t, updated.Enabled)

	_, err = repo.FindPlatform(context.Background(), 2)
	require.NoError(t, err)
	enabledResponse := assertCommittedRuntimeFailure(request(http.MethodPut, "/platforms/2/enabled", map[string]any{"enabled": true, "expectedRevision": 1}))
	require.Equal(t, uint64(2), enabledResponse)
	enabled, err := repo.FindPlatform(context.Background(), enabledResponse)
	require.NoError(t, err)
	require.True(t, enabled.Enabled)
}
