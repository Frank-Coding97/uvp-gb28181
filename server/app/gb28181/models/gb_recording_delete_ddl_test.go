package models_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloudRecordingDeletePermissionsCoverAllDatabasesAndFreshSeeds(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, path := range []string{
		"resource/database/gb28181/migrations/2026-08-29-cloud-recording-deletion.sql",
		"resource/database/gb28181/migrations/2026-08-29-cloud-recording-deletion-postgresql.sql",
		"resource/database/gb28181/migrations/2026-08-29-cloud-recording-deletion-sqlserver.sql",
		"resource/database/uvp-gb28181.sql",
		"resource/database/postgresql_converted.sql",
		"resource/database/sqlserver_converted.sql",
	} {
		body := readNormalizedAlarmManagementDDL(t, root, path)
		for _, token := range []string{
			"gb28181:recording:delete",
			"/api/gb28181/cloud-recordings/files/:id",
			"/api/gb28181/cloud-recordings/files/batch-delete",
			"delete", "post", "sys_menu_api", "sys_role_menu", "sys_casbin_rule", "role_1", "not exists",
		} {
			require.Contains(t, body, token, path)
		}
	}
}
