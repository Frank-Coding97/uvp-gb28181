package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPSPermissionMigration(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver", "-sqlite"} {
		t.Run(suffix, func(t *testing.T) {
			db := openAPIPermissionFixture(t)
			require.NoError(t, db.Exec("INSERT INTO sys_menu(id,permission) VALUES(91,'system:config:update')").Error)
			require.NoError(t, db.Exec("INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) VALUES('p','role_4','/api/config/update','PUT','*','','')").Error)
			name := "2026-10-18-https-management" + suffix + ".sql"
			body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", name))
			require.NoError(t, err)
			for i := 0; i < 2; i++ {
				runOpenAPIPermissionSQL(t, db, string(body), suffix)
			}
			var count int64
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_api WHERE path LIKE '/api/system/https/%'").Scan(&count).Error)
			require.EqualValues(t, 4, count)
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_menu_api WHERE menu_id=91").Scan(&count).Error)
			require.EqualValues(t, 4, count)
			for _, role := range []string{"role_1", "role_4"} {
				require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule WHERE v0=? AND v1 LIKE '/api/system/https/%'", role).Scan(&count).Error)
				require.EqualValues(t, 4, count)
			}
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v1 LIKE '/api/system/https/%'").Scan(&count).Error)
			require.Zero(t, count)
			needed, err := needsBaselineUpgrade(name, allTablesExistProbe())
			require.NoError(t, err)
			require.True(t, needed)
		})
	}
}
