package models

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecordingPlanDDLContractAcrossSupportedDatabases(t *testing.T) {
	_, sourceFile, _, _ := runtime.Caller(0)
	serverRoot := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..")
	migrationDir := filepath.Join(serverRoot, "resource", "database", "gb28181", "migrations")
	files := []string{
		"2026-08-29-recording-plan.sql",
		"2026-08-29-recording-plan-postgresql.sql",
		"2026-08-29-recording-plan-sqlserver.sql",
	}
	want := []string{
		"gb_recording_plan", "gb_recording_plan_period", "gb_recording_plan_binding",
		"gb_recording_plan_channel_state", "gb_recording_plan_execution", "gb_recording_plan_gap",
		"recording_mode", "reconcile_at", "lease_owner", "lease_until", "state_version",
		"uk_recording_plan_binding_channel", "idx_recording_plan_state_reconcile",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(migrationDir, name))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			for _, token := range want {
				require.Containsf(t, text, token, "%s missing %s", name, token)
			}
			for _, forbidden := range []string{" json", " enum("} {
				require.NotContainsf(t, text, forbidden, "%s uses non-portable type %s", name, forbidden)
			}
		})
	}
}

func TestRecordingPlanFreshSchemasContainDomainTables(t *testing.T) {
	_, sourceFile, _, _ := runtime.Caller(0)
	serverRoot := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..")
	files := []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", name))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			for _, token := range []string{
				"gb_recording_plan", "gb_recording_plan_period", "gb_recording_plan_binding",
				"gb_recording_plan_channel_state", "gb_recording_plan_execution", "gb_recording_plan_gap",
				"recording_mode",
			} {
				require.Containsf(t, text, token, "%s missing %s", name, token)
			}
		})
	}
}

func TestRecordingPlanDownMigrationsExist(t *testing.T) {
	_, sourceFile, _, _ := runtime.Caller(0)
	serverRoot := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..")
	migrationDir := filepath.Join(serverRoot, "resource", "database", "gb28181", "migrations")
	for _, name := range []string{
		"2026-08-29-recording-plan-down.sql",
		"2026-08-29-recording-plan-postgresql-down.sql",
		"2026-08-29-recording-plan-sqlserver-down.sql",
	} {
		body, err := os.ReadFile(filepath.Join(migrationDir, name))
		require.NoError(t, err)
		text := strings.ToLower(string(body))
		require.Contains(t, text, "gb_recording_plan_channel_state")
		require.Contains(t, text, "recording_mode")
		require.NotContains(t, text, "gb_recording_file")
		require.NotContains(t, text, "gb_recording_session")
	}
}
