package launcher

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"uvplatform.cn/uvp-gb28181/internal/standalone/readiness"
)

func TestRedisReadinessRequiresAuthenticationAndWrites(t *testing.T) {
	binary := os.Getenv("UVP_T13_REDIS_SERVER")
	if binary == "" {
		binary = "/opt/homebrew/bin/redis-server"
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skip("real Redis executable required")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	password, err := readiness.NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	config := fmt.Sprintf("bind 127.0.0.1\nport %d\nprotected-mode yes\ndir .\nsave \"\"\nappendonly yes\nappendfsync always\nrequirepass %s\nmaxmemory-policy noeviction\nlogfile redis.log\n", port, password)
	if err = os.WriteFile(filepath.Join(dir, "redis.conf"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "redis.conf")
	cmd.Dir = dir
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{Addr: address, Password: password, MaxRetries: -1})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = client.Shutdown(ctx).Err()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = client.Close()
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = checkRedis(ctx, address, password)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = checkRedis(ctx, address, "wrong-password"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if err = client.ConfigSet(ctx, "maxmemory", "1").Err(); err != nil {
		t.Fatal(err)
	}
	if err = client.Ping(ctx).Err(); err != nil {
		t.Fatal("fixture should still answer PING")
	}
	if err = checkRedis(ctx, address, password); err == nil {
		t.Fatal("PING-only readiness accepted OOM server")
	}
	if err = client.ConfigSet(ctx, "maxmemory", "0").Err(); err != nil {
		t.Fatal(err)
	}
	if err = checkRedis(ctx, address, password); err != nil {
		t.Fatal(err)
	}
	if err = shutdownRedis(ctx, address, "wrong-password"); err == nil {
		t.Fatal("unauthenticated shutdown accepted")
	}
	if err = client.Ping(ctx).Err(); err != nil {
		t.Fatal("unauthenticated stop affected Redis", err)
	}
	if err = shutdownRedis(ctx, address, password); err != nil {
		t.Fatal("authenticated shutdown failed", err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal("Redis did not exit successfully", err)
	}
}
