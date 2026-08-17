package service

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
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
