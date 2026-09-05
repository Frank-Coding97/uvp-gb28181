package auth

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func admissionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.Nonce{}, &models.Audit{}))
	for i := int64(1); i <= 2; i++ {
		require.NoError(t, db.Create(&models.Client{ID: i, AK: fmt.Sprintf("uvp_%032x", i), Name: "test", Status: models.StatusActive, OwnerDeptID: 10, SecretCiphertext: []byte{1}, SecretIV: []byte{1}, SecretKeyID: "test"}).Error)
		require.NoError(t, db.Create(&models.ClientScope{ClientID: i, Scope: "device:list", Enabled: true}).Error)
	}
	return db
}

func admissionRequest(now time.Time) AdmissionRequest {
	return AdmissionRequest{ClientID: 1, SecretVersion: 1, AuthEpoch: 1, ScopeEpoch: 1, Scope: "device:list", Nonce: "000102030405060708090a0b0c0d0e0f", Timestamp: fmt.Sprint(now.Unix()), RequestID: "test-request"}
}

func allowed(_ *gorm.DB) error { return nil }

func TestOpenAPIAdmissionFreshness(t *testing.T) {
	for _, delta := range []int64{-301, -300, 0, 300, 301} {
		t.Run(fmt.Sprint(delta), func(t *testing.T) {
			now := time.Unix(1790000000, 0)
			db := admissionDB(t)
			gate := NewAdmission(db, func() time.Time { return now })
			req := admissionRequest(now)
			req.Timestamp = fmt.Sprint(now.Unix() + delta)
			err := gate.Admit(context.Background(), req, allowed)
			if delta < -300 || delta > 300 {
				require.ErrorIs(t, err, ErrExpired)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestOpenAPINonceConcurrentAndIndependent(t *testing.T) {
	now := time.Unix(1790000000, 0)
	db := admissionDB(t)
	gate := NewAdmission(db, func() time.Time { return now })
	var accepted atomic.Int32
	var replay atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := admissionRequest(now)
			req.RequestID = fmt.Sprint(i)
			err := gate.Admit(context.Background(), req, allowed)
			if err == nil {
				accepted.Add(1)
			} else if errors.Is(err, ErrReplay) {
				replay.Add(1)
			} else {
				t.Errorf("unexpected admission error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	require.EqualValues(t, 1, accepted.Load())
	require.EqualValues(t, 99, replay.Load())
	req := admissionRequest(now)
	req.ClientID = 2
	req.RequestID = "other"
	require.NoError(t, gate.Admit(context.Background(), req, allowed))
	require.NoError(t, db.Model(&models.Client{}).Where("id = ?", 1).Update("secret_version", 2).Error)
	req.ClientID = 1
	req.SecretVersion = 2
	req.RequestID = "rotation"
	require.ErrorIs(t, gate.Admit(context.Background(), req, allowed), ErrReplay)
}

func TestOpenAPIAdmissionRejectionDoesNotConsume(t *testing.T) {
	now := time.Unix(1790000000, 0)
	db := admissionDB(t)
	gate := NewAdmission(db, func() time.Time { return now })
	req := admissionRequest(now)
	require.ErrorIs(t, gate.Admit(context.Background(), req, nil), ErrDenied)
	require.ErrorIs(t, gate.Admit(context.Background(), req, func(*gorm.DB) error { return ErrDenied }), ErrDenied)
	req.SecretVersion = 2
	require.ErrorIs(t, gate.Admit(context.Background(), req, allowed), ErrDenied)
	req.SecretVersion = 1
	require.NoError(t, gate.Admit(context.Background(), req, allowed))
	var count int64
	require.NoError(t, db.Model(&models.Audit{}).Where("result = ?", "started").Count(&count).Error)
	require.EqualValues(t, 1, count)
	// Business work is a later transaction: its rollback cannot restore the nonce.
	require.Error(t, db.Transaction(func(tx *gorm.DB) error { return errors.New("business failed") }))
	require.ErrorIs(t, gate.Admit(context.Background(), req, allowed), ErrReplay)
}

func TestOpenAPIAdmissionAuditAndCommitFailure(t *testing.T) {
	now := time.Unix(1790000000, 0)
	t.Run("audit rollback", func(t *testing.T) {
		db := admissionDB(t)
		gate := NewAdmission(db, func() time.Time { return now })
		require.NoError(t, db.Migrator().DropTable(&models.Audit{}))
		require.ErrorIs(t, gate.Admit(context.Background(), admissionRequest(now), allowed), ErrUnavailable)
		var count int64
		require.NoError(t, db.Model(&models.Nonce{}).Count(&count).Error)
		require.Zero(t, count)
	})
	t.Run("uncertain commit no permission to dispatch", func(t *testing.T) {
		db := admissionDB(t)
		gate := NewAdmission(db, func() time.Time { return now })
		real := gate.transact
		gate.transact = func(ctx context.Context, fn func(*gorm.DB) error) error {
			if err := real(ctx, fn); err != nil {
				return err
			}
			return errors.New("connection lost after commit")
		}
		require.ErrorIs(t, gate.Admit(context.Background(), admissionRequest(now), allowed), ErrUnavailable)
		gate.transact = real
		require.ErrorIs(t, gate.Admit(context.Background(), admissionRequest(now), allowed), ErrReplay)
	})
	t.Run("database unavailable", func(t *testing.T) {
		db := admissionDB(t)
		gate := NewAdmission(db, func() time.Time { return now })
		raw, _ := db.DB()
		require.NoError(t, raw.Close())
		require.ErrorIs(t, gate.Admit(context.Background(), admissionRequest(now), allowed), ErrUnavailable)
	})
}

func TestOpenAPINonceRestartAndCleanup(t *testing.T) {
	now := time.Unix(1790000000, 0)
	db := admissionDB(t)
	gate := NewAdmission(db, func() time.Time { return now })
	req := admissionRequest(now)
	require.NoError(t, gate.Admit(context.Background(), req, allowed))
	gate = NewAdmission(db, func() time.Time { return now })
	require.ErrorIs(t, gate.Admit(context.Background(), req, allowed), ErrReplay)
	now = now.Add(659 * time.Second)
	n, err := gate.Cleanup(context.Background())
	require.NoError(t, err)
	require.Zero(t, n)
	now = now.Add(time.Second)
	n, err = gate.Cleanup(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

func TestOpenAPIAdmissionClockRollbackAndRecoveryFreeze(t *testing.T) {
	now := time.Unix(1790000000, 0)
	db := admissionDB(t)
	gate := NewAdmission(db, func() time.Time { return now })
	require.NoError(t, gate.Admit(context.Background(), admissionRequest(now), allowed))
	now = now.Add(-time.Second)
	require.ErrorIs(t, gate.Admit(context.Background(), admissionRequest(now), allowed), ErrFrozen)
	now = now.Add(2 * time.Second)
	require.ErrorIs(t, gate.Admit(context.Background(), admissionRequest(now), allowed), ErrFrozen)
	gate = NewAdmission(db, func() time.Time { return now })
	gate.Freeze()
	require.ErrorIs(t, gate.Admit(context.Background(), admissionRequest(now), allowed), ErrFrozen)
}

func TestOpenAPIAdmissionCurrentState(t *testing.T) {
	for _, mutation := range []string{"status", "auth_epoch", "secret_version", "scope_enabled", "scope_epoch", "missing_client", "missing_scope"} {
		t.Run(mutation, func(t *testing.T) {
			now := time.Unix(1790000000, 0)
			db := admissionDB(t)
			gate := NewAdmission(db, func() time.Time { return now })
			switch mutation {
			case "status":
				require.NoError(t, db.Model(&models.Client{}).Where("id = ?", 1).Update("status", models.StatusDisabled).Error)
			case "auth_epoch", "secret_version":
				require.NoError(t, db.Model(&models.Client{}).Where("id = ?", 1).Update(mutation, 2).Error)
			case "scope_enabled":
				require.NoError(t, db.Model(&models.ClientScope{}).Where("client_id = ?", 1).Update("enabled", false).Error)
			case "scope_epoch":
				require.NoError(t, db.Model(&models.ClientScope{}).Where("client_id = ?", 1).Update("scope_epoch", 2).Error)
			case "missing_client":
				require.NoError(t, db.Delete(&models.Client{}, 1).Error)
			case "missing_scope":
				require.NoError(t, db.Where("client_id = ?", 1).Delete(&models.ClientScope{}).Error)
			}
			require.ErrorIs(t, gate.Admit(context.Background(), admissionRequest(now), allowed), ErrDenied)
			var count int64
			require.NoError(t, db.Model(&models.Nonce{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestOpenAPINonceDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonce.db")
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		return db
	}
	db := open()
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.Nonce{}, &models.Audit{}))
	require.NoError(t, db.Create(&models.Client{ID: 1, AK: "uvp_000102030405060708090a0b0c0d0e0f", Name: "reopen", Status: models.StatusActive, OwnerDeptID: 10, SecretCiphertext: []byte{1}, SecretIV: []byte{1}, SecretKeyID: "test"}).Error)
	require.NoError(t, db.Create(&models.ClientScope{ClientID: 1, Scope: "device:list", Enabled: true}).Error)
	now := time.Unix(1790000000, 0)
	require.NoError(t, NewAdmission(db, func() time.Time { return now }).Admit(context.Background(), admissionRequest(now), allowed))
	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	db = open()
	raw, err = db.DB()
	require.NoError(t, err)
	defer raw.Close()
	require.ErrorIs(t, NewAdmission(db, func() time.Time { return now }).Admit(context.Background(), admissionRequest(now), allowed), ErrReplay)
}
