package controller

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/service"
)

type publishGroupRuntimeFake struct {
	reloads int
	err     error
}

func (r *publishGroupRuntimeFake) Reload(context.Context) error { r.reloads++; return r.err }
func (r *publishGroupRuntimeFake) PlatformIDs() []uint64        { return nil }

func TestPublishGroupHTTPChangesSettingWithoutReturningSecretsAndReportsRuntimeFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{}, &model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}))
	repo := repository.NewGormRepository(db)
	platform := &model.GbCascadePlatform{
		Name: "上级", UpstreamServerID: "34020000002000000002", UpstreamDomain: "3402000000", Host: "192.0.2.1", Port: 5060,
		LocalDeviceID: "34020000002000000001", LocalDomain: "3402000000", LocalSIPIP: "192.0.2.2", LocalSIPPort: 5060,
		SecretCiphertext: []byte("credential-sentinel"), SecretNonce: []byte("nonce-sentinel"), SecretAlg: "AES-256-GCM", SecretKeyVersion: "key-v1",
		RetryPolicy: `{"initialMs":100}`, AutoPushCatalog: true, PublishCivil: true, Enabled: true, ConfigRevision: 1,
	}
	require.NoError(t, repo.CreatePlatform(context.Background(), platform))
	require.NoError(t, repo.ReplaceBusinessGroup(context.Background(), platform.ID, 0, []repository.BusinessNodeInput{{Code: "34020000002150000001", Name: "业务根", Kind: "group"}}, nil))
	runtime := &publishGroupRuntimeFake{}
	ctrl := NewManagementController(service.NewManagementService(repo, nil, runtime, nil), nil)
	router := gin.New()
	router.PUT("/platforms/:id/publish-group", ctrl.SetPublishGroup)
	request := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/platforms/"+stringID(platform.ID)+"/publish-group", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		return recorder
	}

	recorder := request(`{"publishGroup":true,"expectedRevision":1}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"publishGroup":true`)
	require.Contains(t, recorder.Body.String(), `"configRevision":2`)
	require.NotContains(t, recorder.Body.String(), "credential-sentinel")
	require.NotContains(t, recorder.Body.String(), "nonce-sentinel")
	require.Equal(t, 1, runtime.reloads)
	stored, err := repo.FindPlatform(context.Background(), platform.ID)
	require.NoError(t, err)
	require.Equal(t, []byte("credential-sentinel"), stored.SecretCiphertext)
	require.Equal(t, []byte("nonce-sentinel"), stored.SecretNonce)
	require.Equal(t, `{"initialMs":100}`, stored.RetryPolicy)
	require.True(t, stored.AutoPushCatalog)
	require.True(t, stored.PublishCivil)
	require.True(t, stored.Enabled)

	runtime.err = errors.New("upstream credentials leaked by dependency")
	recorder = request(`{"publishGroup":false,"expectedRevision":2}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"runtimeSynced":false`)
	require.Contains(t, recorder.Body.String(), `"publishGroup":false`)
	require.Contains(t, recorder.Body.String(), "运行时同步失败")
	require.NotContains(t, recorder.Body.String(), "credentials leaked")
	stored, err = repo.FindPlatform(context.Background(), platform.ID)
	require.NoError(t, err)
	require.False(t, stored.PublishGroup, "reload failure must not undo the database commit")

	runtime.err = nil
	recorder = request(`{"publishGroup":false,"expectedRevision":3}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"publishGroup":false`)
	require.NotContains(t, recorder.Body.String(), `"runtimeSynced":false`)
	require.Equal(t, 3, runtime.reloads)
}

func TestPublishGroupHTTPRejectsInvalidContractAndMapsConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{}, &model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}))
	repo := repository.NewGormRepository(db)
	platform := &model.GbCascadePlatform{Name: "invalid", LocalDeviceID: "34020000002000000001", ConfigRevision: 1}
	require.NoError(t, repo.CreatePlatform(context.Background(), platform))
	ctrl := NewManagementController(service.NewManagementService(repo, nil, &publishGroupRuntimeFake{}, nil), nil)
	router := gin.New()
	router.PUT("/platforms/:id/publish-group", ctrl.SetPublishGroup)
	request := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		return recorder
	}
	for _, body := range []string{
		`{`,
		`{"expectedRevision":1}`,
		`{"publishGroup":"true","expectedRevision":1}`,
		`{"publishGroup":true}`,
		`{"publishGroup":true,"expectedRevision":1,"name":"must-not-be-accepted"}`,
	} {
		recorder := request("/platforms/"+stringID(platform.ID)+"/publish-group", body)
		require.Equal(t, http.StatusBadRequest, recorder.Code, body)
	}
	recorder := request("/platforms/"+stringID(platform.ID)+"/publish-group", `{"publishGroup":true,"expectedRevision":1}`)
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "215 根节点")
	recorder = request("/platforms/"+stringID(platform.ID)+"/publish-group", `{"publishGroup":false,"expectedRevision":2}`)
	require.Equal(t, http.StatusConflict, recorder.Code)
	recorder = request("/platforms/999/publish-group", `{"publishGroup":false,"expectedRevision":1}`)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	recorder = request("/platforms/nope/publish-group", `{"publishGroup":false,"expectedRevision":1}`)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestPublishGroupHTTPDoesNotEnableOrReloadDisabledPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{}, &model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}))
	repo := repository.NewGormRepository(db)
	platform := &model.GbCascadePlatform{Name: "disabled", LocalDeviceID: "34020000002000000001", ConfigRevision: 1, Enabled: false}
	require.NoError(t, repo.CreatePlatform(context.Background(), platform))
	require.NoError(t, repo.ReplaceBusinessGroup(context.Background(), platform.ID, 0, []repository.BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}}, nil))
	runtime := &publishGroupRuntimeFake{}
	ctrl := NewManagementController(service.NewManagementService(repo, nil, runtime, nil), nil)
	router := gin.New()
	router.PUT("/platforms/:id/publish-group", ctrl.SetPublishGroup)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/platforms/1/publish-group", bytes.NewBufferString(`{"publishGroup":true,"expectedRevision":1}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"enabled":false`)
	require.Zero(t, runtime.reloads)
}

func stringID(id uint64) string { return strconv.FormatUint(id, 10) }
