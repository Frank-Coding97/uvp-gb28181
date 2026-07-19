package sip

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	siplib "github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
)

type lifecycleTraceRuntime struct {
	closed            atomic.Int32
	connectionsClosed atomic.Int32
}

func (r *lifecycleTraceRuntime) ReadFilter(_ siplib.TransportReadProps, data []byte) ([]byte, error) {
	return data, nil
}

func (r *lifecycleTraceRuntime) WriteObserver(siplib.TransportWriteProps, []byte) {}

func (r *lifecycleTraceRuntime) ConnectionClosed(siplib.TransportReadProps) {
	r.connectionsClosed.Add(1)
}

func (r *lifecycleTraceRuntime) Health() gbtrace.HealthSnapshot {
	return gbtrace.HealthSnapshot{State: gbtrace.HealthReady}
}

func (r *lifecycleTraceRuntime) Shutdown(context.Context) error {
	r.closed.Add(1)
	return nil
}

func TestTraceRuntimeReceivesTCPConnectionClose(t *testing.T) {
	cfg := testConfig()
	cfg.Trace.Enabled = true
	runtime := &lifecycleTraceRuntime{}
	server, err := NewServer(cfg, WithTraceFactory(func(gbconfig.TraceConfig) gbtrace.Runtime {
		return runtime
	}))
	require.NoError(t, err)
	defer server.Shutdown(context.Background())

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	go func() { _ = server.srv.ServeTCP(listener) }()

	conn, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool {
		return runtime.connectionsClosed.Load() == 1
	}, 2*time.Second, 10*time.Millisecond)
}

func TestTraceDisabledDoesNotCreateRuntime(t *testing.T) {
	cfg := testConfig()
	cfg.Trace.Enabled = false
	var factoryCalls atomic.Int32

	server, err := NewServer(cfg, WithTraceFactory(func(gbconfig.TraceConfig) gbtrace.Runtime {
		factoryCalls.Add(1)
		return &lifecycleTraceRuntime{}
	}))
	require.NoError(t, err)
	require.Zero(t, factoryCalls.Load())
	require.Nil(t, server.trace)
	require.NoError(t, server.Shutdown(context.Background()))
}

func TestTraceEnabledCreatesAndClosesRuntime(t *testing.T) {
	cfg := testConfig()
	cfg.Trace.Enabled = true
	runtime := &lifecycleTraceRuntime{}
	var gotConfig gbconfig.TraceConfig

	server, err := NewServer(cfg, WithTraceFactory(func(cfg gbconfig.TraceConfig) gbtrace.Runtime {
		gotConfig = cfg
		return runtime
	}))
	require.NoError(t, err)
	require.Same(t, runtime, server.trace)
	require.True(t, gotConfig.Enabled)
	require.NoError(t, server.Shutdown(context.Background()))
	require.EqualValues(t, 1, runtime.closed.Load())
}
