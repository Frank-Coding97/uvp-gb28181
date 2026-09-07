package integration

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

// An additional explicit opt-in prevents broad integration runs from using an
// existing business or previously populated test database for this fixture.
func TestOpenAPIDeviceOperationIntentNative(t *testing.T) {
	if os.Getenv("UVP_OPENAPI_INTENT_NATIVE") != "1" {
		if os.Getenv("UVP_OPENAPI_INTEGRATION_REQUIRED") == "1" {
			t.Fatal("explicit empty intent fixture required")
		}
		t.Skip("no explicit empty intent database; not an acceptance pass")
	}
	dialect, dsn := os.Getenv("UVP_OPENAPI_TEST_DIALECT"), os.Getenv("UVP_OPENAPI_TEST_DSN")
	require.NotEmpty(t, dsn, "explicit fixture DSN required")
	require.Contains(t, []string{"mysql", "postgresql"}, dialect, "SQL Server needs its own licensed fixture; never substitute SQLite")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connection, err := openFullInitializationConnection(ctx, fullInitializationConfig{dialect: dialect, dsn: dsn})
	require.NoError(t, err)
	defer connection.db.Close()
	defer connection.conn.Close()
	name, err := scanInitializationString(ctx, connection.conn, initializationDatabaseNameQuery(dialect))
	require.NoError(t, err)
	require.True(t, isDedicatedInitializationDatabase(name), "refusing non-test database")
	tables, err := listInitializationTables(ctx, connection.conn, dialect)
	require.NoError(t, err)
	require.NoError(t, requireEmptyInitializationInventory(tables), "refusing any populated target")
	connection.db.SetMaxOpenConns(8)
	connection.db.SetMaxIdleConns(8)
	var driver gorm.Dialector
	suffix := ""
	if dialect == "mysql" {
		driver = mysql.New(mysql.Config{Conn: connection.db})
	} else {
		driver = postgres.New(postgres.Config{Conn: connection.db, PreferSimpleProtocol: true})
		suffix = "-postgresql"
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE gb_device (id BIGINT PRIMARY KEY, device_id VARCHAR(20) NOT NULL, access_epoch BIGINT NOT NULL, cleanup_completed_epoch BIGINT NOT NULL, deleted_at TIMESTAMP NULL)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE gb_channel (id BIGINT PRIMARY KEY, device_id VARCHAR(20) NOT NULL, channel_id VARCHAR(20) NOT NULL, deleted_at TIMESTAMP NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO gb_device VALUES (1,'34020000001320000001',1,1,NULL), (2,'34020000001320000002',1,1,NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO gb_channel VALUES (11,'34020000001320000001','34020000001320000003',NULL), (12,'34020000001320000002','34020000001320000003',NULL)`).Error)
	stem := "migrations/2026-09-07-device-operation-intent" + suffix
	up, err := migrationsfs.FS.ReadFile(stem + ".sql")
	require.NoError(t, err)
	down, err := migrationsfs.FS.ReadFile(stem + "-down.sql")
	require.NoError(t, err)
	applyCleanupBarrierScript(t, ctx, connection.conn, up)
	applyCleanupBarrierScript(t, ctx, connection.conn, up)
	store := playauth.NewDeviceOperationIntentStore(db)
	id := playauth.DeviceOperationIntentIdentity{OperationID: "00000000000000000000000000000001", DevicePK: 1, DeviceCode: "34020000001320000001", DeviceEpoch: 1, TargetScope: "channel", TargetPK: 11, TargetCode: "34020000001320000003", Kind: "live"}
	row, err := store.Reserve(ctx, id)
	require.NoError(t, err)
	require.Equal(t, playauth.IntentReserved, row.State)
	_, err = store.Reserve(ctx, id)
	require.NoError(t, err)
	for _, mutation := range []map[string]any{
		{"device_epoch": 0}, {"contract_version": 2}, {"row_version": 0}, {"kind": "any"}, {"target_scope": "any"},
		{"device_code": "bad"}, {"operation_id": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"state": "cancelled"}, {"state": "dispatched"}, {"dispatch_started_at": time.Now().UTC()},
	} {
		require.Error(t, db.Model(&playauth.DeviceOperationIntent{}).Where("operation_id=?", id.OperationID).Updates(mutation).Error, "constraint must reject %v", mutation)
	}
	var winners atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Dispatch(ctx, id, 1)
			if err == nil {
				winners.Add(1)
			} else {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	require.Equal(t, int64(1), winners.Load())
	for err := range errs {
		require.ErrorIs(t, err, playauth.ErrDeviceIntentConflict)
	}
	require.ErrorIs(t, store.CancelReserved(ctx, id.OperationID, 2), playauth.ErrDeviceIntentConflict)
	// Preserve the ambiguous dispatched row across down/up and service restart.
	applyCleanupBarrierScript(t, ctx, connection.conn, down)
	applyCleanupBarrierScript(t, ctx, connection.conn, up)
	rows, err := playauth.NewDeviceOperationIntentStore(db).ListUnsettled(ctx, 1, id.DeviceCode, 2, "", 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, playauth.IntentDispatched, rows[0].State)
	newID := id
	newID.OperationID = "00000000000000000000000000000002"
	_, err = store.Reserve(ctx, newID)
	require.NoError(t, err)
	other := newID
	other.OperationID = "00000000000000000000000000000003"
	other.DevicePK = 2
	other.DeviceCode = "34020000001320000002"
	other.TargetPK = 12
	_, err = store.Reserve(ctx, other)
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	_, err = store.Dispatch(ctx, newID, 1)
	require.ErrorIs(t, err, playauth.ErrDeviceIntentRevoked)
	require.NoError(t, store.CancelReserved(ctx, newID.OperationID, 1))
	_, err = store.Dispatch(ctx, other, 1)
	require.NoError(t, err)
	state, err := playauth.NewDeviceCleanupStore(db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.CleanupCompletedEpoch)
	t.Logf("%s native intent up/up, immutable constraints, 20 concurrent dispatch CAS, restart/down/up preservation and exact transfer rejection passed", dialect)
}
