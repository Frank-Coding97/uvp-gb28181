//go:build windows

package standalone

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"uvplatform.cn/uvp-gb28181/internal/standalone/winprocess"
)

const (
	redisAOFIntegrityTimeout        = 45 * time.Second
	redisAOFIntegrityCleanupTimeout = 10 * time.Second
	redisAOFIntegrityReadyInterval  = 25 * time.Millisecond
)

// TestWindowsNormalRedisRejectsTruncatedIncrementalAOF exercises the bundled
// Redis with the normal instance configuration. It deliberately edits only
// the generated port so the test cannot pass by using the restricted recovery
// configuration. The child has no inherited output streams, so a Redis error
// cannot expose the protected password through test output.
func TestWindowsNormalRedisRejectsTruncatedIncrementalAOF(t *testing.T) {
	binary := strings.TrimSpace(os.Getenv("UVP_RECOVERY_REDIS_BINARY"))
	if binary == "" {
		t.Skip("requires UVP_RECOVERY_REDIS_BINARY pointing to bundled Redis 7.2.16")
	}
	absBinary, err := filepath.Abs(binary)
	if err != nil {
		t.Skip("configured recovery Redis executable is unavailable")
	}
	info, err := os.Stat(absBinary)
	if err != nil || info.IsDir() {
		t.Skip("configured recovery Redis executable is unavailable")
	}

	paths := configTestPaths(t)
	config, err := InitializeConfig(paths)
	if err != nil {
		t.Fatal("initialize normal Redis configuration failed")
	}
	port, err := allocateRedisAOFIntegrityPort()
	if err != nil {
		t.Fatal("allocate isolated Redis port failed")
	}
	if err := rewriteRedisAOFIntegrityPort(paths, config.RedisConfigPath, port); err != nil {
		t.Fatal("rewrite generated Redis port failed")
	}
	configRaw, err := readSecureConfigFile(config.RedisConfigPath)
	if err != nil {
		t.Fatal("read generated Redis configuration failed")
	}
	for _, required := range []string{"appendonly yes\n", "aof-load-truncated no\n", "appendfsync always\n", "maxmemory-policy noeviction\n"} {
		if !bytes.Contains(configRaw, []byte(required)) {
			t.Fatal("generated Redis configuration is missing an integrity setting")
		}
	}

	job, err := winprocess.NewJob()
	if err != nil {
		t.Fatal("create Redis process job failed")
	}
	children := make([]*redisAOFIntegrityProcess, 0, 2)
	clients := make([]*redis.Client, 0, 2)
	defer func() {
		for _, client := range clients {
			if client != nil {
				_ = client.Close()
			}
		}
		if err := cleanupRedisAOFIntegrityProcesses(job, children); err != nil {
			t.Error("Redis process cleanup failed")
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), redisAOFIntegrityTimeout)
	defer cancel()

	first, err := startRedisAOFIntegrityProcess(job, absBinary, config.RedisConfigPath, paths.ConfigDir)
	if err != nil {
		t.Fatal("start normal Redis fixture failed")
	}
	children = append(children, first)
	firstClient := newRedisAOFIntegrityClient(port, config.RedisPassword())
	clients = append(clients, firstClient)
	if err := waitRedisAOFIntegrityReady(ctx, first, firstClient); err != nil {
		t.Fatal("normal Redis fixture did not become ready")
	}
	if err := firstClient.Set(ctx, "aof-integrity:one", "persisted-value", 0).Err(); err != nil {
		t.Fatal("seed first AOF value failed")
	}
	if err := firstClient.Set(ctx, "aof-integrity:two", "second-value", 0).Err(); err != nil {
		t.Fatal("seed second AOF value failed")
	}
	// SHUTDOWN closes the client connection before a response is guaranteed;
	// process exit code below is the authoritative successful shutdown result.
	_ = firstClient.Shutdown(ctx).Err()
	_ = firstClient.Close()
	firstResult, err := first.await(ctx)
	if err != nil || firstResult.err != nil || firstResult.code != 0 {
		t.Fatal("normal Redis fixture did not shut down successfully")
	}

	incrementalPath, originalAOF, err := locateRedisAOFIntegrityIncremental(paths.DataDir)
	if err != nil {
		t.Fatal("locate Redis incremental AOF failed")
	}
	if len(originalAOF) <= 3 {
		t.Fatal("Redis incremental AOF is too short to truncate safely")
	}
	truncatedAOF := originalAOF[:len(originalAOF)-3]
	if err := os.Truncate(incrementalPath, int64(len(truncatedAOF))); err != nil {
		t.Fatal("truncate Redis incremental AOF failed")
	}
	truncatedOnDisk, err := os.ReadFile(incrementalPath)
	if err != nil || !bytes.Equal(truncatedOnDisk, truncatedAOF) {
		t.Fatal("truncated Redis incremental AOF could not be verified")
	}

	second, err := startRedisAOFIntegrityProcess(job, absBinary, config.RedisConfigPath, paths.ConfigDir)
	if err != nil {
		t.Fatal("start Redis with truncated AOF failed")
	}
	children = append(children, second)
	secondClient := newRedisAOFIntegrityClient(port, config.RedisPassword())
	clients = append(clients, secondClient)
	secondResult, err := waitRedisAOFIntegrityRejected(ctx, second, secondClient)
	if err != nil {
		t.Fatal("Redis with truncated AOF did not fail closed")
	}
	if secondResult.err != nil || secondResult.code == 0 {
		t.Fatal("Redis accepted the truncated AOF")
	}

	pingCtx, pingCancel := context.WithTimeout(context.Background(), time.Second)
	pingErr := secondClient.Ping(pingCtx).Err()
	pingCancel()
	_ = secondClient.Close()
	if pingErr == nil {
		t.Fatal("Redis with truncated AOF became ready")
	}
	retainedAOF, err := os.ReadFile(incrementalPath)
	if err != nil || !bytes.Equal(retainedAOF, truncatedAOF) {
		t.Fatal("Redis rewrote or removed the truncated AOF")
	}
}

func allocateRedisAOFIntegrityPort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, err
	}
	return port, nil
}

func rewriteRedisAOFIntegrityPort(paths Paths, configPath string, port int) error {
	return withConfigLock(paths.InstallDir, func() error {
		raw, err := readSecureConfigFile(configPath)
		if err != nil {
			return err
		}
		lines := strings.Split(string(raw), "\n")
		found := 0
		for index, line := range lines {
			if !strings.HasPrefix(line, "port ") {
				continue
			}
			if len(strings.Fields(line)) != 2 {
				return errors.New("normal Redis port directive is malformed")
			}
			lines[index] = fmt.Sprintf("port %d", port)
			found++
		}
		if found != 1 {
			return errors.New("normal Redis configuration must contain one port directive")
		}
		return writeSecureConfigFile(configPath, []byte(strings.Join(lines, "\n")), true, nil)
	})
}

func newRedisAOFIntegrityClient(port int, password string) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		Password:     password,
		DB:           0,
		MaxRetries:   -1,
		DialTimeout:  500 * time.Millisecond,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
	})
}

type redisAOFIntegrityProcess struct {
	process *winprocess.Process
	done    chan struct{}
	mu      sync.Mutex
	result  redisAOFIntegrityWaitResult
}

type redisAOFIntegrityWaitResult struct {
	code uint32
	err  error
}

func startRedisAOFIntegrityProcess(job *winprocess.Job, binary, configPath, configDir string) (*redisAOFIntegrityProcess, error) {
	child, err := job.Start(winprocess.StartSpec{
		NoConsole: true,
		Path:      binary,
		Args:      []string{filepath.Base(configPath)},
		Dir:       configDir,
	})
	if err != nil {
		return nil, err
	}
	inJob, err := job.Contains(child)
	if err != nil || !inJob {
		_ = job.Close()
		_, _ = child.Wait()
		return nil, errors.New("Redis process is outside its Job")
	}
	result := &redisAOFIntegrityProcess{process: child, done: make(chan struct{})}
	go result.wait()
	return result, nil
}

func (p *redisAOFIntegrityProcess) wait() {
	code, err := p.process.Wait()
	p.mu.Lock()
	p.result = redisAOFIntegrityWaitResult{code: code, err: err}
	close(p.done)
	p.mu.Unlock()
}

func (p *redisAOFIntegrityProcess) await(ctx context.Context) (redisAOFIntegrityWaitResult, error) {
	select {
	case <-p.done:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.result, nil
	case <-ctx.Done():
		return redisAOFIntegrityWaitResult{}, ctx.Err()
	}
}

func waitRedisAOFIntegrityReady(ctx context.Context, process *redisAOFIntegrityProcess, client *redis.Client) error {
	ticker := time.NewTicker(redisAOFIntegrityReadyInterval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := client.Ping(ctx).Err(); err == nil {
			return nil
		}
		select {
		case <-process.done:
			return errors.New("Redis exited before readiness")
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitRedisAOFIntegrityRejected(ctx context.Context, process *redisAOFIntegrityProcess, client *redis.Client) (redisAOFIntegrityWaitResult, error) {
	ticker := time.NewTicker(redisAOFIntegrityReadyInterval)
	defer ticker.Stop()
	for {
		select {
		case <-process.done:
			return process.await(ctx)
		default:
		}
		pingCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		pingErr := client.Ping(pingCtx).Err()
		cancel()
		if pingErr == nil {
			return redisAOFIntegrityWaitResult{}, errors.New("Redis with truncated AOF became ready")
		}
		select {
		case <-process.done:
			return process.await(ctx)
		case <-ctx.Done():
			return redisAOFIntegrityWaitResult{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func locateRedisAOFIntegrityIncremental(dataDir string) (string, []byte, error) {
	aofDir := filepath.Join(dataDir, "redis", "appendonlydir")
	entries, err := os.ReadDir(aofDir)
	if err != nil {
		return "", nil, err
	}
	var path string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".incr.aof") {
			continue
		}
		candidate := filepath.Join(aofDir, entry.Name())
		info, err := os.Lstat(candidate)
		if err != nil {
			return "", nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", nil, errors.New("Redis incremental AOF is not a regular file")
		}
		if path != "" {
			return "", nil, errors.New("Redis produced multiple incremental AOF files")
		}
		path = candidate
	}
	if path == "" {
		return "", nil, errors.New("Redis incremental AOF was not created")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	return path, raw, nil
}

func cleanupRedisAOFIntegrityProcesses(job *winprocess.Job, children []*redisAOFIntegrityProcess) error {
	if job == nil {
		return errors.New("Redis process job is missing")
	}
	if err := job.Close(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisAOFIntegrityCleanupTimeout)
	defer cancel()
	for _, child := range children {
		if child == nil {
			continue
		}
		if _, err := child.await(ctx); err != nil {
			return errors.New("Redis process did not exit during cleanup")
		}
	}
	return nil
}
