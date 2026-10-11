package gb28181

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	gbhandler "uvplatform.com/uvp-gb28181/app/gb28181/handler"
	"uvplatform.com/uvp-gb28181/app/gb28181/metrics"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordquery"
	gbsetup "uvplatform.com/uvp-gb28181/app/gb28181/setup"
	"uvplatform.com/uvp-gb28181/app/gb28181/subscribe"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	openapiadapters "uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	openapideviceevents "uvplatform.com/uvp-gb28181/app/openapi/deviceevents"
	openapideviceupgrade "uvplatform.com/uvp-gb28181/app/openapi/deviceupgrade"
)

type openAPIOperationAdapterFunc func(context.Context, catalogruntime.Invocation) (any, error)

func (adapter openAPIOperationAdapterFunc) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	return adapter(ctx, invocation)
}

func TestOpenAPIOperationBindingsFollowSIPGenerationLifecycle(t *testing.T) {
	ClearOpenAPIOperationAdapters()
	t.Cleanup(ClearOpenAPIOperationAdapters)

	key := openapiadapters.DeviceRebootAdapterKey
	forwarder := OpenAPIOperationAdapterBindings()[key]
	require.NotNil(t, forwarder)
	_, err := forwarder.Execute(context.Background(), catalogruntime.Invocation{})
	require.ErrorIs(t, err, catalogruntime.ErrAdapterNotFound)

	require.NoError(t, BindOpenAPIOperationAdapters([]catalogruntime.AdapterRegistration{{
		Key: key, ContractVersion: "v1", Adapter: openAPIOperationAdapterFunc(func(context.Context, catalogruntime.Invocation) (any, error) {
			return "bound", nil
		}),
	}}))
	value, err := forwarder.Execute(context.Background(), catalogruntime.Invocation{})
	require.NoError(t, err)
	require.Equal(t, "bound", value)

	ClearOpenAPIOperationAdapters()
	_, err = forwarder.Execute(context.Background(), catalogruntime.Invocation{})
	require.ErrorIs(t, err, catalogruntime.ErrAdapterNotFound)
}

func TestOpenAPIFirmwareUpgradeBindingsFollowSIPGenerationLifecycle(t *testing.T) {
	ClearOpenAPIOperationAdapters()
	t.Cleanup(ClearOpenAPIOperationAdapters)

	forwarders := OpenAPIOperationAdapterBindings()
	keys := []string{openapideviceupgrade.UpgradeAdapterKey, openapideviceupgrade.ReadAdapterKey}
	for _, key := range keys {
		forwarder := forwarders[key]
		require.NotNil(t, forwarder, "code contract must expose a stable forwarder for %s", key)
		_, err := forwarder.Execute(context.Background(), catalogruntime.Invocation{})
		require.ErrorIs(t, err, catalogruntime.ErrAdapterNotFound)
	}

	registrations := openapideviceupgrade.NewDispatcher(nil, nil).Registrations()
	require.NoError(t, BindOpenAPIOperationAdapters(registrations))
	for _, key := range keys {
		_, err := forwarders[key].Execute(context.Background(), catalogruntime.Invocation{})
		require.ErrorIs(t, err, openapideviceupgrade.ErrUnavailable, "bound forwarder must reach the fail-closed device-upgrade adapter")
	}

	ClearOpenAPIOperationAdapters()
	for _, key := range keys {
		_, err := forwarders[key].Execute(context.Background(), catalogruntime.Invocation{})
		require.ErrorIs(t, err, catalogruntime.ErrAdapterNotFound)
	}
}

type runtimeDeviceEventSubscriptionLifecycle struct{}

func (runtimeDeviceEventSubscriptionLifecycle) Configure(context.Context, *gbmodels.GbDevice, gbmodels.SubscriptionKind, *bool, *int, *int) (*gbmodels.GbDeviceSubscription, error) {
	return nil, nil
}

func (runtimeDeviceEventSubscriptionLifecycle) Renew(context.Context, *gbmodels.GbDevice, gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error) {
	return nil, nil
}

func TestOpenAPIDeviceEventBindingsFollowSIPGenerationLifecycle(t *testing.T) {
	ClearOpenAPIOperationAdapters()
	t.Cleanup(ClearOpenAPIOperationAdapters)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })

	key := openapideviceevents.DevicePositionReadAdapterKey
	forwarder := openAPIOperationAdapters.Forwarders([]string{key})[key]
	require.NotNil(t, forwarder)
	_, err = forwarder.Execute(context.Background(), catalogruntime.Invocation{})
	require.ErrorIs(t, err, catalogruntime.ErrAdapterNotFound)

	registrations, err := newOpenAPIDeviceEventAdapterRegistrations(db, runtimeDeviceEventSubscriptionLifecycle{}, []byte(strings.Repeat("m", 32)))
	require.NoError(t, err)
	require.Len(t, registrations, 7)
	require.NoError(t, BindOpenAPIOperationAdapters(registrations))
	_, err = forwarder.Execute(context.Background(), catalogruntime.Invocation{
		Scope: openapideviceevents.DevicePositionReadScope, Method: "POST",
	})
	require.ErrorIs(t, err, openapideviceevents.ErrInvalidRequest)

	ClearOpenAPIOperationAdapters()
	_, err = forwarder.Execute(context.Background(), catalogruntime.Invocation{})
	require.ErrorIs(t, err, catalogruntime.ErrAdapterNotFound)
}

func TestOpenAPIDeviceEventBindingsFailClosedWithoutStableMasterKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	t.Setenv(openAPIMasterKeyEnv, "")
	registrations, err := openAPIDeviceEventAdapterRegistrations(db, runtimeDeviceEventSubscriptionLifecycle{})
	require.ErrorIs(t, err, openapideviceevents.ErrUnavailable)
	require.Empty(t, registrations)
}

func TestOpenAPIDeviceEventRegistrationsReadConfiguredStableMasterKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	t.Setenv(openAPIMasterKeyEnv, base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("m", 32))))
	registrations, err := openAPIDeviceEventAdapterRegistrations(db, runtimeDeviceEventSubscriptionLifecycle{})
	require.NoError(t, err)
	require.Len(t, registrations, 7)
}

func TestOpenAPIDeviceEventRuntimeRequiresSubscriptionLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	registrations, err := newOpenAPIDeviceEventAdapterRegistrations(db, nil, []byte(strings.Repeat("m", 32)))
	require.ErrorIs(t, err, openapideviceevents.ErrUnavailable)
	require.Empty(t, registrations)
}

func TestOpenAPIDeviceEventRuntimeRejectsTypedNilSubscriptionLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	var upstream *subscribe.Service
	registrations, err := newOpenAPIDeviceEventAdapterRegistrations(db, upstream, []byte(strings.Repeat("m", 32)))
	require.ErrorIs(t, err, openapideviceevents.ErrUnavailable)
	require.Empty(t, registrations)
}

type fakeSIPRuntimeServer struct {
	startErr                error
	shutdownErr             error
	uac                     *uac.UAC
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
func (f *fakeSIPRuntimeServer) SetSnapshotSink(gbhandler.SnapshotSink)                   {}
func (f *fakeSIPRuntimeServer) SetPlaybackEndSink(gbhandler.PlaybackEndSink)             {}
func (f *fakeSIPRuntimeServer) SetSubscriptionWaker(gbhandler.SubscriptionWaker)         {}
func (f *fakeSIPRuntimeServer) SetSubscriptionNotifier(gbhandler.SubscriptionNotifier)   {}
func (f *fakeSIPRuntimeServer) SetAlarmMessageProcessor(gbhandler.AlarmMessageProcessor) {}
func (f *fakeSIPRuntimeServer) Start() error {
	f.started = true
	return f.startErr
}
func (f *fakeSIPRuntimeServer) UAC() *uac.UAC { return f.uac }
func (f *fakeSIPRuntimeServer) Shutdown(context.Context) error {
	if f.events != nil {
		*f.events = append(*f.events, "sip.shutdown")
	}
	return f.shutdownErr
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
		got, err := startSIPRuntime(gbconfig.Config{}, nil, status, func(gbconfig.Config) (sipRuntimeServer, error) {
			return server, nil
		})
		require.Error(t, err)
		require.Same(t, server, got, "failed Start must return its owned instance for root rollback")
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

func TestSnapshotStreamURLUsesZLMLoopback(t *testing.T) {
	require.Equal(t,
		"rtsp://127.0.0.1:10554/rtp/stream-1?play_token=token.value",
		snapshotStreamURL(10554, "stream-1", "token.value"),
	)
	require.Equal(t,
		"rtsp://127.0.0.1:10554/rtp/stream-1",
		snapshotStreamURL(10554, "stream-1", ""),
	)
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
	require.NoError(t, stopSIPDependencies(context.Background()))

	require.Equal(t, []string{"record.sink.clear", "scheduler.stop", "sip.shutdown"}, events)
	require.Nil(t, ptzScheduler)
	require.Nil(t, ptzService)
	require.Nil(t, sipServer)
}

type pendingPTZRuntime struct{ err error }

func (*pendingPTZRuntime) Start(context.Context)                {}
func (*pendingPTZRuntime) Stop()                                {}
func (p *pendingPTZRuntime) FlushResults(context.Context) error { return p.err }

func TestPTZFailedWritebackRetainsRuntimeForRetry(t *testing.T) {
	oldScheduler, oldService := ptzScheduler, ptzService
	defer func() { ptzScheduler, ptzService = oldScheduler, oldService }()
	owner := &pendingPTZRuntime{err: errors.New("result persistence unavailable")}
	ptzScheduler, ptzService = owner, nil
	require.Error(t, stopPTZRuntime())
	require.Same(t, owner, ptzScheduler, "unflushed facts must not lose their owner")
	called := false
	err := startSIPDependenciesWithFactory(gbconfig.Config{}, nil, func(gbconfig.Config) (sipRuntimeServer, error) {
		called = true
		return nil, errors.New("unexpected replacement")
	})
	require.ErrorContains(t, err, "旧运行时尚未释放")
	require.False(t, called)
	owner.err = nil
	require.NoError(t, stopPTZRuntime())
	require.Nil(t, ptzScheduler)
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
	require.NoError(t, stopSIPDependencies(context.Background()))

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

	require.NoError(t, stopSIPDependencies(context.Background()))

	require.Equal(t, []string{"playback.teardown", "playback.rtp.close", "playback.unbind", "record.sink.clear", "sip.shutdown"}, events)
	require.Nil(t, playbackService)
	require.Nil(t, recordQueryService)
	require.Nil(t, sipServer)
}
