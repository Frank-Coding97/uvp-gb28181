package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
)

func TestAuthSessionCreateLoginPersistsOneIndependentSessionPerLogin(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	now := time.Now().UTC()
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })
	tokens := &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
	user := &models.User{BaseModel: models.BaseModel{ID: 7}, Username: "admin", NickName: "管理员", Status: 1}
	metadata := LoginMetadata{ClientIP: "10.0.0.1", LoginLocation: "内网", UserAgent: "test-agent", Browser: "Test", OS: "Test OS"}

	first, err := service.CreateLogin(context.Background(), user, metadata, tokens, 30*24*time.Hour)
	require.NoError(t, err)
	second, err := service.CreateLogin(context.Background(), user, metadata, tokens, 30*24*time.Hour)
	require.NoError(t, err)
	require.NotEqual(t, first.SID, second.SID)
	require.NotEqual(t, first.RefreshToken, second.RefreshToken)

	var count int64
	require.NoError(t, db.Model(&models.SysUserSession{}).Where("user_id = ?", user.ID).Count(&count).Error)
	require.Equal(t, int64(2), count)
	var stored models.SysUserSession
	require.NoError(t, db.First(&stored, "sid = ?", first.SID).Error)
	require.NotNil(t, stored.RefreshTokenHash)
	require.NotEqual(t, first.RefreshToken, *stored.RefreshTokenHash)
	require.True(t, now.Add(30*24*time.Hour).Equal(stored.SessionExpiresAt))
}

func TestAuthSessionCreateLoginDoesNotReturnTokensWhenSessionWriteFails(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	require.NoError(t, db.Exec("DROP TABLE sys_user_sessions").Error)
	now := time.Now().UTC()
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })
	tokens := &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
	user := &models.User{BaseModel: models.BaseModel{ID: 7}, Username: "admin", Status: 1}

	pair, err := service.CreateLogin(context.Background(), user, LoginMetadata{}, tokens, 24*time.Hour)
	require.ErrorIs(t, err, ErrSessionStore)
	require.Nil(t, pair)
}
