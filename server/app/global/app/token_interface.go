package app

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenServiceInterface Token服务接口
type TokenServiceInterface interface {
	// GenerateToken 生成JWT令牌
	GenerateToken(user *ClaimsUser) (string, error)

	// GenerateTokenForSession generates an access token bound to one server session.
	GenerateTokenForSession(user *ClaimsUser, sid, jti string) (string, error)

	// ParseToken 解析JWT令牌
	ParseToken(tokenString string) (*Claims, error)

	// ValidateToken 验证JWT令牌
	ValidateToken(tokenString string) (*Claims, error)

	// GenerateTokenWithCache 生成JWT令牌并存储到缓存
	GenerateTokenWithCache(user *ClaimsUser) (string, error)

	// ValidateTokenWithCache 验证JWT令牌（带缓存检查）
	ValidateTokenWithCache(tokenString string) (*Claims, error)

	// RevokeToken 撤销Token（从缓存中移除）
	RevokeTokenWithCache(tokenString string) error

	// GenerateRefreshToken 生成Refresh Token
	GenerateRefreshToken(userID uint) (string, error)

	// GenerateRefreshTokenForSession generates a refresh token bound to one server session.
	GenerateRefreshTokenForSession(userID uint, sid, jti string) (string, error)

	// GenerateRefreshTokenForSessionUntil signs a refresh token with a fixed absolute expiry.
	GenerateRefreshTokenForSessionUntil(userID uint, sid, jti string, expiresAt time.Time) (string, error)

	// ParseRefreshToken 解析Refresh Token
	ParseRefreshToken(tokenString string) (*RefreshTokenClaims, error)

	// ValidateRefreshToken 验证Refresh Token
	ValidateRefreshToken(tokenString string) (*RefreshTokenClaims, error)

	// RevokeRefreshToken 撤销Refresh Token
	RevokeRefreshToken(userID uint) error

	// RefreshAccessToken 使用Refresh Token刷新Access Token并记录在缓存中
	RefreshAccessTokenWithCache(refreshTokenString string, user *ClaimsUser) (string, error)

	// RotateRefreshToken 轮换Refresh Token（撤销旧的，生成新的，保持相同的剩余过期时间）
	RotateRefreshToken(oldRefreshToken string) (string, error)
}

// ClaimsUser 用户声明信息
type ClaimsUser struct {
	UserID   uint   `json:"userId"`   // 用户ID
	Username string `json:"username"` // 用户名
}

// SessionClaims are mandatory bindings for backend access and refresh tokens.
type SessionClaims struct {
	SID            string `json:"sid"`
	JTI            string `json:"-"` // mirrors jwt.RegisteredClaims.ID after parsing
	TokenUse       string `json:"token_use"`
	SessionVersion int    `json:"session_version"`
}

// Claims JWT声明结构
type Claims struct {
	ClaimsUser
	SessionClaims
	jwt.RegisteredClaims
}

// RefreshTokenClaims Refresh Token声明结构
type RefreshTokenClaims struct {
	UserID uint `json:"userId"`
	SessionClaims
	jwt.RegisteredClaims
}

// RefreshTokenInfo Refresh Token信息
type RefreshTokenInfo struct {
	UserID    uint      `json:"userId"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// TokenInfo Token信息
type TokenInfo struct {
	UserID    uint      `json:"userId"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}
