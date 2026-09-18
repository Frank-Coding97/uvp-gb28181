package controllers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

func TestOpenAPIAdminOwnerDepartmentOptionsFollowCurrentScope(t *testing.T) {
	db, ctrl, permission := newAdminHTTP(t)
	active, inactive, parent := int8(1), int8(0), uint(10)
	child := basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 11}, Name: "child-private", Status: &active, ParentID: &parent}
	disabled := basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 30}, Name: "disabled-private", Status: &inactive}
	deleted := basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 40}, Name: "deleted-private", Status: &active}
	require.NoError(t, db.Create(&child).Error)
	require.NoError(t, db.Create(&disabled).Error)
	require.NoError(t, db.Create(&deleted).Error)
	require.NoError(t, db.Delete(&deleted).Error)
	for _, query := range []string{"", "?ownerDeptId=20", "?page=2&pageSize=1"} {
		out, _ := adminRequest(t, ctrl, 7, "list", "GET", "/api/gb28181/openapi-clients"+query, "")
		require.Equal(t, 200, out.Code)
		var body struct {
			Data struct {
				Departments []struct {
					ID   uint   `json:"id"`
					Name string `json:"name"`
				} `json:"ownerDepartments"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(out.Body.Bytes(), &body))
		require.Len(t, body.Data.Departments, 1)
		require.EqualValues(t, 10, body.Data.Departments[0].ID)
		require.Equal(t, "A", body.Data.Departments[0].Name)
		require.NotContains(t, out.Body.String(), "private")
	}
	require.NoError(t, db.Model(&basemodels.SysRole{}).Where("id = 1").Update("data_scope", 1).Error)
	out, _ := adminRequest(t, ctrl, 7, "list", "GET", "/api/gb28181/openapi-clients", "")
	require.Equal(t, 200, out.Code)
	require.Contains(t, out.Body.String(), "child-private")
	require.NotContains(t, out.Body.String(), "disabled-private")
	require.NotContains(t, out.Body.String(), "deleted-private")
	permission.denied = true
	out, _ = adminRequest(t, ctrl, 7, "list", "GET", "/api/gb28181/openapi-clients", "")
	require.Equal(t, 403, out.Code)
	require.NotContains(t, out.Body.String(), "ownerDepartments")
}
