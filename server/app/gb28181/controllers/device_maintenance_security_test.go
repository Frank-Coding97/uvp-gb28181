package controllers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

type maintenancePermissionProbe struct {
	app.CasbinInterf
	allowed bool
	err     error
	path    string
	method  string
}

func (p *maintenancePermissionProbe) Enforce(_ string, path, method string, _ ...string) (bool, error) {
	p.path, p.method = path, method
	return p.allowed, p.err
}

func setMaintenancePermissionProbe(t *testing.T, probe *maintenancePermissionProbe) {
	t.Helper()
	previousConfig, previousCasbin := app.ConfigYml, app.CasbinV2
	app.ConfigYml, app.CasbinV2 = nil, probe
	t.Cleanup(func() { app.ConfigYml, app.CasbinV2 = previousConfig, previousCasbin })
}

func TestMaintenanceLegacyEntryCannotBypassRebootPermission(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "permission denied"},
		{name: "permission lookup fails", err: errors.New("policy unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOnline)
			seedMaintenanceAdmin(t, fixture.db)
			probe := &maintenancePermissionProbe{err: test.err}
			setMaintenancePermissionProbe(t, probe)
			router := maintenanceRouter(fixture, maintenanceClaims(17))
			request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(fixture.channel.ID)+"/device-control", strings.NewReader(`{"action":"teleboot","confirmed":true}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Contains(t, response.Body.String(), "设备重启权限")
			require.Equal(t, "/api/gb28181/device-mgmt/device/"+uintStr(fixture.device.ID)+"/reboot", probe.path)
			require.Equal(t, http.MethodPost, probe.method)
			require.Zero(t, fixture.sender.calls)
		})
	}
}

func TestMaintenanceLegacyEntryRequiresParentDeviceOwnership(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOnline)
	seedMaintenanceAdmin(t, fixture.db)
	require.NoError(t, fixture.db.Model(&basemodels.SysRole{}).Where("name = ?", "maintenance-admin").Update("data_scope", 3).Error)
	require.NoError(t, fixture.db.Model(fixture.device).Update("owner_dept_id", 2).Error)
	setMaintenancePermissionProbe(t, &maintenancePermissionProbe{allowed: true})
	router := maintenanceRouter(fixture, maintenanceClaims(17))
	request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(fixture.channel.ID)+"/device-control", strings.NewReader(`{"action":"teleboot","confirmed":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Contains(t, response.Body.String(), "无权限")
	require.Zero(t, fixture.sender.calls)
}

func TestMaintenanceLegacyEntryRejectsAnonymousCaller(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOnline)
	router := maintenanceRouter(fixture)
	request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(fixture.channel.ID)+"/device-control", strings.NewReader(`{"action":"teleboot","confirmed":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Contains(t, response.Body.String(), "未登录")
	require.Zero(t, fixture.sender.calls)
}

func TestMaintenanceHistoryProjectsStaleLegacyQueuedAsUnknown(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOffline, gbmodels.ChannelStatusOffline)
	seedMaintenanceAdmin(t, fixture.db)
	rows := []gbmodels.GbPTZOperation{
		{OperationID: "stale-queued", IdempotencyKey: "stale", DeviceID: fixture.device.ID, Action: "teleboot", Status: gbmodels.PTZOperationQueued, CreatedAt: time.Now().Add(-2 * time.Minute)},
		{OperationID: "fresh-queued", IdempotencyKey: "fresh", DeviceID: fixture.device.ID, Action: "teleboot", Status: gbmodels.PTZOperationQueued, CreatedAt: time.Now()},
	}
	require.NoError(t, fixture.db.Create(&rows).Error)
	router := maintenanceRouter(fixture, maintenanceClaims(17))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/device/"+uintStr(fixture.device.ID)+"/maintenance-operations", nil))
	var envelope struct {
		Data struct {
			List []struct {
				OperationID string `json:"operationId"`
				Status      string `json:"status"`
			} `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	statuses := make(map[string]string)
	for _, row := range envelope.Data.List {
		statuses[row.OperationID] = row.Status
	}
	require.Equal(t, "unknown", statuses["stale-queued"])
	require.Equal(t, "queued", statuses["fresh-queued"])
	var stored gbmodels.GbPTZOperation
	require.NoError(t, fixture.db.Where("operation_id = ?", "stale-queued").First(&stored).Error)
	require.Equal(t, gbmodels.PTZOperationQueued, stored.Status, "projection must preserve the audit row")
	require.Zero(t, fixture.sender.calls)
}
