package controllers

import (
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/service"
)

func TestUserSessionRevocationRollsBackWithUserTransaction(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.SysUserSession{}))
	user := &models.User{BaseModel: models.BaseModel{ID: 7}, Username: "admin", Password: "x", Status: 1}
	require.NoError(t, db.Create(user).Error)
	now := time.Now().UTC()
	session := &models.SysUserSession{
		SID: "sid-a", UserID: user.ID, ClientIP: "10.0.0.1", LoginLocation: "内网", UserAgent: "test",
		Browser: "test", OS: "test", LoginAt: now, LastActiveAt: now, SessionExpiresAt: now.Add(time.Hour),
	}
	require.NoError(t, db.Create(session).Error)
	controller := &UserController{AuthSessions: service.NewAuthSessionService(db)}

	rollbackErr := errors.New("rollback")
	err = db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, tx.Model(&models.User{}).Where("id = ?", user.ID).Update("status", 0).Error)
		require.NoError(t, controller.revokeUserSessionsTx(tx, user.ID, "user_disabled"))
		return rollbackErr
	})
	require.ErrorIs(t, err, rollbackErr)
	var stored models.SysUserSession
	require.NoError(t, db.First(&stored, "sid = ?", session.SID).Error)
	require.Nil(t, stored.RevokedAt)
	var storedUser models.User
	require.NoError(t, db.First(&storedUser, user.ID).Error)
	require.Equal(t, int8(1), storedUser.Status)
}
