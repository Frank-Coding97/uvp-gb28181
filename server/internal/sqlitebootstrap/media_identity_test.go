package sqlitebootstrap

import (
	"context"
	"database/sql"
	"errors"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
)

func TestPublishedMediaIdentityMigrationPreservesUnknownRowsAndExactKey(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_, err := Initialize(ctx, db)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO gb_zlm_managed_resource(node_id,resource_type,resource_key,app,stream,identity_fingerprint,created_by,created_at,updated_at) VALUES(7,'pull_proxy','camera','live','camera','fingerprint',42,'2026-09-07 00:00:00','2026-09-07 00:00:00')`).Error)
	require.NoError(t, Migrate(ctx, db))
	var row struct {
		Schema, Vhost, App, Stream string
		CreatedBy                  int
	}
	require.NoError(t, db.Raw(`SELECT schema,vhost,app,stream,created_by FROM gb_zlm_managed_resource WHERE id=1`).Scan(&row).Error)
	require.Empty(t, row.Schema)
	require.Empty(t, row.Vhost)
	require.Equal(t, "live", row.App)
	require.Equal(t, 42, row.CreatedBy)
	require.NoError(t, db.Exec(`INSERT INTO gb_zlm_managed_resource(node_id,resource_type,resource_key,schema,vhost,app,stream,identity_fingerprint,created_at,updated_at) VALUES(7,'pull_proxy','camera','rtsp','tenant','live','camera','fingerprint','2026-09-07','2026-09-07')`).Error)
	require.Error(t, db.Exec(`INSERT INTO gb_zlm_managed_resource(node_id,resource_type,resource_key,schema,vhost,app,stream,identity_fingerprint,created_at,updated_at) VALUES(7,'pull_proxy','camera','rtsp','tenant','live','camera','fingerprint','2026-09-07','2026-09-07')`).Error)
	require.NoError(t, Migrate(ctx, db))
	require.EqualValues(t, 2, migrationMarkerCount(t, db))
}

func TestPublishedMediaIdentityMigrationFailureRollsBackAndRetries(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_, err := Initialize(ctx, db)
	require.NoError(t, err)
	require.NotEmpty(t, compiledMigrations)
	specs := append([]migrationSpec(nil), compiledMigrations...)
	specs[0].After = func(context.Context, *gorm.DB, *sql.Conn) error {
		return errors.New("injected media validation failure")
	}
	require.ErrorContains(t, migrateWithSpecs(ctx, db, specs), "injected media validation failure")
	require.False(t, db.Migrator().HasColumn("gb_zlm_managed_resource", "schema"))
	require.EqualValues(t, 1, migrationMarkerCount(t, db))
	require.NoError(t, Migrate(ctx, db))
	require.True(t, db.Migrator().HasColumn("gb_zlm_managed_resource", "schema"))
}
