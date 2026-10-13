package integration

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

func readInitializationContractSQL(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
	require.NoError(t, err)
	return string(body)
}

func TestSQLServerInitializationBatchVariablesAreUnique(t *testing.T) {
	body := stripInitializationSQLCommentsAndLiterals(readInitializationContractSQL(t, "sqlserver_converted.sql"))
	declarations := regexp.MustCompile(`(?im)^\s*DECLARE\s+(@[a-z0-9_]+)\b`).FindAllStringSubmatch(body, -1)
	seen := map[string]bool{}
	for _, declaration := range declarations {
		name := strings.ToLower(declaration[1])
		require.False(t, seen[name], "SQL Server batch variables collide under default case-insensitive collation: %s", name)
		seen[name] = true
	}
}

func TestSQLServerInitializationCreatesPermissionTablesBeforeSeeds(t *testing.T) {
	body := readInitializationContractSQL(t, "sqlserver_converted.sql")
	for _, table := range []string{"sys_menu", "sys_menu_api", "sys_role_menu"} {
		create := strings.Index(body, "CREATE TABLE ["+table+"]")
		firstInsert := regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+\[?` + table + `\]?\s`).FindStringIndex(body)
		require.GreaterOrEqual(t, create, 0, table)
		require.NotNil(t, firstInsert, table)
		require.Greater(t, firstInsert[0], create, "fresh initialization must create %s before its first seed", table)
	}
}

func TestMySQLInitializationPTZTableUsesSingleStatementTerminator(t *testing.T) {
	body := readInitializationContractSQL(t, "uvp-gb28181.sql")
	match := regexp.MustCompile("(?s)CREATE TABLE `gb_ptz_state` \\(.*?\\n\\) ENGINE=.*?;").FindString(body)
	require.NotEmpty(t, match)
	require.Equal(t, 1, strings.Count(match, ";"), match)
	require.True(t, strings.HasSuffix(match, ";"), match)
}

func TestPostgreSQLInitializationQuotesSysJobsGroupIdentifier(t *testing.T) {
	body := readInitializationContractSQL(t, "postgresql_converted.sql")

	require.Contains(t, body, "  \"group\" VARCHAR(100) NOT NULL,")
	require.Contains(t, body, `COMMENT ON COLUMN "sys_jobs"."group" IS '任务分组名称';`)
	require.NotContains(t, body, "\n    group VARCHAR(100) NOT NULL,")
	require.NotContains(t, body, "COMMENT ON COLUMN sys_jobs.group IS")
}

func TestPostgreSQLInitializationRestoresJobResultsForeignKey(t *testing.T) {
	body := readInitializationContractSQL(t, "postgresql_converted.sql")

	require.Contains(t, body, `CONSTRAINT "sys_job_results_ibfk_1" FOREIGN KEY ("job_id") REFERENCES "sys_jobs" ("id") ON DELETE CASCADE ON UPDATE CASCADE`)
	require.NotContains(t, body, "CONSTRAINT TEXT")
}

func TestPostgreSQLInitializationDoesNotContainRemovedTenantSchema(t *testing.T) {
	body := readInitializationContractSQL(t, "postgresql_converted.sql")

	for _, removedToken := range []string{"sys_tenants", "sys_user_tenant", "tenant_id", "platform_domain", "menu_permission"} {
		require.NotContains(t, body, removedToken, "removed tenant schema token %q", removedToken)
	}
}

func TestPostgreSQLInitializationMatchesIntegerFlagModels(t *testing.T) {
	body := readInitializationContractSQL(t, "postgresql_converted.sql")
	int8Type := reflect.TypeOf(int8(0))
	for _, model := range []struct {
		name   string
		value  any
		fields []string
	}{
		{name: "SysMenu", value: basemodels.SysMenu{}, fields: []string{"IsFull", "Hide", "Disable", "KeepAlive", "Affix", "IsLink", "Iframe"}},
		{name: "SysDepartment", value: basemodels.SysDepartment{}, fields: []string{"Status"}},
		{name: "SysDict", value: basemodels.SysDict{}, fields: []string{"Status"}},
		{name: "SysDictItem", value: basemodels.SysDictItem{}, fields: []string{"Status"}},
		{name: "User", value: basemodels.User{}, fields: []string{"Status"}},
	} {
		typ := reflect.TypeOf(model.value)
		for _, fieldName := range model.fields {
			field, ok := typ.FieldByName(fieldName)
			require.True(t, ok, "%s.%s must exist", model.name, fieldName)
			fieldType := field.Type
			if fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			require.Equal(t, int8Type, fieldType, "%s.%s must remain int8-backed", model.name, fieldName)
		}
	}

	for _, table := range []struct {
		name        string
		definitions []string
	}{
		{name: "sys_menu", definitions: []string{
			"is_full SMALLINT DEFAULT 0,", "hide SMALLINT DEFAULT 0,", "disable SMALLINT DEFAULT 0,",
			"keep_alive SMALLINT DEFAULT 0,", "affix SMALLINT DEFAULT 0,", "iframe SMALLINT DEFAULT 0,",
			"is_link SMALLINT DEFAULT 0,",
		}},
		{name: "sys_department", definitions: []string{"status SMALLINT,"}},
		{name: "sys_dict", definitions: []string{"status SMALLINT,"}},
		{name: "sys_dict_item", definitions: []string{"status SMALLINT,"}},
		{name: "sys_users", definitions: []string{"status SMALLINT DEFAULT 1,"}},
	} {
		section := postgresCreateTableSection(t, body, table.name)
		for _, definition := range table.definitions {
			require.Contains(t, section, definition, "%s must use %s", table.name, definition)
		}
		require.NotContains(t, section, "BOOLEAN", "%s must match its integer-backed Go model", table.name)
	}

	legacyBooleanAssignment := regexp.MustCompile(`(?i)\b(?:is_full|hide|disable|keep_alive|affix|is_link|iframe)\s*=\s*(?:true|false)\b`)
	for _, statement := range strings.Split(body, ";") {
		lowerStatement := strings.ToLower(statement)
		if strings.Contains(lowerStatement, "insert into sys_menu ") || strings.Contains(lowerStatement, "insert into sys_menu(") {
			require.NotContains(t, lowerStatement, "true", "sys_menu data seed must use integer flags")
			require.NotContains(t, lowerStatement, "false", "sys_menu data seed must use integer flags")
		}
		if strings.Contains(lowerStatement, "update sys_menu ") {
			require.NotRegexp(t, legacyBooleanAssignment, statement, "sys_menu updates must use integer flags")
		}
	}
	normalizedBody := strings.ReplaceAll(body, `"`, "")
	require.Contains(t, strings.ToLower(normalizedBody), "recovery_required boolean not null default false,", "real boolean fields must remain boolean")
	require.Contains(t, strings.ToLower(normalizedBody), "must_auth_locked boolean not null default false,", "OpenAPI security latch must match its bool model")
	require.Contains(t, strings.ToLower(normalizedBody), `constraint ck_openapi_security_state check (((must_auth_locked = false`)
}

func postgresCreateTableSection(t *testing.T, body, table string) string {
	t.Helper()
	marker := `CREATE TABLE "` + table + `" (`
	start := strings.Index(body, marker)
	require.GreaterOrEqual(t, start, 0, "missing CREATE TABLE for %s", table)
	rest := body[start+len(marker):]
	end := strings.Index(rest, "\n);")
	require.GreaterOrEqual(t, end, 0, "unterminated CREATE TABLE for %s", table)
	return strings.ReplaceAll(rest[:end], `"`, "")
}
