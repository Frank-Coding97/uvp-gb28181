package database_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMySQLReleaseInitializationContract(t *testing.T) {
	body, err := os.ReadFile("uvp-gb28181.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(body))

	require.Contains(t, sql, "mysql 8.0")
	require.Contains(t, sql, "create table `sys_civil_code`")
	require.Contains(t, sql, "create table `gb_cascade_platform`")
	require.Contains(t, sql, "create table `gb_recording_file`")
	require.Contains(t, sql, "create table `gb_sip_trace_message`")

	for _, forbidden := range []string{
		"create table `demo_students`",
		"create table `demo_teacher`",
		"create table `example`",
		"'demo'",
		"/public/uploads/",
		"18800000006",
		"13800000001",
		"headquarters@company.com",
	} {
		require.NotContains(t, sql, forbidden)
	}

	for _, seeded := range []string{
		"sys_api",
		"sys_casbin_rule",
		"sys_civil_code",
		"sys_dict",
		"sys_dict_item",
		"sys_menu",
		"sys_menu_api",
		"sys_role",
		"sys_role_menu",
		"sys_user_role",
		"sys_users",
	} {
		require.Regexp(t, regexp.MustCompile(`(?m)^insert into `+"`"+seeded+"`"), sql, seeded)
	}

	for _, environmentData := range []string{
		"gb_device",
		"gb_channel",
		"gb_sip_config",
		"gb_cascade_platform",
		"meta_node",
		"sys_operation_logs",
	} {
		require.NotRegexp(t, regexp.MustCompile(`(?m)^insert into `+"`"+environmentData+"`"), sql, environmentData)
	}
}

func TestInitializationDiagnosisSchemaContract(t *testing.T) {
	files := []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"}
	columns := []string{
		"gb_sip_trace_session_diagnosis", "session_day", "observed_at", "correlation_key",
		"state", "category", "code", "stage", "source", "device_id", "channel_id",
		"call_id", "cseq", "method", "status_code", "stream_id", "resolved_at",
		"uk_sip_trace_diagnosis_session", "idx_sip_trace_diagnosis_category_state_observed",
		"idx_sip_trace_diagnosis_device_observed", "idx_sip_trace_diagnosis_call_cseq",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			body, err := os.ReadFile(file)
			require.NoError(t, err)
			text := diagnosisSchemaSection(strings.ToLower(string(body)))
			for _, column := range columns {
				require.Containsf(t, text, column, "fresh schema missing diagnosis field or index %s", column)
			}
			for _, forbidden := range []string{"json", "enum", "partial index"} {
				require.NotContainsf(t, text, forbidden, "fresh schema contains unsupported feature %s", forbidden)
			}
		})
	}
}

func TestPostgreSQLInitializationMenuTypeSupportsDirectoryPageAndButton(t *testing.T) {
	body, err := os.ReadFile("postgresql_converted.sql")
	require.NoError(t, err)
	menuSchema := strings.ToLower(string(body))
	require.Contains(t, menuSchema, "type smallint default 2", "sys_menu.type 必须承载目录/页面/按钮的 1/2/3")
	require.NotContains(t, menuSchema, "type boolean default 2", "BOOLEAN 无法保存页面和按钮类型")
}

func diagnosisSchemaSection(text string) string {
	tableStart := -1
	for _, declaration := range []string{
		"create table `gb_sip_trace_session_diagnosis`",
		"create table gb_sip_trace_session_diagnosis",
		"create table [gb_sip_trace_session_diagnosis]",
	} {
		if candidate := strings.Index(text, declaration); candidate >= 0 && (tableStart < 0 || candidate < tableStart) {
			tableStart = candidate
		}
	}
	if tableStart < 0 {
		return text
	}
	section := text[tableStart:]
	for _, marker := range []string{"\n-- table structure for `", "\ncreate table ", "\nif object_id(n'"} {
		if end := strings.Index(section[1:], marker); end >= 0 {
			section = section[:end+1]
		}
	}
	return section
}
