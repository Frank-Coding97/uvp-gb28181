package playauth

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	cleanupDeviceA = "34020000001320000001"
	cleanupDeviceB = "34020000001320000002"
	cleanupDeviceC = "34020000001320000003"
)

var cleanupStoreTestDBID atomic.Int64

type deviceCleanupFixture struct {
	db *gorm.DB
}

func newDeviceCleanupFixture(t *testing.T) *deviceCleanupFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:device_cleanup_store_%d?mode=memory&cache=shared&_busy_timeout=5000", cleanupStoreTestDBID.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Exec(`
		CREATE TABLE gb_device (
			id INTEGER PRIMARY KEY,
			device_id TEXT NULL,
			access_epoch INTEGER NULL,
			cleanup_completed_epoch INTEGER NULL,
			deleted_at DATETIME NULL
		)
	`).Error)
	return &deviceCleanupFixture{db: db}
}

func (f *deviceCleanupFixture) add(t *testing.T, id int64, deviceID string, access, completed int64) {
	t.Helper()
	require.NoError(t, f.db.Exec(`
		INSERT INTO gb_device(id, device_id, access_epoch, cleanup_completed_epoch, deleted_at)
		VALUES (?, ?, ?, ?, NULL)
	`, id, deviceID, access, completed).Error)
}

func requireCleanupLoadError(t *testing.T, store *DeviceCleanupStore, ctx context.Context, deviceID string, target error) {
	t.Helper()
	_, err := store.Load(ctx, deviceID)
	require.ErrorIs(t, err, target)
}

func requireCleanupListError(t *testing.T, store *DeviceCleanupStore, ctx context.Context, limit int, target error) {
	t.Helper()
	_, err := store.ListPending(ctx, limit)
	require.ErrorIs(t, err, target)
}

func TestDeviceCleanupStoreAuthorizeRequiresCompletedCurrentEpoch(t *testing.T) {
	f := newDeviceCleanupFixture(t)
	f.add(t, 1, cleanupDeviceA, 1, 1)
	f.add(t, 2, cleanupDeviceB, 2, 1)
	store := NewDeviceCleanupStore(f.db)

	state, err := store.Load(context.Background(), cleanupDeviceA)
	require.NoError(t, err)
	require.Equal(t, DeviceCleanupState{DeviceID: cleanupDeviceA, AccessEpoch: 1, CleanupCompletedEpoch: 1}, state)
	require.NoError(t, store.Authorize(context.Background(), cleanupDeviceA, 1))

	require.ErrorIs(t, store.Authorize(context.Background(), cleanupDeviceB, 2), ErrDeviceCleanupPending)
	err = store.Authorize(context.Background(), cleanupDeviceB, 1)
	require.ErrorIs(t, err, ErrDeviceCleanupRevoked)
	require.ErrorIs(t, err, ErrDeviceCleanupMismatch)

	require.NoError(t, store.Complete(context.Background(), cleanupDeviceB, 2))
	require.NoError(t, store.Authorize(context.Background(), cleanupDeviceB, 2))
	require.NoError(t, store.Complete(context.Background(), cleanupDeviceB, 2), "completion is idempotent")
	state, err = store.Load(context.Background(), cleanupDeviceB)
	require.NoError(t, err)
	require.Equal(t, int64(2), state.CleanupCompletedEpoch)
}

func TestDeviceCleanupStoreEpochsAreMonotonicAndDeviceScoped(t *testing.T) {
	f := newDeviceCleanupFixture(t)
	f.add(t, 1, cleanupDeviceA, 1, 1)
	f.add(t, 2, cleanupDeviceB, 1, 1)
	store := NewDeviceCleanupStore(f.db)

	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	require.ErrorIs(t, store.Authorize(context.Background(), cleanupDeviceA, 2), ErrDeviceCleanupPending)
	require.NoError(t, store.Complete(context.Background(), cleanupDeviceA, 2))

	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=3 WHERE id=1").Error)
	require.ErrorIs(t, store.Complete(context.Background(), cleanupDeviceA, 2), ErrDeviceCleanupStaleTarget)
	require.NoError(t, store.Complete(context.Background(), cleanupDeviceA, 3))
	require.NoError(t, store.Complete(context.Background(), cleanupDeviceA, 3))

	state, err := store.Load(context.Background(), cleanupDeviceA)
	require.NoError(t, err)
	require.Equal(t, int64(3), state.AccessEpoch)
	require.Equal(t, int64(3), state.CleanupCompletedEpoch)
	other, err := store.Load(context.Background(), cleanupDeviceB)
	require.NoError(t, err)
	require.Equal(t, DeviceCleanupState{DeviceID: cleanupDeviceB, AccessEpoch: 1, CleanupCompletedEpoch: 1}, other)
}

func TestDeviceCleanupStoreCompleteRejectsInvalidTargetsAndMalformedState(t *testing.T) {
	f := newDeviceCleanupFixture(t)
	f.add(t, 1, cleanupDeviceA, 2, 1)
	store := NewDeviceCleanupStore(f.db)

	for _, target := range []int64{0, -1, 3} {
		require.ErrorIs(t, store.Complete(context.Background(), cleanupDeviceA, target), ErrDeviceCleanupInvalid)
	}
	require.ErrorIs(t, store.Complete(context.Background(), cleanupDeviceA, 1), ErrDeviceCleanupStaleTarget)

	malformed := []struct {
		name  string
		query string
		args  []any
	}{
		{name: "completed null", query: "UPDATE gb_device SET cleanup_completed_epoch=NULL WHERE id=1"},
		{name: "completed zero", query: "UPDATE gb_device SET cleanup_completed_epoch=0 WHERE id=1"},
		{name: "completed negative", query: "UPDATE gb_device SET cleanup_completed_epoch=-1 WHERE id=1"},
		{name: "completed future", query: "UPDATE gb_device SET cleanup_completed_epoch=3 WHERE id=1"},
		{name: "access null", query: "UPDATE gb_device SET access_epoch=NULL WHERE id=1"},
		{name: "access zero", query: "UPDATE gb_device SET access_epoch=0 WHERE id=1"},
		{name: "access negative", query: "UPDATE gb_device SET access_epoch=-1 WHERE id=1"},
	}
	for _, test := range malformed {
		t.Run(test.name, func(t *testing.T) {
			f := newDeviceCleanupFixture(t)
			f.add(t, 1, cleanupDeviceA, 2, 1)
			require.NoError(t, f.db.Exec(test.query, test.args...).Error)
			store := NewDeviceCleanupStore(f.db)
			requireCleanupLoadError(t, store, context.Background(), cleanupDeviceA, ErrDeviceCleanupUnavailable)
			require.ErrorIs(t, store.Authorize(context.Background(), cleanupDeviceA, 2), ErrDeviceCleanupUnavailable)
			require.ErrorIs(t, store.Complete(context.Background(), cleanupDeviceA, 2), ErrDeviceCleanupUnavailable)
		})
	}
}

func TestDeviceCleanupStoreFailsClosedOnMissingDeletedDuplicateAndCanceled(t *testing.T) {
	t.Run("missing column", func(t *testing.T) {
		f := newDeviceCleanupFixture(t)
		require.NoError(t, f.db.Exec("ALTER TABLE gb_device DROP COLUMN cleanup_completed_epoch").Error)
		require.NoError(t, f.db.Exec("INSERT INTO gb_device(id, device_id, access_epoch) VALUES (1, ?, 1)", cleanupDeviceA).Error)
		store := NewDeviceCleanupStore(f.db)
		requireCleanupLoadError(t, store, context.Background(), cleanupDeviceA, ErrDeviceCleanupUnavailable)
		requireCleanupListError(t, store, context.Background(), 10, ErrDeviceCleanupUnavailable)
	})

	t.Run("deleted and duplicate", func(t *testing.T) {
		f := newDeviceCleanupFixture(t)
		f.add(t, 1, cleanupDeviceA, 2, 1)
		require.NoError(t, f.db.Exec("UPDATE gb_device SET deleted_at=CURRENT_TIMESTAMP WHERE id=1").Error)
		store := NewDeviceCleanupStore(f.db)
		requireCleanupLoadError(t, store, context.Background(), cleanupDeviceA, ErrDeviceCleanupUnavailable)

		f.add(t, 2, cleanupDeviceB, 2, 1)
		f.add(t, 3, cleanupDeviceB, 2, 1)
		requireCleanupLoadError(t, store, context.Background(), cleanupDeviceB, ErrDeviceCleanupUnavailable)
	})

	t.Run("canceled and nil database", func(t *testing.T) {
		f := newDeviceCleanupFixture(t)
		f.add(t, 1, cleanupDeviceA, 1, 1)
		store := NewDeviceCleanupStore(f.db)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		requireCleanupLoadError(t, store, ctx, cleanupDeviceA, ErrDeviceCleanupUnavailable)
		require.ErrorIs(t, store.Authorize(ctx, cleanupDeviceA, 1), ErrDeviceCleanupUnavailable)
		require.ErrorIs(t, store.Complete(ctx, cleanupDeviceA, 1), ErrDeviceCleanupUnavailable)
		requireCleanupListError(t, store, ctx, 10, ErrDeviceCleanupUnavailable)

		var absent *DeviceCleanupStore
		requireCleanupLoadError(t, absent, context.Background(), cleanupDeviceA, ErrDeviceCleanupUnavailable)
		require.ErrorIs(t, absent.Authorize(context.Background(), cleanupDeviceA, 1), ErrDeviceCleanupUnavailable)
		require.ErrorIs(t, absent.Complete(context.Background(), cleanupDeviceA, 1), ErrDeviceCleanupUnavailable)
		requireCleanupListError(t, absent, context.Background(), 10, ErrDeviceCleanupUnavailable)
	})
}

func TestDeviceCleanupStoreListPendingIsBoundedStableAndDoesNotComplete(t *testing.T) {
	f := newDeviceCleanupFixture(t)
	f.add(t, 1, cleanupDeviceA, 1, 1)
	f.add(t, 2, cleanupDeviceB, 2, 1)
	f.add(t, 3, cleanupDeviceC, 3, 2)
	f.add(t, 4, "34020000001320000004", 4, 4)
	require.NoError(t, f.db.Exec("INSERT INTO gb_device(id, device_id, access_epoch, cleanup_completed_epoch) VALUES (5, ?, 5, 1)", "34020000001320000005").Error)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET deleted_at=CURRENT_TIMESTAMP WHERE id=5").Error)
	store := NewDeviceCleanupStore(f.db)

	pending, err := store.ListPending(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, []DeviceCleanupState{
		{DeviceID: cleanupDeviceB, AccessEpoch: 2, CleanupCompletedEpoch: 1},
		{DeviceID: cleanupDeviceC, AccessEpoch: 3, CleanupCompletedEpoch: 2},
	}, pending)

	pending, err = NewDeviceCleanupStore(f.db).ListPending(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, []DeviceCleanupState{{DeviceID: cleanupDeviceB, AccessEpoch: 2, CleanupCompletedEpoch: 1}}, pending)

	for i := int64(6); i <= 106; i++ {
		f.add(t, i, fmt.Sprintf("3402000000132%07d", i), 2, 1)
	}
	pending, err = store.ListPending(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, pending, 100, "zero uses the conservative default page size")
	state, err := store.Load(context.Background(), cleanupDeviceB)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.CleanupCompletedEpoch, "listing never auto-completes")
}

func TestDeviceCleanupStoreConcurrentIdempotentCompleteDoesNotRegress(t *testing.T) {
	// The SQLite fixture serializes one connection; this checks idempotent
	// CAS behavior only and is not evidence of native row-lock concurrency.
	f := newDeviceCleanupFixture(t)
	f.add(t, 1, cleanupDeviceA, 2, 1)
	store := NewDeviceCleanupStore(f.db)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.Complete(context.Background(), cleanupDeviceA, 2)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	state, err := store.Load(context.Background(), cleanupDeviceA)
	require.NoError(t, err)
	require.Equal(t, int64(2), state.CleanupCompletedEpoch)
}
