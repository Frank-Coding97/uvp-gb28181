package cachehelper

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
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type isolatedRedis struct {
	cmd      *exec.Cmd
	raw      *redis.Client
	addr     string
	password string
	dir      string
}

func startIsolatedRedis(t *testing.T, maxMemory string) *isolatedRedis {
	t.Helper()
	dir := t.TempDir()
	password := randomRedisPassword(t)
	return startIsolatedRedisAt(t, dir, password, maxMemory)
}

func startIsolatedRedisAt(t *testing.T, dir, password, maxMemory string) *isolatedRedis {
	return startIsolatedRedisAtWithOptions(t, dir, password, maxMemory, false)
}

func startIsolatedRedisPersistenceFailure(t *testing.T) *isolatedRedis {
	t.Helper()
	dir := t.TempDir()
	return startIsolatedRedisAtWithOptions(t, dir, randomRedisPassword(t), "", true)
}

func startIsolatedRedisAtWithOptions(t *testing.T, dir, password, maxMemory string, persistenceFailure bool) *isolatedRedis {
	t.Helper()
	binary := os.Getenv("UVP_T13_REDIS_SERVER")
	if binary == "" {
		binary = "/opt/homebrew/bin/redis-server"
	}
	if _, err := os.Stat(binary); err != nil {
		if resolved, lookErr := exec.LookPath(binary); lookErr == nil {
			binary = resolved
		} else {
			t.Skipf("T13 real Redis test requires redis-server (%s)", binary)
		}
	}

	port := reserveRedisPort(t)
	configPath := filepath.Join(dir, "redis.conf")
	maxMemoryLine := ""
	if maxMemory != "" {
		maxMemoryLine = "maxmemory " + maxMemory + "\n"
	}
	dbFilename := "dump.rdb"
	saveConfig := "save \"\""
	if persistenceFailure {
		dbFilename = "rdb-failure"
		saveConfig = "save 1 1"
		require.NoError(t, os.Mkdir(filepath.Join(dir, dbFilename), 0o700))
	}
	config := fmt.Sprintf("bind 127.0.0.1\nport %d\nprotected-mode yes\ndaemonize no\nsupervised no\ndir .\ndbfilename %s\nappendonly yes\nappendfilename appendonly.aof\nappendfsync always\n%s\nstop-writes-on-bgsave-error yes\nrequirepass %s\n%smaxmemory-policy noeviction\nlogfile redis.log\n",
		port, dbFilename, saveConfig, password, maxMemoryLine)
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))

	cmd := exec.Command(binary, filepath.Base(configPath))
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	require.NoError(t, cmd.Start())

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	raw := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DialTimeout:  250 * time.Millisecond,
		ReadTimeout:  250 * time.Millisecond,
		WriteTimeout: 250 * time.Millisecond,
	})
	readyUntil := time.Now().Add(8 * time.Second)
	for time.Now().Before(readyUntil) {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		err := raw.Ping(ctx).Err()
		cancel()
		if err == nil {
			server := &isolatedRedis{cmd: cmd, raw: raw, addr: addr, password: password, dir: dir}
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

func reserveRedisPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

func (s *isolatedRedis) stop() {
	_ = s.stopGracefully()
}

func (s *isolatedRedis) stopGracefully() error {
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

func randomRedisPassword(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return "t13-" + hex.EncodeToString(b)
}

func openRedisCache(t *testing.T, server *isolatedRedis) app.CacheInterf {
	t.Helper()
	cache, err := NewRedisHelper(server.addr, server.password, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cache.Close() })
	return cache
}

func TestRedisHelperGetDelConcurrentExactlyOnce(t *testing.T) {
	server := startIsolatedRedis(t, "")
	cache := openRedisCache(t, server)
	ctx := context.Background()
	require.NoError(t, cache.Set(ctx, "t13:qr", "sip-secret-payload", time.Minute))

	const consumers = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes, missing, other := 0, 0, []error{}
	start := make(chan struct{})
	for i := 0; i < consumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			value, err := cache.GetDel(ctx, "t13:qr")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
				if value != "sip-secret-payload" {
					other = append(other, fmt.Errorf("unexpected payload %q", value))
				}
			case errors.Is(err, app.ErrKeyNotFound):
				missing++
			default:
				other = append(other, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Empty(t, other)
	require.Equal(t, 1, successes)
	require.Equal(t, consumers-1, missing)
}

func TestRedisHelperGetDelExpiredAndMissing(t *testing.T) {
	server := startIsolatedRedis(t, "")
	cache := openRedisCache(t, server)
	ctx := context.Background()

	require.NoError(t, cache.Set(ctx, "t13:expired", "sip-secret-payload", 120*time.Millisecond))
	time.Sleep(250 * time.Millisecond)
	value, err := cache.GetDel(ctx, "t13:expired")
	require.ErrorIs(t, err, app.ErrKeyNotFound)
	require.Empty(t, value)

	value, err = cache.GetDel(ctx, "t13:missing")
	require.ErrorIs(t, err, app.ErrKeyNotFound)
	require.Empty(t, value)
}

func TestNewRedisHelperRejectsWrongPasswordAndUnavailablePromptly(t *testing.T) {
	server := startIsolatedRedis(t, "")
	started := time.Now()
	cache, err := NewRedisHelper(server.addr, server.password+"-wrong", 0)
	require.Error(t, err)
	require.Nil(t, cache)
	require.Less(t, time.Since(started), 2*time.Second)

	require.NoError(t, server.stopGracefully())
	started = time.Now()
	cache, err = NewRedisHelper(server.addr, server.password, 0)
	require.Error(t, err)
	require.Nil(t, cache)
	require.Less(t, time.Since(started), 2*time.Second)
}

func TestRedisHelperNoEvictionWriteErrorIsReturned(t *testing.T) {
	server := startIsolatedRedis(t, "2mb")
	cache := openRedisCache(t, server)

	err := cache.Set(context.Background(), "t13:oom", strings.Repeat("x", 8<<20), time.Minute)
	require.Error(t, err)
	require.Contains(t, strings.ToUpper(err.Error()), "OOM")
}

func TestRedisHelperPersistenceWriteErrorIsReturned(t *testing.T) {
	server := startIsolatedRedisPersistenceFailure(t)
	cache := openRedisCache(t, server)
	ctx := context.Background()
	require.NoError(t, cache.Set(ctx, "t13:persist-before-failure", "sip-secret-payload", time.Minute))

	deadline := time.Now().Add(5 * time.Second)
	for {
		info, err := server.raw.Info(ctx, "persistence").Result()
		require.NoError(t, err)
		if strings.Contains(info, "rdb_last_bgsave_status:err") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Redis did not report a failed BGSAVE: %s", info)
		}
		time.Sleep(25 * time.Millisecond)
	}

	err := cache.Set(ctx, "t13:persist-write-failure", "sip-secret-payload", time.Minute)
	require.Error(t, err)
	require.Contains(t, strings.ToUpper(err.Error()), "MISCONF")
	value, err := cache.GetDel(ctx, "t13:persist-before-failure")
	require.Empty(t, value)
	require.Error(t, err)
	require.Contains(t, strings.ToUpper(err.Error()), "MISCONF")
}

func TestRedisHelperRestartDoesNotExtendLockTTL(t *testing.T) {
	dir := t.TempDir()
	password := randomRedisPassword(t)
	first := startIsolatedRedisAt(t, dir, password, "")
	cache := openRedisCache(t, first)
	require.NoError(t, cache.Set(context.Background(), "account_locked:t13", "1", 5*time.Second))
	initial, err := first.raw.PTTL(context.Background(), "account_locked:t13").Result()
	require.NoError(t, err)
	require.Greater(t, initial, int64(0))
	time.Sleep(1500 * time.Millisecond)
	require.NoError(t, first.stopGracefully())

	second := startIsolatedRedisAt(t, dir, password, "")
	defer second.stop()
	after, err := second.raw.PTTL(context.Background(), "account_locked:t13").Result()
	require.NoError(t, err)
	require.Greater(t, after, int64(0))
	require.Less(t, after, initial-500*time.Millisecond)
	require.Greater(t, after, time.Second)
}
