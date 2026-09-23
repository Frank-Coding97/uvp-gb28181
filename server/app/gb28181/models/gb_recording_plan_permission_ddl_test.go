package models_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecordingPlanPermissionMigrationsCoverAllDatabases(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		up := readNormalizedAlarmManagementDDL(t, root, "resource/database/gb28181/migrations/2026-08-29-recording-plan-permissions"+suffix+".sql")
		for _, token := range []string{
			"/api/gb28181/recording-plans", "/api/gb28181/recording-plans/:id/assignments",
			"/api/gb28181/recording-plans/channels/:channelid/recording-mode",
			"/api/gb28181/recording-plans/:id/channels", "/api/gb28181/recording-plans/channels/:channelid/diagnosis",
			"gb28181:recording-plan:view", "gb28181:recording-plan:maintain", "gb28181:recording-plan:assign",
			"sys_api", "sys_menu_api", "sys_role_menu", "sys_casbin_rule", "not exists",
		} {
			require.Contains(t, up, token, suffix+" up")
		}

		down := readNormalizedAlarmManagementDDL(t, root, "resource/database/gb28181/migrations/2026-08-29-recording-plan-permissions"+suffix+"-down.sql")
		for _, token := range []string{"sys_api", "sys_menu_api", "sys_role_menu", "sys_casbin_rule", "/api/gb28181/recording-plans"} {
			require.Contains(t, down, token, suffix+" down")
		}
	}
}
