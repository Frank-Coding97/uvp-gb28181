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

func validEffectiveConfig() gbsetup.EffectiveSIPConfig {
	return gbsetup.EffectiveSIPConfig{
		DeploymentMode: gbsetup.DeploymentLAN,
		ListenIP:       "0.0.0.0",
		AdvertiseIP:    "192.168.1.10",
		Port:           5061,
		Domain:         "3402000000",
		ServerID:       "34020000002000000001",
		Password:       "Secret123",
		Transport:      []string{"udp", "tcp"},
		Source:         gbsetup.ConfigSourceDatabase,
	}
}

func TestApplyEffectiveSIPConfig(t *testing.T) {
	base := gbconfig.Config{Enabled: true}
	got, err := applyEffectiveSIPConfig(base, validEffectiveConfig())
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0", got.SIP.ListenIP)
	require.Equal(t, "192.168.1.10", got.SIP.AdvertiseIP)
	require.Equal(t, []string{"udp", "tcp"}, got.SIP.Transport)
}

func TestApplyEffectiveSIPConfig_MissingIsUnconfigured(t *testing.T) {
	_, err := applyEffectiveSIPConfig(gbconfig.Config{Enabled: true}, gbsetup.EffectiveSIPConfig{Source: gbsetup.ConfigSourceMissing})
	require.ErrorIs(t, err, ErrSIPUnconfigured)
}

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
