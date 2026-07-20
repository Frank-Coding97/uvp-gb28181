package gb28181

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbhandler "uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type fakeSIPRuntimeServer struct {
	startErr error
	onError  func(error)
	started  bool
}

func (f *fakeSIPRuntimeServer) SetRecorder(metrics.Recorder)                             {}
func (f *fakeSIPRuntimeServer) SetErrorHandler(fn func(error))                           { f.onError = fn }
func (f *fakeSIPRuntimeServer) SetPTZMessageProcessor(gbhandler.PTZMessageProcessor)     {}
func (f *fakeSIPRuntimeServer) SetPTZNotifyProcessor(gbhandler.PTZNotifyProcessor)       {}
func (f *fakeSIPRuntimeServer) SetSubscriptionWaker(gbhandler.SubscriptionWaker)         {}
func (f *fakeSIPRuntimeServer) SetSubscriptionNotifier(gbhandler.SubscriptionNotifier)   {}
func (f *fakeSIPRuntimeServer) SetAlarmMessageProcessor(gbhandler.AlarmMessageProcessor) {}
func (f *fakeSIPRuntimeServer) Start() error {
	f.started = true
	return f.startErr
}
func (f *fakeSIPRuntimeServer) UAC() *uac.UAC                  { return nil }
func (f *fakeSIPRuntimeServer) Shutdown(context.Context) error { return nil }

func TestStartSIPRuntime_TracksFailuresAndAsyncListenError(t *testing.T) {
	t.Run("start failure", func(t *testing.T) {
		status := gbsetup.NewRuntimeStatus()
		server := &fakeSIPRuntimeServer{startErr: errors.New("bind failed")}
		_, err := startSIPRuntime(gbconfig.Config{}, nil, status, func(gbconfig.Config) (sipRuntimeServer, error) {
			return server, nil
		})
		require.Error(t, err)
		require.Equal(t, gbsetup.RuntimeFailed, status.Snapshot().State)
	})

	t.Run("running then async failure", func(t *testing.T) {
		status := gbsetup.NewRuntimeStatus()
		server := &fakeSIPRuntimeServer{}
		got, err := startSIPRuntime(gbconfig.Config{}, nil, status, func(gbconfig.Config) (sipRuntimeServer, error) {
			return server, nil
		})
		require.NoError(t, err)
		require.Same(t, server, got)
		require.True(t, server.started)
		require.Equal(t, gbsetup.RuntimeRunning, status.Snapshot().State)

		server.onError(errors.New("listen failed password=Secret123"))
		snapshot := status.Snapshot()
		require.Equal(t, gbsetup.RuntimeFailed, snapshot.State)
		require.NotContains(t, snapshot.ErrorSummary, "Secret123")
	})
}
