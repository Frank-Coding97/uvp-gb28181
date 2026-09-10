package ginhelper

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingShutdownBoundBeforeReady(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer occupied.Close()
	core, logs := observer.New(zap.DebugLevel)
	cleaned := 0
	err = serveUntilCanceled(context.Background(), &http.Server{Addr: occupied.Addr().String()}, zap.New(core), func(context.Context) error { cleaned++; return nil })
	require.Error(t, err)
	require.Equal(t, 1, cleaned)
	require.Zero(t, logs.FilterField(zap.String("event", "http.ready")).Len())
}

func TestLoggingShutdownKeepsDependenciesUntilHTTPCompletes(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var order []string
	record := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		record("http")
		w.WriteHeader(204)
	})}
	done := make(chan error, 1)
	go func() {
		done <- serveUntilCanceled(ctx, server, zap.New(core), func(ctx context.Context) error {
			return Shutdown(ctx, zap.New(core),
				ShutdownStep{Component: "scheduler", Stop: func(context.Context) error { record("scheduler"); return nil }},
				ShutdownStep{Component: "results", Stop: func(context.Context) error { record("results"); return nil }},
				ShutdownStep{Component: "sip", Stop: func(context.Context) error { record("sip"); return nil }})
		})
	}()
	require.Eventually(t, func() bool { return logs.FilterField(zap.String("event", "http.ready")).Len() == 1 }, time.Second, time.Millisecond)
	addr := logs.FilterField(zap.String("event", "http.ready")).All()[0].ContextMap()["address"].(string)
	response := make(chan error, 1)
	go func() {
		r, e := http.Get("http://" + addr)
		if e == nil {
			_, e = io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}
		response <- e
	}()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("stopped with active HTTP request")
	case <-time.After(20 * time.Millisecond):
	}
	mu.Lock()
	require.Empty(t, order)
	mu.Unlock()
	close(release)
	require.NoError(t, <-response)
	require.NoError(t, <-done)
	require.Equal(t, []string{"http", "scheduler", "results", "sip"}, order)
}

func TestLoggingShutdownReportsUnfinishedComponent(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := Shutdown(ctx, zap.New(core), ShutdownStep{Component: "scheduler", Stop: func(ctx context.Context) error { return ctx.Err() }}, ShutdownStep{Component: "sip", Stop: func(context.Context) error { called = true; return errors.New("transport failed") }})
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, called, "remaining components still need a stop signal")
	require.Equal(t, 2, logs.FilterField(zap.String("event", "lifecycle.shutdown_incomplete")).Len())
	require.Zero(t, logs.FilterField(zap.String("event", "lifecycle.stopped")).Len())
}

func TestLoggingShutdownCompletedStepWinsExpiredDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, Shutdown(ctx, zap.NewNop(), ShutdownStep{Component: "already_stopped", Stop: func(context.Context) error { return nil }}))
}

func TestLoggingShutdownComponentSurvivesSafeCore(t *testing.T) {
	cfg, err := logging.ParseConfig(logValues{"logs.outputs": []string{"stdout"}, "logs.stdoutformat": "json"}, t.TempDir())
	require.NoError(t, err)
	sink := &lockedLogBuffer{}
	runtime, err := logging.NewRuntime(logging.Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": sink}})
	require.NoError(t, err)
	err = Shutdown(context.Background(), runtime.Root, ShutdownStep{Component: "scheduler", Stop: func(context.Context) error { return context.DeadlineExceeded }})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, runtime.Close())
	rows := sink.rows(t)
	require.Len(t, rows, 1)
	require.Equal(t, "lifecycle", rows[0]["component"])
	require.Equal(t, "scheduler", rows[0]["shutdown_component"])
	require.Equal(t, true, rows[0]["shutdown_timeout"])
}

type unfinishedShutdownError struct{}

func (unfinishedShutdownError) Error() string { return "private shutdown diagnostic" }
func (unfinishedShutdownError) Unwrap() error { return context.DeadlineExceeded }
func (unfinishedShutdownError) UnfinishedComponents() []string {
	return []string{"sip_transport", "metrics_persistence"}
}

func TestLoggingShutdownReportsNestedUnfinishedComponents(t *testing.T) {
	cfg, err := logging.ParseConfig(logValues{"logs.outputs": []string{"stdout"}, "logs.stdoutformat": "json"}, t.TempDir())
	require.NoError(t, err)
	sink := &lockedLogBuffer{}
	runtime, err := logging.NewRuntime(logging.Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": sink}})
	require.NoError(t, err)
	err = Shutdown(context.Background(), runtime.Root, ShutdownStep{Component: "gb28181", Stop: func(context.Context) error {
		return errors.Join(errors.New("private component failure"), unfinishedShutdownError{})
	}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, runtime.Close())
	rows := sink.rows(t)
	require.Len(t, rows, 3)
	require.Equal(t, "gb28181", rows[0]["shutdown_component"])
	for i, component := range []string{"sip_transport", "metrics_persistence"} {
		require.Equal(t, component, rows[i+1]["shutdown_component"])
		require.Equal(t, true, rows[i+1]["shutdown_timeout"])
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	require.NotContains(t, sink.b.String(), "private")
}
