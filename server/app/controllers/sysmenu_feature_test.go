package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/models"
)

func TestFilterDisabledFeatureMenusRemovesTraceMenuAndChildren(t *testing.T) {
	menus := models.SysMenuList{
		{BaseModel: models.BaseModel{ID: 10}, Path: "/gb28181", Title: "国标平台"},
		{BaseModel: models.BaseModel{ID: 11}, ParentID: 10, Path: "/gb28181/sip-traces", Title: "SIP 日志"},
		{BaseModel: models.BaseModel{ID: 12}, ParentID: 11, Path: "/gb28181/sip-traces/detail", Title: "报文详情"},
		{BaseModel: models.BaseModel{ID: 13}, ParentID: 10, Path: "/gb28181/device-mgmt", Title: "设备管理"},
	}

	filtered := filterDisabledFeatureMenus(menus, map[string]bool{"/gb28181/sip-traces": true})
	require.Len(t, filtered, 2)
	require.Equal(t, uint(10), filtered[0].ID)
	require.Equal(t, uint(13), filtered[1].ID)
	require.Len(t, filterDisabledFeatureMenus(menus, nil), 4)
}
