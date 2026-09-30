package playauth

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var unsettledFixtureDBID atomic.Int64

const unsettledDeviceCode = "34020000001320000031"

// newUnsettledFixture creates the minimum schema HasUnsettledBefore reads plus
// the intent table. The device row starts already transferred: access_epoch is
// ahead of cleanup_completed_epoch, which is the state a real transfer leaves.
func newUnsettledFixture(t *testing.T, access, completed int64) (*gorm.DB, *DeviceOperationIntentStore) {
	t.Helper()
	dsn := fmt.Sprintf("file:unsettled_before_%d?mode=memory&cache=shared&_busy_timeout=5000", unsettledFixtureDBID.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&DeviceOperationIntent{}))
	require.NoError(t, db.Exec(`CREATE TABLE gb_device (
		id INTEGER PRIMARY KEY, device_id TEXT NULL, access_epoch INTEGER NULL,
		cleanup_completed_epoch INTEGER NULL, deleted_at DATETIME NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO gb_device(id, device_id, access_epoch, cleanup_completed_epoch)
		VALUES (1, ?, ?, ?)`, unsettledDeviceCode, access, completed).Error)
	return db, NewDeviceOperationIntentStore(db)
}

func insertIntent(t *testing.T, db *gorm.DB, id string, epoch int64, state string, version int64, dispatchAt, cancelledAt any) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	err := db.Exec(`INSERT INTO gb_device_operation_intent
		(operation_id, device_pk, device_code, target_scope, target_pk, target_code,
		 contract_version, device_epoch, kind, state, row_version, created_at, updated_at,
		 dispatch_started_at, cancelled_at)
		VALUES (?, 1, ?, 'device', 1, ?, 1, ?, 'live', ?, ?, ?, ?, ?, ?)`,
		id, unsettledDeviceCode, unsettledDeviceCode, epoch, state, version, now, now, dispatchAt, cancelledAt).Error
	require.NoError(t, err)
}

func TestHasUnsettledBeforeIsExactOverTheWholeSet(t *testing.T) {
	ctx := context.Background()

	t.Run("no intent at all", func(t *testing.T) {
		_, store := newUnsettledFixture(t, 2, 1)
		unsettled, err := store.HasUnsettledBefore(ctx, unsettledDeviceCode, 2)
		require.NoError(t, err)
		require.False(t, unsettled, "no durable work means the device is clean")
	})

	t.Run("reserved intent from the older epoch", func(t *testing.T) {
		db, store := newUnsettledFixture(t, 2, 1)
		insertIntent(t, db, strings.Repeat("a", 32), 1, IntentReserved, 1, nil, nil)
		unsettled, err := store.HasUnsettledBefore(ctx, unsettledDeviceCode, 2)
		require.NoError(t, err)
		require.True(t, unsettled, "a reserved intent may still be dispatched and must block")
	})

	t.Run("dispatched intent from the older epoch", func(t *testing.T) {
		db, store := newUnsettledFixture(t, 2, 1)
		insertIntent(t, db, strings.Repeat("b", 32), 1, IntentDispatched, 2, time.Now().UTC().Truncate(time.Second), nil)
		unsettled, err := store.HasUnsettledBefore(ctx, unsettledDeviceCode, 2)
		require.NoError(t, err)
		require.True(t, unsettled, "a dispatched intent may have produced side effects and must block")
	})

	t.Run("cancelled intent is settled", func(t *testing.T) {
		db, store := newUnsettledFixture(t, 2, 1)
		insertIntent(t, db, strings.Repeat("c", 32), 1, IntentCancelled, 2, nil, time.Now().UTC().Truncate(time.Second))
		unsettled, err := store.HasUnsettledBefore(ctx, unsettledDeviceCode, 2)
		require.NoError(t, err)
		require.False(t, unsettled)
	})

	t.Run("current epoch intent is not older work", func(t *testing.T) {
		db, store := newUnsettledFixture(t, 2, 1)
		insertIntent(t, db, strings.Repeat("d", 32), 2, IntentReserved, 1, nil, nil)
		unsettled, err := store.HasUnsettledBefore(ctx, unsettledDeviceCode, 2)
		require.NoError(t, err)
		require.False(t, unsettled, "only epochs strictly older than the target are cleanup work")
	})

	t.Run("page size cannot hide a later intent", func(t *testing.T) {
		db, store := newUnsettledFixture(t, 2, 1)
		for i := 0; i < 5; i++ {
			insertIntent(t, db, fmt.Sprintf("%032x", i+1), 1, IntentReserved, 1, nil, nil)
		}
		unsettled, err := store.HasUnsettledBefore(ctx, unsettledDeviceCode, 2)
		require.NoError(t, err)
		require.True(t, unsettled)
	})
}

func TestHasUnsettledBeforeFailsClosed(t *testing.T) {
	ctx := context.Background()

	t.Run("missing device", func(t *testing.T) {
		db, _ := newUnsettledFixture(t, 2, 1)
		store := NewDeviceOperationIntentStore(db)
		unsettled, err := store.HasUnsettledBefore(ctx, "34020000001320000099", 2)
		require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		require.False(t, unsettled)
	})

	t.Run("ambiguous device rows", func(t *testing.T) {
		db, store := newUnsettledFixture(t, 2, 1)
		require.NoError(t, db.Exec(`INSERT INTO gb_device(id, device_id, access_epoch, cleanup_completed_epoch)
			VALUES (2, ?, 2, 1)`, unsettledDeviceCode).Error)
		unsettled, err := store.HasUnsettledBefore(ctx, unsettledDeviceCode, 2)
		require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		require.False(t, unsettled)
	})

	t.Run("epoch already completed is revoked rather than clean", func(t *testing.T) {
		_, store := newUnsettledFixture(t, 2, 2)
		unsettled, err := store.HasUnsettledBefore(ctx, unsettledDeviceCode, 2)
		require.ErrorIs(t, err, ErrDeviceIntentRevoked)
		require.False(t, unsettled)
	})

	t.Run("invalid code or epoch", func(t *testing.T) {
		_, store := newUnsettledFixture(t, 2, 1)
		for _, code := range []string{"", "not-a-gbid"} {
			_, err := store.HasUnsettledBefore(ctx, code, 2)
			require.ErrorIs(t, err, ErrDeviceIntentInvalid)
		}
		_, err := store.HasUnsettledBefore(ctx, unsettledDeviceCode, 0)
		require.ErrorIs(t, err, ErrDeviceIntentInvalid)
	})

	t.Run("canceled context", func(t *testing.T) {
		_, store := newUnsettledFixture(t, 2, 1)
		dead, cancel := context.WithCancel(ctx)
		cancel()
		_, err := store.HasUnsettledBefore(dead, unsettledDeviceCode, 2)
		require.Error(t, err)
	})
}
