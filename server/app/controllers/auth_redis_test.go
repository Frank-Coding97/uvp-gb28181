package controllers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
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

type authRedisProcess struct {
	cmd      *exec.Cmd
	raw      *redis.Client
	addr     string
	password string
}

func startAuthPersistenceFailureRedis(t *testing.T) *authRedisProcess {
	t.Helper()
	binary := os.Getenv("UVP_T13_REDIS_SERVER")
	if binary == "" {
		binary = "/opt/homebrew/bin/redis-server"
	}
	if _, err := os.Stat(binary); err != nil {
		if resolved, lookErr := exec.LookPath(binary); lookErr == nil {
			binary = resolved
		} else {
			t.Skipf("T13 real Redis auth test requires redis-server (%s)", binary)
		}
	}

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "rdb-failure"), 0o700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	passwordBytes := make([]byte, 16)
	_, err = rand.Read(passwordBytes)
	require.NoError(t, err)
	password := "t13-" + hex.EncodeToString(passwordBytes)
	configPath := filepath.Join(dir, "redis.conf")
	config := fmt.Sprintf("bind 127.0.0.1\nport %d\nprotected-mode yes\ndaemonize no\nsupervised no\ndir .\ndbfilename rdb-failure\nappendonly yes\nappendfilename appendonly.aof\nappendfsync always\nsave 1 1\nstop-writes-on-bgsave-error yes\nrequirepass %s\nmaxmemory-policy noeviction\nlogfile redis.log\n", port, password)
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))

	cmd := exec.Command(binary, filepath.Base(configPath))
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	require.NoError(t, cmd.Start())
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	raw := redis.NewClient(&redis.Options{Addr: addr, Password: password, DialTimeout: 250 * time.Millisecond, ReadTimeout: 250 * time.Millisecond, WriteTimeout: 250 * time.Millisecond})
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		err = raw.Ping(ctx).Err()
		cancel()
		if err == nil {
			server := &authRedisProcess{cmd: cmd, raw: raw, addr: addr, password: password}
			t.Cleanup(func() { _ = server.stopGracefully() })
			return server
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = raw.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	t.Fatalf("isolated Redis did not become ready")
	return nil
}

func (s *authRedisProcess) stopGracefully() error {
	if s == nil || s.cmd == nil {
		return nil
	}
	if s.raw != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.raw.ShutdownNoSave(ctx).Err()
		cancel()
		_ = s.raw.Close()
	}
	command := s.cmd
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = command.Wait()
		close(done)
	}()
	select {
	case <-done:
		s.cmd = nil
		return waitErr
	case <-time.After(3 * time.Second):
		killErr := command.Process.Kill()
		select {
		case <-done:
			s.cmd = nil
			if killErr != nil {
				return fmt.Errorf("graceful Redis shutdown timed out and kill failed: %w", killErr)
			}
			return errors.New("graceful Redis shutdown timed out; process was killed")
		case <-time.After(3 * time.Second):
			return errors.New("timed out waiting for Redis process to exit")
		}
	}
}

func TestAuthControllerLoginFailsClosedWithRealRedisPersistenceRefusal(t *testing.T) {
	db, _ := setupLoginControllerTest(t, 3)
	seedLoginUser(t, db, "real-cache-persistence-failure-user", "correct-password")

	server := startAuthPersistenceFailureRedis(t)
	cache, err := cachehelper.NewRedisHelper(server.addr, server.password, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cache.Close() })
	ctx := context.Background()
	require.NoError(t, cache.Set(ctx, "t13:persist-before-auth-failure", "sip-secret-payload", time.Minute))

	deadline := time.Now().Add(5 * time.Second)
	for {
		info, infoErr := server.raw.Info(ctx, "persistence").Result()
		require.NoError(t, infoErr)
		if strings.Contains(info, "rdb_last_bgsave_status:err") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Redis did not report a failed BGSAVE: %s", info)
		}
		time.Sleep(25 * time.Millisecond)
	}

	app.Cache = cache
	recorder := invokeLogin(t, NewAuthController(), "real-cache-persistence-failure-user", "wrong-password")
	require.Equal(t, 503, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "correct-password")
}

func seedLoginUser(t *testing.T, db *gorm.DB, username, password string) {
	t.Helper()
	hash, err := passwordhelper.HashPassword(password)
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.User{Username: username, Password: hash, Status: 1}).Error)
}
