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
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
	sysmodels "uvplatform.com/uvp-gb28181/app/models"
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
	require.NoError(t, db.AutoMigrate(&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{}, &model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	repo := repository.NewGormRepository(db)
	ctx := context.Background()
	a := &model.GbCascadePlatform{Name: "上级A", LocalDeviceID: "34020000002000000001", Host: "192.0.2.1", SecretCiphertext: []byte("secret-sentinel"), ConfigRevision: 1}
	b := &model.GbCascadePlatform{Name: "上级B", LocalDeviceID: "34020000002000000002", Host: "192.0.2.2", ConfigRevision: 1}
	require.NoError(t, repo.CreatePlatform(ctx, a))
	require.NoError(t, repo.CreatePlatform(ctx, b))
	require.NoError(t, repo.ReplaceProjection(ctx, a.ID, 0, []repository.DeviceProjectionInput{{SourceDeviceID: 7, PublishedDeviceID: "34020000001320000007"}}, []repository.ChannelProjectionInput{{SourceDeviceID: 7, SourceChannelID: 8, PublishedChannelID: "34020000001320000008", Name: "通道A"}}))
	require.NoError(t, repo.ReplaceProjection(ctx, b.ID, 0, []repository.DeviceProjectionInput{{SourceDeviceID: 9, PublishedDeviceID: "34020000001320000009"}}, []repository.ChannelProjectionInput{{SourceDeviceID: 9, SourceChannelID: 10, PublishedChannelID: "34020000001320000010"}}))
	sourceDevice := gbmodels.GbDevice{DeviceID: "34020000001320000011", Name: "新设备", OwnerDeptID: 10}
	require.NoError(t, db.Create(&sourceDevice).Error)
	sourceChannel := gbmodels.GbChannel{DeviceID: sourceDevice.DeviceID, ChannelID: "34020000001320000012", Name: "新通道", OwnerDeptID: 10}
	require.NoError(t, db.Create(&sourceChannel).Error)
	ctrl := NewManagementController(service.NewManagementService(repo, nil, nil, nil), db)
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
	payload := businessGroupRequest{
		Nodes: nodes,
		Bindings: []repository.BusinessBindingInput{
			{SourceChannelID: 8, NodeCode: nodes[1].Code},
			{SourceChannelID: uint64(sourceChannel.ID), NodeCode: nodes[1].Code},
		},
		ShareChannelIDs:            []uint64{uint64(sourceChannel.ID)},
		ExpectedProjectionRevision: 1,
	}
	w = request(http.MethodPut, path, payload)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var saved businessGroupResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, a.ID, saved.PlatformID)
	require.EqualValues(t, 2, saved.Revision)
	require.Len(t, saved.Nodes, 2)
	require.Len(t, saved.Channels, 2)
	require.Len(t, saved.Bindings, 2)
	require.Contains(t, []uint64{saved.Bindings[0].SourceChannelID, saved.Bindings[1].SourceChannelID}, uint64(sourceChannel.ID))
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
	require.Len(t, snapshot.Bindings, 2)
	require.Contains(t, []uint64{snapshot.Bindings[0].SourceChannelID, snapshot.Bindings[1].SourceChannelID}, uint64(sourceChannel.ID))
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

func TestBusinessGroupHTTPRejectsAndRollsBackUnauthorizedChannelShare(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(
		&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{},
		&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{},
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceGrant{},
		&sysmodels.User{}, &sysmodels.SysRole{}, &sysmodels.SysUserRole{}, &sysmodels.SysDepartment{},
	))
	require.NoError(t, db.Create(&sysmodels.User{Username: "scoped-user", Password: "hash", Status: 1, DeptID: 10}).Error)
	require.NoError(t, db.Create(&sysmodels.SysRole{Status: 1, DataScope: 3}).Error)
	require.NoError(t, db.Create(&sysmodels.SysUserRole{UserID: 1, RoleID: 1}).Error)

	repo := repository.NewGormRepository(db)
	platform := &model.GbCascadePlatform{Name: "上级", LocalDeviceID: "34020000002000000001"}
	require.NoError(t, repo.CreatePlatform(context.Background(), platform))
	sourceDevice := gbmodels.GbDevice{DeviceID: "34020000001320000011", Name: "其他部门设备", OwnerDeptID: 20}
	require.NoError(t, db.Create(&sourceDevice).Error)
	sourceChannel := gbmodels.GbChannel{DeviceID: sourceDevice.DeviceID, ChannelID: "34020000001320000012", Name: "未授权通道", OwnerDeptID: 20}
	require.NoError(t, db.Create(&sourceChannel).Error)

	ctrl := NewManagementController(service.NewManagementService(repo, nil, nil, nil), db)
	router := gin.New()
	router.PUT("/platforms/:id/business-group", func(ctx *gin.Context) {
		ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 1, Username: "scoped-user"}})
		ctrl.ReplaceBusinessGroup(ctx)
	})
	nodes := []repository.BusinessNodeInput{
		{Code: "34020000002150000001", Name: "业务根", Kind: "group"},
		{Code: "34020000002160000001", Name: "组织", Kind: "organization", ParentCode: "34020000002150000001"},
	}
	payload, err := json.Marshal(businessGroupRequest{
		Nodes:                      nodes,
		Bindings:                   []repository.BusinessBindingInput{{SourceChannelID: uint64(sourceChannel.ID), NodeCode: nodes[1].Code}},
		ShareChannelIDs:            []uint64{uint64(sourceChannel.ID)},
		ExpectedProjectionRevision: 0,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/platforms/%d/business-group", platform.ID), bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())

	snapshot, err := repo.ProjectionSnapshot(context.Background(), platform.ID)
	require.NoError(t, err)
	require.Zero(t, snapshot.Revision)
	require.Empty(t, snapshot.Devices)
	require.Empty(t, snapshot.Channels)
	require.Empty(t, snapshot.Nodes)
	require.Empty(t, snapshot.Bindings)
}
