package standalone

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestRecoveryRestrictionsExportImportWithAbsoluteExpiry(t *testing.T) {
	server := newRecoveryRedisServer(t)
	source := server.client(0)
	target := server.client(1)
	defer source.Close()
	defer target.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, source.FlushDB(ctx).Err())
	require.NoError(t, target.FlushDB(ctx).Err())

	require.NoError(t, source.Set(ctx, "account_locked:alice", "1", time.Hour).Err())
	require.NoError(t, source.Set(ctx, "login_fail_count:bob", "3", time.Hour).Err())
	require.NoError(t, source.Set(ctx, "account_locked:expired", "1", time.Millisecond).Err())
	require.NoError(t, source.Set(ctx, "unrelated:secret", "do-not-copy", time.Hour).Err())
	time.Sleep(10 * time.Millisecond)

	items, err := exportRecoveryRestrictions(ctx, source)
	require.NoError(t, err)
	require.Len(t, items, 2)
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	require.Equal(t, "account_locked:alice", items[0].Key)
	require.Equal(t, "1", items[0].Value)
	require.Equal(t, "login_fail_count:bob", items[1].Key)
	require.Equal(t, "3", items[1].Value)
	require.Greater(t, items[0].ExpiresAtMillis, time.Now().UnixMilli())
	require.Greater(t, items[1].ExpiresAtMillis, time.Now().UnixMilli())

	require.NoError(t, importRecoveryRestrictions(ctx, target, items))
	for _, item := range items {
		got, err := target.Get(ctx, item.Key).Result()
		require.NoError(t, err)
		require.Equal(t, item.Value, got)
		expiresAt, err := target.Do(ctx, "PEXPIRETIME", item.Key).Int64()
		require.NoError(t, err)
		require.Equal(t, item.ExpiresAtMillis, expiresAt)
	}
	require.EqualValues(t, 2, target.DBSize(ctx).Val())
}

func TestRecoveryRestrictionsExportFailsClosedForInvalidEntries(t *testing.T) {
	server := newRecoveryRedisServer(t)
	source := server.client(0)
	defer source.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tests := []struct {
		name  string
		key   string
		value string
		list  bool
	}{
		{name: "empty username", key: "account_locked:", value: "1"},
		{name: "locked value", key: "account_locked:wrong", value: "yes"},
		{name: "negative count", key: "login_fail_count:negative", value: "-1"},
		{name: "count overflow", key: "login_fail_count:overflow", value: "9223372036854775808"},
		{name: "persistent key", key: "account_locked:persistent", value: "1"},
		{name: "wrong type", key: "login_fail_count:list", list: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, source.FlushDB(ctx).Err())
			if tt.list {
				require.NoError(t, source.LPush(ctx, tt.key, "value").Err())
			} else {
				require.NoError(t, source.Set(ctx, tt.key, tt.value, time.Hour).Err())
				if tt.name == "persistent key" {
					require.NoError(t, source.Persist(ctx, tt.key).Err())
				}
			}
			_, err := exportRecoveryRestrictions(ctx, source)
			require.Error(t, err)
			require.NotContains(t, err.Error(), tt.key)
			if tt.value != "" {
				require.NotContains(t, err.Error(), tt.value)
			}
		})
	}
}

func TestRecoveryRestrictionsImportValidatesBeforeWritingAndRejectsDuplicates(t *testing.T) {
	server := newRecoveryRedisServer(t)
	target := server.client(1)
	defer target.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	valid := recoveryRestriction{Key: "account_locked:alice", Value: "1", ExpiresAtMillis: time.Now().Add(time.Hour).UnixMilli()}
	require.NoError(t, target.FlushDB(ctx).Err())
	require.NoError(t, target.Set(ctx, "existing:key", "value", 0).Err())
	require.Error(t, importRecoveryRestrictions(ctx, target, []recoveryRestriction{valid}))
	_, err := target.Get(ctx, valid.Key).Result()
	require.ErrorIs(t, err, redis.Nil)

	require.NoError(t, target.FlushDB(ctx).Err())
	duplicate := valid
	require.Error(t, importRecoveryRestrictions(ctx, target, []recoveryRestriction{valid, duplicate}))
	_, err = target.Get(ctx, valid.Key).Result()
	require.ErrorIs(t, err, redis.Nil)
}

func TestRecoveryRestrictionsImportSkipsExpiredWithoutReviving(t *testing.T) {
	server := newRecoveryRedisServer(t)
	target := server.client(1)
	defer target.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, target.FlushDB(ctx).Err())

	items := []recoveryRestriction{
		{Key: "account_locked:expired", Value: "1", ExpiresAtMillis: time.Now().Add(-time.Second).UnixMilli()},
		{Key: "login_fail_count:active", Value: "0", ExpiresAtMillis: time.Now().Add(time.Hour).UnixMilli()},
	}
	require.NoError(t, importRecoveryRestrictions(ctx, target, items))
	_, err := target.Get(ctx, "account_locked:expired").Result()
	require.ErrorIs(t, err, redis.Nil)
	got, err := target.Get(ctx, "login_fail_count:active").Result()
	require.NoError(t, err)
	require.Equal(t, "0", got)
}

func TestRecoveryRedisWindowsBundledVersion(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Redis 7.2 opt-in test")
	}
	server := newRecoveryRedisServer(t)
	client := server.client(0)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := client.Info(ctx, "server").Result()
	require.NoError(t, err)
	require.Contains(t, info, "redis_version:7.2.16")
	t.Log("bundled Redis 7.2.16; all restriction tests use newly created loopback test processes")
}

type recoveryRedisServer struct {
	binary string
	dir    string
	port   int
	cmd    *exec.Cmd
}

func newRecoveryRedisServer(t *testing.T) *recoveryRedisServer {
	t.Helper()
	binary := os.Getenv("UVP_RECOVERY_REDIS_BINARY")
	if binary == "" {
		for _, candidate := range []string{"/opt/homebrew/bin/redis-server", "/usr/local/bin/redis-server"} {
			if _, err := os.Stat(candidate); err == nil {
				binary = candidate
				break
			}
		}
	}
	if binary == "" {
		binary, _ = exec.LookPath("redis-server")
	}
	if binary == "" {
		t.Skip("redis-server required for recovery Redis tests")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	dir := t.TempDir()
	config := fmt.Sprintf("bind 127.0.0.1\nport %d\nprotected-mode no\ndaemonize no\nsave \"\"\nappendonly no\nlogfile \"\"\ndir .\n", port)
	configPath := filepath.Join(dir, "redis.conf")
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))
	cmd := exec.Command(binary, filepath.Base(configPath))
	cmd.Dir = dir
	startupLog, err := os.Create(filepath.Join(dir, "startup.log"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = startupLog.Close() })
	cmd.Stdout = startupLog
	cmd.Stderr = startupLog
	require.NoError(t, cmd.Start())
	server := &recoveryRedisServer{binary: binary, dir: dir, port: port, cmd: cmd}
	t.Cleanup(func() {
		client := server.client(0)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = client.Shutdown(ctx).Err()
		cancel()
		_ = client.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	client := server.client(0)
	defer client.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := client.Ping(context.Background()).Err(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			output, _ := os.ReadFile(startupLog.Name())
			t.Fatalf("timed out waiting for test Redis: %s", output)
		}
		time.Sleep(25 * time.Millisecond)
	}
	return server
}

func (s *recoveryRedisServer) client(db int) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: fmt.Sprintf("127.0.0.1:%d", s.port), DB: db})
}
