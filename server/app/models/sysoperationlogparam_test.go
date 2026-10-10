package models

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSysOperationLogListRequestFiltersPath(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SysOperationLog{}))
	require.NoError(t, db.Create(&[]SysOperationLog{
		{Path: "/api/gb28181/device-mgmt/permission-workbench/assignments"},
		{Path: "/api/gb28181/device-mgmt/permission-workbench/grants/apply"},
		{Path: "/api/gb28181/device-mgmt/devices"},
	}).Error)

	request := SysOperationLogListRequest{Path: "permission-workbench"}
	var logs []SysOperationLog
	require.NoError(t, db.WithContext(context.Background()).Scopes(request.Handle()).Order("id ASC").Find(&logs).Error)
	require.Len(t, logs, 2)
}
