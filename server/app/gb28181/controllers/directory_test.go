package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	gbdirectory "uvplatform.com/uvp-gb28181/app/gb28181/directory"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/civilcode"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/response"
)

func TestDirectoryControllerDepartmentViewUsesVisibleDepartmentTreeAndScopesSelection(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbCustomGroup{}, &gbmodels.GbCustomGroupDevice{},
		&basemodels.SysDepartment{}, &basemodels.User{}, &basemodels.SysRole{}, &basemodels.SysUserRole{},
	))
	status := int8(1)
	sortFirst, sortSecond := 1, 2
	dept10, dept11, dept12, dept20 := uint(10), uint(11), uint(12), uint(20)
	require.NoError(t, db.Create(&[]basemodels.SysDepartment{
		{BaseModel: basemodels.BaseModel{ID: dept10}, Name: "研发部", Status: &status, Sort: &sortSecond},
		{BaseModel: basemodels.BaseModel{ID: dept11}, Name: "平台组", ParentID: &dept10, Status: &status, Sort: &sortFirst},
		{BaseModel: basemodels.BaseModel{ID: dept12}, Name: "空部门", ParentID: &dept10, Status: &status},
		{BaseModel: basemodels.BaseModel{ID: dept20}, Name: "运维部", Status: &status, Sort: &sortFirst},
		{BaseModel: basemodels.BaseModel{ID: 30}, Name: "已删除部门", Status: &status},
	}).Error)
	require.NoError(t, db.Model(&basemodels.SysDepartment{}).Where("id = ?", 30).Update("deleted_at", time.Now()).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{
		{BaseModel: basemodels.BaseModel{ID: 1}, DeviceID: "D10", Name: "研发设备", OwnerDeptID: dept10, Status: gbmodels.DeviceStatusOnline},
		{BaseModel: basemodels.BaseModel{ID: 2}, DeviceID: "D11", Name: "平台设备", OwnerDeptID: dept11, Status: gbmodels.DeviceStatusOffline},
		{BaseModel: basemodels.BaseModel{ID: 3}, DeviceID: "D20", Name: "越权设备", OwnerDeptID: dept20, Status: gbmodels.DeviceStatusOnline},
		{BaseModel: basemodels.BaseModel{ID: 4}, DeviceID: "D0", Name: "未分配设备", OwnerDeptID: 0, Status: gbmodels.DeviceStatusOffline},
		{BaseModel: basemodels.BaseModel{ID: 5}, DeviceID: "D99", Name: "部门缺失设备", OwnerDeptID: 99, Status: gbmodels.DeviceStatusOffline},
		{BaseModel: basemodels.BaseModel{ID: 6}, DeviceID: "D30", Name: "已删除部门设备", OwnerDeptID: 30, Status: gbmodels.DeviceStatusOffline},
	}).Error)
	require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: 7}, Username: "department-user", Password: "test", Status: 1, DeptID: dept10}).Error)
	require.NoError(t, db.Create(&basemodels.SysRole{BaseModel: basemodels.BaseModel{ID: 8}, Name: "部门范围", Status: 1, DataScope: 2, CheckedDepts: "10,11,12"}).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: 7, RoleID: 8}).Error)

	ctrl := NewDirectoryController(func() *gorm.DB { return db })
	r := gin.New()
	r.GET("/tree", ctrl.Tree)
	fullResponse := httptest.NewRecorder()
	r.ServeHTTP(fullResponse, httptest.NewRequest(http.MethodGet, "/tree?view=department&sync=1", nil))
	require.Equal(t, http.StatusOK, fullResponse.Code, fullResponse.Body.String())
	var fullBody struct {
		Data struct {
			List []gbdirectory.DirectoryNodeVO `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fullResponse.Body.Bytes(), &fullBody))
	require.Len(t, fullBody.Data.List, 3, "two sorted departments and the unassigned bucket")
	require.Equal(t, "department:dept:20", fullBody.Data.List[0].Key, "department sort order should follow sys_department")
	require.Equal(t, "department:dept:10", fullBody.Data.List[1].Key)
	require.Equal(t, "运维部", fullBody.Data.List[0].Name)
	require.Equal(t, "department:unassigned", fullBody.Data.List[2].Key)
	require.Equal(t, 3, fullBody.Data.List[2].Count)
	fullUnassigned, err := json.Marshal(fullBody.Data.List[2].Children)
	require.NoError(t, err)
	require.Contains(t, string(fullUnassigned), "D0")
	require.Contains(t, string(fullUnassigned), "D99")
	require.Contains(t, string(fullUnassigned), "D30")

	scoped := gin.New()
	scoped.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
		c.Next()
	})
	scoped.GET("/tree", ctrl.Tree)
	scopedResponse := httptest.NewRecorder()
	scoped.ServeHTTP(scopedResponse, httptest.NewRequest(http.MethodGet, "/tree?view=department&sync=1", nil))
	require.Equal(t, http.StatusOK, scopedResponse.Code, scopedResponse.Body.String())
	var scopedBody struct {
		Data struct {
			List     []gbdirectory.DirectoryNodeVO `json:"list"`
			Revision string                        `json:"revision"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(scopedResponse.Body.Bytes(), &scopedBody))
	require.Len(t, scopedBody.Data.List, 1)
	dept := scopedBody.Data.List[0]
	require.Equal(t, "department:dept:10", dept.Key)
	require.Equal(t, 2, dept.Count)
	require.Len(t, dept.Children, 3)
	require.Equal(t, "department:dept:11", dept.Children[0].Key)
	require.Equal(t, 1, dept.Children[0].Count)
	require.Equal(t, "department:dept:12", dept.Children[1].Key)
	require.Equal(t, 0, dept.Children[1].Count, "visible empty departments stay in the tree")
	require.Equal(t, "D10", dept.Children[2].Device.DeviceID)
	require.Empty(t, dept.Children[2].Children, "device leaves should not inherit sibling channels or devices")
	encoded, err := json.Marshal(scopedBody.Data.List)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "D20")
	require.NotContains(t, string(encoded), "D0", "restricted users cannot discover an unassigned device outside their data scope")
	require.NotContains(t, string(encoded), "D99")
	require.NotContains(t, string(encoded), "D30")

	filteredResponse := httptest.NewRecorder()
	scoped.ServeHTTP(filteredResponse, httptest.NewRequest(http.MethodGet, "/tree?view=department&assetKind=device&q=平台设备", nil))
	require.Equal(t, http.StatusOK, filteredResponse.Code, filteredResponse.Body.String())
	var filteredBody struct {
		Data struct {
			List []gbdirectory.DirectoryNodeVO `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(filteredResponse.Body.Bytes(), &filteredBody))
	filteredEncoded, err := json.Marshal(filteredBody.Data.List)
	require.NoError(t, err)
	require.Contains(t, string(filteredEncoded), "D11")
	require.NotContains(t, string(filteredEncoded), "D10")
	require.NotContains(t, string(filteredEncoded), "department:dept:12")

	statusResponse := httptest.NewRecorder()
	scoped.ServeHTTP(statusResponse, httptest.NewRequest(http.MethodGet, "/tree?view=department&assetKind=device&status=online", nil))
	require.Equal(t, http.StatusOK, statusResponse.Code, statusResponse.Body.String())
	var statusBody struct {
		Data struct {
			List []gbdirectory.DirectoryNodeVO `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(statusResponse.Body.Bytes(), &statusBody))
	filteredEncoded, err = json.Marshal(statusBody.Data.List)
	require.NoError(t, err)
	require.Contains(t, string(filteredEncoded), "D10")
	require.NotContains(t, string(filteredEncoded), "D11")

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
	ctx.Request = httptest.NewRequest(http.MethodGet, "/devices?directoryView=department&directoryKey=department:dept:10", nil)
	scope, err := resolveDirectoryDeviceScope(ctx, db)
	require.NoError(t, err)
	require.ElementsMatch(t, []uint{1, 2}, scope.IDs, "a department selection includes descendant department devices")
	require.ElementsMatch(t, []string{"D10", "D11"}, scope.Codes)
	ctx, _ = gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
	ctx.Request = httptest.NewRequest(http.MethodGet, "/devices?directoryView=department&directoryKey=department:dept:12", nil)
	emptyScope, err := resolveDirectoryDeviceScope(ctx, db)
	require.NoError(t, err)
	require.Empty(t, emptyScope.IDs, "an empty but visible department must produce an empty resource scope")

	for _, key := range []string{"department:dept:20", "department:dept:999", "department:dept:bad"} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
		ctx.Request = httptest.NewRequest(http.MethodGet, "/devices?directoryView=department&directoryKey="+key, nil)
		filteredScope, err := resolveDirectoryDeviceScope(ctx, db)
		require.Error(t, err, "invalid or unauthorized department keys must fail closed: %s", key)
		require.Nil(t, filteredScope)
	}

	require.NotEmpty(t, scopedBody.Data.Revision)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("id = ?", 2).Update("name", "平台摄像机").Error)
	renameResponse := httptest.NewRecorder()
	scoped.ServeHTTP(renameResponse, httptest.NewRequest(http.MethodGet, "/tree?view=department&sync=1&since="+scopedBody.Data.Revision, nil))
	require.Equal(t, http.StatusOK, renameResponse.Code, renameResponse.Body.String())
	var renameSync struct {
		Data gbdirectory.TreeSync `json:"data"`
	}
	require.NoError(t, json.Unmarshal(renameResponse.Body.Bytes(), &renameSync))
	require.False(t, renameSync.Data.Reset)
	renamedLeaf := false
	for _, upsert := range renameSync.Data.Upserts {
		if upsert.Node.Key == "department:device:11:2" {
			renamedLeaf = true
			require.Equal(t, "平台摄像机", upsert.Node.Device.Name)
		}
	}
	require.True(t, renamedLeaf, "a device rename should upsert the department directory leaf")

	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("id = ?", 1).Update("owner_dept_id", 11).Error)
	ownerChangeResponse := httptest.NewRecorder()
	scoped.ServeHTTP(ownerChangeResponse, httptest.NewRequest(http.MethodGet, "/tree?view=department&sync=1&since="+renameSync.Data.Revision, nil))
	require.Equal(t, http.StatusOK, ownerChangeResponse.Code, ownerChangeResponse.Body.String())
	var ownerChangeSync struct {
		Data gbdirectory.TreeSync `json:"data"`
	}
	require.NoError(t, json.Unmarshal(ownerChangeResponse.Body.Bytes(), &ownerChangeSync))
	require.False(t, ownerChangeSync.Data.Reset)
	require.Contains(t, ownerChangeSync.Data.Removed, "department:device:10:1")
	movedLeaf := false
	for _, upsert := range ownerChangeSync.Data.Upserts {
		if upsert.Node.Key == "department:device:11:1" {
			movedLeaf = true
		}
		if upsert.Node.Key == "department:dept:11" {
			require.Equal(t, 2, upsert.Node.Count)
		}
	}
	require.True(t, movedLeaf, "changing owner department should remove the old leaf and upsert it under the new department")

	ctx, _ = gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/devices?directoryView=department&directoryKey=department:unassigned", nil)
	unassignedScope, err := resolveDirectoryDeviceScope(ctx, db)
	require.NoError(t, err)
	require.ElementsMatch(t, []uint{4, 5, 6}, unassignedScope.IDs, "unassigned and stale department owners remain discoverable without broadening department selection")
}

func TestDirectoryControllerViews(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbCatalogNode{}, &gbmodels.GbChannel{}, &gbmodels.GbAlarmResource{}, &gbmodels.GbCustomGroup{}, &gbmodels.GbCustomGroupDevice{}, &civilcode.SysCivilCode{}, &basemodels.SysDepartment{}))
	require.NoError(t, db.Create(&civilcode.SysCivilCode{Code: "370000", ShortName: "山东省", Level: 1}).Error)
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 10}, Name: "研发部"}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "37000000001180000001", OwnerDeptID: 10}).Error)
	ctrl := NewDirectoryController(func() *gorm.DB { return db })
	r := gin.New()
	r.GET("/tree", ctrl.Tree)
	for _, view := range []string{"national", "administrative", "business", "custom"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tree?view="+view, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"list"`)
	}
	for _, query := range []string{"", "?view=foo"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tree"+query, nil))
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
}

func TestDirectoryControllerCustomUngroupedNodesAreDeptScopedAndReadable(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbCustomGroup{}, &gbmodels.GbCustomGroupDevice{}, &basemodels.SysDepartment{}))
	require.NoError(t, db.Create(&[]basemodels.SysDepartment{
		{BaseModel: basemodels.BaseModel{ID: 10}, Name: "研发部"},
		{BaseModel: basemodels.BaseModel{ID: 20}, Name: "运维部"},
	}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{
		{DeviceID: "D10", OwnerDeptID: 10},
		{DeviceID: "D20", OwnerDeptID: 20},
	}).Error)
	ctrl := NewDirectoryController(func() *gorm.DB { return db })
	r := gin.New()
	r.GET("/tree", ctrl.Tree)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tree?view=custom", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"key":"custom:ungrouped:10"`)
	require.Contains(t, w.Body.String(), `"name":"未分组（研发部）"`)
	require.Contains(t, w.Body.String(), `"key":"custom:ungrouped:20"`)
	require.Contains(t, w.Body.String(), `"name":"未分组（运维部）"`)
}

func TestDirectoryControllerResourceSync(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbCatalogNode{}, &gbmodels.GbCustomGroup{}, &gbmodels.GbCustomGroupDevice{}, &basemodels.SysDepartment{}))
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{
		{BaseModel: basemodels.BaseModel{ID: 1}, DeviceID: "D1", Name: "Door", OwnerDeptID: 10, Status: gbmodels.DeviceStatusOnline},
		{BaseModel: basemodels.BaseModel{ID: 2}, DeviceID: "D2", Name: "Yard", OwnerDeptID: 10, Status: gbmodels.DeviceStatusOffline},
	}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbChannel{
		{ChannelID: "C1", DeviceID: "D1", Name: "Gate", OwnerDeptID: 10, Status: gbmodels.ChannelStatusOffline},
		{ChannelID: "C2", DeviceID: "D2", Name: "Hall", OwnerDeptID: 10, Status: gbmodels.ChannelStatusOnline},
	}).Error)
	var hall gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", "C2").First(&hall).Error)
	require.NoError(t, db.Create(&gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeChannel, ChannelID: &hall.ID}).Error)
	var catalog gbmodels.GbCatalogNode
	require.NoError(t, db.First(&catalog).Error)
	businessTree := []gbdirectory.DirectoryNodeVO{{Key: "group", Children: []gbdirectory.DirectoryNodeVO{
		{Key: fmt.Sprintf("business:catalog:%d", catalog.ID), Type: "channel", OnlineCount: 1, Count: 1},
	}}}
	for _, query := range []string{"assetKind=channel&q=Hall&status=online", "assetKind=device&q=Yard&status=offline"} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodGet, "/tree?view=business&"+query, nil)
		filtered, err := filterDirectoryResources(ctx, db, businessTree)
		require.NoError(t, err)
		require.Len(t, filtered, 1)
		require.Equal(t, 1, filtered[0].Count)
	}
	ctrl := NewDirectoryController(func() *gorm.DB { return db })
	r := gin.New()
	r.GET("/tree", ctrl.Tree)
	get := func(query string) map[string]any {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tree?view=custom&sync=1"+query, nil))
		require.Equal(t, 200, w.Code, w.Body.String())
		var result struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		return result.Data
	}
	first := get("")
	rev := first["revision"].(string)
	same := get("&since=" + rev)
	require.Equal(t, false, same["reset"])
	require.Empty(t, same["upserts"])
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("id = ?", 1).Update("status", gbmodels.DeviceStatusOffline).Error)
	delta := get("&since=" + rev)
	require.Equal(t, false, delta["reset"])
	require.NotEmpty(t, delta["upserts"])
	filtered := get("&assetKind=channel&q=Hall&status=online&since=" + rev)
	require.Equal(t, true, filtered["reset"])
	encoded, _ := json.Marshal(filtered["list"])
	require.Contains(t, string(encoded), `"deviceId":"D2"`)
	require.NotContains(t, string(encoded), `"deviceId":"D1"`)
	deviceFiltered := get("&assetKind=device&q=Door&status=offline")
	encoded, _ = json.Marshal(deviceFiltered["list"])
	require.Contains(t, string(encoded), `"deviceId":"D1"`)
	require.NotContains(t, string(encoded), `"deviceId":"D2"`)
	empty := get("&assetKind=channel&q=missing")
	require.Empty(t, empty["list"])
}
