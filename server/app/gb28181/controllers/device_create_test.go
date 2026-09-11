package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

// T13 RED-1: 复现修复——创建的设备归属创建人部门(修复前恒为 0)
func TestCreateDevice_OwnerIsCreatorDept(t *testing.T) {
	r, db := newDeviceMgmtRouter(t, withClaims(100))
	seedDeptScopedUser(t, db, 100, 10)

	body := `{"deviceId":"34020000002000000021","name":"手动创建测试机"}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/gb28181/device-mgmt/device", strings.NewReader(body)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var device gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", "34020000002000000021").First(&device).Error)
	assert.EqualValues(t, 10, device.OwnerDeptID, "归属应为创建人部门")
	assert.EqualValues(t, 100, device.CreatedBy)
}

// T13 RED-2: 创建人部门停用 → 拒绝创建
func TestCreateDevice_DeptDisabledRejected(t *testing.T) {
	r, db := newDeviceMgmtRouter(t, withClaims(100))
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.Model(&basemodels.SysDepartment{}).Where("id = ?", 10).Update("status", 0).Error)

	body := `{"deviceId":"34020000002000000022","name":"停用部门创建"}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/gb28181/device-mgmt/device", strings.NewReader(body)))
	assert.NotEqual(t, http.StatusOK, w.Code, w.Body.String())

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", "34020000002000000022").Count(&count).Error)
	assert.Zero(t, count, "不应创建设备")
}

// T13 RED-3: 无用户身份 → 拒绝创建
func TestCreateDevice_NoClaimsRejected(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)

	body := `{"deviceId":"34020000002000000023","name":"无身份创建"}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/gb28181/device-mgmt/device", strings.NewReader(body)))
	assert.NotEqual(t, http.StatusOK, w.Code, w.Body.String())

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", "34020000002000000023").Count(&count).Error)
	assert.Zero(t, count)
}
