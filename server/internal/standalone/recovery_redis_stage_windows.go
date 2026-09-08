//go:build windows

package standalone

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"uvplatform.cn/uvp-gb28181/internal/standalone/winprocess"
)

const (
	recoveryRedisStageTimeout  = 2 * time.Minute
	recoveryRedisCleanupWait   = 10 * time.Second
	recoveryRedisReadyInterval = 25 * time.Millisecond
)

// recoverRedisStage rebuilds only the security restriction keyspaces in a
// disposable Redis target. The caller must pass sourceCopyDir, a disposable
// copy of the backup Redis data. This function intentionally never starts
// Redis in an original backup directory because it cannot identify one safely.
// indexDB is the selected Redis database and must be in the configured
// default range 0..15.
// The caller retains the instance lock and restoring gate for the whole call.
func recoverRedisStage(ctx context.Context, redisExe, sourceCopyDir, targetDir, controlDir string, indexDB int) (resultErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if indexDB < 0 || indexDB > 15 {
		return errors.New("recovery Redis database index must be between 0 and 15")
	}
	if err := validateRecoveryRedisStageInputs(redisExe, sourceCopyDir, targetDir, controlDir); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := prepareRecoveryRedisControlDir(controlDir); err != nil {
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, recoveryRedisStageTimeout)
	defer cancel()

	job, err := winprocess.NewJob()
	if err != nil {
		return fmt.Errorf("create recovery Redis process job: %w", err)
	}
	children := make([]*recoveryRedisProcess, 0, 3)
	configDirs := make([]string, 0, 3)
	defer func() {
		cleanupErr := cleanupRecoveryRedisStage(job, children, configDirs)
		if cleanupErr != nil {
			if resultErr == nil {
				resultErr = cleanupErr
			} else {
				resultErr = errors.Join(resultErr, cleanupErr)
			}
		}
	}()

	source, err := startRecoveryRedis(runCtx, job, redisExe, sourceCopyDir, controlDir, indexDB, false, &configDirs)
	if source != nil {
		children = append(children, source)
	}
	if err != nil {
		return err
	}
	items, err := exportRecoveryRestrictions(runCtx, source.client)
	if err != nil {
		return err
	}
	if err := stopRecoveryRedis(runCtx, job, source); err != nil {
		return err
	}

	target, err := startRecoveryRedis(runCtx, job, redisExe, targetDir, controlDir, indexDB, true, &configDirs)
	if target != nil {
		children = append(children, target)
	}
	if err != nil {
		return err
	}
	if err := ensureRecoveryRedisTargetEmpty(runCtx, target.client); err != nil {
		return err
	}
	if err := importRecoveryRestrictions(runCtx, target.client, items); err != nil {
		return err
	}
	if err := stopRecoveryRedis(runCtx, job, target); err != nil {
		return err
	}

	// The data directory is deliberately reused. A fresh protected config and
	// port exercise the AOF written by the first target process.
	restarted, err := startRecoveryRedis(runCtx, job, redisExe, targetDir, controlDir, indexDB, true, &configDirs)
	if restarted != nil {
		children = append(children, restarted)
	}
	if err != nil {
		return err
	}
	if err := verifyRecoveryRestrictions(runCtx, restarted.client, items); err != nil {
		return err
	}
	if err := stopRecoveryRedis(runCtx, job, restarted); err != nil {
		return err
	}
	return nil
}

type recoveryRedisProcess struct {
	client  *redis.Client
	process *winprocess.Process
	done    chan struct{}
	mu      sync.Mutex
	result  recoveryRedisWaitResult
}

type recoveryRedisWaitResult struct {
	code uint32
	err  error
}

func startRecoveryRedis(ctx context.Context, job *winprocess.Job, redisExe, dataDir, controlDir string, indexDB int, allowWrites bool, configDirs *[]string) (*recoveryRedisProcess, error) {
	if indexDB < 0 || indexDB > 15 {
		return nil, errors.New("recovery Redis database index must be between 0 and 15")
	}
	password, err := newRecoveryRedisPassword()
	if err != nil {
		return nil, errors.New("generate temporary recovery Redis password")
	}
	port, err := allocateRecoveryRedisPort()
	if err != nil {
		return nil, errors.New("allocate temporary recovery Redis port")
	}
	configDir, err := os.MkdirTemp(controlDir, "redis-")
	if err != nil {
		return nil, errors.New("create temporary recovery Redis configuration directory")
	}
	*configDirs = append(*configDirs, configDir)
	if err := protectConfigDir(configDir, true); err != nil {
		return nil, errors.New("protect temporary recovery Redis configuration directory")
	}
	configPath := filepath.Join(configDir, "redis.conf")
	config, err := recoveryRedisConfig(configDir, dataDir, port, password, allowWrites)
	if err != nil {
		return nil, err
	}
	if err := writeSecureConfigFile(configPath, config, false, nil); err != nil {
		return nil, errors.New("write temporary recovery Redis configuration")
	}

	child, err := job.Start(winprocess.StartSpec{
		NoConsole: true,
		Path:      redisExe,
		Args:      []string{filepath.Base(configPath)},
		// Do not put the temporary password in the child environment. A nil Env
		// lets winprocess retain the normal Windows runtime environment.
		Dir: configDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start recovery Redis: %w", err)
	}
	result := &recoveryRedisProcess{
		client: redis.NewClient(&redis.Options{
			Addr:     net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
			Username: "recovery",
			Password: password,
			DB:       indexDB,
			// -1 disables go-redis' default three retries. After SHUTDOWN the
			// server may close before the client observes the response; the
			// process exit is the authoritative result.
			MaxRetries:   -1,
			DialTimeout:  500 * time.Millisecond,
			ReadTimeout:  500 * time.Millisecond,
			WriteTimeout: 500 * time.Millisecond,
		}),
		process: child,
		done:    make(chan struct{}),
	}
	go result.wait()
	if err := waitRecoveryRedisReady(ctx, result); err != nil {
		return result, err
	}
	return result, nil
}

func (p *recoveryRedisProcess) wait() {
	code, err := p.process.Wait()
	p.mu.Lock()
	p.result = recoveryRedisWaitResult{code: code, err: err}
	close(p.done)
	p.mu.Unlock()
}

func (p *recoveryRedisProcess) await(ctx context.Context) (recoveryRedisWaitResult, error) {
	select {
	case <-p.done:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.result, nil
	default:
	}
	select {
	case <-p.done:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.result, nil
	case <-ctx.Done():
		return recoveryRedisWaitResult{}, ctx.Err()
	}
}

func waitRecoveryRedisReady(ctx context.Context, p *recoveryRedisProcess) error {
	ticker := time.NewTicker(recoveryRedisReadyInterval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.client.Ping(ctx).Err(); err == nil {
			return nil
		}
		select {
		case <-p.done:
			return errors.New("recovery Redis exited before readiness")
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func ensureRecoveryRedisTargetEmpty(ctx context.Context, client *redis.Client) error {
	size, err := client.DBSize(ctx).Result()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("inspect recovery Redis target database")
	}
	if size != 0 {
		return errors.New("recovery Redis target database is not empty")
	}
	return nil
}

func stopRecoveryRedis(ctx context.Context, job *winprocess.Job, p *recoveryRedisProcess) error {
	if p == nil {
		return nil
	}
	if p.client != nil {
		// Redis closes the connection before replying to SHUTDOWN. go-redis
		// treats that expected EOF as success. Regardless of the client result,
		// the process exit below is authoritative.
		_ = p.client.Shutdown(ctx).Err()
		_ = p.client.Close()
		p.client = nil
	}
	result, waitErr := p.await(ctx)
	if waitErr != nil {
		_ = job.Close()
		waitRecoveryRedisAfterJobClose(p)
		_ = p.process.Close()
		return waitErr
	}
	if result.err != nil || result.code != 0 {
		_ = p.process.Close()
		return errors.New("recovery Redis exited unsuccessfully")
	}
	_ = p.process.Close()
	return nil
}

func waitRecoveryRedisAfterJobClose(p *recoveryRedisProcess) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), recoveryRedisCleanupWait)
	defer cancel()
	_, _ = p.await(cleanupCtx)
}

func cleanupRecoveryRedisStage(job *winprocess.Job, children []*recoveryRedisProcess, configDirs []string) error {
	for _, child := range children {
		if child != nil && child.client != nil {
			_ = child.client.Close()
			child.client = nil
		}
	}
	jobErr := job.Close()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), recoveryRedisCleanupWait)
	defer cancel()
	var waitErr error
	for _, child := range children {
		if child == nil {
			continue
		}
		result, err := child.await(cleanupCtx)
		if err != nil && waitErr == nil {
			waitErr = errors.New("recovery Redis process did not exit")
		}
		if child.process != nil {
			_ = child.process.Close()
		}
		// A nonzero exit is acceptable during forced cleanup; the process has
		// still exited and the caller's operation error is authoritative.
		if err == nil && result.err != nil && waitErr == nil {
			waitErr = errors.New("recovery Redis process exit could not be verified")
		}
	}
	for _, configDir := range configDirs {
		if err := os.RemoveAll(configDir); err != nil && waitErr == nil {
			waitErr = errors.New("remove temporary recovery Redis configuration")
		}
	}
	if jobErr != nil && waitErr == nil {
		waitErr = errors.New("close recovery Redis process job")
	}
	return waitErr
}

func validateRecoveryRedisStageInputs(redisExe, sourceCopyDir, targetDir, controlDir string) error {
	if !isRecoveryWindowsAbsolute(redisExe) || !isRecoveryWindowsAbsolute(sourceCopyDir) || !isRecoveryWindowsAbsolute(targetDir) || !isRecoveryWindowsAbsolute(controlDir) {
		return errors.New("recovery Redis paths must be absolute Windows paths")
	}
	for _, path := range []string{redisExe, sourceCopyDir, targetDir, controlDir} {
		info, err := os.Lstat(path)
		if err != nil {
			return errors.New("recovery Redis path is unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("recovery Redis paths cannot be symbolic links")
		}
		if path == redisExe && (info.IsDir() || info.Mode().IsRegular() == false) {
			return errors.New("recovery Redis executable is not a regular file")
		}
		if path != redisExe && !info.IsDir() {
			return errors.New("recovery Redis data paths must be directories")
		}
	}
	if recoveryRedisSamePath(redisExe, sourceCopyDir) || recoveryRedisSamePath(redisExe, targetDir) || recoveryRedisSamePath(redisExe, controlDir) {
		return errors.New("recovery Redis executable and data paths must be distinct")
	}
	if recoveryRedisPathsOverlap(sourceCopyDir, targetDir) || recoveryRedisPathsOverlap(sourceCopyDir, controlDir) || recoveryRedisPathsOverlap(targetDir, controlDir) {
		return errors.New("recovery Redis data and control directories must be distinct")
	}
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return errors.New("inspect recovery Redis target directory")
	}
	if len(entries) != 0 {
		return errors.New("recovery Redis target directory must be empty")
	}
	return nil
}

func prepareRecoveryRedisControlDir(controlDir string) error {
	entries, err := os.ReadDir(controlDir)
	if err != nil {
		return errors.New("inspect recovery Redis control directory")
	}
	if err := protectConfigDir(controlDir, len(entries) == 0); err != nil {
		return errors.New("protect recovery Redis control directory")
	}
	return nil
}

func recoveryRedisSamePath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func recoveryRedisPathsOverlap(left, right string) bool {
	if recoveryRedisSamePath(left, right) {
		return true
	}
	for _, pair := range [][2]string{{left, right}, {right, left}} {
		relative, err := filepath.Rel(filepath.Clean(pair[0]), filepath.Clean(pair[1]))
		if err != nil {
			continue
		}
		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != "." {
			return true
		}
	}
	return false
}

func isRecoveryWindowsAbsolute(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.VolumeName(path) != ""
}

func allocateRecoveryRedisPort() (int, error) {
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

func newRecoveryRedisPassword() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func recoveryRedisConfig(configDir, dataDir string, port int, password string, allowWrites bool) ([]byte, error) {
	if password == "" || strings.ContainsAny(password, "\r\n \t") {
		return nil, errors.New("temporary recovery Redis password is invalid")
	}
	relative, err := filepath.Rel(configDir, dataDir)
	if err != nil || relative == "" || filepath.IsAbs(relative) {
		return nil, errors.New("recovery Redis data directory must be on the config volume")
	}
	relative = filepath.ToSlash(relative)
	if strings.ContainsAny(relative, "\r\n\x00") {
		return nil, errors.New("recovery Redis data directory is invalid")
	}
	commands := "+ping +scan +get +pexpiretime +select +shutdown"
	if allowWrites {
		commands += " +dbsize +set"
	}
	config := fmt.Sprintf("bind 127.0.0.1\nport %d\nprotected-mode yes\ndaemonize no\nsupervised no\ndatabases 16\ndir %s\nappendonly yes\naof-load-truncated no\nappendfilename appendonly.aof\nappendfsync always\nsave \"\"\nmaxmemory-policy noeviction\nlogfile \"\"\nuser default off\nuser recovery on >%s ~* %s\n", port, strconv.Quote(relative), password, commands)
	return []byte(config), nil
}
