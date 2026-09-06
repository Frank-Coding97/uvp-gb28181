package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAPIClientMenuIsIncludedAfterPermissionsInFullInitialization(t *testing.T) {
	for _, tc := range []struct {
		name           string
		initialization string
		migration      string
	}{
		{name: "mysql", initialization: "uvp-gb28181.sql", migration: "2026-09-06-openapi-client-menu.sql"},
		{name: "postgresql", initialization: "postgresql_converted.sql", migration: "2026-09-06-openapi-client-menu-postgresql.sql"},
		{name: "sqlserver", initialization: "sqlserver_converted.sql", migration: "2026-09-06-openapi-client-menu-sqlserver.sql"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initialization := readMenuInitializationFile(t, tc.initialization)
			migration := readMenuInitializationMigration(t, tc.migration)

			const (
				beginMarker       = "-- openapi-client-menu:begin"
				endMarker         = "-- openapi-client-menu:end"
				permissionsMarker = "-- openapi-aksk-permissions:end"
			)
			require.Equal(t, 1, strings.Count(initialization, beginMarker), "initialization must contain one menu block begin marker")
			require.Equal(t, 1, strings.Count(initialization, endMarker), "initialization must contain one menu block end marker")
			require.Equal(t, 1, strings.Count(migration, beginMarker), "migration must contain one menu block begin marker")
			require.Equal(t, 1, strings.Count(migration, endMarker), "migration must contain one menu block end marker")

			initializationBlock := extractMenuInitializationBlock(t, initialization, beginMarker, endMarker)
			migrationBlock := extractMenuInitializationBlock(t, migration, beginMarker, endMarker)
			require.Equal(t, migrationBlock, initializationBlock, "full initialization menu block must remain byte-for-byte identical to its dialect migration")

			permissionsEnd := strings.Index(initialization, permissionsMarker)
			require.NotEqual(t, -1, permissionsEnd, "full initialization must include the OpenAPI permissions block")
			menuBegin := strings.Index(initialization, beginMarker)
			require.Greater(t, menuBegin, permissionsEnd, "menu block must follow the permissions block")
		})
	}
}

func readMenuInitializationFile(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
	require.NoError(t, err)
	return string(body)
}

func readMenuInitializationMigration(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", name))
	require.NoError(t, err)
	return string(body)
}

func extractMenuInitializationBlock(t *testing.T, body, beginMarker, endMarker string) string {
	t.Helper()
	start := strings.Index(body, beginMarker)
	require.NotEqual(t, -1, start, "missing block begin marker %q", beginMarker)
	endRelative := strings.Index(body[start:], endMarker)
	require.NotEqual(t, -1, endRelative, "missing block end marker %q", endMarker)
	end := start + endRelative + len(endMarker)
	return body[start:end]
}
