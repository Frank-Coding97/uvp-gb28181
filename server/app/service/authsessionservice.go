package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
)

var (
	ErrSessionUnavailable = errors.New("session unavailable")
	ErrSessionStore       = errors.New("session store unavailable")
)

const (
	defaultActivityWindow = 5 * time.Minute
	defaultTouchInterval  = time.Minute
)

// AuthSessionService owns the persistent lifecycle of backend login sessions.
type AuthSessionService struct {
	db            *gorm.DB
	now           func() time.Time
	activeWindow  time.Duration
	touchInterval time.Duration
}

type SessionTokenPair struct {
	AccessToken  string
	RefreshToken string
}

func NewAuthSessionService(db *gorm.DB) *AuthSessionService {
	return &AuthSessionService{
		db: db, now: time.Now,
		activeWindow: defaultActivityWindow, touchInterval: defaultTouchInterval,
	}
}

func (s *AuthSessionService) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *AuthSessionService) ValidateSession(ctx context.Context, sid string, userID uint) error {
	_, err := s.Authenticate(ctx, sid, userID)
	return err
}

func (s *AuthSessionService) TouchSession(ctx context.Context, sid string) error {
	return s.Touch(ctx, sid)
}

func (s *AuthSessionService) Create(ctx context.Context, session *models.SysUserSession) error {
	if session == nil || session.SID == "" || session.UserID == 0 || session.SessionExpiresAt.IsZero() {
		return fmt.Errorf("%w: invalid session", ErrSessionStore)
	}
	now := s.now()
	if session.LoginAt.IsZero() {
		session.LoginAt = now
	}
	if session.LastActiveAt.IsZero() {
		session.LastActiveAt = now
	}
	if session.LoginLocation == "" {
		session.LoginLocation = "未知"
	}
	if session.Browser == "" {
		session.Browser = "未知"
	}
	if session.OS == "" {
		session.OS = "未知"
	}
	if err := s.db.WithContext(ctx).Create(session).Error; err != nil {
		return fmt.Errorf("%w: %v", ErrSessionStore, err)
	}
	return nil
}

func (s *AuthSessionService) Authenticate(ctx context.Context, sid string, userID uint) (*models.SysUserSession, error) {
	var session models.SysUserSession
	err := s.db.WithContext(ctx).
		Where("sid = ? AND user_id = ? AND revoked_at IS NULL AND session_expires_at > ?", sid, userID, s.now()).
		First(&session).Error
	if err == nil {
		return &session, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionUnavailable
	}
	return nil, fmt.Errorf("%w: %v", ErrSessionStore, err)
}

func (s *AuthSessionService) Touch(ctx context.Context, sid string) error {
	now := s.now()
	result := s.db.WithContext(ctx).Model(&models.SysUserSession{}).
		Where("sid = ? AND revoked_at IS NULL AND session_expires_at > ? AND last_active_at < ?", sid, now, now.Add(-s.touchInterval)).
		Updates(map[string]any{"last_active_at": now, "updated_at": now})
	if result.Error != nil {
		return fmt.Errorf("%w: %v", ErrSessionStore, result.Error)
	}
	return nil
}

func (s *AuthSessionService) Revoke(ctx context.Context, sid, reason string, revokedBy *uint) (bool, error) {
	now := s.now()
	result := s.db.WithContext(ctx).Model(&models.SysUserSession{}).
		Where("sid = ? AND revoked_at IS NULL", sid).
		Updates(map[string]any{
			"revoked_at": now, "revoke_reason": reason, "revoked_by": revokedBy,
			"refresh_token_hash": nil, "refresh_jti": nil, "updated_at": now,
		})
	if result.Error != nil {
		return false, fmt.Errorf("%w: %v", ErrSessionStore, result.Error)
	}
	return result.RowsAffected == 1, nil
}

// RotateRefresh performs a database CAS before returning newly signed tokens.
func (s *AuthSessionService) RotateRefresh(ctx context.Context, rawRefresh string, tokens app.TokenServiceInterface) (*SessionTokenPair, error) {
	claims, err := tokens.ParseRefreshToken(rawRefresh)
	if err != nil {
		return nil, ErrSessionUnavailable
	}
	now := s.now()
	var session models.SysUserSession
	if err := s.db.WithContext(ctx).
		Where("sid = ? AND user_id = ? AND revoked_at IS NULL AND session_expires_at > ? AND EXISTS (SELECT 1 FROM sys_users WHERE sys_users.id = sys_user_sessions.user_id AND sys_users.status = ? AND sys_users.deleted_at IS NULL)", claims.SID, claims.UserID, now, 1).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionUnavailable
		}
		return nil, fmt.Errorf("%w: %v", ErrSessionStore, err)
	}
	oldHash := tokenhelper.HashRefreshToken(rawRefresh)
	if session.RefreshTokenHash == nil || *session.RefreshTokenHash != oldHash || session.RefreshJTI == nil || *session.RefreshJTI != claims.JTI {
		return nil, ErrSessionUnavailable
	}
	newJTI := uuid.NewString()
	newRefresh, err := tokens.GenerateRefreshTokenForSessionUntil(claims.UserID, claims.SID, newJTI, session.SessionExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("%w: sign refresh: %v", ErrSessionStore, err)
	}
	newAccess, err := tokens.GenerateTokenForSession(&app.ClaimsUser{UserID: claims.UserID}, claims.SID, uuid.NewString())
	if err != nil {
		return nil, fmt.Errorf("%w: sign access: %v", ErrSessionStore, err)
	}
	newHash := tokenhelper.HashRefreshToken(newRefresh)
	result := s.db.WithContext(ctx).Model(&models.SysUserSession{}).
		Where("sid = ? AND user_id = ? AND refresh_token_hash = ? AND refresh_jti = ? AND revoked_at IS NULL AND session_expires_at > ? AND EXISTS (SELECT 1 FROM sys_users WHERE sys_users.id = sys_user_sessions.user_id AND sys_users.status = ? AND sys_users.deleted_at IS NULL)", claims.SID, claims.UserID, oldHash, claims.JTI, now, 1).
		Updates(map[string]any{"refresh_token_hash": newHash, "refresh_jti": newJTI, "last_active_at": now, "updated_at": now})
	if result.Error != nil {
		return nil, fmt.Errorf("%w: rotate: %v", ErrSessionStore, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrSessionUnavailable
	}
	return &SessionTokenPair{AccessToken: newAccess, RefreshToken: newRefresh}, nil
}

func (s *AuthSessionService) Status(session models.SysUserSession, now time.Time) string {
	if session.RevokedAt != nil || !session.SessionExpiresAt.After(now) {
		return "offline"
	}
	if !session.LastActiveAt.Before(now.Add(-s.activeWindow)) {
		return "active"
	}
	return "idle"
}
