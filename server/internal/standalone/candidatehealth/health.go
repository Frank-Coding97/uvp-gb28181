// Package candidatehealth checks migrated state without starting listeners,
// background jobs or automatic migrations. Its caller owns maintenance
// admission and the lifetime of the database and Redis connections.
package candidatehealth

import (
	"context"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/casbinhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func Check(ctx context.Context, db *gorm.DB, cache *redis.Client, cfg app.YmlConfigInterf) error {
	if ctx == nil || db == nil || cache == nil || cfg == nil {
		return errors.New("candidate health: missing dependencies")
	}
	if err := sqlitebootstrap.CheckCurrentSchema(ctx, db); err != nil {
		return errors.New("candidate health: schema")
	}
	if err := probeRedis(ctx, cache); err != nil {
		return errors.New("candidate health: redis")
	}
	if err := casbinhelper.ValidatePolicyReadOnly(ctx, db, cfg.GetString("casbin.modelconfig"), cfg.GetString("casbin.tableprefix"), cfg.GetString("casbin.tablename")); err != nil {
		return errors.New("candidate health: policy")
	}
	for _, query := range []string{
		"SELECT sid,user_id,revoked_at,refresh_token_hash,refresh_jti FROM sys_user_sessions WHERE 1=0",
		"SELECT id,username,status,password FROM sys_users WHERE 1=0",
	} {
		var rows []map[string]any
		if err := db.WithContext(ctx).Raw(query).Scan(&rows).Error; err != nil {
			return errors.New("candidate health: session storage")
		}
	}
	tokens := &tokenhelper.TokenService{JWTSecret: cfg.GetString("token.jwttokensignkey"), TokenExpire: cfg.GetDuration("token.jwttokenexpire")}
	sid, jti := uuid.NewString(), uuid.NewString()
	access, err := tokens.GenerateTokenForSession(&app.ClaimsUser{UserID: 1}, sid, jti)
	if err != nil {
		return errors.New("candidate health: access token construction")
	}
	if _, err := tokens.ParseToken(access); err != nil {
		return errors.New("candidate health: access token verification")
	}
	refresh, err := tokens.GenerateRefreshTokenForSessionUntil(1, sid, uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil {
		return errors.New("candidate health: refresh token construction")
	}
	if _, err := tokens.ParseRefreshToken(refresh); err != nil {
		return errors.New("candidate health: refresh token verification")
	}
	return nil
}

func probeRedis(ctx context.Context, client *redis.Client) error {
	key := "uvp:standalone:maintenance-health:" + uuid.NewString()
	value := uuid.NewString()
	created, err := client.SetNX(ctx, key, value, 10*time.Second).Result()
	if err != nil || !created {
		return errors.New("write failed")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = client.Del(cleanup, key).Err()
	}()
	actual, err := client.Get(ctx, key).Result()
	if err != nil || actual != value {
		return errors.New("read failed")
	}
	deleted, err := client.Del(ctx, key).Result()
	if err != nil || deleted != 1 {
		return errors.New("delete failed")
	}
	return nil
}
