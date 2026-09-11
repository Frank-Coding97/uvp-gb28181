package tokenhelper

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

func TestSessionTokensCarryStrictBindingClaims(t *testing.T) {
	service := &TokenService{Ctx: context.Background(), JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
	user := &app.ClaimsUser{UserID: 7, Username: "admin"}

	access, err := service.GenerateTokenForSession(user, "sid-1", "access-jti-1")
	require.NoError(t, err)
	accessClaims, err := service.ParseToken(access)
	require.NoError(t, err)
	require.Equal(t, "sid-1", accessClaims.SID)
	require.Equal(t, "access-jti-1", accessClaims.JTI)
	require.Equal(t, TokenUseAccess, accessClaims.TokenUse)
	require.Equal(t, SessionVersion, accessClaims.SessionVersion)

	refresh, err := service.GenerateRefreshTokenForSession(user.UserID, "sid-1", "refresh-jti-1")
	require.NoError(t, err)
	refreshClaims, err := service.ParseRefreshToken(refresh)
	require.NoError(t, err)
	require.Equal(t, "sid-1", refreshClaims.SID)
	require.Equal(t, "refresh-jti-1", refreshClaims.JTI)
	require.Equal(t, TokenUseRefresh, refreshClaims.TokenUse)
	require.Equal(t, SessionVersion, refreshClaims.SessionVersion)
}

func TestSessionTokenParserRejectsLegacyOrCrossUseTokens(t *testing.T) {
	service := &TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
	cases := []struct {
		name  string
		make  func() string
		parse func(string) error
	}{
		{"legacy access claims", func() string {
			return signClaims(t, service.JWTSecret, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 1}, RegisteredClaims: validClaims()})
		}, func(token string) error { _, err := service.ParseToken(token); return err }},
		{"refresh used as access", func() string {
			token, err := service.GenerateRefreshTokenForSession(1, "sid-1", "refresh-jti")
			require.NoError(t, err)
			return token
		}, func(token string) error { _, err := service.ParseToken(token); return err }},
		{"access used as refresh", func() string {
			token, err := service.GenerateTokenForSession(&app.ClaimsUser{UserID: 1}, "sid-1", "access-jti")
			require.NoError(t, err)
			return token
		}, func(token string) error { _, err := service.ParseRefreshToken(token); return err }},
		{"none algorithm", func() string {
			token := jwt.NewWithClaims(jwt.SigningMethodNone, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 1}, SessionClaims: app.SessionClaims{SID: "sid", JTI: "jti", TokenUse: TokenUseAccess, SessionVersion: SessionVersion}, RegisteredClaims: validClaims()})
			encoded, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
			require.NoError(t, err)
			return encoded
		}, func(token string) error { _, err := service.ParseToken(token); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { require.Error(t, tc.parse(tc.make())) })
	}
}

func validClaims() jwt.RegisteredClaims {
	now := time.Now()
	return jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now)}
}

func signClaims(t *testing.T, secret string, claims jwt.Claims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	encoded, err := token.SignedString([]byte(secret))
	require.NoError(t, err)
	return encoded
}
