package migration

import (
	"fmt"
	"sort"
	"strings"

	migrationsfs "uvplatform.com/uvp-gb28181/resource/database/gb28181"
)

// FilterUpFiles 从文件名列表中筛出当前方言的 up 迁移文件,字典序排列。
// 命名约定:默认方言(MySQL)无后缀,PostgreSQL 为 -postgresql.sql,
// SQL Server 为 -sqlserver.sql,SQLite 为 -sqlite.sql,达梦为 -dm.sql;
// down 文件为 <up>-down.sql。
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

// otherDialectSuffixes 是「带后缀的方言」的完整清单。
//
// MySQL 是主方言、文件名不带后缀，所以它的判据只能是「排除了别人」——
// 一旦这里漏一个新方言，那套脚本会**同时**被当成 MySQL 脚本执行一遍，
// 报错是「表已存在」而根因在文件名。新增方言时必须同步这里。
var otherDialectSuffixes = []string{"-postgresql", "-sqlserver", "-sqlite", "-dm"}

func isUpFile(name string, d Dialect) bool {
	if !strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, "-down.sql") {
		return false
	}
	switch d {
	case DialectPostgres:
		return strings.HasSuffix(name, "-postgresql.sql")
	case DialectSQLServer:
		return strings.HasSuffix(name, "-sqlserver.sql")
	case DialectSQLite:
		return strings.HasSuffix(name, "-sqlite.sql")
	case DialectDM:
		return strings.HasSuffix(name, "-dm.sql")
	default: // DialectMySQL
		for _, suffix := range otherDialectSuffixes {
			if strings.HasSuffix(name, suffix+".sql") {
				return false
			}
		}
		return true
	}
}

// DownFileName 返回 up 迁移文件对应的 down 文件名(可能不存在于目录中)。
func DownFileName(upName string) string {
	return strings.TrimSuffix(upName, ".sql") + "-down.sql"
}
