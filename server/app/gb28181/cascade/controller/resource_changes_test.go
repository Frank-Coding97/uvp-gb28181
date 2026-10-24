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
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
	sysmodels "uvplatform.com/uvp-gb28181/app/models"
)

func TestBusinessGroupResourceChangesShareWithoutBusinessTreeAndReuseProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(
		&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{},
		&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{},
	))

	repo := repository.NewGormRepository(db)
	platform := &model.GbCascadePlatform{Name: "上级", LocalDeviceID: "34020000002000000001", PTZEnabled: true}
	require.NoError(t, repo.CreatePlatform(context.Background(), platform))
	device := gbmodels.GbDevice{DeviceID: "34020000001320000011", Name: "源设备", OwnerDeptID: 10}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000012", Name: "源通道", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)

	ctrl := NewManagementController(service.NewManagementService(repo, nil, nil, nil), db)
	router := gin.New()
	router.PUT("/platforms/:id/business-group", ctrl.ReplaceBusinessGroup)
	request := func(revision uint64, changes map[string]any) *httptest.ResponseRecorder {
		payload, marshalErr := json.Marshal(map[string]any{
			"expectedProjectionRevision": revision,
			"nodes":                      []any{},
			"bindings":                   []any{},
			"resourceChanges":            changes,
		})
		require.NoError(t, marshalErr)
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/platforms/%d/business-group", platform.ID), bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}

	response := request(0, map[string]any{"shareChannelIds": []uint64{uint64(channel.ID)}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var saved businessGroupResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &saved))
	require.EqualValues(t, 1, saved.Revision)
	require.Empty(t, saved.Nodes)
	require.Empty(t, saved.Bindings)
	require.Len(t, saved.Channels, 1)
	require.True(t, saved.Channels[0].PTZAllowed)
	publishedID := saved.Channels[0].PublishedChannelID

	response = request(1, map[string]any{"unshareChannelIds": []uint64{uint64(channel.ID)}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &saved))
	require.EqualValues(t, 2, saved.Revision)
	require.Empty(t, saved.Channels)

	response = request(2, map[string]any{"shareChannelIds": []uint64{uint64(channel.ID)}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &saved))
	require.EqualValues(t, 3, saved.Revision)
	require.Len(t, saved.Channels, 1)
	require.Equal(t, publishedID, saved.Channels[0].PublishedChannelID, "unshare should retain the inactive projection so it can be reused")

	response = request(3, map[string]any{"channelPermissions": []map[string]any{{"sourceChannelId": channel.ID, "ptzAllowed": false}}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &saved))
	require.EqualValues(t, 4, saved.Revision)
	require.False(t, saved.Channels[0].PTZAllowed)
}

func TestBusinessGroupResourceChangesReturnsCommittedSnapshotWhenRuntimeReloadFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(
		&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{},
		&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{},
	))

	repo := repository.NewGormRepository(db)
	platform := &model.GbCascadePlatform{Name: "上级", LocalDeviceID: "34020000002000000001"}
	require.NoError(t, repo.CreatePlatform(context.Background(), platform))
	device := gbmodels.GbDevice{DeviceID: "34020000001320000011", Name: "源设备", OwnerDeptID: 10}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000012", Name: "源通道", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	runtime := &publishGroupRuntimeFake{err: errors.New("runtime unavailable")}
	ctrl := NewManagementController(service.NewManagementService(repo, nil, runtime, nil), db)
	router := gin.New()
	router.PUT("/platforms/:id/business-group", ctrl.ReplaceBusinessGroup)
	body, err := json.Marshal(map[string]any{
		"expectedProjectionRevision": uint64(0),
		"nodes":                      []any{},
		"bindings":                   []any{},
		"resourceChanges":            map[string]any{"shareChannelIds": []uint64{uint64(channel.ID)}},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/platforms/%d/business-group", platform.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"runtimeSynced":false`)
	require.Contains(t, response.Body.String(), "资源已保存")
	require.NotContains(t, response.Body.String(), "runtime unavailable")
	var saved businessGroupResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &saved))
	require.Equal(t, platform.ID, saved.PlatformID)
	require.EqualValues(t, 1, saved.Revision)
	require.Len(t, saved.Channels, 1)
	require.Equal(t, 1, runtime.reloads)
	stored, err := repo.ProjectionSnapshot(context.Background(), platform.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, stored.Revision, "runtime reload failure must not roll back the share")
}

func TestBusinessGroupResourceChangesPreservesLegacyTopLevelShareBindingRequirement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(
		&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{},
		&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{},
	))
	repo := repository.NewGormRepository(db)
	platform := &model.GbCascadePlatform{Name: "上级", LocalDeviceID: "34020000002000000001"}
	require.NoError(t, repo.CreatePlatform(context.Background(), platform))
	device := gbmodels.GbDevice{DeviceID: "34020000001320000011", Name: "源设备", OwnerDeptID: 10}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000012", Name: "源通道", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	ctrl := NewManagementController(service.NewManagementService(repo, nil, nil, nil), db)
	router := gin.New()
	router.PUT("/platforms/:id/business-group", ctrl.ReplaceBusinessGroup)
	payload, err := json.Marshal(map[string]any{
		"expectedProjectionRevision": 0,
		"nodes":                      []any{},
		"bindings":                   []any{},
		"shareChannelIds":            []uint64{uint64(channel.ID)},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/platforms/%d/business-group", platform.ID), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	snapshot, err := repo.ProjectionSnapshot(context.Background(), platform.ID)
	require.NoError(t, err)
	require.Zero(t, snapshot.Revision)
	require.Empty(t, snapshot.Devices)
	require.Empty(t, snapshot.Channels)
}

func TestBusinessGroupResourceChangesAuthorizeEveryModifiedChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(
		&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{},
		&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{},
		&gbmodels.GbDeviceGrant{}, &sysmodels.User{}, &sysmodels.SysRole{}, &sysmodels.SysUserRole{}, &sysmodels.SysDepartment{},
	))
	require.NoError(t, db.Create(&sysmodels.User{Username: "scoped-user", Password: "hash", Status: 1, DeptID: 10}).Error)
	require.NoError(t, db.Create(&sysmodels.SysRole{Status: 1, DataScope: 3}).Error)
	require.NoError(t, db.Create(&sysmodels.SysUserRole{UserID: 1, RoleID: 1}).Error)

	repo := repository.NewGormRepository(db)
	platform := &model.GbCascadePlatform{Name: "上级", LocalDeviceID: "34020000002000000001"}
	require.NoError(t, repo.CreatePlatform(context.Background(), platform))
	device := gbmodels.GbDevice{DeviceID: "34020000001320000011", Name: "其他部门设备", OwnerDeptID: 20}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000012", Name: "未授权通道", OwnerDeptID: 20}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, repo.ReplaceProjection(context.Background(), platform.ID, 0,
		[]repository.DeviceProjectionInput{{SourceDeviceID: uint64(device.ID), PublishedDeviceID: device.DeviceID}},
		[]repository.ChannelProjectionInput{{SourceDeviceID: uint64(device.ID), SourceChannelID: uint64(channel.ID), PublishedChannelID: channel.ChannelID}},
	))

	ctrl := NewManagementController(service.NewManagementService(repo, nil, nil, nil), db)
	router := gin.New()
	router.PUT("/platforms/:id/business-group", func(ctx *gin.Context) {
		ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 1, Username: "scoped-user"}})
		ctrl.ReplaceBusinessGroup(ctx)
	})
	for _, change := range []map[string]any{
		{"unshareChannelIds": []uint64{uint64(channel.ID)}},
		{"channelPermissions": []map[string]any{{"sourceChannelId": channel.ID, "ptzAllowed": true}}},
	} {
		payload, marshalErr := json.Marshal(map[string]any{
			"expectedProjectionRevision": 1,
			"nodes":                      []any{},
			"bindings":                   []any{},
			"resourceChanges":            change,
		})
		require.NoError(t, marshalErr)
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/platforms/%d/business-group", platform.ID), bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
		snapshot, snapshotErr := repo.ProjectionSnapshot(context.Background(), platform.ID)
		require.NoError(t, snapshotErr)
		require.EqualValues(t, 1, snapshot.Revision)
		require.Len(t, snapshot.Channels, 1)
		require.False(t, snapshot.Channels[0].PTZAllowed)
	}
}

func TestBusinessGroupModeChangeRequiresManageButSameModeDoesNot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(
		&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{},
		&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{},
	))
	repo := repository.NewGormRepository(db)
	platform := &model.GbCascadePlatform{Name: "上级", LocalDeviceID: "34020000002000000001"}
	require.NoError(t, repo.CreatePlatform(context.Background(), platform))
	checker := &cascadePermissionCheckerFake{allowed: false}
	ctrl := NewManagementController(service.NewManagementService(repo, nil, nil, nil), db)
	ctrl.permissionChecker = checker
	router := gin.New()
	router.PUT("/platforms/:id/business-group", func(ctx *gin.Context) {
		ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7, Username: "operator"}})
		ctrl.ReplaceBusinessGroup(ctx)
	})
	request := func(payload map[string]any) *httptest.ResponseRecorder {
		body, marshalErr := json.Marshal(payload)
		require.NoError(t, marshalErr)
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/platforms/%d/business-group", platform.ID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	root := "34020000002150000001"
	organization := "34020000002160000001"
	validNodes := []repository.BusinessNodeInput{
		{Code: root, Name: "业务根", Kind: "group"},
		{Code: organization, Name: "东区", Kind: "organization", ParentCode: root},
	}

	response := request(map[string]any{
		"expectedProjectionRevision": uint64(0),
		"nodes":                      validNodes,
		"bindings":                   []any{},
		"resourceChanges": map[string]any{
			"publishGroup": true, "expectedConfigRevision": uint64(1),
		},
	})
	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	require.Equal(t, 1, checker.calls)
	stored, err := repo.FindPlatform(context.Background(), platform.ID)
	require.NoError(t, err)
	require.False(t, stored.PublishGroup)
	require.Zero(t, stored.ProjectionRevision)

	response = request(map[string]any{
		"expectedProjectionRevision": uint64(0), "expectedConfigRevision": uint64(0), "publishGroup": false,
		"nodes": []repository.BusinessNodeInput{{Code: root, Name: "业务根", Kind: "group"}}, "bindings": []any{},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, 1, checker.calls, "unchanged publish mode must not check cascade manage permission")
	require.Contains(t, response.Body.String(), `"publishGroup":false`)
	require.Contains(t, response.Body.String(), `"configRevision":1`)
}

func TestBusinessGroupModeRuntimeFailureMarkerReportsCommittedModeAndRevision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(
		&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{},
		&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{},
		&gbmodels.GbDeviceGrant{}, &sysmodels.User{}, &sysmodels.SysRole{}, &sysmodels.SysUserRole{}, &sysmodels.SysDepartment{},
	))
	repo := repository.NewGormRepository(db)
	platform := &model.GbCascadePlatform{Name: "上级", LocalDeviceID: "34020000002000000001"}
	require.NoError(t, repo.CreatePlatform(context.Background(), platform))
	require.NoError(t, db.Create(&sysmodels.User{BaseModel: sysmodels.BaseModel{ID: 7}, Username: "operator", Password: "hash", Status: 1, DeptID: 10}).Error)
	require.NoError(t, db.Create(&sysmodels.SysRole{Status: 1, DataScope: 1}).Error)
	require.NoError(t, db.Create(&sysmodels.SysUserRole{UserID: 7, RoleID: 1}).Error)
	device := gbmodels.GbDevice{DeviceID: "34020000001320000011", Name: "源设备", OwnerDeptID: 10}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000012", Name: "源通道", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	runtime := &publishGroupRuntimeFake{err: errors.New("runtime unavailable")}
	ctrl := NewManagementController(service.NewManagementService(repo, nil, runtime, nil), db)
	ctrl.permissionChecker = &cascadePermissionCheckerFake{allowed: true}
	router := gin.New()
	router.PUT("/platforms/:id/business-group", func(ctx *gin.Context) {
		ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7, Username: "operator"}})
		ctrl.ReplaceBusinessGroup(ctx)
	})
	root := "34020000002150000001"
	organization := "34020000002160000001"
	body, err := json.Marshal(map[string]any{
		"expectedProjectionRevision": uint64(0),
		"nodes": []repository.BusinessNodeInput{
			{Code: root, Name: "业务根", Kind: "group"},
			{Code: organization, Name: "东区", Kind: "organization", ParentCode: root},
		},
		"bindings": []repository.BusinessBindingInput{{SourceChannelID: uint64(channel.ID), NodeCode: organization}},
		"resourceChanges": map[string]any{
			"shareChannelIds": []uint64{uint64(channel.ID)}, "publishGroup": true, "expectedConfigRevision": uint64(1),
		},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/platforms/%d/business-group", platform.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"runtimeSynced":false`)
	require.Contains(t, response.Body.String(), `"publishGroup":true`)
	require.Contains(t, response.Body.String(), `"configRevision":2`)
	var saved businessGroupResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &saved))
	require.True(t, saved.PublishGroup)
	require.EqualValues(t, 2, saved.ConfigRevision)
	require.EqualValues(t, 1, saved.Revision)
	require.Len(t, saved.Channels, 1)
}

type cascadePermissionCheckerFake struct {
	allowed bool
	calls   int
}

func (f *cascadePermissionCheckerFake) Enforce(string, string, string, ...string) (bool, error) {
	f.calls++
	return f.allowed, nil
}
