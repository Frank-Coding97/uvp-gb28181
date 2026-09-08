//go:build windows

package launcher

import (
	"context"
	"crypto/sha256"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

// This opt-in test owns an entire prepared, stopped test installation. Never
// point it at a customer installation: it deliberately pauses this Redis.
func TestWindowsUnresponsiveRedisUsesBoundedOwnedCleanup(t *testing.T) {
	root := os.Getenv("UVP_T16_FAULT_INSTALL_DIR")
	if root == "" {
		t.Skip("requires an explicitly prepared isolated Windows fixture")
	}
	release, err := standalone.LoadRelease(root)
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "t16-stop" {
		t.Fatal("fault test requires the isolated t16-stop release")
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{InstallDir: root, ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), ResourceDir: release.ResourceDir, WebDir: release.WebDir})
	if err != nil {
		t.Fatal(err)
	}
	config, err := standalone.LoadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- Launch(ctx, root, "", func(status Status) {
			if status.State == Ready {
				ready <- struct{}{}
			}
		})
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("fixture startup failed: %v", err)
	case <-time.After(45 * time.Second):
		cancel()
		t.Fatal("fixture startup timed out")
	}
	client := redis.NewClient(&redis.Options{Addr: config.RedisAddress(), Password: config.RedisPassword(), MaxRetries: -1, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second})
	pauseCtx, endPause := context.WithTimeout(context.Background(), 3*time.Second)
	err = client.ClientPause(pauseCtx, 70*time.Second).Err()
	endPause()
	_ = client.Close()
	if err != nil {
		cancel()
		<-done
		t.Fatal("isolated Redis pause failed")
	}
	started := time.Now()
	cancel()
	select {
	case err = <-done:
	case <-time.After(70 * time.Second):
		t.Fatal("owned cleanup exceeded shutdown budget")
	}
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), "redis") || elapsed < 55*time.Second || elapsed > 70*time.Second {
		t.Fatalf("unexpected deadline result: duration=%s error=%v", elapsed, err)
	}
	if _, err := os.Stat(filepath.Join(paths.DataDir, ".uvp-running.json")); err != nil {
		t.Fatal("failed stop did not retain abnormal marker")
	}
	markerPath := filepath.Join(paths.DataDir, ".uvp-running.json")
	markerBefore, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal("read retained abnormal marker", err)
	}
	markerBeforeSHA := sha256.Sum256(markerBefore)
	for _, address := range []string{config.RedisAddress(), config.BackendAddress(), config.MediaAddress()} {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal("owned port not released", address)
		}
		_ = listener.Close()
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("startup cancellation replaced shutdown result")
	}
	t.Logf("unresponsive Redis cleanup returned in %s; marker retained and all ports released", elapsed)

	recoveryCtx, cancelRecovery := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancelRecovery()
	recoveryDone := make(chan error, 1)
	recovered := make(chan Status, 1)
	go func() {
		recoveryDone <- Launch(recoveryCtx, root, "", func(status Status) {
			if status.State == Ready {
				recovered <- status
			}
		})
	}()
	select {
	case status := <-recovered:
		cancelRecovery()
		t.Fatalf("restart after failed stop reached Ready: %+v", status)
	case err := <-recoveryDone:
		if !errors.Is(err, standalone.ErrUncleanRecoveryRequired) {
			t.Fatalf("restart after failed stop returned %v, want %v", err, standalone.ErrUncleanRecoveryRequired)
		}
	case <-time.After(45 * time.Second):
		cancelRecovery()
		t.Fatal("restart after failed stop timed out")
	}
	markerAfter, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal("read abnormal marker after rejected restart", err)
	}
	if sha256.Sum256(markerAfter) != markerBeforeSHA {
		t.Fatal("rejected restart changed the abnormal marker")
	}
	t.Log("restart retained the abnormal marker and required explicit recovery")
}
