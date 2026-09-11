package service

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
)

func TestRestoreRevokesEverySessionAndClearsHistoricalRefreshMaterial(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	svc := NewAuthSessionService(db)
	svc.SetClock(func() time.Time { return now })
	old := now.Add(-time.Hour)
	for i, sid := range []string{"live", "expired", "revoked"} {
		s := testSession(sid, uint(i+1), now)
		hash, jti := "historical-hash", "historical-jti"
		s.RefreshTokenHash, s.RefreshJTI = &hash, &jti
		if sid == "expired" {
			s.SessionExpiresAt = old
		}
		if sid == "revoked" {
			s.RevokedAt, s.RevokeReason = &old, "logout"
		}
		require.NoError(t, db.Create(s).Error)
	}
	rollback := errors.New("injected later restore failure")
	err := db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, svc.RevokeAllForRestoreTx(tx))
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	var unchanged models.SysUserSession
	require.NoError(t, db.First(&unchanged, "sid = ?", "live").Error)
	require.Nil(t, unchanged.RevokedAt)
	require.NotNil(t, unchanged.RefreshTokenHash)
	for attempt := 0; attempt < 2; attempt++ {
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return svc.RevokeAllForRestoreTx(tx) }))
	}
	var sessions []models.SysUserSession
	require.NoError(t, db.Find(&sessions).Error)
	require.Len(t, sessions, 3)
	for _, s := range sessions {
		require.NotNil(t, s.RevokedAt)
		require.Nil(t, s.RefreshTokenHash)
		require.Nil(t, s.RefreshJTI)
		if s.SID == "revoked" {
			require.True(t, old.Equal(*s.RevokedAt))
			require.Equal(t, "logout", s.RevokeReason)
		} else {
			require.True(t, now.Equal(*s.RevokedAt))
			require.Equal(t, "backup_restore", s.RevokeReason)
		}
	}
	require.Error(t, svc.RevokeAllForRestoreTx(nil))
}
