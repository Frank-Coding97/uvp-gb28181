package ymlconfig

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type configStopper interface {
	StopContext(context.Context) error
}

func TestLoggingConfigStopWaitsForInFlightCallback(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	cfg, path, _ := newWatchedConfig(t, func() {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			close(completed)
		}
	})
	stopper := requireConfigStopper(t, cfg)

	triggerConfigChange(t, path, "value: 2\n", started)

	stopResult := make(chan error, 1)
	go func() { stopResult <- stopper.StopContext(context.Background()) }()
	select {
	case err := <-stopResult:
		t.Fatalf("StopContext returned before the callback completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	waitForSignal(t, completed)
	require.NoError(t, <-stopResult)
	require.Equal(t, int32(1), calls.Load())
}

func TestLoggingConfigStopContextTimeoutDoesNotPretendComplete(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	cfg, path, _ := newWatchedConfig(t, func() {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			close(completed)
		}
	})
	stopper := requireConfigStopper(t, cfg)

	triggerConfigChange(t, path, "value: 2\n", started)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, stopper.StopContext(ctx), context.DeadlineExceeded)
	select {
	case <-completed:
		t.Fatal("callback completed before its release")
	default:
	}

	close(release)
	waitForSignal(t, completed)
	require.NoError(t, stopper.StopContext(context.Background()))
	require.Equal(t, int32(1), calls.Load())
}

func TestLoggingConfigStopPreventsFutureCallbackAndLogAndIsIdempotent(t *testing.T) {
	var calls atomic.Int32
	called := make(chan struct{})
	cfg, path, observed := newWatchedConfig(t, func() {
		if calls.Add(1) == 1 {
			close(called)
		}
	})
	stopper := requireConfigStopper(t, cfg)

	triggerConfigChange(t, path, "value: 2\n", called)
	time.Sleep(100 * time.Millisecond)
	beforeCalls := calls.Load()
	beforeLogs := observed.Len()

	require.NoError(t, stopper.StopContext(context.Background()))
	require.NoError(t, stopper.StopContext(context.Background()))
	time.Sleep(1100 * time.Millisecond)
	waitForFileWrite(t, path, "value: 3\n")
	time.Sleep(300 * time.Millisecond)

	require.Equal(t, beforeCalls, calls.Load())
	require.Equal(t, beforeLogs, observed.Len())
}

func newWatchedConfig(t *testing.T, callback func()) (app.YmlConfigInterf, string, *observer.ObservedLogs) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(path, []byte("value: 1\n"), 0600))
	cfg, err := LoadYamlFactory(dir)
	require.NoError(t, err)

	core, observed := observer.New(zap.DebugLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })
	cfg.ConfigFileChangeListen(callback)
	time.Sleep(50 * time.Millisecond)
	return cfg, path, observed
}

func requireConfigStopper(t *testing.T, cfg app.YmlConfigInterf) configStopper {
	t.Helper()
	stopper, ok := cfg.(configStopper)
	require.True(t, ok, "configuration must expose StopContext")
	return stopper
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
}

func triggerConfigChange(t *testing.T, path, content string, signal <-chan struct{}) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		writeConfig(t, path, content)
		select {
		case <-signal:
			return
		case <-time.After(100 * time.Millisecond):
			select {
			case <-deadline:
				t.Fatal("timed out waiting for configuration callback")
				return
			default:
			}
		}
	}
}

func waitForFileWrite(t *testing.T, path, content string) {
	t.Helper()
	watcher, err := fsnotify.NewWatcher()
	require.NoError(t, err)
	defer watcher.Close()
	require.NoError(t, watcher.Add(filepath.Dir(path)))
	writeConfig(t, path, content)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-watcher.Events:
			if filepath.Clean(event.Name) == filepath.Clean(path) && event.Has(fsnotify.Write) {
				return
			}
		case err := <-watcher.Errors:
			t.Fatalf("filesystem watcher failed: %v", err)
		case <-deadline:
			t.Fatal("timed out waiting for filesystem write")
		}
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for configuration callback")
	}
}
