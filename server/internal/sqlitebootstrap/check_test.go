package sqlitebootstrap

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type checkMarkerSnapshot struct {
	Version  string
	Checksum string
}

type checkSchemaSnapshot struct {
	Type string
	Name string
	SQL  string
}

type checkBusinessSnapshot struct {
	ID    int
	Value string
}

func currentSchemaFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db := testDB(t)
	ctx := context.Background()
	_, err := Initialize(ctx, db)
	require.NoError(t, err)
	require.NoError(t, Migrate(ctx, db))
	require.NoError(t, db.Exec("CREATE TABLE check_business_fixture(id INTEGER PRIMARY KEY, value TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO check_business_fixture(id, value) VALUES(1, 'preserve')").Error)
	return db
}

func snapshotCurrentSchema(t *testing.T, db *gorm.DB) ([]checkMarkerSnapshot, []checkSchemaSnapshot) {
	t.Helper()
	var markers []checkMarkerSnapshot
	require.NoError(t, db.Raw("SELECT version, checksum FROM "+migrationMarkerTable+" ORDER BY version").Scan(&markers).Error)
	var schema []checkSchemaSnapshot
	require.NoError(t, db.Raw("SELECT type, name, sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name").Scan(&schema).Error)
	return markers, schema
}

func snapshotCheckBusinessRows(t *testing.T, db *gorm.DB) []checkBusinessSnapshot {
	t.Helper()
	var rows []checkBusinessSnapshot
	require.NoError(t, db.Raw("SELECT id, value FROM check_business_fixture ORDER BY id").Scan(&rows).Error)
	return rows
}

func TestCheckCurrentSchemaAcceptsCompleteCurrentSchema(t *testing.T) {
	db := currentSchemaFixture(t)
	beforeMarkers, beforeSchema := snapshotCurrentSchema(t, db)
	beforeRows := snapshotCheckBusinessRows(t, db)
	require.NoError(t, CheckCurrentSchema(context.Background(), db))
	afterMarkers, afterSchema := snapshotCurrentSchema(t, db)
	afterRows := snapshotCheckBusinessRows(t, db)
	require.True(t, reflect.DeepEqual(beforeMarkers, afterMarkers))
	require.True(t, reflect.DeepEqual(beforeSchema, afterSchema))
	require.True(t, reflect.DeepEqual(beforeRows, afterRows))
}

func TestCheckCurrentSchemaRejectsMissingLastMigrationWithoutWriting(t *testing.T) {
	require.GreaterOrEqual(t, len(compiledMigrations), 2)
	db := testDB(t)
	ctx := context.Background()
	_, err := Initialize(ctx, db)
	require.NoError(t, err)
	require.NoError(t, migrateWithSpecs(ctx, db, compiledMigrations[:len(compiledMigrations)-1]))
	beforeMarkers, beforeSchema := snapshotCurrentSchema(t, db)

	err = CheckCurrentSchema(ctx, db)
	require.ErrorContains(t, err, compiledMigrations[len(compiledMigrations)-1].Version)
	afterMarkers, afterSchema := snapshotCurrentSchema(t, db)
	require.True(t, reflect.DeepEqual(beforeMarkers, afterMarkers))
	require.True(t, reflect.DeepEqual(beforeSchema, afterSchema))
}

func TestCheckCurrentSchemaRejectsChecksumMismatch(t *testing.T) {
	db := currentSchemaFixture(t)
	require.NoError(t, db.Exec("UPDATE "+migrationMarkerTable+" SET checksum=? WHERE version=?", "tampered", compiledMigrations[0].Version).Error)
	err := CheckCurrentSchema(context.Background(), db)
	require.ErrorContains(t, err, "checksum")
}

func TestCheckCurrentSchemaRejectsUnknownMarker(t *testing.T) {
	db := currentSchemaFixture(t)
	require.NoError(t, db.Exec("INSERT INTO "+migrationMarkerTable+"(version, applied_at, checksum) VALUES(?, ?, ?)", "2099-01-01-future-sqlite.sql", time.Now().UTC(), "future").Error)
	err := CheckCurrentSchema(context.Background(), db)
	require.ErrorContains(t, err, "unknown")
}

func TestCheckCurrentSchemaRejectsWrongOrder(t *testing.T) {
	require.GreaterOrEqual(t, len(compiledMigrations), 2)
	db := testDB(t)
	ctx := context.Background()
	markMatchingBaseline(t, db)
	require.NoError(t, db.Exec("INSERT INTO "+migrationMarkerTable+"(version, applied_at, checksum) VALUES(?, ?, ?)", compiledMigrations[1].Version, time.Now().UTC(), compiledMigrations[1].SHA256).Error)
	err := CheckCurrentSchema(ctx, db)
	require.ErrorContains(t, err, "applied before")
}

func TestCheckCurrentSchemaRejectsMissingBaseline(t *testing.T) {
	db := testDB(t)
	require.NoError(t, db.Exec("CREATE TABLE "+migrationMarkerTable+"(version TEXT PRIMARY KEY NOT NULL, applied_at DATETIME NOT NULL, checksum TEXT NOT NULL)").Error)
	err := CheckCurrentSchema(context.Background(), db)
	require.ErrorContains(t, err, "baseline")
}

func TestCheckCurrentSchemaRejectsCanceledContext(t *testing.T) {
	db := currentSchemaFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := CheckCurrentSchema(ctx, db)
	require.ErrorIs(t, err, context.Canceled)
}

func TestCheckCurrentSchemaFailsClosedForNilAndWrongDialect(t *testing.T) {
	require.Error(t, CheckCurrentSchema(context.Background(), nil))
	require.Error(t, CheckCurrentSchema(context.Background(), &gorm.DB{}))
	wrongDialect := &gorm.DB{Config: &gorm.Config{Dialector: mysql.Open("invalid")}}
	require.Error(t, CheckCurrentSchema(context.Background(), wrongDialect))
}
