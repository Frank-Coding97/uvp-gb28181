package sqlitebootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStandaloneInstallationMigrationSeedsPendingStateAndRemovesOnlyExactOrphan(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_, err := Initialize(ctx, db)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) VALUES('g','user_1','role_1','tenant','','','')`).Error)

	require.NoError(t, Migrate(ctx, db))
	require.NoError(t, Migrate(ctx, db))
	require.EqualValues(t, len(compiledMigrations)+1, migrationMarkerCount(t, db))

	var exact int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM sys_casbin_rule WHERE ptype='g' AND v0='user_1' AND v1='role_1' AND v2='*' AND v3='' AND v4='' AND v5=''`).Scan(&exact).Error)
	require.Zero(t, exact)
	var nonExact int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM sys_casbin_rule WHERE ptype='g' AND v0='user_1' AND v1='role_1' AND v2='tenant' AND v3='' AND v4='' AND v5=''`).Scan(&nonExact).Error)
	require.EqualValues(t, 1, nonExact)

	var state struct {
		ID              uint
		Phase           string
		AdministratorID *uint `gorm:"column:admin_user_id"`
		CompletedAt     *string
	}
	require.NoError(t, db.Table("standalone_installation").Where("id = ?", 1).Take(&state).Error)
	require.EqualValues(t, 1, state.ID)
	require.Equal(t, "pending_admin", state.Phase)
	require.Nil(t, state.AdministratorID)
	require.Nil(t, state.CompletedAt)
}

func TestStandaloneInstallationMigrationPreservesRelationWhenUserOneExists(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_, err := Initialize(ctx, db)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO sys_users(id,username,password,status,dept_id,nick_name,sex,description) VALUES(1,'existing','existing-hash',1,1,'existing','1','existing user')`).Error)

	require.NoError(t, Migrate(ctx, db))
	var exact int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM sys_casbin_rule WHERE ptype='g' AND v0='user_1' AND v1='role_1' AND v2='*' AND v3='' AND v4='' AND v5=''`).Scan(&exact).Error)
	require.EqualValues(t, 1, exact)
}
