package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/cachehelper"
)

func TestLoginLogManagementDeletesSelectedAndClearsLogs(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	logs := []models.SysLoginLog{{Username: "alice", Result: LoginResultFailure}, {Username: "bob", Result: LoginResultSuccess}}
	require.NoError(t, db.Create(&logs).Error)
	svc := NewLoginLogManagementService(db, nil)

	deleted, err := svc.Delete(context.Background(), []uint{logs[0].ID})
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	cleared, err := svc.Clear(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, cleared)
	var remaining int64
	require.NoError(t, db.Model(&models.SysLoginLog{}).Count(&remaining).Error)
	require.Zero(t, remaining)
}

func TestLoginLogManagementUnlockClearsLockAndFailureCount(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	user := models.User{Username: "locked-user", Password: "hash", Status: 1, Description: "test"}
	require.NoError(t, db.Create(&user).Error)
	log := models.SysLoginLog{UserID: &user.ID, Username: user.Username, Result: LoginResultFailure, FailureReason: LoginFailureAccountLocked}
	require.NoError(t, db.Create(&log).Error)
	cache := cachehelper.NewMemoryHelper()
	t.Cleanup(func() { _ = cache.Close() })
	require.NoError(t, cache.Set(context.Background(), "account_locked:"+user.Username, "1", 3600))
	require.NoError(t, cache.Set(context.Background(), "login_fail_count:"+user.Username, "5", 3600))
	svc := NewLoginLogManagementService(db, cache)

	require.NoError(t, svc.Unlock(context.Background(), log.ID))
	locked, err := cache.Exists(context.Background(), "account_locked:"+user.Username)
	require.NoError(t, err)
	require.Zero(t, locked)
	failCount, err := cache.Exists(context.Background(), "login_fail_count:"+user.Username)
	require.NoError(t, err)
	require.Zero(t, failCount)
}

func TestLoginLogManagementUnlockRejectsMissingLogAndUser(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	cache := cachehelper.NewMemoryHelper()
	t.Cleanup(func() { _ = cache.Close() })
	svc := NewLoginLogManagementService(db, cache)

	require.ErrorIs(t, svc.Unlock(context.Background(), 99), ErrLoginLogNotFound)
	missingUserID := uint(404)
	log := models.SysLoginLog{UserID: &missingUserID, Username: "missing-user", Result: LoginResultFailure, FailureReason: LoginFailureAccountLocked}
	require.NoError(t, db.Create(&log).Error)
	require.ErrorIs(t, svc.Unlock(context.Background(), log.ID), ErrLoginLogUnlockUser)
	locked, err := cache.Exists(context.Background(), "account_locked:")
	require.NoError(t, err)
	require.Zero(t, locked)
}
