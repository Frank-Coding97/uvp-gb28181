package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
)

func TestPermissionWorkbench_StrictAssignmentFilter(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	previousConfig := app.ConfigYml
	app.ConfigYml = ymlconfig.CreateYamlFactory(filepath.Join("..", "..", "..", "config"))
	app.ConfigYml.Set("gb28181.device.default_owner_dept_id", 1)
	t.Cleanup(func() { app.ConfigYml = previousConfig })

	active := int8(1)
	require.NoError(t, db.Create(&[]basemodels.SysDepartment{
		{BaseModel: basemodels.BaseModel{ID: 1}, Name: "总部", Status: &active},
		{BaseModel: basemodels.BaseModel{ID: 10}, Name: "安保部", Status: &active},
	}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{
		{DeviceID: "owner-zero", Name: "未分配", OwnerDeptID: 0, SubscribeCapability: gbmodels.SubscribeUnknown},
		{DeviceID: "owner-root", Name: "总部设备", OwnerDeptID: 1, SubscribeCapability: gbmodels.SubscribeUnknown},
		{DeviceID: "owner-dept", Name: "部门设备", OwnerDeptID: 10, SubscribeCapability: gbmodels.SubscribeUnknown},
	}).Error)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/devices?assignment=unassigned", nil))
	require.Equal(t, http.StatusOK, w.Code)
	list := unmarshal(t, w)["data"].(map[string]any)["list"].([]any)
	require.Len(t, list, 1)
	require.Equal(t, "owner-zero", list[0].(map[string]any)["deviceId"])

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/devices?assignment=assigned", nil))
	require.Equal(t, http.StatusOK, w.Code)
	list = unmarshal(t, w)["data"].(map[string]any)["list"].([]any)
	require.Len(t, list, 2)
}

func TestPermissionWorkbench_RejectsUnknownAssignmentFilter(t *testing.T) {
	r, _ := newDeviceMgmtRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/devices?assignment=unknown", nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
}
