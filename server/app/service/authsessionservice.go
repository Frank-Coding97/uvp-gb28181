package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
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

func (s *AuthSessionService) Status(session models.SysUserSession, now time.Time) string {
	if session.RevokedAt != nil || !session.SessionExpiresAt.After(now) {
		return "offline"
	}
	if !session.LastActiveAt.Before(now.Add(-s.activeWindow)) {
		return "active"
	}
	return "idle"
}
