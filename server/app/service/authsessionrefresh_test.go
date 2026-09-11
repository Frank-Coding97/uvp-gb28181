package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
)

func TestAuthSessionRefreshCASAllowsExactlyOneWinner(t *testing.T) {
	db := newSQLiteSystemTestDB(t)

	now := time.Now().UTC()
	tokens := &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
	refresh, err := tokens.GenerateRefreshTokenForSessionUntil(7, "sid-a", "refresh-jti", now.Add(24*time.Hour))
	require.NoError(t, err)
	hash := tokenhelper.HashRefreshToken(refresh)
	jti := "refresh-jti"
	session := testSession("sid-a", 7, now)
	session.RefreshTokenHash = &hash
	session.RefreshJTI = &jti
	require.NoError(t, db.Create(&models.User{BaseModel: models.BaseModel{ID: 7}, Username: "admin", Password: "x", Status: 1}).Error)
	require.NoError(t, db.Create(session).Error)

	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, rotateErr := service.RotateRefresh(context.Background(), refresh, tokens)
			results <- rotateErr
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for result := range results {
		if result == nil {
			success++
		}
	}
	require.Equal(t, 1, success)
}

func TestRefreshAndForceLogoutAlwaysEndOffline(t *testing.T) {
	for _, order := range []string{"refresh-first", "logout-first"} {
		t.Run(order, func(t *testing.T) {
			db := newSQLiteSystemTestDB(t)
			now := time.Now().UTC()
			tokens := &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
			refresh, err := tokens.GenerateRefreshTokenForSessionUntil(7, "sid-a", "refresh-jti", now.Add(24*time.Hour))
			require.NoError(t, err)
			hash, jti := tokenhelper.HashRefreshToken(refresh), "refresh-jti"
			session := testSession("sid-a", 7, now)
			session.RefreshTokenHash, session.RefreshJTI = &hash, &jti
			require.NoError(t, db.Create(&models.User{BaseModel: models.BaseModel{ID: 7}, Username: "admin", Password: "x", Status: 1}).Error)
			require.NoError(t, db.Create(session).Error)
			service := NewAuthSessionService(db)
			service.SetClock(func() time.Time { return now })

			if order == "refresh-first" {
				_, err = service.RotateRefresh(context.Background(), refresh, tokens)
				require.NoError(t, err)
				_, err = service.Revoke(context.Background(), "sid-a", "forced", uintPtr(99))
				require.NoError(t, err)
			} else {
				_, err = service.Revoke(context.Background(), "sid-a", "forced", uintPtr(99))
				require.NoError(t, err)
				_, err = service.RotateRefresh(context.Background(), refresh, tokens)
				require.Error(t, err)
			}
			_, err = service.Authenticate(context.Background(), "sid-a", 7)
			require.ErrorIs(t, err, ErrSessionUnavailable)
		})
	}
}

var _ app.TokenServiceInterface = (*tokenhelper.TokenService)(nil)
