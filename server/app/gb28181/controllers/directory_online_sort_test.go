package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbdirectory "uvplatform.com/uvp-gb28181/app/gb28181/directory"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/response"
)

// 接口契约：目录树上同一目录下的设备按「在线优先」返回。
// 数据库里按 device_id 升序是 D1(离线)、D2(在线)、D3(离线)，接口必须返回 D2、D1、D3。
// ⛔ 别再把这个顺序改回前端排：增量同步（sync=1）按这里的顺序算 children，前端只认接口给的顺序。
func TestDirectoryControllerSortsOnlineDevicesFirst(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbCustomGroup{}, &gbmodels.GbCustomGroupDevice{},
		&basemodels.SysDepartment{}, &basemodels.User{}, &basemodels.SysRole{}, &basemodels.SysUserRole{},
	))
	status := int8(1)
	deptID := uint(10)
	require.NoError(t, db.Create(&basemodels.SysDepartment{
		BaseModel: basemodels.BaseModel{ID: deptID}, Name: "一号库", Status: &status,
	}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{
		{BaseModel: basemodels.BaseModel{ID: 1}, DeviceID: "D1", Name: "离线设备", OwnerDeptID: deptID, Status: gbmodels.DeviceStatusOffline},
		{BaseModel: basemodels.BaseModel{ID: 2}, DeviceID: "D2", Name: "在线设备", OwnerDeptID: deptID, Status: gbmodels.DeviceStatusOnline},
		{BaseModel: basemodels.BaseModel{ID: 3}, DeviceID: "D3", Name: "离线设备二", OwnerDeptID: deptID, Status: gbmodels.DeviceStatusOffline},
	}).Error)

	ctrl := NewDirectoryController(func() *gorm.DB { return db })
	r := gin.New()
	r.GET("/tree", ctrl.Tree)
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/tree?view=department", nil))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var body struct {
		Data struct {
			List []gbdirectory.DirectoryNodeVO `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Len(t, body.Data.List, 1)
	children := body.Data.List[0].Children
	require.Len(t, children, 3)
	require.Equal(t, "D2", children[0].Device.DeviceID, "在线设备排最上边")
	require.Equal(t, "D1", children[1].Device.DeviceID, "同状态内保持原始顺序")
	require.Equal(t, "D3", children[2].Device.DeviceID)
}
