package controller

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestBusinessGroupErrorsExplainHowToEnablePublishing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctrl := NewManagementController(nil, nil)
	ctrl.fail(ctx, fmt.Errorf("%w：请先配置业务分组根，再开启发布", service.ErrInvalidBusinessGroup))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "请先配置业务分组根")
}

func TestBusinessGroupHTTPScopesRevisionAndDoesNotExposePlatformSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{}, &model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}))
	repo := repository.NewGormRepository(db)
	ctx := context.Background()
	a := &model.GbCascadePlatform{Name: "上级A", LocalDeviceID: "34020000002000000001", Host: "192.0.2.1", SecretCiphertext: []byte("secret-sentinel"), ConfigRevision: 1}
	b := &model.GbCascadePlatform{Name: "上级B", LocalDeviceID: "34020000002000000002", Host: "192.0.2.2", ConfigRevision: 1}
	require.NoError(t, repo.CreatePlatform(ctx, a))
	require.NoError(t, repo.CreatePlatform(ctx, b))
	require.NoError(t, repo.ReplaceProjection(ctx, a.ID, 0, []repository.DeviceProjectionInput{{SourceDeviceID: 7, PublishedDeviceID: "34020000001320000007"}}, []repository.ChannelProjectionInput{{SourceDeviceID: 7, SourceChannelID: 8, PublishedChannelID: "34020000001320000008", Name: "通道A"}}))
	require.NoError(t, repo.ReplaceProjection(ctx, b.ID, 0, []repository.DeviceProjectionInput{{SourceDeviceID: 9, PublishedDeviceID: "34020000001320000009"}}, []repository.ChannelProjectionInput{{SourceDeviceID: 9, SourceChannelID: 10, PublishedChannelID: "34020000001320000010"}}))
	ctrl := NewManagementController(service.NewManagementService(repo, nil, nil, nil), nil)
	router := gin.New()
	router.GET("/platforms/:id/business-group", ctrl.GetBusinessGroup)
	router.PUT("/platforms/:id/business-group", ctrl.ReplaceBusinessGroup)
	request := func(method, path string, payload any) *httptest.ResponseRecorder {
		t.Helper()
		var body []byte
		if raw, ok := payload.(string); ok {
			body = []byte(raw)
		} else if payload != nil {
			body, err = json.Marshal(payload)
			require.NoError(t, err)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	path := fmt.Sprintf("/platforms/%d/business-group", a.ID)
	w := request(http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"nodes":[]`)
	require.Contains(t, w.Body.String(), `"bindings":[]`)
	require.NotContains(t, w.Body.String(), "secret-sentinel")
	require.NotContains(t, w.Body.String(), "192.0.2.1")
	nodes := []repository.BusinessNodeInput{{Code: "34020000002150000001", Name: "业务", Kind: "group"}, {Code: "34020000002160000001", Name: "东区", Kind: "organization", ParentCode: "34020000002150000001"}}
	payload := businessGroupRequest{Nodes: nodes, Bindings: []repository.BusinessBindingInput{{SourceChannelID: 8, NodeCode: nodes[1].Code}}, ExpectedProjectionRevision: 1}
	w = request(http.MethodPut, path, payload)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var saved businessGroupResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, a.ID, saved.PlatformID)
	require.EqualValues(t, 2, saved.Revision)
	require.Len(t, saved.Nodes, 2)
	require.Len(t, saved.Channels, 1)
	require.EqualValues(t, 8, saved.Bindings[0].SourceChannelID)
	w = request(http.MethodPut, path, payload)
	require.Equal(t, http.StatusConflict, w.Code)
	payload.ExpectedProjectionRevision = 2
	payload.Bindings[0].SourceChannelID = 10
	w = request(http.MethodPut, path, payload)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "已共享")
	snapshot, err := repo.ProjectionSnapshot(ctx, a.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, snapshot.Revision)
	require.EqualValues(t, 8, snapshot.Bindings[0].SourceChannelID)
	other, err := repo.ProjectionSnapshot(ctx, b.ID)
	require.NoError(t, err)
	require.Empty(t, other.Nodes)
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/platforms/invalid/business-group", "", http.StatusBadRequest},
		{http.MethodGet, "/platforms/999/business-group", "", http.StatusNotFound},
		{http.MethodPut, path, "{", http.StatusBadRequest},
	} {
		w = request(tc.method, tc.path, tc.body)
		require.Equal(t, tc.status, w.Code, w.Body.String())
	}
}
