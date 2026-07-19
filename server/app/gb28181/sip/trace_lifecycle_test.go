package sip

import (
	"context"
	"sync/atomic"
	"testing"

	siplib "github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
)

type lifecycleTraceRuntime struct {
	closed atomic.Int32
}

func (r *lifecycleTraceRuntime) ReadFilter(_ siplib.TransportReadProps, data []byte) ([]byte, error) {
	return data, nil
}

func (r *lifecycleTraceRuntime) WriteObserver(siplib.TransportWriteProps, []byte) {}

func (r *lifecycleTraceRuntime) Shutdown(context.Context) error {
	r.closed.Add(1)
	return nil
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
