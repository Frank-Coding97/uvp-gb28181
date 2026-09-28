package migration

import (
	"sort"
	"strings"
)

// FilterUpFiles 从文件名列表中筛出当前方言的 up 迁移文件,字典序排列。
// 命名约定:默认方言(MySQL)无后缀,PostgreSQL 为 -postgresql.sql,
// SQL Server 为 -sqlserver.sql;down 文件为 <up>-down.sql。
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

func isUpFile(name string, d Dialect) bool {
	if !strings.HasSuffix(name, ".sql") {
		return false
	}
	switch d {
	case DialectPostgres:
		return strings.HasSuffix(name, "-postgresql.sql")
	case DialectSQLServer:
		return strings.HasSuffix(name, "-sqlserver.sql")
	default: // DialectMySQL
		if strings.Contains(name, "-postgresql") || strings.Contains(name, "-sqlserver") {
			return false
		}
		return !strings.Contains(name, "-down.sql")
	}
}

// DownFileName 返回 up 迁移文件对应的 down 文件名(可能不存在于目录中)。
func DownFileName(upName string) string {
	return strings.TrimSuffix(upName, ".sql") + "-down.sql"
}
