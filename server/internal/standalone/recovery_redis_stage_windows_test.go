//go:build windows

package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone/winprocess"
)

func TestRecoveryRedisStageConfigUsesRelativeDirAndReadOnlySourceACL(t *testing.T) {
	config, err := recoveryRedisConfig(`C:\control\source`, `C:\data\redis`, 16379, "fixture-password", false)
	if err != nil {
		t.Fatal("building source Redis config failed")
	}
	text := string(config)
	for _, required := range []string{
		"dir \"../../data/redis\"\n",
		"appendonly yes\n",
		"aof-load-truncated no\n",
		"appendfsync always\n",
		"maxmemory-policy noeviction\n",
		"user default off\n",
		"+ping +scan +get +pexpiretime +select +shutdown",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("source Redis config is missing required setting")
		}
	}
	for _, forbidden := range []string{"+set", "+del", "+flushdb", "+eval"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("source Redis config grants a write command")
		}
	}
}

func TestRecoveryRedisStageWindowsSourceACLRejectsWrites(t *testing.T) {
	binary := os.Getenv("UVP_RECOVERY_REDIS_BINARY")
	if binary == "" {
		t.Skip("requires UVP_RECOVERY_REDIS_BINARY pointing to bundled Redis 7.2.16")
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skip("configured recovery Redis executable is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	job, err := newRecoveryRedisStageTestJob()
	if err != nil {
		t.Fatal("create Redis process job failed")
	}
	defer job.Close()
	dataDir := t.TempDir()
	controlDir := t.TempDir()
	configDirs := make([]string, 0, 1)
	child, err := startRecoveryRedis(ctx, job, binary, dataDir, controlDir, 3, false, &configDirs)
	if child != nil {
		defer cleanupRecoveryRedisStage(job, []*recoveryRedisProcess{child}, configDirs)
	}
	if err != nil {
		t.Fatal("start source Redis failed")
	}
	if err := child.client.Set(ctx, "account_locked:should-not-write", "1", time.Minute).Err(); err == nil {
		t.Fatal("source ACL allowed a business write")
	}
	if err := stopRecoveryRedis(ctx, job, child); err != nil {
		t.Fatal("stop source Redis failed")
	}
}

func TestRecoverRedisStageWindowsCopiesOnlyRestrictionsAndPreservesSource(t *testing.T) {
	binary := os.Getenv("UVP_RECOVERY_REDIS_BINARY")
	if binary == "" {
		t.Skip("requires UVP_RECOVERY_REDIS_BINARY pointing to bundled Redis 7.2.16")
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skip("configured recovery Redis executable is unavailable")
	}

	seed := newRecoveryRedisServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	seedClient := seed.client(3)
	validExpiry := time.Now().Add(20 * time.Minute)
	if err := seedClient.Set(ctx, "account_locked:alice", "1", time.Until(validExpiry)).Err(); err != nil {
		t.Fatal("seed account lock failed")
	}
	if err := seedClient.Set(ctx, "login_fail_count:bob", "3", time.Until(validExpiry)).Err(); err != nil {
		t.Fatal("seed login failure count failed")
	}
	if err := seedClient.Set(ctx, "account_locked:expired", "1", 2*time.Millisecond).Err(); err != nil {
		t.Fatal("seed expired lock failed")
	}
	if err := seedClient.Set(ctx, "qr_secret:fixture", "unknown-qr-secret", time.Until(validExpiry)).Err(); err != nil {
		t.Fatal("seed unknown QR value failed")
	}
	if err := seedClient.Set(ctx, "refresh:fixture", "unknown-refresh-secret", time.Until(validExpiry)).Err(); err != nil {
		t.Fatal("seed unknown refresh value failed")
	}
	time.Sleep(20 * time.Millisecond)
	if err := seedClient.Save(ctx).Err(); err != nil {
		t.Fatal("persist source fixture failed")
	}
	// SHUTDOWN closes the connection; the successful process exit below is
	// authoritative even when the client reports EOF or retries the connection.
	_ = seedClient.Shutdown(ctx).Err()
	seedClient.Close()
	if err := seed.cmd.Wait(); err != nil {
		t.Fatal("wait for source fixture failed")
	}

	sourceOriginal := t.TempDir()
	copyRecoveryRedisTree(t, seed.dir, sourceOriginal)
	sourceCopy := t.TempDir()
	copyRecoveryRedisTree(t, sourceOriginal, sourceCopy)
	target := t.TempDir()
	control := t.TempDir()
	originalIdentity, err := recoveryTreeIdentity(context.Background(), sourceOriginal)
	if err != nil {
		t.Fatal("hash source backup fixture failed")
	}

	runCtx, runCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer runCancel()
	if err := recoverRedisStage(runCtx, binary, sourceCopy, target, control, 3); err != nil {
		t.Fatal("offline Redis recovery failed")
	}
	currentIdentity, err := recoveryTreeIdentity(context.Background(), sourceOriginal)
	if err != nil {
		t.Fatal("hash source backup fixture after recovery failed")
	}
	if currentIdentity != originalIdentity {
		t.Fatal("original source backup changed during recovery")
	}
	if !recoveryRedisAOFExists(target) {
		t.Fatal("recovery target AOF was not created")
	}
}

func TestRecoverRedisStageWindowsRejectsInvalidDatabaseIndex(t *testing.T) {
	err := recoverRedisStage(context.Background(), "", "", "", "", 16)
	if err == nil || !strings.Contains(err.Error(), "database index must be between 0 and 15") {
		t.Fatal("invalid Redis database index was not rejected")
	}
}

func TestRecoverRedisStageWindowsRejectsNonEmptyTargetBeforeStartingRedis(t *testing.T) {
	binary := os.Getenv("UVP_RECOVERY_REDIS_BINARY")
	if binary == "" {
		t.Skip("requires UVP_RECOVERY_REDIS_BINARY pointing to bundled Redis 7.2.16")
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skip("configured recovery Redis executable is unavailable")
	}
	sourceCopy := t.TempDir()
	target := t.TempDir()
	control := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "unexpected"), []byte("fixture"), 0o600); err != nil {
		t.Fatal("prepare non-empty target failed")
	}
	err := recoverRedisStage(context.Background(), binary, sourceCopy, target, control, 0)
	if err == nil || !strings.Contains(err.Error(), "target directory must be empty") {
		t.Fatal("non-empty target was not rejected")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("target validation unexpectedly returned cancellation")
	}
}

func copyRecoveryRedisTree(t *testing.T, source, target string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("source fixture contains symbolic link")
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, contents, 0o600)
	})
	if err != nil {
		t.Fatal("copy Redis fixture failed")
	}
}

func recoveryRedisAOFExists(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if entry.Name() == "manifest" || strings.HasSuffix(entry.Name(), ".aof") {
			found = true
		}
		return nil
	})
	return found
}

func newRecoveryRedisStageTestJob() (*winprocess.Job, error) {
	return winprocess.NewJob()
}
