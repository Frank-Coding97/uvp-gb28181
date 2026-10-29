package migration

import (
	"fmt"
	"sort"
	"strings"

	migrationsfs "uvplatform.com/uvp-gb28181/resource/database/gb28181"
)

// FilterUpFiles 从文件名列表中筛出当前方言的 up 迁移文件,字典序排列。
// 命名约定:默认方言(MySQL)无后缀,PostgreSQL 为 -postgresql.sql,
// SQL Server 为 -sqlserver.sql,SQLite 为 -sqlite.sql;down 文件为 <up>-down.sql。
func FilterUpFiles(names []string, d Dialect) []string {
	var out []string
	for _, name := range names {
		if isUpFile(name, d) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// AvailableUpFiles returns the embedded migration set for a dialect. It is
// read-only and is used by the upgrade preview API; execution remains owned by
// the migration runner.
func AvailableUpFiles(d Dialect) ([]string, error) {
	entries, err := migrationsfs.FS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("列迁移文件失败: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return FilterUpFiles(names, d), nil
}

func isUpFile(name string, d Dialect) bool {
	if !strings.HasSuffix(name, ".sql") {
		return false
	}
	switch d {
	case DialectPostgres:
		return strings.HasSuffix(name, "-postgresql.sql")
	case DialectSQLServer:
		return strings.HasSuffix(name, "-sqlserver.sql")
	case DialectSQLite:
		return strings.HasSuffix(name, "-sqlite.sql")
	default: // DialectMySQL
		if strings.Contains(name, "-postgresql") || strings.Contains(name, "-sqlserver") || strings.Contains(name, "-sqlite") {
			return false
		}
		return !strings.Contains(name, "-down.sql")
	}
}

// DownFileName 返回 up 迁移文件对应的 down 文件名(可能不存在于目录中)。
func DownFileName(upName string) string {
	return strings.TrimSuffix(upName, ".sql") + "-down.sql"
}
