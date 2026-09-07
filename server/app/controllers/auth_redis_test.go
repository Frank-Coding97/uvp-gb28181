package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/cachehelper"
	"uvplatform.cn/uvp-gb28181/app/utils/passwordhelper"
)

type loginCacheErrorStub struct {
	app.CacheInterf
	existsErr   error
	getErr      error
	setErr      error
	setCalls    int
	setErrAfter int
	delErr      error
}

func (s *loginCacheErrorStub) Exists(context.Context, ...string) (int64, error) {
	return 0, s.existsErr
}

func (s *loginCacheErrorStub) Get(context.Context, string) (string, error) {
	if s.getErr != nil {
		return "", s.getErr
	}
	return s.CacheInterf.Get(context.Background(), "login-failure-count")
}

func (s *loginCacheErrorStub) Set(context.Context, string, string, time.Duration) error {
	s.setCalls++
	if s.setErrAfter > 0 && s.setCalls < s.setErrAfter {
		return nil
	}
	return s.setErr
}

func (s *loginCacheErrorStub) Del(context.Context, ...string) error {
	return s.delErr
}

func newLoginCacheErrorStub(t *testing.T) *loginCacheErrorStub {
	t.Helper()
	base := cachehelper.NewMemoryHelper()
	t.Cleanup(func() { _ = base.Close() })
	return &loginCacheErrorStub{CacheInterf: base}
}

func TestAuthControllerLoginFailsClosedWhenLoginLockReadFails(t *testing.T) {
	db, _ := setupLoginControllerTest(t, 3)
	seedLoginUser(t, db, "cache-read-failure-user", "correct-password")

	cache := newLoginCacheErrorStub(t)
	cache.existsErr = errors.New("redis: connection refused")
	app.Cache = cache

	recorder := invokeLogin(t, NewAuthController(), "cache-read-failure-user", "wrong-password")
	require.Equal(t, 503, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "correct-password")
}

func TestAuthControllerLoginFailsClosedWhenLoginCacheIsMissing(t *testing.T) {
	db, _ := setupLoginControllerTest(t, 3)
	seedLoginUser(t, db, "cache-missing-user", "correct-password")
	app.Cache = nil

	recorder := invokeLogin(t, NewAuthController(), "cache-missing-user", "wrong-password")
	require.Equal(t, 503, recorder.Code)
}

func TestAuthControllerLoginFailsClosedWhenLoginLockWriteFails(t *testing.T) {
	db, _ := setupLoginControllerTest(t, 3)
	seedLoginUser(t, db, "cache-write-failure-user", "correct-password")

	cache := newLoginCacheErrorStub(t)
	cache.getErr = app.ErrKeyNotFound
	cache.setErr = errors.New("OOM command not allowed when used memory > maxmemory")
	app.Cache = cache

	recorder := invokeLogin(t, NewAuthController(), "cache-write-failure-user", "wrong-password")
	require.Equal(t, 503, recorder.Code)
}

func TestAuthControllerLoginFailsClosedWhenAccountLockWriteFails(t *testing.T) {
	db, _ := setupLoginControllerTest(t, 1)
	seedLoginUser(t, db, "cache-account-lock-failure-user", "correct-password")

	cache := newLoginCacheErrorStub(t)
	cache.getErr = app.ErrKeyNotFound
	cache.setErrAfter = 2
	cache.setErr = errors.New("OOM command not allowed when used memory > maxmemory")
	app.Cache = cache

	recorder := invokeLogin(t, NewAuthController(), "cache-account-lock-failure-user", "wrong-password")
	require.Equal(t, 503, recorder.Code)
}

func TestAuthControllerLoginFailsClosedWhenLoginLockClearFails(t *testing.T) {
	db, _ := setupLoginControllerTest(t, 3)
	seedLoginUser(t, db, "cache-clear-failure-user", "correct-password")

	cache := newLoginCacheErrorStub(t)
	cache.delErr = errors.New("MISCONF Redis is configured to save RDB snapshots")
	app.Cache = cache

	recorder := invokeLogin(t, NewAuthController(), "cache-clear-failure-user", "correct-password")
	require.Equal(t, 503, recorder.Code)
}

func seedLoginUser(t *testing.T, db *gorm.DB, username, password string) {
	t.Helper()
	hash, err := passwordhelper.HashPassword(password)
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.User{Username: username, Password: hash, Status: 1}).Error)
}
