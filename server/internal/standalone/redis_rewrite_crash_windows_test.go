//go:build windows

package standalone

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"uvplatform.cn/uvp-gb28181/internal/standalone/winprocess"
)

const (
	redisRewriteCrashRounds        = 20
	redisRewriteCrashTimeout       = 30 * time.Second
	redisRewriteReadyTimeout       = 15 * time.Second
	redisRewritePollInterval       = 2 * time.Millisecond
	redisRewritePayloadSize  int64 = 32 << 20
)

func TestWindowsRedisRewriteCrashRecovery(t *testing.T) {
	binary := strings.TrimSpace(os.Getenv("UVP_RECOVERY_REDIS_BINARY"))
	if binary == "" {
		t.Skip("requires UVP_RECOVERY_REDIS_BINARY pointing to bundled Redis 7.2.16")
	}
	absBinary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatalf("resolve configured bundled Redis: %v", err)
	}
	info, err := os.Stat(absBinary)
	if err != nil || info.IsDir() {
		t.Fatalf("configured bundled Redis is unavailable: %v", err)
	}
	for round := 1; round <= redisRewriteCrashRounds; round++ {
		round := round
		t.Run(fmt.Sprintf("round-%02d", round), func(t *testing.T) {
			if err := runRedisRewriteCrashRound(t, absBinary, round); err != nil {
				t.Error(err)
			}
		})
	}
}

func runRedisRewriteCrashRound(t *testing.T, binary string, round int) (runErr error) {
	t.Helper()
	roundRoot, err := os.MkdirTemp("", "uvp-t27a-redis-rewrite-")
	if err != nil {
		return fmt.Errorf("create round directory: %w", err)
	}
	defer func() {
		if runErr == nil {
			_ = os.RemoveAll(roundRoot)
			return
		}
		t.Logf("retained Redis crash evidence at %s", roundRoot)
	}()

	password, err := newRecoveryRedisPassword()
	if err != nil {
		return fmt.Errorf("generate temporary Redis password: %w", err)
	}
	sourceConfigDir := filepath.Join(roundRoot, "source-config")
	sourceDataDir := filepath.Join(roundRoot, "source-data")
	if err := os.MkdirAll(sourceConfigDir, 0o700); err != nil {
		return fmt.Errorf("create source config directory: %w", err)
	}
	if err := os.MkdirAll(sourceDataDir, 0o700); err != nil {
		return fmt.Errorf("create source data directory: %w", err)
	}
	port, err := allocateRecoveryRedisPort()
	if err != nil {
		return fmt.Errorf("allocate source Redis port: %w", err)
	}
	configPath, err := writeRedisRewriteCrashConfig(sourceConfigDir, sourceDataDir, port, password)
	if err != nil {
		return fmt.Errorf("write source Redis config: %w", err)
	}

	redisProcess, logFile, err := startRedisRewriteCrashProcess(binary, configPath, sourceConfigDir)
	if err != nil {
		return fmt.Errorf("start source Redis: %w", err)
	}
	defer func() {
		if redisProcess != nil && !redisProcess.waited {
			_ = redisProcess.killAndWait()
		}
		if logFile != nil {
			_ = logFile.Close()
		}
	}()
	client := newRedisAOFIntegrityClient(port, password)
	defer client.Close()
	readyCtx, readyCancel := context.WithTimeout(context.Background(), redisRewriteReadyTimeout)
	if err := waitRedisRewriteCrashReady(readyCtx, client); err != nil {
		readyCancel()
		return fmt.Errorf("source Redis did not become ready: %w", err)
	}
	readyCancel()

	sentinels := map[string]string{
		fmt.Sprintf("t27a:%02d:sentinel:alpha", round): "confirmed-alpha",
		fmt.Sprintf("t27a:%02d:sentinel:omega", round): "confirmed-omega",
	}
	writeCtx, writeCancel := context.WithTimeout(context.Background(), redisRewriteCrashTimeout)
	for key, value := range sentinels {
		if err := client.Set(writeCtx, key, value, 0).Err(); err != nil {
			writeCancel()
			return fmt.Errorf("confirmed sentinel write %s: %w", key, err)
		}
	}
	payload := strings.Repeat("0123456789abcdef", int(redisRewritePayloadSize/16))
	bulkKey := fmt.Sprintf("t27a:%02d:rewrite-payload", round)
	if err := client.Set(writeCtx, bulkKey, payload, 0).Err(); err != nil {
		writeCancel()
		return fmt.Errorf("seed rewrite payload: %w", err)
	}
	for key, want := range sentinels {
		got, err := client.Get(writeCtx, key).Result()
		if err != nil {
			writeCancel()
			return fmt.Errorf("confirmed sentinel %s was not readable before crash: %w", key, err)
		}
		if got != want {
			writeCancel()
			return fmt.Errorf("confirmed sentinel %s changed before crash", key)
		}
	}
	writeCancel()

	triggerCtx, triggerCancel := context.WithTimeout(context.Background(), redisRewriteCrashTimeout)
	if err := client.Do(triggerCtx, "BGREWRITEAOF").Err(); err != nil {
		triggerCancel()
		return fmt.Errorf("start AOF rewrite: %w", err)
	}
	barrierErr := waitRedisRewriteCrashBarrier(triggerCtx, client)
	triggerCancel()
	if barrierErr != nil {
		// A configured real Redis that never exposes the in-progress state is a
		// failed crash-window test, never an optional skip. The deferred cleanup
		// kills the process, and this round directory remains for inspection.
		return fmt.Errorf("rewrite barrier not reached: %w", barrierErr)
	}

	// This is deliberately the OS process handle for the parent, rather than a
	// generic job cleanup: the test kills Redis exactly after INFO reports a
	// rewrite in progress, waits for that parent, then closes its Job so a
	// forked rewrite child cannot keep writing the source evidence.
	if err := redisProcess.killAndWait(); err != nil {
		return fmt.Errorf("kill Redis during AOF rewrite: %w", err)
	}
	if err := waitRedisRewriteCrashTreeStable(sourceDataDir); err != nil {
		return fmt.Errorf("wait for retained Redis AOF evidence: %w", err)
	}
	if err := client.Close(); err != nil {
		return fmt.Errorf("close source Redis client: %w", err)
	}

	recoveryDataDir := filepath.Join(roundRoot, "recovery-data")
	if err := os.MkdirAll(recoveryDataDir, 0o700); err != nil {
		return fmt.Errorf("create recovery data directory: %w", err)
	}
	if err := copyRedisRewriteTree(sourceDataDir, recoveryDataDir); err != nil {
		return fmt.Errorf("preserve Redis AOF evidence for recovery: %w", err)
	}
	sourceIdentity, err := recoveryTreeIdentity(context.Background(), sourceDataDir)
	if err != nil {
		return fmt.Errorf("hash retained source Redis evidence: %w", err)
	}
	recoveryIdentity, err := recoveryTreeIdentity(context.Background(), recoveryDataDir)
	if err != nil {
		return fmt.Errorf("hash copied Redis evidence: %w", err)
	}
	if sourceIdentity != recoveryIdentity {
		return errors.New("post-kill Redis evidence changed while being preserved")
	}
	recoveryConfigDir := filepath.Join(roundRoot, "recovery-config")
	if err := os.MkdirAll(recoveryConfigDir, 0o700); err != nil {
		return fmt.Errorf("create recovery config directory: %w", err)
	}
	recoveryPort, err := allocateRecoveryRedisPort()
	if err != nil {
		return fmt.Errorf("allocate recovery Redis port: %w", err)
	}
	recoveryConfigPath, err := writeRedisRewriteCrashConfig(recoveryConfigDir, recoveryDataDir, recoveryPort, password)
	if err != nil {
		return fmt.Errorf("write recovery Redis config: %w", err)
	}
	recoveryProcess, recoveryLog, err := startRedisRewriteCrashProcess(binary, recoveryConfigPath, recoveryConfigDir)
	if err != nil {
		return fmt.Errorf("start Redis from preserved AOF evidence: %w", err)
	}
	defer func() {
		if recoveryProcess != nil && !recoveryProcess.waited {
			_ = recoveryProcess.killAndWait()
		}
		if recoveryLog != nil {
			_ = recoveryLog.Close()
		}
	}()
	recoveryClient := newRedisAOFIntegrityClient(recoveryPort, password)
	defer recoveryClient.Close()
	recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), redisRewriteCrashTimeout)
	if err := waitRedisRewriteCrashReady(recoveryCtx, recoveryClient); err != nil {
		recoveryCancel()
		return fmt.Errorf("confirmed recovery failed from preserved AOF evidence: %w", err)
	}
	for key, want := range sentinels {
		got, err := recoveryClient.Get(recoveryCtx, key).Result()
		if err != nil {
			recoveryCancel()
			return fmt.Errorf("confirmed sentinel %s did not recover: %w", key, err)
		}
		if got != want {
			recoveryCancel()
			return fmt.Errorf("confirmed sentinel %s recovered with unexpected value", key)
		}
	}
	recoveredPayload, err := recoveryClient.Get(recoveryCtx, bulkKey).Result()
	if err != nil || recoveredPayload != payload {
		recoveryCancel()
		return errors.New("confirmed rewrite payload did not recover intact")
	}
	recoveryCancel()

	// The restart copy is disposable. The preserved evidence directory above
	// is never opened by Redis, repaired, truncated, or removed by this test.
	if err := recoveryProcess.killAndWait(); err != nil {
		return fmt.Errorf("stop Redis recovery process: %w", err)
	}
	finalSourceIdentity, err := recoveryTreeIdentity(context.Background(), sourceDataDir)
	if err != nil {
		return fmt.Errorf("hash source Redis evidence after recovery: %w", err)
	}
	if finalSourceIdentity != sourceIdentity {
		return errors.New("source Redis AOF evidence changed during recovery")
	}
	return nil
}

func writeRedisRewriteCrashConfig(configDir, dataDir string, port int, password string) (string, error) {
	if password == "" || strings.ContainsAny(password, "\r\n \t") {
		return "", errors.New("temporary Redis password is invalid")
	}
	relative, err := filepath.Rel(configDir, dataDir)
	if err != nil || relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("Redis data directory must be relative to config directory")
	}
	relative = filepath.ToSlash(relative)
	config := fmt.Sprintf("bind 127.0.0.1\nport %d\nprotected-mode yes\ndaemonize no\nsupervised no\ndatabases 16\ndir %s\nappendonly yes\naof-load-truncated no\nappendfilename appendonly.aof\nappendfsync always\nsave \"\"\nlogfile \"\"\nmaxmemory-policy noeviction\nrequirepass %s\n", port, strconv.Quote(relative), password)
	path := filepath.Join(configDir, "redis.conf")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

type redisRewriteProcess struct {
	parent    *os.Process
	owned     *winprocess.Process
	job       *winprocess.Job
	waited    bool
	jobClosed bool
}

func startRedisRewriteCrashProcess(binary, configPath, configDir string) (*redisRewriteProcess, *os.File, error) {
	logPath := filepath.Join(configDir, "redis-startup.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, err
	}
	job, err := winprocess.NewJob()
	if err != nil {
		_ = logFile.Close()
		return nil, nil, fmt.Errorf("create Redis process job: %w", err)
	}
	owned, err := job.Start(winprocess.StartSpec{
		NoConsole: true,
		Path:      binary,
		Args:      []string{filepath.Base(configPath)},
		Dir:       configDir,
		Stdout:    logFile,
		Stderr:    logFile,
	})
	if err != nil {
		_ = job.Close()
		_ = logFile.Close()
		return nil, nil, err
	}
	parent, err := os.FindProcess(owned.PID())
	if err != nil {
		_ = job.Close()
		_, _ = owned.Wait()
		_ = logFile.Close()
		return nil, nil, fmt.Errorf("open Redis parent process handle: %w", err)
	}
	return &redisRewriteProcess{parent: parent, owned: owned, job: job}, logFile, nil
}

func (p *redisRewriteProcess) killAndWait() error {
	if p == nil {
		return errors.New("Redis process is missing")
	}
	if p.waited {
		if p.job != nil && !p.jobClosed {
			p.jobClosed = true
			return p.job.Close()
		}
		return nil
	}
	if p.parent == nil {
		return errors.New("Redis parent process handle is missing")
	}
	killErr := p.parent.Kill()
	state, parentWaitErr := p.parent.Wait()
	p.waited = true
	var failures []error
	if killErr != nil {
		failures = append(failures, fmt.Errorf("kill Redis parent: %w", killErr))
	}
	if parentWaitErr != nil {
		failures = append(failures, fmt.Errorf("wait for Redis parent: %w", parentWaitErr))
	} else if state == nil || state.Success() {
		failures = append(failures, errors.New("Redis parent exited cleanly after forced kill"))
	}
	if p.job != nil && !p.jobClosed {
		p.jobClosed = true
		if err := p.job.Close(); err != nil {
			failures = append(failures, fmt.Errorf("close Redis process job: %w", err))
		}
	}
	if p.owned != nil {
		if _, err := p.owned.Wait(); err != nil {
			failures = append(failures, fmt.Errorf("wait for owned Redis parent: %w", err))
		}
	}
	return errors.Join(failures...)
}

func waitRedisRewriteCrashReady(ctx context.Context, client *redis.Client) error {
	ticker := time.NewTicker(redisRewritePollInterval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := client.Ping(ctx).Err(); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitRedisRewriteCrashBarrier(ctx context.Context, client *redis.Client) error {
	ticker := time.NewTicker(redisRewritePollInterval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := client.Info(ctx, "persistence").Result()
		if err != nil {
			return fmt.Errorf("INFO persistence: %w", err)
		}
		inProgress, err := redisRewriteCrashInProgress(info)
		if err != nil {
			return err
		}
		if inProgress {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitRedisRewriteCrashTreeStable(root string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	previous := ""
	for {
		identity, err := recoveryTreeIdentity(ctx, root)
		if err == nil && identity == previous {
			return nil
		}
		if err == nil {
			previous = identity
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func redisRewriteCrashInProgress(info string) (bool, error) {
	value := ""
	found := 0
	for _, line := range strings.Split(info, "\n") {
		if !strings.HasPrefix(line, "aof_rewrite_in_progress:") {
			continue
		}
		found++
		value = strings.TrimSpace(strings.TrimPrefix(line, "aof_rewrite_in_progress:"))
	}
	if found != 1 {
		return false, errors.New("INFO persistence omitted aof_rewrite_in_progress")
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || (parsed != 0 && parsed != 1) {
		return false, errors.New("INFO persistence returned an invalid rewrite state")
	}
	return parsed == 1, nil
}

func copyRedisRewriteTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
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
			return errors.New("Redis evidence contains a symbolic link")
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, contents, 0o600)
	})
}
