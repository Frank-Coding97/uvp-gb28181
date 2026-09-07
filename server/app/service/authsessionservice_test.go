package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

func TestAuthSessionLifecycleTracksIndependentSessionsAndThrottlesActivity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysUserSession{}))

	now := time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC)
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })
	ctx := context.Background()

	first := testSession("sid-a", 7, now)
	second := testSession("sid-b", 7, now)
	require.NoError(t, service.Create(ctx, first))
	require.NoError(t, service.Create(ctx, second))

	got, err := service.Authenticate(ctx, "sid-a", 7)
	require.NoError(t, err)
	require.Equal(t, "sid-a", got.SID)

	first.LastActiveAt = now.Add(-2 * time.Minute)
	require.NoError(t, db.Model(&models.SysUserSession{}).Where("sid = ?", first.SID).Update("last_active_at", first.LastActiveAt).Error)
	require.NoError(t, service.Touch(ctx, "sid-a"))
	var touched models.SysUserSession
	require.NoError(t, db.First(&touched, "sid = ?", "sid-a").Error)
	require.Equal(t, now, touched.LastActiveAt)

	// A second request inside the one-minute write interval must not write again.
	now = now.Add(30 * time.Second)
	require.NoError(t, service.Touch(ctx, "sid-a"))
	require.NoError(t, db.First(&touched, "sid = ?", "sid-a").Error)
	require.Equal(t, now.Add(-30*time.Second), touched.LastActiveAt)

	active, idle := service.Status(touched, now), service.Status(models.SysUserSession{SessionExpiresAt: now.Add(time.Hour), LastActiveAt: now.Add(-6 * time.Minute)}, now)
	require.Equal(t, "active", active)
	require.Equal(t, "idle", idle)
}

func TestAuthSessionRevokeIsIdempotentAndFailClosed(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysUserSession{}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:disable_raise_record_not_found", func(g *gorm.DB) {
		g.Statement.RaiseErrorOnNotFound = false
	}))

	now := time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC)
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })
	require.NoError(t, service.Create(context.Background(), testSession("sid-a", 7, now)))

	revoked, err := service.Revoke(context.Background(), "sid-a", "forced", uintPtr(99))
	require.NoError(t, err)
	require.True(t, revoked)
	revoked, err = service.Revoke(context.Background(), "sid-a", "forced", uintPtr(99))
	require.NoError(t, err)
	require.False(t, revoked)
	_, err = service.Authenticate(context.Background(), "sid-a", 7)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrSessionUnavailable))
}

func testSession(sid string, userID uint, now time.Time) *models.SysUserSession {
	return &models.SysUserSession{
		SID: sid, UserID: userID, ClientIP: "10.0.0.1", LoginLocation: "内网", UserAgent: "test",
		Browser: "test", OS: "test", LoginAt: now, LastActiveAt: now, SessionExpiresAt: now.Add(24 * time.Hour),
	}
}

func uintPtr(value uint) *uint { return &value }
