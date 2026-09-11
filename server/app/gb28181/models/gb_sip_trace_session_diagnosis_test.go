package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGbSipTraceSessionDiagnosisAutoMigrateContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:gb_sip_trace_session_diagnosis_contract?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&GbSipTraceSessionDiagnosis{}))

	migrator := db.Migrator()
	require.True(t, migrator.HasTable(&GbSipTraceSessionDiagnosis{}))
	for _, column := range []string{
		"id", "session_day", "observed_at", "correlation_key", "state", "category", "code",
		"stage", "source", "device_id", "channel_id", "call_id", "cseq", "method",
		"status_code", "stream_id", "resolved_at",
	} {
		require.Truef(t, migrator.HasColumn(&GbSipTraceSessionDiagnosis{}, column), "missing column %s", column)
	}
	for _, index := range []string{
		"uk_sip_trace_diagnosis_session",
		"idx_sip_trace_diagnosis_category_state_observed",
		"idx_sip_trace_diagnosis_device_observed",
		"idx_sip_trace_diagnosis_call_cseq",
	} {
		require.Truef(t, migrator.HasIndex(&GbSipTraceSessionDiagnosis{}, index), "missing index %s", index)
	}
}

func TestGbSipTraceSessionDiagnosisMigrationDDLContract(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
	upFiles := []string{
		"2026-08-14-sip-trace-session-diagnosis.sql",
		"2026-08-14-sip-trace-session-diagnosis-postgresql.sql",
		"2026-08-14-sip-trace-session-diagnosis-sqlserver.sql",
	}
	columns := []string{
		"id", "session_day", "observed_at", "correlation_key", "state", "category", "code",
		"stage", "source", "device_id", "channel_id", "call_id", "cseq", "method",
		"status_code", "stream_id", "resolved_at",
	}
	indexes := []string{
		"uk_sip_trace_diagnosis_session",
		"idx_sip_trace_diagnosis_category_state_observed",
		"idx_sip_trace_diagnosis_device_observed",
		"idx_sip_trace_diagnosis_call_cseq",
	}
	for _, name := range upFiles {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			require.Contains(t, text, "gb_sip_trace_session_diagnosis")
			for _, column := range columns {
				require.Containsf(t, text, column, "missing column %s", column)
			}
			for _, index := range indexes {
				require.Containsf(t, text, index, "missing index %s", index)
			}
			for _, forbidden := range []string{"json", "enum", "partial index"} {
				require.NotContainsf(t, text, forbidden, "unsupported schema feature %s", forbidden)
			}
		})
	}
}

func TestGbSipTraceSessionDiagnosisDownProtectsNonEmptyTable(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
	cases := []struct {
		name     string
		contains []string
	}{
		{"mysql", []string{"gb_sip_trace_session_diagnosis", "temporary", "select 1 from gb_sip_trace_session_diagnosis"}},
		{"postgresql", []string{"gb_sip_trace_session_diagnosis", "raise exception", "exists (select 1 from gb_sip_trace_session_diagnosis"}},
		{"sqlserver", []string{"gb_sip_trace_session_diagnosis", "throw", "if exists (select 1 from gb_sip_trace_session_diagnosis"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			name := "2026-08-14-sip-trace-session-diagnosis-down.sql"
			if tt.name != "mysql" {
				name = "2026-08-14-sip-trace-session-diagnosis-" + tt.name + "-down.sql"
			}
			body, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			for _, token := range tt.contains {
				require.Containsf(t, text, token, "down migration must protect non-empty data: %s", token)
			}
		})
	}
}
