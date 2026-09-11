package launcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
	"uvplatform.cn/uvp-gb28181/internal/standalone/winprocess"
)

// The upgrade owner holds InstanceLock throughout this call. There is no normal
// backend, SIP listener or maintenance gate bypass: the backend runs only its
// one-use offline health operation. Only these Job-owned children are stopped.
func runMaintenanceCandidateHealth(ctx context.Context, paths standalone.Paths, operationID, version string) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	journal, err := standalone.ReadMaintenanceJournal(paths.InstallDir)
	if err != nil {
		return err
	}
	if journal.OperationID != operationID || journal.CandidateVersion != version || journal.Phase != standalone.MaintenanceUpgrading {
		return errors.New("candidate health maintenance identity changed")
	}
	lock, err := standalone.AcquireInstanceLock(paths.InstallDir)
	if err == nil {
		_ = lock.Close()
		return errors.New("candidate health requires upgrade ownership")
	}
	if !errors.Is(err, standalone.ErrInstanceRunning) {
		return err
	}
	release, err := standalone.LoadReleaseVersion(paths.InstallDir, version)
	if err != nil {
		return err
	}
	config, err := standalone.LoadConfig(paths)
	if err != nil {
		return err
	}
	if err := checkPorts([]string{config.RedisAddress()}, config.MediaListeners()); err != nil {
		return err
	}
	job, err := winprocess.NewJob()
	if err != nil {
		return err
	}
	type child struct {
		process *winprocess.Process
		exit    chan error
		done    chan struct{}
	}
	var children []*child
	exits := make(chan error, 2)
	defer func() {
		result = errors.Join(result, job.Close())
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for _, owned := range children {
			select {
			case <-owned.done:
				result = errors.Join(result, owned.process.Close())
			case <-deadline.C:
				result = errors.Join(result, errors.New("candidate component cleanup timed out"))
				return
			}
		}
	}()
	start := func(path string, args []string, input *os.File) (*child, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		process, err := job.Start(winprocess.StartSpec{NoConsole: true, Path: path, Args: args, Dir: paths.ConfigDir, Env: componentEnvironment(paths), Stdin: input})
		if err != nil {
			return nil, errors.New("candidate component could not start")
		}
		owned := &child{process: process, exit: make(chan error, 1), done: make(chan struct{})}
		children = append(children, owned)
		go func() {
			code, err := process.Wait()
			if err == nil && code != 0 {
				err = errors.New("candidate component exited unsuccessfully")
			}
			owned.exit <- err
			exits <- errors.New("candidate component exited")
			close(owned.done)
		}()
		return owned, nil
	}
	redis, err := start(release.RedisExe, []string{filepath.Base(config.RedisConfigPath)}, nil)
	if err != nil {
		return err
	}
	if err := awaitReady(ctx, exits, 30*time.Second, func(ctx context.Context) error {
		return checkRedis(ctx, config.RedisAddress(), config.RedisPassword())
	}); err != nil {
		return errors.New("candidate Redis readiness failed")
	}
	input, control, err := os.Pipe()
	if err != nil {
		return err
	}
	defer input.Close()
	defer control.Close()
	media, err := start(release.MediaExe, mediaArguments(paths, config), input)
	_ = input.Close()
	if err != nil {
		return err
	}
	mediaProbe := func(ctx context.Context) error {
		return checkMedia(ctx, "http://"+config.MediaAddress(), config.ZLMSecret())
	}
	if err := awaitReady(ctx, exits, 30*time.Second, mediaProbe); err != nil {
		return errors.New("candidate media readiness failed")
	}
	if err := runMaintenanceChild(ctx, paths, operationID, "candidate_health", version); err != nil {
		return err
	}
	if err := mediaProbe(ctx); err != nil {
		return err
	}
	if err := checkRedis(ctx, config.RedisAddress(), config.RedisPassword()); err != nil {
		return errors.New("candidate Redis final readiness failed")
	}
	stopCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if _, err := control.Write([]byte("shutdown\n")); err != nil {
		return errors.New("candidate media shutdown failed")
	}
	if err := waitOwnedExit(stopCtx, media.exit); err != nil {
		return err
	}
	if err := shutdownRedis(stopCtx, config.RedisAddress(), config.RedisPassword()); err != nil {
		return errors.New("candidate Redis shutdown failed")
	}
	return waitOwnedExit(stopCtx, redis.exit)
}
