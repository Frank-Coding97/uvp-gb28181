package gb28181

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbhandler "uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordquery"
	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type fakeSIPRuntimeServer struct {
	startErr                error
	onError                 func(error)
	started                 bool
	events                  *[]string
	activeAtRecordSinkClear *int
}

func (f *fakeSIPRuntimeServer) SetRecorder(metrics.Recorder)                         {}
func (f *fakeSIPRuntimeServer) SetErrorHandler(fn func(error))                       { f.onError = fn }
func (f *fakeSIPRuntimeServer) SetPTZMessageProcessor(gbhandler.PTZMessageProcessor) {}
func (f *fakeSIPRuntimeServer) SetRecordInfoSink(sink gbhandler.RecordInfoSink) {
	if sink == nil && f.events != nil {
		if f.activeAtRecordSinkClear != nil && recordQueryService != nil {
			*f.activeAtRecordSinkClear = recordQueryService.Active()
		}
		*f.events = append(*f.events, "record.sink.clear")
	}
}
func (f *fakeSIPRuntimeServer) SetPlaybackEndSink(gbhandler.PlaybackEndSink)             {}
func (f *fakeSIPRuntimeServer) SetSubscriptionWaker(gbhandler.SubscriptionWaker)         {}
func (f *fakeSIPRuntimeServer) SetSubscriptionNotifier(gbhandler.SubscriptionNotifier)   {}
func (f *fakeSIPRuntimeServer) SetAlarmMessageProcessor(gbhandler.AlarmMessageProcessor) {}
func (f *fakeSIPRuntimeServer) Start() error {
	f.started = true
	return f.startErr
}
func (f *fakeSIPRuntimeServer) UAC() *uac.UAC { return nil }
func (f *fakeSIPRuntimeServer) Shutdown(context.Context) error {
	if f.events != nil {
		*f.events = append(*f.events, "sip.shutdown")
	}
	return nil
}

type fakePTZSchedulerLifecycle struct {
	events *[]string
}

func (f *fakePTZSchedulerLifecycle) Start(context.Context) {
	*f.events = append(*f.events, "scheduler.start")
}

func (f *fakePTZSchedulerLifecycle) Stop() {
	*f.events = append(*f.events, "scheduler.stop")
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

		server.onError(errors.New("listen failed password=Sec12345Aa!!"))
		snapshot := status.Snapshot()
		require.Equal(t, gbsetup.RuntimeFailed, snapshot.State)
		require.NotContains(t, snapshot.ErrorSummary, "Sec12345Aa!!")
	})
}

func TestPTZServiceReloadStopsSchedulerBeforeSIP(t *testing.T) {
	events := []string{}
	previousServer, previousScheduler, previousService := sipServer, ptzScheduler, ptzService
	defer func() {
		sipServer, ptzScheduler, ptzService = previousServer, previousScheduler, previousService
	}()

	sipServer = &fakeSIPRuntimeServer{events: &events}
	ptzScheduler = &fakePTZSchedulerLifecycle{events: &events}
	ptzService = nil
	stopSIPDependencies(context.Background())

	require.Equal(t, []string{"record.sink.clear", "scheduler.stop", "sip.shutdown"}, events)
	require.Nil(t, ptzScheduler)
	require.Nil(t, ptzService)
	require.Nil(t, sipServer)
}

type recordQueryRuntimeSender struct{}

func (recordQueryRuntimeSender) SendMessageTracked(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
	return uac.TrackedMessageResult{StatusCode: 200}, nil
}

func TestRecordQueryReloadClosesRegistryBeforeSinkAndSIP(t *testing.T) {
	events := []string{}
	activeAtSinkClear := -1
	previousServer, previousQueryService := sipServer, recordQueryService
	defer func() {
		sipServer, recordQueryService = previousServer, previousQueryService
	}()

	service, err := recordquery.NewService(recordQueryRuntimeSender{}, recordquery.Options{
		Timeout: time.Second, MaxActiveQueries: 2, MaxRecordsPerQuery: 10,
		ResultTTL: time.Minute, Location: time.UTC,
	})
	require.NoError(t, err)
	recordQueryService = service
	sipServer = &fakeSIPRuntimeServer{events: &events, activeAtRecordSinkClear: &activeAtSinkClear}
	stopSIPDependencies(context.Background())

	require.Equal(t, 0, activeAtSinkClear)
	require.Equal(t, []string{"record.sink.clear", "sip.shutdown"}, events)
	require.Nil(t, recordQueryService)
	require.Nil(t, sipServer)
}

type playbackRuntimeCleanup struct{ events *[]string }

func (r playbackRuntimeCleanup) Teardown(context.Context) error {
	*r.events = append(*r.events, "playback.teardown")
	return nil
}
func (r playbackRuntimeCleanup) CloseRTP(context.Context) error {
	*r.events = append(*r.events, "playback.rtp.close")
	return nil
}
func (r playbackRuntimeCleanup) Unbind(context.Context) error {
	*r.events = append(*r.events, "playback.unbind")
	return nil
}

func TestReloadClosesPlaybackBeforeRecordQueryAndSIP(t *testing.T) {
	events := []string{}
	previousServer, previousPlayback, previousQuery := sipServer, playbackService, recordQueryService
	defer func() {
		sipServer, playbackService, recordQueryService = previousServer, previousPlayback, previousQuery
	}()

	registry := gbplayback.NewRegistry(gbplayback.RegistryConfig{})
	_, err := registry.Create(context.Background(), gbplayback.CreateRequest{
		OwnerID: "1", ChannelID: "2", RecordKey: "record-1",
		SegmentStart: time.Now(), SegmentEnd: time.Now().Add(time.Minute),
		Resources: playbackRuntimeCleanup{events: &events},
	})
	require.NoError(t, err)
	playbackService = gbplayback.NewService(registry, nil, nil, nil, nil, gbplayback.ServiceConfig{})
	query, err := recordquery.NewService(recordQueryRuntimeSender{}, recordquery.Options{
		Timeout: time.Second, MaxActiveQueries: 2, MaxRecordsPerQuery: 10,
		ResultTTL: time.Minute, Location: time.UTC,
	})
	require.NoError(t, err)
	recordQueryService = query
	sipServer = &fakeSIPRuntimeServer{events: &events}

	stopSIPDependencies(context.Background())

	require.Equal(t, []string{"playback.teardown", "playback.rtp.close", "playback.unbind", "record.sink.clear", "sip.shutdown"}, events)
	require.Nil(t, playbackService)
	require.Nil(t, recordQueryService)
	require.Nil(t, sipServer)
}
