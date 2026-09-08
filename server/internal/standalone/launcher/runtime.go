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
	"uvplatform.cn/uvp-gb28181/internal/standalone/bootstrapcredential"
	"uvplatform.cn/uvp-gb28181/internal/standalone/control"
	"uvplatform.cn/uvp-gb28181/internal/standalone/controlpipe"
	"uvplatform.cn/uvp-gb28181/internal/standalone/readiness"
	"uvplatform.cn/uvp-gb28181/internal/standalone/winprocess"
)

const (
	businessReadinessInterval       = 3 * time.Second
	businessReadinessRequestTimeout = time.Second
)

type businessProbe func(context.Context) (readiness.Status, error)

func observeBusiness(ctx context.Context, interval, requestTimeout time.Duration, probe businessProbe) <-chan BusinessStatus {
	updates := make(chan BusinessStatus, 1)
	if ctx == nil {
		close(updates)
		return updates
	}
	if interval <= 0 {
		interval = businessReadinessInterval
	}
	if requestTimeout <= 0 {
		requestTimeout = businessReadinessRequestTimeout
	}
	go func() {
		defer close(updates)
		for {
			if ctx.Err() != nil {
				return
			}
			probeCtx, cancel := context.WithTimeout(ctx, requestTimeout)
			var state readiness.Status
			var err error
			if probe == nil {
				err = errors.New("business readiness probe is missing")
			} else {
				state, err = probe(probeCtx)
			}
			cancel()
			update := businessStatusFromReadiness(state, err)
			select {
			case updates <- update:
			case <-ctx.Done():
				return
			}
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return updates
}

func businessStatusFromReadiness(state readiness.Status, err error) BusinessStatus {
	if err != nil {
		return BusinessStatus{SIPState: state.SIPState, BusinessReason: "status_unavailable"}
	}
	if !state.BusinessReady && state.BusinessReason == "" {
		state.BusinessReason = businessReasonForSIPState(state.SIPState)
	}
	return BusinessStatus{SIPState: state.SIPState, BusinessReady: state.BusinessReady, BusinessReason: state.BusinessReason}
}

func businessReasonForSIPState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "unconfigured", "pending", "pending_sip":
		return "SIP 尚未配置"
	case "starting", "starting...":
		return "SIP 正在启动"
	case "failed", "error":
		return "SIP 启动失败"
	default:
		return "业务状态未就绪"
	}
}

func managementEntry(baseURL, installationPhase, bootstrapToken string) string {
	if installationPhase == "pending_admin" {
		return baseURL + "#/standalone-setup?bootstrap_token=" + bootstrapToken
	}
	return baseURL
}

func readyBrowserEntry(opened *bool, status Status, installationPhase, bootstrapToken string) (string, bool) {
	if opened == nil || *opened || status.State != Ready {
		return "", false
	}
	*opened = true
	return managementEntry(status.ManagementURL, installationPhase, bootstrapToken), true
}

// Launch holds installation ownership until all created components terminate.
// A normal stop drains the backend around media shutdown, then stops Redis.
func Launch(ctx context.Context, installDir, recordingsDir string, notify func(Status)) error {
	return LaunchWithBrowser(ctx, installDir, recordingsDir, notify, nil)
}

// LaunchWithBrowser delivers the ephemeral entry URL only to the browser
// callback. It is deliberately excluded from progress status and logs.
func LaunchWithBrowser(ctx context.Context, installDir, recordingsDir string, notify func(Status), openBrowser func(string)) error {
	ctx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	owner := newControlOwner(cancelRun)
	controlCtx, cancelControl := context.WithCancel(context.Background())
	defer cancelControl()
	var controlDone chan error
	var marker *standalone.RunMarker
	var mediaInput *os.File
	var redisDone, backendDone, mediaDone <-chan error
	lock, err := standalone.AcquireInstanceLock(installDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := standalone.CheckMaintenanceGate(installDir); err != nil {
		return err
	}
	job, err := winprocess.NewJob()
	if err != nil {
		return err
	}
	exits := make(chan error, 4) // three components and the control listener
	type child struct {
		process *winprocess.Process
		done    chan struct{}
	}
	children := []child{}
	var release standalone.Release
	var paths standalone.Paths
	var config standalone.InstanceConfig
	var backendAddress string
	var backendPID int
	var bootstrapToken, installationPhase string
	browserOpened := false
	start := func(name, path, dir string, args []string, stdin *os.File, monitor bool) (*winprocess.Process, <-chan error, error) {
		log, err := os.OpenFile(filepath.Join(paths.LogsDir, name+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, nil, err
		}
		process, err := job.Start(winprocess.StartSpec{NoConsole: true, Path: path, Args: args, Dir: dir, Env: componentEnvironment(paths), Stdin: stdin, Stdout: log, Stderr: log})
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
		if mediaInput != nil {
			result = errors.Join(result, mediaInput.Close())
		}
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
		if err = checkPorts([]string{config.RedisAddress(), config.BackendAddress()}, config.MediaListeners()); err != nil {
			return err
		}
		marker, err = standalone.BeginRun(paths)
		if err != nil {
			return err
		}
		name, key, err := control.Endpoint(paths.InstallDir, "launcher", config.JWTSecret())
		if err != nil {
			return err
		}
		listener, err := controlpipe.Listen(name, key)
		if err != nil {
			return err
		}
		controlDone = make(chan error, 1)
		go func() {
			defer listener.Close()
			err := control.Serve(controlCtx, listener, owner.handle)
			if err != nil {
				exits <- errors.New("launcher control listener failed")
			}
			controlDone <- err
		}()
		return nil
	}
	steps.Redis = func(ctx context.Context) error {
		var err error
		if _, redisDone, err = start("redis", release.RedisExe, paths.ConfigDir, []string{filepath.Base(config.RedisConfigPath)}, nil, true); err != nil {
			return err
		}
		return awaitReady(ctx, exits, 30*time.Second, func(ctx context.Context) error { return checkRedis(ctx, config.RedisAddress(), config.RedisPassword()) })
	}
	steps.Database = func(ctx context.Context) error {
		for _, mode := range []string{"-bootstrap-db", "-migrate-up"} {
			_, done, err := start("database", release.BackendExe, release.ReleaseDir, []string{mode}, nil, false)
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
		var err error
		bootstrapToken, err = bootstrapcredential.NewToken()
		if err != nil {
			return "", err
		}
		reader, writer, err := os.Pipe()
		if err != nil {
			return "", errors.New("create backend bootstrap pipe failed")
		}
		defer reader.Close()
		// The fixed frame fits in the empty pipe. Close the writer before process
		// creation so the child always receives a complete frame followed by EOF.
		err = bootstrapcredential.Write(writer, bootstrapToken)
		err = errors.Join(err, writer.Close())
		if err != nil {
			return "", errors.New("prepare backend bootstrap credential failed")
		}
		process, done, err := start("backend", release.BackendExe, release.ReleaseDir, nil, reader, true)
		backendDone = done
		if err != nil {
			return "", err
		}
		backendPID = process.PID()
		var status readiness.Status
		err = awaitReady(ctx, exits, 30*time.Second, func(ctx context.Context) error {
			var err error
			status, err = readiness.Check(ctx, nil, "http://"+backendAddress, config.JWTSecret(), process.PID())
			if err == nil {
				switch status.InstallationPhase {
				case "pending_admin":
					if !status.CredentialAccepted {
						err = errors.New("backend bootstrap credential not accepted")
					}
				case "pending_sip", "complete":
				default:
					err = errors.New("backend installation state is unavailable")
				}
			}
			if err == nil {
				installationPhase = status.InstallationPhase
			}
			return err
		})
		return status.SIPState, err
	}
	steps.Media = func(ctx context.Context) error {
		input, output, err := os.Pipe()
		if err != nil {
			return err
		}
		mediaInput = output
		defer input.Close()
		if _, mediaDone, err = start("media", release.MediaExe, paths.ConfigDir, mediaArguments(paths, config), input, true); err != nil {
			return err
		}
		return awaitReady(ctx, exits, 30*time.Second, func(ctx context.Context) error {
			return checkMedia(ctx, "http://"+config.MediaAddress(), config.ZLMSecret())
		})
	}
	steps.ObserveBusiness = func(ctx context.Context) <-chan BusinessStatus {
		return observeBusiness(ctx, businessReadinessInterval, businessReadinessRequestTimeout, func(ctx context.Context) (readiness.Status, error) {
			return readiness.Check(ctx, nil, "http://"+backendAddress, config.JWTSecret(), backendPID)
		})
	}
	backendCommand := func(ctx context.Context, command control.Command, expected control.Reply) error {
		name, key, err := control.Endpoint(paths.InstallDir, "backend", config.JWTSecret())
		if err != nil {
			return err
		}
		reply, err := control.Call(ctx, name, key, command)
		if err != nil {
			return err
		}
		if reply != expected {
			return errors.New("backend shutdown was not confirmed")
		}
		return nil
	}
	steps.Stop = func(ctx context.Context) error {
		return Shutdown(ctx, ShutdownSteps{
			Quiesce: func(ctx context.Context) error { return backendCommand(ctx, control.Quiesce, control.MediaReady) },
			Media: func(ctx context.Context) error {
				// This exclusive pipe has never been written: the single short command fits
				// in its empty kernel buffer even if the child has stopped reading.
				if _, err := mediaInput.Write([]byte("shutdown\n")); err != nil {
					return err
				}
				return waitOwnedExit(ctx, mediaDone)
			},
			Finalize: func(ctx context.Context) error { return backendCommand(ctx, control.Finalize, control.Finalized) },
			Backend:  func(ctx context.Context) error { return waitOwnedExit(ctx, backendDone) },
			Redis: func(ctx context.Context) error {
				if err := shutdownRedis(ctx, config.RedisAddress(), config.RedisPassword()); err != nil {
					return err
				}
				return waitOwnedExit(ctx, redisDone)
			},
		})
	}
	result := Run(ctx, steps, func(status Status) {
		if status.State == Stopped {
			return
		} // publish only after marker removal
		owner.publish(status.State)
		if marker != nil {
			status.PreviousUnclean = marker.PreviousUnclean
		}
		if backendAddress != "" {
			status.ManagementURL = "http://" + backendAddress + "/"
		}
		if notify != nil {
			notify(status)
		}
		if entry, ok := readyBrowserEntry(&browserOpened, status, installationPhase, bootstrapToken); ok {
			if openBrowser != nil {
				openBrowser(entry)
			}
			bootstrapToken = ""
		}
	})
	if result == nil && marker != nil {
		result = marker.Finish()
	}
	owner.finish(result)
	if controlDone != nil {
		joined := false
		if owner.stopRequested() && result == nil {
			select {
			case err := <-controlDone:
				joined = true
				result = errors.Join(result, err)
			case <-time.After(5 * time.Second):
				result = errors.New("stop acknowledgement timed out")
			}
		}
		cancelControl()
		if !joined {
			select {
			case err := <-controlDone:
				result = errors.Join(result, err)
			case <-time.After(5 * time.Second):
				result = errors.Join(result, errors.New("control listener cleanup timed out"))
			}
		}
	}
	if result == nil {
		owner.publish(Stopped)
		if notify != nil {
			notify(Status{State: Stopped})
		}
	}
	return result
}

func waitOwnedExit(ctx context.Context, done <-chan error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if done == nil {
		return errors.New("owned process is missing")
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
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

type portResource interface {
	Close() error
}

func checkPorts(addresses []string, media ...[]standalone.MediaListener) error {
	bindings := make([]standalone.MediaListener, 0, len(addresses))
	for _, address := range addresses {
		bindings = append(bindings, standalone.MediaListener{Network: "tcp", Address: address})
	}
	for _, listeners := range media {
		bindings = append(bindings, listeners...)
	}
	resources := make([]portResource, 0, len(bindings))
	defer func() {
		for _, resource := range resources {
			_ = resource.Close()
		}
	}()
	for _, binding := range bindings {
		var resource portResource
		var err error
		network := binding.Network
		// Windows can bind an IPv6 wildcard while the required IPv4 socket is occupied.
		if host, _, splitErr := net.SplitHostPort(binding.Address); splitErr == nil {
			if ip := net.ParseIP(host); ip != nil {
				if ip.To4() != nil {
					network += "4"
				} else {
					network += "6"
				}
			}
		}
		switch binding.Network {
		case "tcp":
			resource, err = net.Listen(network, binding.Address)
		case "udp":
			resource, err = net.ListenPacket(network, binding.Address)
		default:
			return fmt.Errorf("unsupported required port network %q", binding.Network)
		}
		if err != nil {
			return fmt.Errorf("required %s port %s is unavailable: %w", binding.Network, binding.Address, err)
		}
		resources = append(resources, resource)
	}
	return nil
}

func mediaArguments(paths standalone.Paths, config standalone.InstanceConfig) []string {
	return []string{"-c", filepath.Base(config.ZLMConfigPath), "--affinity", "0", "--uvp-stdin-control", "--log-dir", filepath.Join(paths.LogsDir, "media")}
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
