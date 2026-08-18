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
	SID          string
}

type LoginMetadata struct {
	ClientIP      string
	LoginLocation string
	UserAgent     string
	Browser       string
	OS            string
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

// CreateLogin signs one access/refresh pair and persists its independently revocable session.
// Tokens are returned only after the session transaction commits.
func (s *AuthSessionService) CreateLogin(ctx context.Context, user *models.User, metadata LoginMetadata, tokens app.TokenServiceInterface, sessionTTL time.Duration) (*SessionTokenPair, error) {
	if user == nil || user.ID == 0 || user.Status != 1 || tokens == nil || sessionTTL <= 0 {
		return nil, fmt.Errorf("%w: invalid login", ErrSessionStore)
	}
	now := s.now()
	sid, accessJTI, refreshJTI := uuid.NewString(), uuid.NewString(), uuid.NewString()
	expiresAt := now.Add(sessionTTL)
	accessToken, err := tokens.GenerateTokenForSession(&app.ClaimsUser{UserID: user.ID, Username: user.Username}, sid, accessJTI)
	if err != nil {
		return nil, fmt.Errorf("%w: sign access: %v", ErrSessionStore, err)
	}
	refreshToken, err := tokens.GenerateRefreshTokenForSessionUntil(user.ID, sid, refreshJTI, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("%w: sign refresh: %v", ErrSessionStore, err)
	}
	refreshHash := tokenhelper.HashRefreshToken(refreshToken)
	session := &models.SysUserSession{
		SID: sid, UserID: user.ID, RefreshTokenHash: &refreshHash, RefreshJTI: &refreshJTI,
		ClientIP: metadata.ClientIP, LoginLocation: metadata.LoginLocation, UserAgent: metadata.UserAgent,
		Browser: metadata.Browser, OS: metadata.OS, LoginAt: now, LastActiveAt: now, SessionExpiresAt: expiresAt,
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
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Create(session).Error
	}); err != nil {
		return nil, fmt.Errorf("%w: create login: %v", ErrSessionStore, err)
	}
	return &SessionTokenPair{AccessToken: accessToken, RefreshToken: refreshToken, SID: sid}, nil
}

func (s *AuthSessionService) Authenticate(ctx context.Context, sid string, userID uint) (*models.SysUserSession, error) {
	var session models.SysUserSession
	result := s.db.WithContext(ctx).
		Where("sid = ? AND user_id = ? AND revoked_at IS NULL AND session_expires_at > ?", sid, userID, s.now()).
		First(&session)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %v", ErrSessionStore, result.Error)
	}
	// gormhelper disables RaiseErrorOnNotFound globally, so RowsAffected is the
	// authoritative signal for an absent, revoked, or expired session.
	if result.RowsAffected != 1 {
		return nil, ErrSessionUnavailable
	}
	return &session, nil
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

// RevokeAllForUserTx revokes every still-live session using the caller's transaction.
func (s *AuthSessionService) RevokeAllForUserTx(tx *gorm.DB, userID uint, reason string) error {
	if tx == nil || userID == 0 {
		return fmt.Errorf("%w: invalid user session transaction", ErrSessionStore)
	}
	now := s.now()
	return tx.Model(&models.SysUserSession{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Updates(map[string]any{
			"revoked_at": now, "revoke_reason": reason,
			"refresh_token_hash": nil, "refresh_jti": nil, "updated_at": now,
		}).Error
}

// CleanupTerminal removes revoked or naturally expired sessions older than the cutoff.
func (s *AuthSessionService) CleanupTerminal(ctx context.Context, cutoff time.Time) (int64, error) {
	result := s.db.WithContext(ctx).Where(
		"(revoked_at IS NOT NULL AND revoked_at < ?) OR (revoked_at IS NULL AND session_expires_at < ?)",
		cutoff, cutoff,
	).Delete(&models.SysUserSession{})
	if result.Error != nil {
		return 0, fmt.Errorf("%w: cleanup sessions: %v", ErrSessionStore, result.Error)
	}
	return result.RowsAffected, nil
}

// RotateRefresh performs a database CAS before returning newly signed tokens.
func (s *AuthSessionService) RotateRefresh(ctx context.Context, rawRefresh string, tokens app.TokenServiceInterface) (*SessionTokenPair, error) {
	claims, err := tokens.ParseRefreshToken(rawRefresh)
	if err != nil {
		return nil, ErrSessionUnavailable
	}
	now := s.now()
	var session models.SysUserSession
	sessionResult := s.db.WithContext(ctx).
		Where("sid = ? AND user_id = ? AND revoked_at IS NULL AND session_expires_at > ? AND EXISTS (SELECT 1 FROM sys_users WHERE sys_users.id = sys_user_sessions.user_id AND sys_users.status = ? AND sys_users.deleted_at IS NULL)", claims.SID, claims.UserID, now, 1).
		First(&session)
	if sessionResult.Error != nil && !errors.Is(sessionResult.Error, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %v", ErrSessionStore, sessionResult.Error)
	}
	if sessionResult.RowsAffected != 1 {
		return nil, ErrSessionUnavailable
	}
	var user models.User
	userResult := s.db.WithContext(ctx).Select("id", "username").Where("id = ? AND status = ?", claims.UserID, 1).First(&user)
	if userResult.Error != nil && !errors.Is(userResult.Error, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: load refresh user: %v", ErrSessionStore, userResult.Error)
	}
	if userResult.RowsAffected != 1 {
		return nil, ErrSessionUnavailable
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
	newAccess, err := tokens.GenerateTokenForSession(&app.ClaimsUser{UserID: claims.UserID, Username: user.Username}, claims.SID, uuid.NewString())
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
	return &SessionTokenPair{AccessToken: newAccess, RefreshToken: newRefresh, SID: claims.SID}, nil
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
