package launcher

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
	"uvplatform.cn/uvp-gb28181/internal/standalone/readiness"
	"uvplatform.cn/uvp-gb28181/internal/standalone/winprocess"
)

// Launch holds installation ownership until all created components terminate.
// Cancellation currently uses abnormal Job cleanup; graceful user stop is T16.
func Launch(ctx context.Context, installDir, recordingsDir string, notify func(Status)) error {
	lock, err := standalone.AcquireInstanceLock(installDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	job, err := winprocess.NewJob()
	if err != nil {
		return err
	}
	exits := make(chan error, 3)
	type child struct {
		process *winprocess.Process
		done    chan struct{}
	}
	children := []child{}
	var release standalone.Release
	var paths standalone.Paths
	var config standalone.InstanceConfig
	var backendAddress string
	start := func(name, path, dir string, args []string, monitor bool) (*winprocess.Process, <-chan error, error) {
		log, err := os.OpenFile(filepath.Join(paths.LogsDir, name+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, nil, err
		}
		process, err := job.Start(winprocess.StartSpec{Path: path, Args: args, Dir: dir, Env: componentEnvironment(paths), Stdout: log, Stderr: log})
		_ = log.Close()
		if err != nil {
			return nil, nil, err
		}
		result := make(chan error, 1)
		done := make(chan struct{})
		children = append(children, child{process, done})
		go func() {
			code, err := process.Wait()
			if err == nil && code != 0 {
				err = fmt.Errorf("%s exited with code %d; see logs/%s.log", name, code, name)
			}
			result <- err
			if monitor {
				if err == nil {
					err = fmt.Errorf("%s exited unexpectedly; see logs/%s.log", name, name)
				}
				exits <- err
			}
			close(done)
		}()
		return process, result, nil
	}
	steps := Steps{Exits: exits}
	steps.Cleanup = func() error {
		result := job.Close()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for _, child := range children {
			select {
			case <-child.done:
			case <-deadline.C:
				return errors.Join(result, errors.New("owned process cleanup timed out"))
			}
		}
		return result
	}
	steps.Preflight = func(context.Context) error {
		var err error
		release, err = standalone.LoadRelease(installDir)
		if err != nil {
			return err
		}
		paths, err = standalone.ResolvePaths(standalone.PathOptions{InstallDir: installDir, ConfigDir: filepath.Join(installDir, "config"), DataDir: filepath.Join(installDir, "data"), ResourceDir: release.ResourceDir, WebDir: release.WebDir, RecordingsDir: recordingsDir})
		if err != nil {
			return err
		}
		for _, dir := range []string{paths.ConfigDir, paths.DataDir, paths.RecordingsDir, paths.LogsDir} {
			if err = os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
				return err
			}
		}
		config, err = standalone.InitializeConfig(paths)
		if err != nil {
			return err
		}
		backendAddress, err = localProbeAddress(config.BackendAddress())
		if err != nil {
			return err
		}
		return checkPorts([]string{config.RedisAddress(), config.BackendAddress(), config.MediaAddress()})
	}
	steps.Redis = func(ctx context.Context) error {
		if _, _, err := start("redis", release.RedisExe, paths.ConfigDir, []string{filepath.Base(config.RedisConfigPath)}, true); err != nil {
			return err
		}
		return awaitReady(ctx, exits, 30*time.Second, func(ctx context.Context) error { return checkRedis(ctx, config.RedisAddress(), config.RedisPassword()) })
	}
	steps.Database = func(ctx context.Context) error {
		for _, mode := range []string{"-bootstrap-db", "-migrate-up"} {
			_, done, err := start("database", release.BackendExe, release.ReleaseDir, []string{mode}, false)
			if err != nil {
				return err
			}
			timeout := time.NewTimer(2 * time.Minute)
			select {
			case err = <-done:
				timeout.Stop()
				if err != nil {
					return err
				}
			case err = <-exits:
				timeout.Stop()
				return err
			case <-ctx.Done():
				timeout.Stop()
				return ctx.Err()
			case <-timeout.C:
				return errors.New("database preparation timed out")
			}
		}
		return nil
	}
	steps.Backend = func(ctx context.Context) (string, error) {
		process, _, err := start("backend", release.BackendExe, release.ReleaseDir, nil, true)
		if err != nil {
			return "", err
		}
		var status readiness.Status
		err = awaitReady(ctx, exits, 30*time.Second, func(ctx context.Context) error {
			var err error
			status, err = readiness.Check(ctx, nil, "http://"+backendAddress, config.JWTSecret(), process.PID())
			return err
		})
		return status.SIPState, err
	}
	steps.Media = func(ctx context.Context) error {
		if _, _, err := start("media", release.MediaExe, paths.ConfigDir, []string{"-c", filepath.Base(config.ZLMConfigPath), "--affinity", "0"}, true); err != nil {
			return err
		}
		return awaitReady(ctx, exits, 30*time.Second, func(ctx context.Context) error {
			return checkMedia(ctx, "http://"+config.MediaAddress(), config.ZLMSecret())
		})
	}
	return Run(ctx, steps, func(status Status) {
		if backendAddress != "" {
			status.ManagementURL = "http://" + backendAddress + "/"
		}
		if notify != nil {
			notify(status)
		}
	})
}

func componentEnvironment(paths standalone.Paths) []string {
	values := map[string]string{standalone.EnvInstallDir: paths.InstallDir, standalone.EnvConfigDir: paths.ConfigDir, standalone.EnvResourceDir: paths.ResourceDir, standalone.EnvWebDir: paths.WebDir, standalone.EnvDataDir: paths.DataDir, standalone.EnvRecordingsDir: paths.RecordingsDir}
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, override := values[strings.ToUpper(key)]; !override {
			env = append(env, entry)
		}
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

func localProbeAddress(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	number, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || number < 1 || number > 65535 {
		return "", errors.New("invalid backend management address")
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	if !net.ParseIP(host).IsLoopback() {
		return "", errors.New("backend management must listen on loopback or a wildcard address")
	}
	return net.JoinHostPort(host, port), nil
}

func checkPorts(addresses []string) error {
	listeners := []net.Listener{}
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	for _, address := range addresses {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return fmt.Errorf("required port %s is unavailable: %w", address, err)
		}
		listeners = append(listeners, listener)
	}
	return nil
}

func awaitReady(ctx context.Context, exits <-chan error, timeout time.Duration, probe func(context.Context) error) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var last error
	for {
		attempt, stop := context.WithTimeout(deadline, time.Second)
		last = probe(attempt)
		stop()
		if last == nil {
			return nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-deadline.Done():
			timer.Stop()
			return errors.Join(deadline.Err(), last)
		case err := <-exits:
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}
