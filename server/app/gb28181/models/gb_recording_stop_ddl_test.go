package models_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloudRecordingStopPermissionCoversAllDatabasesAndFreshSeeds(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, path := range []string{
		"resource/database/gb28181/migrations/2026-08-29-cloud-recording-stop.sql",
		"resource/database/gb28181/migrations/2026-08-29-cloud-recording-stop-postgresql.sql",
		"resource/database/gb28181/migrations/2026-08-29-cloud-recording-stop-sqlserver.sql",
		"resource/database/uvp-gb28181.sql",
		"resource/database/postgresql_converted.sql",
		"resource/database/sqlserver_converted.sql",
	} {
		body := readNormalizedAlarmManagementDDL(t, root, path)
		for _, token := range []string{
			"gb28181:recording:stop", "/api/gb28181/cloud-recordings/active/:id/stop", "post",
			"sys_menu_api", "sys_role_menu", "sys_casbin_rule", "role_1", "not exists",
		} {
			require.Contains(t, body, token, path)
		}
	}
}
