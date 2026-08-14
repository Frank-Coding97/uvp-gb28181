package migration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

// contractThreshold 契约生效日期:该日期及以后的迁移文件必须
// 三方言齐全 + up/down 成对。存量文件(阈值前)豁免,只约束新文件。
const contractThreshold = "2026-08-14"

// 6.1/6.2:阈值日期及以后的每个 MySQL up 迁移,配套
// -postgresql.sql / -sqlserver.sql / -down.sql 必须齐全。
func TestMigrationFileContract(t *testing.T) {
	entries, err := migrationsfs.FS.ReadDir("migrations")
	require.NoError(t, err)

	var newFiles []string
	for _, e := range entries {
		name := e.Name()
		if len(name) < len(contractThreshold) || !strings.HasSuffix(name, ".sql") {
			continue
		}
		date := name[:len(contractThreshold)]
		if date >= contractThreshold {
			newFiles = append(newFiles, name)
		}
	}

	mysqlUps := FilterUpFiles(newFiles, DialectMySQL)
	if len(mysqlUps) == 0 {
		t.Skip("阈值日期后暂无新迁移文件,契约断言空转")
	}

	all := make(map[string]bool, len(newFiles))
	for _, name := range newFiles {
		all[name] = true
	}
	for _, up := range mysqlUps {
		base := strings.TrimSuffix(up, ".sql")
		for _, required := range []string{
			base + "-postgresql.sql",
			base + "-sqlserver.sql",
			base + "-down.sql",
		} {
			require.Truef(t, all[required], "迁移 %s 缺少配套文件 %s(三方言 + down 必须齐全)", up, required)
		}
	}
}
