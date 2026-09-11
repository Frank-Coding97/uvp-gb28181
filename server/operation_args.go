package main

import (
	"strings"
	"uvplatform.cn/uvp-gb28181/app/gb28181/migration"
)

func migrateUpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "-migrate-up" {
			return true
		}
	}
	return false
}

func databaseIdentitySQL(dialect migration.Dialect) string {
	switch dialect {
	case migration.DialectSQLite:
		return "SELECT 'main' AS database_name, sqlite_version() AS database_version"
	case migration.DialectPostgres:
		return "SELECT current_database() AS database_name, version() AS database_version"
	case migration.DialectSQLServer:
		return "SELECT DB_NAME() AS database_name, CAST(SERVERPROPERTY('ProductVersion') AS varchar(128)) AS database_version"
	case migration.DialectMySQL:
		return "SELECT DATABASE() AS database_name, VERSION() AS database_version"
	default:
		return ""
	}
}

func migrationDifference(before, after []string) []string {
	existing := make(map[string]struct{}, len(before))
	for _, version := range before {
		existing[version] = struct{}{}
	}
	added := make([]string, 0)
	for _, version := range after {
		if _, ok := existing[version]; !ok {
			added = append(added, version)
		}
	}
	return added
}

func parseArgs(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-migrate-down=") {
			return strings.TrimPrefix(arg, "-migrate-down=")
		}
	}
	return ""
}
