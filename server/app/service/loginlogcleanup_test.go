package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

func TestLoginLogCleanupDeletesInBatchesAndPreservesBoundary(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysLoginLog{}))
	now := time.Date(2026, 8, 18, 3, 0, 0, 0, time.UTC)
	cutoff := now.Add(-180 * 24 * time.Hour)
	rows := make([]models.SysLoginLog, 0, 2503)
	for i := 0; i < 2501; i++ {
		rows = append(rows, loginLogCleanupRow(fmt.Sprintf("old-%d", i), cutoff.Add(-time.Second)))
	}
	rows = append(rows, loginLogCleanupRow("boundary", cutoff), loginLogCleanupRow("fresh", now.Add(-time.Hour)))
	require.NoError(t, db.CreateInBatches(rows, 500).Error)

	deleted, err := NewLoginLogService(db).CleanupBefore(context.Background(), cutoff, 1000)
	require.NoError(t, err)
	require.EqualValues(t, 2501, deleted)
	var remaining []models.SysLoginLog
	require.NoError(t, db.Order("id").Find(&remaining).Error)
	require.Len(t, remaining, 2)
	require.Equal(t, "boundary", remaining[0].Username)
	require.Equal(t, "fresh", remaining[1].Username)
}

func TestLoginLogCleanupStopsAfterFailedSecondBatch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysLoginLog{}))
	cutoff := time.Now().Add(-180 * 24 * time.Hour)
	rows := make([]models.SysLoginLog, 2501)
	for i := range rows {
		rows[i] = loginLogCleanupRow(fmt.Sprintf("old-%d", i), cutoff.Add(-time.Hour))
	}
	require.NoError(t, db.CreateInBatches(rows, 500).Error)
	deleteCalls := 0
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("test:fail_second_login_log_batch", func(tx *gorm.DB) {
		deleteCalls++
		if deleteCalls == 2 {
			tx.AddError(errors.New("second batch failed"))
		}
	}))

	deleted, err := NewLoginLogService(db).CleanupBefore(context.Background(), cutoff, 1000)
	require.ErrorContains(t, err, "second batch failed")
	require.EqualValues(t, 1000, deleted)
	require.Equal(t, 2, deleteCalls)
}

func loginLogCleanupRow(username string, createdAt time.Time) models.SysLoginLog {
	return models.SysLoginLog{
		Username: username, Result: LoginResultFailure, FailureReason: LoginFailurePasswordIncorrect,
		IP: "10.0.0.1", Location: "内网", UserAgent: "test", Browser: "test", OS: "test", CreatedAt: createdAt,
	}
}
