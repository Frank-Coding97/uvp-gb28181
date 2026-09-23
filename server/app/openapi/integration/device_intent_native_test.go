package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
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
	if !authoritytest.InProcess(t) {
		return
	}
	dialect, dsn := os.Getenv("UVP_OPENAPI_TEST_DIALECT"), os.Getenv("UVP_OPENAPI_TEST_DSN")
	require.NotEmpty(t, dsn, "explicit fixture DSN required")
	require.Contains(t, []string{"mysql", "postgresql", "sqlserver"}, dialect, "an explicit supported native database is required; never substitute SQLite")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connection, err := openInitializationConnector(ctx, fullInitializationConfig{dialect: dialect, dsn: dsn}, authoritytest.CommitFaultConnector)
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
	dateType := "TIMESTAMP"
	switch dialect {
	case "mysql":
		driver = mysql.New(mysql.Config{Conn: connection.db})
	case "postgresql":
		driver = postgres.New(postgres.Config{Conn: connection.db, PreferSimpleProtocol: true})
		suffix = "-postgresql"
	case "sqlserver":
		driver = sqlserver.New(sqlserver.Config{Conn: connection.db})
		suffix, dateType = "-sqlserver", "DATETIME2(6)"
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE gb_device (id BIGINT PRIMARY KEY, device_id VARCHAR(20) NOT NULL, access_epoch BIGINT NOT NULL, cleanup_completed_epoch BIGINT NOT NULL, deleted_at `+dateType+` NULL)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE gb_channel (id BIGINT PRIMARY KEY, device_id VARCHAR(20) NOT NULL, channel_id VARCHAR(20) NOT NULL, deleted_at `+dateType+` NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO gb_device VALUES (1,'34020000001320000001',1,1,NULL), (2,'34020000001320000002',1,1,NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO gb_channel VALUES (11,'34020000001320000001','34020000001320000003',NULL), (12,'34020000001320000002','34020000001320000003',NULL)`).Error)
	stem := "migrations/2026-09-07-device-operation-intent" + suffix
	up, err := migrationsfs.FS.ReadFile(stem + ".sql")
	require.NoError(t, err)
	down, err := migrationsfs.FS.ReadFile(stem + "-down.sql")
	require.NoError(t, err)
	applyCleanupBarrierScript(t, ctx, connection.conn, up)
	applyCleanupBarrierScript(t, ctx, connection.conn, up)
	// Use product migrations, never AutoMigrate, so native qualification also
	// exercises the real authority schema and its transaction lock order.
	authorityUp, err := migrationsfs.FS.ReadFile("migrations/2026-09-08-openapi-process-authority" + suffix + ".sql")
	require.NoError(t, err)
	applyCleanupBarrierScript(t, ctx, connection.conn, authorityUp)
	applyCleanupBarrierScript(t, ctx, connection.conn, authorityUp)
	lock, err := processauthority.AcquireLocalLock(authoritytest.StateDirectory(t))
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Close()) }()
	authority, err := processauthority.Register(ctx, db, lock)
	require.NoError(t, err)
	defer authority.Seal()
	store, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authority)
	require.NoError(t, err)
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD legacy_revoked_before "+dateType+" NULL").Error)
	if os.Getenv("UVP_OPENAPI_PTZ_NATIVE") == "1" {
		exercisePTZNative(t, ctx, connection.conn, db, store, authority, dialect, suffix)
	}
	barrier, err := playauth.NewAuthorizedDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db), authority)
	require.NoError(t, err)
	var fenceBefore int64
	require.NoError(t, db.Table("sys_openapi_process_authority").Pluck("row_version", &fenceBefore).Error)
	// Each admitted lease must commit the current generation fence on the
	// real engine. Concurrent leases may coexist; they are not one-shot CAS.
	barrierErrors := make(chan error, 20)
	var barrierWG sync.WaitGroup
	for n := 0; n < 20; n++ {
		barrierWG.Add(1)
		go func() {
			defer barrierWG.Done()
			lease, err := barrier.BeginEpoch(ctx, "34020000001320000001", 1)
			if lease != nil {
				lease.Release()
			}
			barrierErrors <- err
		}()
	}
	barrierWG.Wait()
	close(barrierErrors)
	for err := range barrierErrors {
		require.NoError(t, err)
	}
	var fenceAfter int64
	require.NoError(t, db.Table("sys_openapi_process_authority").Pluck("row_version", &fenceAfter).Error)
	require.Equal(t, fenceBefore+20, fenceAfter)
	_, err = playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db)).BeginEpoch(ctx, "34020000001320000001", 1)
	require.ErrorIs(t, err, playauth.ErrDeviceOperationUnavailable)
	t.Log("native ordinary admission: 20 confirmed generation fences; observer constructor denied")
	id := playauth.DeviceOperationIntentIdentity{OperationID: "00000000000000000000000000000001", DevicePK: 1, DeviceCode: "34020000001320000001", DeviceEpoch: 1, TargetScope: "channel", TargetPK: 11, TargetCode: "34020000001320000003", Kind: "live"}
	row, err := store.Reserve(ctx, id)
	require.NoError(t, err)
	require.Equal(t, playauth.IntentReserved, row.State)
	_, err = store.Reserve(ctx, id)
	require.NoError(t, err)
	rtpStem := "migrations/2026-09-07-device-operation-rtp-steps" + suffix
	rtpUp, err := migrationsfs.FS.ReadFile(rtpStem + ".sql")
	require.NoError(t, err)
	applyCleanupBarrierScript(t, ctx, connection.conn, rtpUp)
	applyCleanupBarrierScript(t, ctx, connection.conn, rtpUp)
	sipStem := "migrations/2026-09-07-device-operation-sip-steps" + suffix
	sipUp, err := migrationsfs.FS.ReadFile(sipStem + ".sql")
	require.NoError(t, err)
	applyCleanupBarrierScript(t, ctx, connection.conn, sipUp)
	applyCleanupBarrierScript(t, ctx, connection.conn, sipUp)
	var unknownCount int64
	require.NoError(t, db.Table("gb_device_operation_intent").Where("operation_id=? AND rtp_steps_json IS NULL AND sip_steps_json IS NULL", id.OperationID).Count(&unknownCount).Error)
	require.EqualValues(t, 1, unknownCount, "upgrade must preserve historical unknown, not fabricate coverage")
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
	rollback := errors.New("fixture transfer rollback")
	transfer := func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error; err != nil {
			return err
		}
		return playauth.CancelReservedDeviceOperationIntents(ctx, tx, 1, id.DeviceCode, 2)
	}
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		if err := transfer(tx); err != nil {
			return err
		}
		return rollback
	}), rollback)
	var beforeCommit playauth.DeviceOperationIntent
	require.NoError(t, db.Where("operation_id=?", newID.OperationID).Take(&beforeCommit).Error)
	require.Equal(t, playauth.IntentReserved, beforeCommit.State)
	require.NoError(t, db.Transaction(transfer))
	_, err = store.Dispatch(ctx, newID, 1)
	require.ErrorIs(t, err, playauth.ErrDeviceIntentRevoked)
	var cancelled playauth.DeviceOperationIntent
	require.NoError(t, db.Where("operation_id=?", newID.OperationID).Take(&cancelled).Error)
	require.Equal(t, playauth.IntentCancelled, cancelled.State)
	rows, err = store.ListUnsettled(ctx, 1, id.DeviceCode, 2, "", 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, id.OperationID, rows[0].OperationID)
	_, err = store.Dispatch(ctx, other, 1)
	require.NoError(t, err)
	verifyRTPResourceStepsNative(t, ctx, db, store, other)
	rtpDown, err := migrationsfs.FS.ReadFile(rtpStem + "-down.sql")
	require.NoError(t, err)
	beforeDown, err := store.LoadRTPResourceSteps(ctx, other)
	require.NoError(t, err)
	applyCleanupBarrierScript(t, ctx, connection.conn, rtpDown)
	applyCleanupBarrierScript(t, ctx, connection.conn, rtpUp)
	afterUp, err := playauth.NewDeviceOperationIntentStore(db).LoadRTPResourceSteps(ctx, other)
	require.NoError(t, err)
	require.Equal(t, beforeDown, afterUp)
	// Separate device: the RTP fixture already transferred its original device.
	require.NoError(t, db.Exec(`INSERT INTO gb_device (id,device_id,access_epoch,cleanup_completed_epoch,deleted_at) VALUES (3,'34020000001320000004',1,1,NULL)`).Error)
	sipID := playauth.DeviceOperationIntentIdentity{OperationID: "00000000000000000000000000000004", DevicePK: 3, DeviceCode: "34020000001320000004", DeviceEpoch: 1, TargetScope: "device", TargetPK: 3, TargetCode: "34020000001320000004", Kind: "playback"}
	_, err = store.Reserve(ctx, sipID)
	require.NoError(t, err)
	_, err = store.Dispatch(ctx, sipID, 1)
	require.NoError(t, err)
	verifySIPInviteStepsNative(t, ctx, db, store, sipID)
	sipBefore, err := store.LoadSIPInviteSteps(ctx, sipID)
	require.NoError(t, err)
	sipDown, err := migrationsfs.FS.ReadFile(sipStem + "-down.sql")
	require.NoError(t, err)
	applyCleanupBarrierScript(t, ctx, connection.conn, sipDown)
	applyCleanupBarrierScript(t, ctx, connection.conn, sipUp)
	sipAfter, err := playauth.NewDeviceOperationIntentStore(db).LoadSIPInviteSteps(ctx, sipID)
	require.NoError(t, err)
	require.Equal(t, sipBefore, sipAfter)
	state, err := playauth.NewDeviceCleanupStore(db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.CleanupCompletedEpoch)
	t.Logf("%s native intent up/up, schema constraints, 20 concurrent dispatch CAS, restart/down/up preservation and transfer TX cancellation/rollback passed", dialect)
}

func verifyRTPResourceStepsNative(t *testing.T, ctx context.Context, db *gorm.DB, store *playauth.DeviceOperationIntentStore, id playauth.DeviceOperationIntentIdentity) {
	t.Helper()
	identity := func(n int) playauth.DeviceRTPResourceIdentity {
		resourceID, err := playauth.NewDeviceRTPResourceID(id.OperationID, fmt.Sprintf("%032x", n), time.UnixMilli(1788750000000))
		require.NoError(t, err)
		return playauth.DeviceRTPResourceIdentity{StepID: fmt.Sprintf("%032x", n), NodePK: 3, NodeUUID: "fixture-node", NodeRevision: 9,
			BootNonce: strings.Repeat("a", 32), ResourceID: resourceID,
			VHost: "__defaultVhost__", App: "rtp", Stream: fmt.Sprintf("fixture-step-%d", n), LocalIP: "127.0.0.1", SSRC: 12345}
	}
	for n := 1; n <= 16; n++ {
		out, err := store.AddRTPResourceStep(ctx, id, int64(n+1), identity(n))
		require.NoError(t, err)
		require.Len(t, out.Steps, n)
	}
	_, err := store.AddRTPResourceStep(ctx, id, 18, identity(17))
	require.ErrorIs(t, err, playauth.ErrDeviceIntentConflict)
	var wins atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.DispatchRTPResourceStep(ctx, id, 18, identity(1).StepID)
			if err == nil {
				wins.Add(1)
			} else {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	require.EqualValues(t, 1, wins.Load())
	for err := range errs {
		require.ErrorIs(t, err, playauth.ErrDeviceIntentConflict)
	}
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Len(t, loaded.Steps, 16)
	require.Equal(t, playauth.RTPStepMayHaveDispatched, loaded.Steps[0].State)
	for n, step := range loaded.Steps {
		require.Equal(t, identity(n+1), step.Identity)
	}
	for _, raw := range []string{"", strings.Repeat("x", 32769)} {
		require.Error(t, db.Table("gb_device_operation_intent").Where("operation_id=?", id.OperationID).Update("rtp_steps_json", raw).Error)
	}
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=?", id.DevicePK).Error)
	_, err = store.DispatchRTPResourceStep(ctx, id, 19, identity(2).StepID)
	require.ErrorIs(t, err, playauth.ErrDeviceIntentRevoked)
	_, err = store.AddRTPResourceStep(ctx, id, 19, identity(17))
	require.ErrorIs(t, err, playauth.ErrDeviceIntentRevoked)
	loadedAfter, err := playauth.NewDeviceOperationIntentStore(db).LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, loaded, loadedAfter)
	t.Log("RTP steps: 16 immutable resources, bounded growth, 20 concurrent single-step CAS, native byte constraints and old-epoch read-only recovery passed")
}
