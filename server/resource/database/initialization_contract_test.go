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
