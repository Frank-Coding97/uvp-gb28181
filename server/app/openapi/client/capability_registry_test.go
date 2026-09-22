package client

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
)

func TestCapabilityCatalogReusesSysAPITitlesAndGroups(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&appmodels.SysApi{}))
	rows := make([]appmodels.SysApi, 0, len(capabilityDefinitions))
	for index, definition := range capabilityDefinitions {
		rows = append(rows, appmodels.SysApi{
			BaseModel: appmodels.BaseModel{ID: uint(index + 1)},
			Title:     "标题 " + definition.scope,
			Path:      definition.internalPath,
			Method:    definition.method,
			ApiGroup:  capabilityGroupNames[definition.groupCode],
		})
	}
	require.NoError(t, db.Create(&rows).Error)

	catalog, err := CapabilityCatalog(context.Background(), db)
	require.NoError(t, err)
	require.Len(t, catalog, 3)
	require.Equal(t, "device-control", catalog[0].Code)
	require.Equal(t, "设备控制", catalog[0].Name)
	require.Equal(t, "ptz:operation:read", catalog[0].Capabilities[0].Scope)
	require.Equal(t, "device-management", catalog[1].Code)
	require.Equal(t, "设备管理", catalog[1].Name)
	require.Equal(t, "channel:detail", catalog[1].Capabilities[0].Scope)
	require.Equal(t, "标题 channel:detail", catalog[1].Capabilities[0].Name)
	require.Equal(t, "/openapi/v1/devices/{deviceId}/channels/{channelId}", catalog[1].Capabilities[0].ExternalPath)
	require.Equal(t, "playback", catalog[2].Code)
}

func TestCapabilityCatalogFailsClosedWhenInternalAssetDrifts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&appmodels.SysApi{}))
	definition := capabilityDefinitions[0]
	require.NoError(t, db.Create(&appmodels.SysApi{BaseModel: appmodels.BaseModel{ID: 1}, Title: "设备列表", Path: definition.internalPath, Method: definition.method, ApiGroup: "错误分组"}).Error)
	_, err = CapabilityCatalog(context.Background(), db)
	require.ErrorIs(t, err, ErrCapabilityDrift)
}
