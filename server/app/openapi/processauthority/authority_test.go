package processauthority

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func authorityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "authority.sqlite")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&models.ProcessGeneration{}, &models.ProcessAuthority{}))
	return db
}

func authorityLock(t *testing.T) *LocalLock {
	t.Helper()
	lock, err := AcquireLocalLock(privateStateDir(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close() })
	return lock
}

func TestProcessAuthorityRegistrationAndTransactionalChecks(t *testing.T) {
	db, lock := authorityDB(t), authorityLock(t)
	var root registrationLatch
	authority, err := root.register(context.Background(), db, lock)
	require.NoError(t, err)
	require.Len(t, authority.GenerationID(), 32)
	again, err := root.register(context.Background(), db.Session(&gorm.Session{}), lock)
	require.ErrorIs(t, err, ErrProcessAuthorityUnavailable, "reload must reuse root's original handle")
	require.Nil(t, again)
	var rows []models.ProcessGeneration
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, authority.GenerationID(), rows[0].GenerationID)
	require.ErrorIs(t, authority.CheckTx(db), ErrProcessAuthorityUnavailable, "an autocommit handle is not a transaction")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return authority.CheckTx(tx) }))
	other := authorityDB(t)
	require.ErrorIs(t, other.Transaction(func(tx *gorm.DB) error { return authority.CheckTx(tx) }), ErrProcessAuthorityUnavailable)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return authority.CheckTx(tx) }), "wrong argument does not corrupt the real authority")
	var counterfeit Authority
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return counterfeit.CheckTx(tx) }), ErrProcessAuthorityUnavailable)
	require.NoError(t, lock.Close())
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return authority.CheckTx(tx) }), ErrProcessAuthorityUnavailable)
}

func TestProcessAuthorityRetiredGenerationRequiresKnownSameDomain(t *testing.T) {
	db, lock := authorityDB(t), authorityLock(t)
	domain, err := lock.DomainID()
	require.NoError(t, err)
	old := models.ProcessGeneration{GenerationID: strings.Repeat("1", 32), DomainID: domain, StartedAt: time.Now().UTC().Add(-time.Hour)}
	require.NoError(t, db.Create(&old).Error)
	require.NoError(t, db.Omit("Generation").Create(&models.ProcessAuthority{ID: 1, DomainID: domain, CurrentGenerationID: old.GenerationID, RowVersion: 7}).Error)
	var root registrationLatch
	authority, err := root.register(context.Background(), db, lock)
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return authority.RequireRetiredTx(tx, old.GenerationID) }))
	other := models.ProcessGeneration{GenerationID: strings.Repeat("2", 32), DomainID: strings.Repeat("3", 32), StartedAt: old.StartedAt}
	require.NoError(t, db.Create(&other).Error)
	for _, unknown := range []string{"", strings.Repeat("0", 32), strings.Repeat("4", 32), authority.GenerationID(), other.GenerationID} {
		require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return authority.RequireRetiredTx(tx, unknown) }), ErrProcessAuthorityUnavailable)
	}
}

func TestProcessAuthorityRegistrationFailureIsSticky(t *testing.T) {
	for _, kind := range []string{"missing-tables", "domain-mismatch", "missing-singleton", "same-process-generation"} {
		t.Run(kind, func(t *testing.T) {
			db, lock := authorityDB(t), authorityLock(t)
			domain, err := lock.DomainID()
			require.NoError(t, err)
			generation := strings.Repeat("a", 32)
			if kind == "same-process-generation" {
				generation, err = ProcessID()
				require.NoError(t, err)
			}
			if kind == "domain-mismatch" {
				domain = strings.Repeat("b", 32)
			}
			if kind == "missing-tables" {
				require.NoError(t, db.Migrator().DropTable(&models.ProcessAuthority{}, &models.ProcessGeneration{}))
			} else {
				require.NoError(t, db.Create(&models.ProcessGeneration{GenerationID: generation, DomainID: domain, StartedAt: time.Now().UTC()}).Error)
				if kind != "missing-singleton" {
					require.NoError(t, db.Omit("Generation").Create(&models.ProcessAuthority{ID: 1, DomainID: domain, CurrentGenerationID: generation, RowVersion: 1}).Error)
				}
			}
			var root registrationLatch
			authority, err := root.register(context.Background(), db, lock)
			require.ErrorIs(t, err, ErrProcessAuthorityUnavailable)
			require.Nil(t, authority)
			// Supplying a different pristine fixture cannot reset failed root registration.
			authority, err = root.register(context.Background(), authorityDB(t), authorityLock(t))
			require.ErrorIs(t, err, ErrProcessAuthorityUnavailable)
			require.Nil(t, authority)
		})
	}
}
