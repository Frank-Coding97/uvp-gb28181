package cachehelper

import (
	"context"
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
	password := fmt.Sprintf("t13-%d", time.Now().UnixNano())
	return startIsolatedRedisAt(t, dir, password, maxMemory)
}

func startIsolatedRedisAt(t *testing.T, dir, password, maxMemory string) *isolatedRedis {
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
	logPath := filepath.Join(dir, "redis.log")
	maxMemoryLine := ""
	if maxMemory != "" {
		maxMemoryLine = "maxmemory " + maxMemory + "\n"
	}
	config := fmt.Sprintf("bind 127.0.0.1\nport %d\nprotected-mode yes\ndaemonize no\nsupervised no\ndir %s\ndbfilename %s\nappendonly yes\nappendfilename %s\nappendfsync always\nsave \"\"\nrequirepass %s\n%smaxmemory-policy noeviction\nlogfile %s\n",
		port, strconv.Quote(dir), strconv.Quote("dump.rdb"), strconv.Quote("appendonly.aof"), strconv.Quote(password), maxMemoryLine, strconv.Quote(logPath))
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))

	cmd := exec.Command(binary, configPath)
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
			t.Cleanup(server.stop)
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
	if s == nil || s.cmd == nil {
		return
	}
	if s.raw != nil {
		_ = s.raw.Close()
	}
	_ = s.cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() {
		_ = s.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
	s.cmd = nil
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

	server.stop()
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

func TestRedisHelperRestartDoesNotExtendLockTTL(t *testing.T) {
	dir := t.TempDir()
	password := fmt.Sprintf("t13-%d", time.Now().UnixNano())
	first := startIsolatedRedisAt(t, dir, password, "")
	cache := openRedisCache(t, first)
	require.NoError(t, cache.Set(context.Background(), "account_locked:t13", "1", 5*time.Second))
	initial, err := first.raw.PTTL(context.Background(), "account_locked:t13").Result()
	require.NoError(t, err)
	require.Greater(t, initial, int64(0))
	time.Sleep(1500 * time.Millisecond)
	first.stop()

	second := startIsolatedRedisAt(t, dir, password, "")
	defer second.stop()
	after, err := second.raw.PTTL(context.Background(), "account_locked:t13").Result()
	require.NoError(t, err)
	require.Greater(t, after, int64(0))
	require.Less(t, after, initial-500*time.Millisecond)
	require.Greater(t, after, int64(1000))
}
