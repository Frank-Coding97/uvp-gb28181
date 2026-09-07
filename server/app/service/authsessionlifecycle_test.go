package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

func TestAuthSessionRevokeAllForUserAndCleanupTerminal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysUserSession{}))
	now := time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC)
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })
	for _, session := range []*models.SysUserSession{
		testSession("sid-a", 7, now),
		testSession("sid-b", 7, now),
		testSession("sid-c", 8, now),
	} {
		require.NoError(t, db.Create(session).Error)
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return service.RevokeAllForUserTx(tx, 7, "user_disabled")
	}))
	var revoked int64
	require.NoError(t, db.Model(&models.SysUserSession{}).Where("user_id = ? AND revoked_at IS NOT NULL", 7).Count(&revoked).Error)
	require.Equal(t, int64(2), revoked)

	old := now.Add(-31 * 24 * time.Hour)
	oldRevoked := testSession("sid-old-revoked", 9, now)
	oldRevoked.RevokedAt = &old
	oldRevoked.RevokeReason = "logout"
	oldExpired := testSession("sid-old-expired", 9, now)
	oldExpired.SessionExpiresAt = old
	boundary := testSession("sid-boundary", 9, now)
	boundaryRevokedAt := now.Add(-30 * 24 * time.Hour)
	boundary.RevokedAt = &boundaryRevokedAt
	active := testSession("sid-active", 9, now)
	require.NoError(t, db.Create([]*models.SysUserSession{oldRevoked, oldExpired, boundary, active}).Error)
	deleted, err := service.CleanupTerminal(context.Background(), now.Add(-30*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)
	var remaining int64
	require.NoError(t, db.Model(&models.SysUserSession{}).Where("sid IN ?", []string{"sid-boundary", "sid-active"}).Count(&remaining).Error)
	require.Equal(t, int64(2), remaining)
}
