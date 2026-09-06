package config

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestMustAuthStateModelUsesSingletonAndDatabaseChecks(t *testing.T) {
	t.Run("id is exactly one", func(t *testing.T) {
		db := newMustAuthTestDB(t, "singleton-id")
		require.NoError(t, db.AutoMigrate(&models.SecurityState{}))
		require.Error(t, db.Create(&models.SecurityState{ID: 2}).Error)
	})

	cases := []struct {
		name  string
		state models.SecurityState
	}{
		{name: "unlocked version", state: models.SecurityState{ID: 1, LockVersion: 1}},
		{name: "unlocked timestamp", state: models.SecurityState{ID: 1, LockedAt: timePtr(time.Unix(1, 0).UTC())}},
		{name: "locked version zero", state: models.SecurityState{ID: 1, MustAuthLocked: true, LockedAt: timePtr(time.Unix(1, 0).UTC())}},
		{name: "locked timestamp null", state: models.SecurityState{ID: 1, MustAuthLocked: true, LockVersion: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newMustAuthTestDB(t, strings.ReplaceAll(tc.name, " ", "-"))
			require.NoError(t, db.AutoMigrate(&models.SecurityState{}))
			require.Error(t, db.Create(&tc.state).Error)
		})
	}

	t.Run("valid unlocked row", func(t *testing.T) {
		db := newMustAuthTestDB(t, "valid-unlocked")
		require.NoError(t, db.AutoMigrate(&models.SecurityState{}))
		require.NoError(t, db.Create(&models.SecurityState{ID: 1}).Error)
	})
	t.Run("valid locked row", func(t *testing.T) {
		db := newMustAuthTestDB(t, "valid-locked")
		require.NoError(t, db.AutoMigrate(&models.SecurityState{}))
		require.NoError(t, db.Create(&models.SecurityState{ID: 1, MustAuthLocked: true, LockedAt: timePtr(time.Unix(1, 0).UTC()), LockVersion: 1}).Error)
	})
}

func TestMustAuthStoreDoesNotCreateOrSeed(t *testing.T) {
	db := newMustAuthTestDB(t, "no-auto-create")
	require.False(t, db.Migrator().HasTable(&models.SecurityState{}))
	store := NewMustAuthStore(db, nil)
	_, err := store.Load(context.Background())
	require.ErrorIs(t, err, ErrUnavailable)
	require.False(t, db.Migrator().HasTable(&models.SecurityState{}))
}

func TestMustAuthStoreLoadRequiresExactlyOneHealthyRow(t *testing.T) {
	t.Run("missing table", func(t *testing.T) {
		db := newMustAuthTestDB(t, "load-missing-table")
		_, err := NewMustAuthStore(db, nil).Load(context.Background())
		require.ErrorIs(t, err, ErrUnavailable)
		require.NotContains(t, err.Error(), "no such table")
	})

	t.Run("missing row", func(t *testing.T) {
		db := newMustAuthTestDB(t, "load-missing-row")
		require.NoError(t, db.AutoMigrate(&models.SecurityState{}))
		_, err := NewMustAuthStore(db, nil).Load(context.Background())
		require.ErrorIs(t, err, ErrUnavailable)
	})

	corruptRows := []struct {
		name   string
		locked any
		at     any
		ver    any
	}{
		{name: "unlocked nonzero version", locked: false, at: nil, ver: 1},
		{name: "unlocked timestamp", locked: false, at: time.Unix(1, 0).UTC(), ver: 0},
		{name: "locked zero version", locked: true, at: time.Unix(1, 0).UTC(), ver: 0},
		{name: "locked null timestamp", locked: true, at: nil, ver: 1},
		{name: "null locked flag", locked: nil, at: nil, ver: 0},
		{name: "null version", locked: false, at: nil, ver: nil},
	}
	for _, tc := range corruptRows {
		t.Run(tc.name, func(t *testing.T) {
			db := newMustAuthTestDB(t, "load-corrupt-"+strings.ReplaceAll(tc.name, " ", "-"))
			createUnconstrainedSecurityTable(t, db)
			require.NoError(t, db.Exec("INSERT INTO sys_openapi_security_state (id, must_auth_locked, locked_at, lock_version) VALUES (?, ?, ?, ?)", 1, tc.locked, tc.at, tc.ver).Error)
			_, err := NewMustAuthStore(db, nil).Load(context.Background())
			require.ErrorIs(t, err, ErrUnavailable)
			require.NotContains(t, err.Error(), "constraint")
		})
	}

	t.Run("extra singleton row", func(t *testing.T) {
		db := newMustAuthTestDB(t, "load-extra-row")
		createUnconstrainedSecurityTable(t, db)
		require.NoError(t, db.Exec("INSERT INTO sys_openapi_security_state (id, must_auth_locked, locked_at, lock_version) VALUES (?, ?, ?, ?), (?, ?, ?, ?)", 1, false, nil, 0, 2, false, nil, 0).Error)
		_, err := NewMustAuthStore(db, nil).Load(context.Background())
		require.ErrorIs(t, err, ErrUnavailable)
	})

	t.Run("duplicate singleton row", func(t *testing.T) {
		db := newMustAuthTestDB(t, "load-duplicate-row")
		createUnconstrainedSecurityTable(t, db)
		require.NoError(t, db.Exec("INSERT INTO sys_openapi_security_state (id, must_auth_locked, locked_at, lock_version) VALUES (?, ?, ?, ?), (?, ?, ?, ?)", 1, false, nil, 0, 1, false, nil, 0).Error)
		_, err := NewMustAuthStore(db, nil).Load(context.Background())
		require.ErrorIs(t, err, ErrUnavailable)
	})
}

func TestMustAuthStoreLatchIsPersistentAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-state.sqlite")
	db := openMustAuthFileDB(t, path)
	require.NoError(t, db.AutoMigrate(&models.SecurityState{}))
	require.NoError(t, db.Create(&models.SecurityState{ID: 1}).Error)

	firstNow := time.Date(2026, 9, 6, 12, 34, 56, 123456789, time.FixedZone("CST", 8*60*60))
	store := NewMustAuthStore(db, func() time.Time { return firstNow })
	first, err := store.Latch(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), first.ID)
	require.True(t, first.MustAuthLocked)
	require.Equal(t, int64(1), first.LockVersion)
	require.NotNil(t, first.LockedAt)
	require.Equal(t, firstNow.UTC().Round(0), *first.LockedAt)

	secondNow := firstNow.Add(24 * time.Hour)
	second, err := NewMustAuthStore(db, func() time.Time { return secondNow }).Latch(context.Background())
	require.NoError(t, err)
	require.Equal(t, first, second, "a repeated latch must not advance version or rewrite locked_at")

	dbConn, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, dbConn.Close())
	reopened := openMustAuthFileDB(t, path)
	recovered, err := NewMustAuthStore(reopened, func() time.Time { return secondNow.Add(time.Hour) }).Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, first, recovered, "restart must recover the durable latch")
}

func TestMustAuthStoreLatchConcurrentInstancesHaveOneVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-state-concurrent.sqlite")
	bootstrap := openMustAuthFileDB(t, path)
	require.NoError(t, bootstrap.AutoMigrate(&models.SecurityState{}))
	require.NoError(t, bootstrap.Create(&models.SecurityState{ID: 1}).Error)
	bootstrapConn, err := bootstrap.DB()
	require.NoError(t, err)
	require.NoError(t, bootstrapConn.Close())

	const workers = 12
	stores := make([]*MustAuthStore, 0, workers)
	dbs := make([]*gorm.DB, 0, workers)
	now := time.Date(2026, 9, 6, 13, 0, 0, 0, time.UTC)
	for i := 0; i < workers; i++ {
		db := openMustAuthFileDB(t, path)
		dbs = append(dbs, db)
		stores = append(stores, NewMustAuthStore(db, func() time.Time { return now }))
	}
	t.Cleanup(func() {
		for _, db := range dbs {
			if conn, err := db.DB(); err == nil {
				_ = conn.Close()
			}
		}
	})

	start := make(chan struct{})
	states := make(chan State, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for _, store := range stores {
		store := store
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			state, err := store.Latch(context.Background())
			if err != nil {
				errs <- err
				return
			}
			states <- state
		}()
	}
	close(start)
	wg.Wait()
	close(states)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Len(t, states, workers)
	for state := range states {
		require.True(t, state.MustAuthLocked)
		require.Equal(t, int64(1), state.LockVersion)
	}

	checkDB := openMustAuthFileDB(t, path)
	checkConn, err := checkDB.DB()
	require.NoError(t, err)
	defer checkConn.Close()
	var row models.SecurityState
	result := checkDB.Where("id = ?", 1).Find(&row)
	require.NoError(t, result.Error)
	require.Equal(t, int64(1), result.RowsAffected)
	require.True(t, row.MustAuthLocked)
	require.Equal(t, int64(1), row.LockVersion)
}

func TestMustAuthStoreLatchRollsBackAndFailsClosed(t *testing.T) {
	db := newMustAuthTestDB(t, "latch-rollback")
	require.NoError(t, db.AutoMigrate(&models.SecurityState{}))
	require.NoError(t, db.Create(&models.SecurityState{ID: 1}).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_must_auth_latch BEFORE UPDATE ON sys_openapi_security_state
WHEN NEW.must_auth_locked = 1
BEGIN
  SELECT RAISE(ABORT, 'test rollback');
END`).Error)

	_, err := NewMustAuthStore(db, func() time.Time { return time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC) }).Latch(context.Background())
	require.ErrorIs(t, err, ErrUnavailable)
	var row models.SecurityState
	require.NoError(t, db.Where("id = ?", 1).First(&row).Error)
	require.False(t, row.MustAuthLocked)
	require.Equal(t, int64(0), row.LockVersion)
	require.NoError(t, db.Exec("DROP TRIGGER reject_must_auth_latch").Error)
	_, err = NewMustAuthStore(db, nil).Latch(context.Background())
	require.NoError(t, err)
}

func TestMustAuthStoreNilAndCanceledInputsFailClosed(t *testing.T) {
	_, err := (*MustAuthStore)(nil).Load(context.Background())
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = (*MustAuthStore)(nil).Latch(context.Background())
	require.ErrorIs(t, err, ErrUnavailable)
	db := newMustAuthTestDB(t, "nil-context")
	require.NoError(t, db.AutoMigrate(&models.SecurityState{}))
	require.NoError(t, db.Create(&models.SecurityState{ID: 1}).Error)
	store := NewMustAuthStore(db, nil)
	_, err = store.Load(nil)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = store.Latch(nil)
	require.ErrorIs(t, err, ErrUnavailable)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db = newMustAuthTestDB(t, "canceled")
	require.NoError(t, db.AutoMigrate(&models.SecurityState{}))
	require.NoError(t, db.Create(&models.SecurityState{ID: 1}).Error)
	_, err = NewMustAuthStore(db, nil).Latch(ctx)
	require.ErrorIs(t, err, ErrUnavailable)
}

func newMustAuthTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	return openMustAuthFileDB(t, filepath.Join(t.TempDir(), fmt.Sprintf("%s.sqlite", name)))
}

func openMustAuthFileDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	dsn := path + "?_pragma=busy_timeout(10000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(8)
	raw.SetMaxIdleConns(8)
	require.NoError(t, db.Exec("PRAGMA journal_mode=WAL").Error)
	t.Cleanup(func() { _ = raw.Close() })
	return db
}

func createUnconstrainedSecurityTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(`CREATE TABLE sys_openapi_security_state (
id INTEGER,
must_auth_locked INTEGER NULL,
locked_at DATETIME NULL,
lock_version INTEGER NULL
)`).Error)
}

func timePtr(value time.Time) *time.Time { return &value }
