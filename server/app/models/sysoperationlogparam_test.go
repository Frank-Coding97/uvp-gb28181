package models

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func TestSysOperationLogListRequestFiltersPath(t *testing.T) {
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "system.db"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	_, err = sqlitebootstrap.Initialize(ctx, db)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
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
