package service

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/models"
)

func TestOnlineUserListFiltersAndUsesOneActivityCutoff(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.SysDepartment{}, &models.SysUserSession{}))
	now := time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC)
	status := int8(1)
	require.NoError(t, db.Create(&models.SysDepartment{BaseModel: models.BaseModel{ID: 3}, Name: "研发", Status: &status}).Error)
	require.NoError(t, db.Create(&models.User{BaseModel: models.BaseModel{ID: 7}, Username: "admin", NickName: "管理员", Password: "x", Status: 1, DeptID: 3}).Error)
	active := testSession("sid-active", 7, now)
	active.LastActiveAt = now.Add(-5 * time.Minute)
	idle := testSession("sid-idle", 7, now.Add(-time.Minute))
	idle.LastActiveAt = now.Add(-5*time.Minute - time.Nanosecond)
	require.NoError(t, db.Create([]*models.SysUserSession{active, idle}).Error)
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })

	list, total, err := service.ListOnline(context.Background(), OnlineSessionFilter{Username: "adm", DepartmentID: 3, Status: "active"})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, list, 1)
	require.Equal(t, "sid-active", list[0].SID)
	require.Equal(t, "active", list[0].Status)
	require.Equal(t, "研发", list[0].DepartmentName)
}

func TestOnlineUserListOnlyIncludesActiveValidSessions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.SysDepartment{}, &models.SysUserSession{}))
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create([]models.User{
		{BaseModel: models.BaseModel{ID: 7}, Username: "admin", Password: "x", Status: 1, DeptID: 3},
		{BaseModel: models.BaseModel{ID: 8}, Username: "disabled", Password: "x", Status: 0},
		{BaseModel: models.BaseModel{ID: 9}, Username: "deleted", Password: "x", Status: 1},
	}).Error)
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", 8).Update("status", 0).Error)
	require.NoError(t, db.Delete(&models.User{}, 9).Error)
	boundary := testSession("sid-boundary", 7, now.Add(-time.Minute))
	boundary.LastActiveAt = now.Add(-5 * time.Minute)
	idle := testSession("sid-idle", 7, now)
	idle.LastActiveAt = now.Add(-5*time.Minute - time.Nanosecond)
	revoked := testSession("sid-revoked", 7, now)
	revoked.RevokedAt = &now
	expired := testSession("sid-expired", 7, now)
	expired.SessionExpiresAt = now
	require.NoError(t, db.Create([]*models.SysUserSession{
		testSession("sid-active", 7, now), boundary, idle, revoked, expired,
		testSession("sid-disabled", 8, now), testSession("sid-deleted", 9, now),
	}).Error)
	sessions := NewAuthSessionService(db)
	sessions.SetClock(func() time.Time { return now })
	for _, status := range []string{"", "active", "idle"} {
		t.Run("status="+status, func(t *testing.T) {
			list, total, err := sessions.ListOnline(context.Background(), OnlineSessionFilter{Status: status})
			require.NoError(t, err)
			if status == "idle" {
				require.Zero(t, total)
				require.Empty(t, list)
				return
			}
			require.Equal(t, int64(2), total)
			require.Len(t, list, 2)
			require.Equal(t, "sid-active", list[0].SID)
			require.Equal(t, "sid-boundary", list[1].SID)
			for _, session := range list {
				require.Equal(t, "active", session.Status)
			}
		})
	}
	for page := 1; page <= 2; page++ {
		list, total, err := sessions.ListOnline(context.Background(), OnlineSessionFilter{
			PageNum: page, PageSize: 1, Username: "adm", DepartmentID: 3, ClientIP: "10.0.0.1",
		})
		require.NoError(t, err)
		require.Equal(t, int64(2), total)
		require.Len(t, list, 1)
		require.Equal(t, []string{"sid-active", "sid-boundary"}[page-1], list[0].SID)
	}
}

func TestOnlineUserIdleSessionRemainsValidAndReturnsAfterActivity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.SysDepartment{}, &models.SysUserSession{}))
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&models.User{BaseModel: models.BaseModel{ID: 7}, Username: "admin", Password: "x", Status: 1}).Error)
	idle := testSession("sid-idle", 7, now)
	idle.LastActiveAt = now.Add(-6 * time.Minute)
	require.NoError(t, db.Create(idle).Error)
	sessions := NewAuthSessionService(db)
	sessions.SetClock(func() time.Time { return now })
	list, total, err := sessions.ListOnline(context.Background(), OnlineSessionFilter{})
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, list)
	_, err = sessions.Authenticate(context.Background(), idle.SID, 7)
	require.NoError(t, err)
	require.NoError(t, sessions.Touch(context.Background(), idle.SID))
	list, total, err = sessions.ListOnline(context.Background(), OnlineSessionFilter{})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, list, 1)
	require.Equal(t, idle.SID, list[0].SID)
	require.Equal(t, "active", list[0].Status)
}

func TestOnlineUserForceLogoutRejectsCurrentAndIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.SysUserSession{}))
	now := time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&models.User{BaseModel: models.BaseModel{ID: 7}, Username: "admin", Password: "x", Status: 1}).Error)
	require.NoError(t, db.Create([]*models.SysUserSession{testSession("sid-current", 7, now), testSession("sid-other", 7, now)}).Error)
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })

	_, err = service.ForceLogout(context.Background(), "sid-current", "sid-current", 99)
	require.ErrorIs(t, err, ErrCurrentSessionForceLogout)
	first, err := service.ForceLogout(context.Background(), "sid-other", "sid-current", 99)
	require.NoError(t, err)
	require.True(t, first.Revoked)
	second, err := service.ForceLogout(context.Background(), "sid-other", "sid-current", 99)
	require.NoError(t, err)
	require.False(t, second.Revoked)
	_, err = service.Authenticate(context.Background(), "sid-current", 7)
	require.NoError(t, err)
}

func TestOnlineUserForceLogoutRejectsUnknownSessionWhenNotFoundErrorMasked(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.SysUserSession{}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:disable_raise_record_not_found", func(g *gorm.DB) {
		g.Statement.RaiseErrorOnNotFound = false
	}))

	service := NewAuthSessionService(db)
	_, err = service.ForceLogout(context.Background(), "missing", "sid-current", 99)
	require.ErrorIs(t, err, ErrSessionUnavailable)
}
