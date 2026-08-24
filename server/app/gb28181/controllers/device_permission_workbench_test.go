package controllers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
)

func registerPermissionWorkbenchRoutes(r *gin.Engine, db *gorm.DB) {
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	r.GET("/api/gb28181/device-mgmt/permission-workbench/summary", controller.PermissionWorkbenchSummary)
	r.POST("/api/gb28181/device-mgmt/permission-workbench/devices/resolve", controller.ResolvePermissionWorkbenchDevices)
	r.POST("/api/gb28181/device-mgmt/permission-workbench/grants/query", controller.QueryPermissionWorkbenchGrants)
	r.GET("/api/gb28181/device-mgmt/permission-workbench/grant-targets", controller.SearchPermissionWorkbenchGrantTargets)
	r.POST("/api/gb28181/device-mgmt/permission-workbench/assignments", controller.ApplyPermissionWorkbenchAssignments)
	r.POST("/api/gb28181/device-mgmt/permission-workbench/assignments/departments", controller.ApplyPermissionWorkbenchDepartmentAssignment)
}

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

func TestPermissionWorkbench_SummaryEndpoint(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	registerPermissionWorkbenchRoutes(r, db)
	active := int8(1)
	require.NoError(t, db.Create(&[]basemodels.SysDepartment{
		{BaseModel: basemodels.BaseModel{ID: 10}, Name: "父部门", Status: &active},
		{BaseModel: basemodels.BaseModel{ID: 11}, ParentID: uintPointer(10), Name: "子部门", Status: &active},
	}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{
		{DeviceID: "summary-zero", OwnerDeptID: 0},
		{DeviceID: "summary-parent", OwnerDeptID: 10},
		{DeviceID: "summary-child", OwnerDeptID: 11},
	}).Error)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/permission-workbench/summary", nil))
	require.Equal(t, http.StatusOK, w.Code)
	data := unmarshal(t, w)["data"].(map[string]any)
	require.EqualValues(t, 3, data["allCount"])
	require.EqualValues(t, 2, data["assignedCount"])
	require.EqualValues(t, 1, data["unassignedCount"])
	departments := data["departments"].([]any)
	require.Len(t, departments, 2)
	require.EqualValues(t, 2, departments[0].(map[string]any)["subtreeCount"])
}

func TestPermissionWorkbench_ResolveEndpoint(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	registerPermissionWorkbenchRoutes(r, db)
	active := int8(1)
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 10}, Name: "安保部", Status: &active}).Error)
	devices := []gbmodels.GbDevice{
		{DeviceID: "resolve-endpoint-a", Name: "A", OwnerDeptID: 10, Status: gbmodels.DeviceStatusOnline},
		{DeviceID: "resolve-endpoint-b", Name: "B", OwnerDeptID: 0},
	}
	require.NoError(t, db.Create(&devices).Error)
	body, err := json.Marshal(map[string]any{"deviceIds": []uint{devices[1].ID, devices[0].ID, devices[1].ID, 999}})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/gb28181/device-mgmt/permission-workbench/devices/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	data := unmarshal(t, w)["data"].(map[string]any)
	resolved := data["devices"].([]any)
	require.Len(t, resolved, 2)
	require.Equal(t, "resolve-endpoint-b", resolved[0].(map[string]any)["deviceId"])
	require.Equal(t, "安保部", resolved[1].(map[string]any)["ownerDeptName"])
	require.Equal(t, []any{float64(999)}, data["unavailableIds"])
}

func uintPointer(value uint) *uint { return &value }

func TestPermissionWorkbench_AssignmentSkipsSameOwner(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	registerPermissionWorkbenchRoutes(r, db)
	active := int8(1)
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 10}, Name: "安保部", Status: &active}).Error)
	device := gbmodels.GbDevice{DeviceID: "assignment-skip", Name: "跳过设备", OwnerDeptID: 10}
	require.NoError(t, db.Create(&device).Error)
	body, err := json.Marshal(map[string]any{
		"items":        []map[string]any{{"deviceId": device.ID, "expectedOwnerDeptId": 10}},
		"targetDeptId": 10,
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/gb28181/device-mgmt/permission-workbench/assignments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	summary := unmarshal(t, w)["data"].(map[string]any)["summary"].(map[string]any)
	require.EqualValues(t, 1, summary["skipped"])
}

func TestPermissionWorkbench_DepartmentAssignmentRejectsChangedCount(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	registerPermissionWorkbenchRoutes(r, db)
	active := int8(1)
	require.NoError(t, db.Create(&[]basemodels.SysDepartment{
		{BaseModel: basemodels.BaseModel{ID: 10}, Name: "源部门", Status: &active},
		{BaseModel: basemodels.BaseModel{ID: 20}, Name: "目标部门", Status: &active},
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "department-stale-count", OwnerDeptID: 10}).Error)
	body := []byte(`{"sourceDeptId":10,"targetDeptId":20,"includeChildren":false,"expectedCount":0}`)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/gb28181/device-mgmt/permission-workbench/assignments/departments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code)
}

func TestPermissionWorkbench_GrantQueryEndpoint(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	registerPermissionWorkbenchRoutes(r, db)
	active := int8(1)
	department := basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 10}, Name: "安保部", Status: &active}
	require.NoError(t, db.Create(&department).Error)
	device := gbmodels.GbDevice{DeviceID: "grant-query", Name: "共享设备", OwnerDeptID: 10}
	require.NoError(t, db.Create(&device).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceGrant{DeviceID: device.ID, TargetType: gbmodels.GrantTargetTypeDept, TargetID: department.ID}).Error)
	body, err := json.Marshal(map[string]any{"deviceIds": []uint{device.ID, 999}})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/gb28181/device-mgmt/permission-workbench/grants/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	data := unmarshal(t, w)["data"].(map[string]any)
	devices := data["devices"].([]any)
	require.Len(t, devices, 1)
	require.NotEmpty(t, devices[0].(map[string]any)["revision"])
	grants := devices[0].(map[string]any)["grants"].([]any)
	require.Equal(t, "安保部", grants[0].(map[string]any)["targetName"])
	require.Equal(t, []any{float64(999)}, data["unavailableIds"])
}

func TestPermissionWorkbench_GrantTargetsUseTrustedScope(t *testing.T) {
	r, db := newDeviceMgmtRouter(t, withClaims(100))
	registerPermissionWorkbenchRoutes(r, db)
	active := int8(1)
	require.NoError(t, db.Create(&[]basemodels.SysDepartment{
		{BaseModel: basemodels.BaseModel{ID: 10}, Name: "可见部门", Status: &active},
		{BaseModel: basemodels.BaseModel{ID: 20}, Name: "越权部门", Status: &active},
	}).Error)
	require.NoError(t, db.Create(&[]basemodels.User{
		{BaseModel: basemodels.BaseModel{ID: 100}, Username: "operator", Password: "x", Status: 1, DeptID: 10},
		{BaseModel: basemodels.BaseModel{ID: 101}, Username: "guard-inside", Password: "x", Status: 1, DeptID: 10},
		{BaseModel: basemodels.BaseModel{ID: 102}, Username: "guard-outside", Password: "x", Status: 1, DeptID: 20},
	}).Error)
	role := basemodels.SysRole{Name: "本部门", Status: 1, DataScope: 3}
	require.NoError(t, db.Create(&role).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: 100, RoleID: role.ID}).Error)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/gb28181/device-mgmt/permission-workbench/grant-targets?type=user&q=guard&page=1&pageSize=50", nil))
	require.Equal(t, http.StatusOK, w.Code)
	data := unmarshal(t, w)["data"].(map[string]any)
	require.EqualValues(t, 1, data["total"])
	list := data["list"].([]any)
	require.Len(t, list, 1)
	require.Equal(t, "guard-inside", list[0].(map[string]any)["name"])
}
