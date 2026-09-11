package control

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
)

func TestServiceForwardsBasicControlToSourceTarget(t *testing.T) {
	tests := []struct {
		name, raw, action string
	}{
		{name: "direction", raw: "A50F010A080800CF", action: "left_up"},
		{name: "stop", raw: "A50F0100000000B5", action: "stop"},
		{name: "zoom", raw: "A50F0110000010D5", action: "zoom_in"},
		{name: "focus", raw: "A50F0142080000FF", action: "focus_near"},
		{name: "iris", raw: "A50F014800080005", action: "iris_close"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeExecutor{}
			service := testService(true, true, executor)
			result, err := service.Forward(context.Background(), ForwardRequest{
				PlatformID: 1, CallID: "call-a", Body: controlBody("2016", 7, "published-a", test.raw),
			})
			require.NoError(t, err)
			require.Equal(t, test.action, result.Action)
			require.Equal(t, uint(11), executor.target.ChannelID)
			require.Equal(t, "source-channel", executor.target.ChannelCode)
			require.Equal(t, "cascade_"+test.action, executor.command.Action)
			require.Equal(t, "cascade:1:call-a:7", executor.command.IdempotencyKey)
			require.Equal(t, uint64(1), executor.command.Payload["platformId"])
			require.Equal(t, "call-a", executor.command.Payload["callId"])
			require.Equal(t, 7, executor.command.Payload["upstreamSn"])
			require.Equal(t, "published-a", executor.command.Payload["publishedChannelId"])
			require.Equal(t, test.raw, executor.command.Payload["rawPtz"])

			downstream, buildErr := executor.command.Build(99)
			require.NoError(t, buildErr)
			require.Contains(t, string(downstream), "<SN>99</SN>")
			require.Contains(t, string(downstream), "<DeviceID>source-channel</DeviceID>")
			require.Contains(t, string(downstream), "<PTZCmd>"+test.raw+"</PTZCmd>")
		})
	}
}

func TestServiceEnforcesPlatformAndChannelAuthorizationBeforeOperation(t *testing.T) {
	tests := []struct {
		name       string
		ptzEnabled bool
		ptzAllowed bool
		deviceID   string
		want       error
	}{
		{name: "platform disabled", ptzEnabled: false, ptzAllowed: true, deviceID: "published-a", want: ErrFeatureUnavailable},
		{name: "channel denied", ptzEnabled: true, ptzAllowed: false, deviceID: "published-a", want: ErrControlUnauthorized},
		{name: "other platform id", ptzEnabled: true, ptzAllowed: true, deviceID: "published-b", want: ErrControlUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeExecutor{}
			service := testService(test.ptzEnabled, test.ptzAllowed, executor)
			_, err := service.Forward(context.Background(), ForwardRequest{
				PlatformID: 1, CallID: "call-a", Body: controlBody("2016", 7, test.deviceID, "A50F0100000000B5"),
			})
			require.ErrorIs(t, err, test.want)
			require.Zero(t, executor.calls)
		})
	}
}

func TestServiceExplainsChannelAuthorizationFailure(t *testing.T) {
	executor := &fakeExecutor{}
	service := testService(true, false, executor)
	_, err := service.Forward(context.Background(), ForwardRequest{
		PlatformID: 1, CallID: "call-a", Body: controlBody("2016", 7, "published-a", "A50F0100000000B5"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrControlUnauthorized)
	require.Contains(t, err.Error(), `channel "published-a" PTZ permission disabled`)
}

func TestServiceRejectsOutOfScopeControlWithoutOperation(t *testing.T) {
	executor := &fakeExecutor{}
	service := testService(true, true, executor)
	_, err := service.Forward(context.Background(), ForwardRequest{
		PlatformID: 1, CallID: "call-a", Body: controlBody("2016", 8, "published-a", "A50F018100030039"),
	})
	require.ErrorIs(t, err, ErrFeatureUnavailable)
	require.Zero(t, executor.calls)
}

func TestServiceUsesUpstreamProfileWithoutChangingDownstreamProfile(t *testing.T) {
	executor := &fakeExecutor{target: ptz.Target{DeviceID: 2, DeviceCode: "source-device", ChannelID: 11, ChannelCode: "source-channel",
		IP: "192.0.2.10", Port: 5060, Transport: "UDP", DeviceOnline: true, ChannelOnline: true,
		Profile: protocol.ProfileFor(protocol.Version2016)}}
	service := testServiceWithVersion("2022", true, true, executor)
	_, err := service.Forward(context.Background(), ForwardRequest{
		PlatformID: 1, CallID: "call-a", Body: controlBody("2022", 9, "published-a", "A50F0100000000B5"),
	})
	require.NoError(t, err)
	require.Equal(t, protocol.Version(protocol.Version2016), executor.command.Profile.Version)
	body, err := executor.command.Build(100)
	require.NoError(t, err)
	require.Contains(t, string(body), `encoding="GB2312"`)
}

func TestResultSeparatesTransportAndBusinessState(t *testing.T) {
	tests := []struct {
		status    gbmodels.PTZOperationStatus
		transport TransportState
		business  BusinessState
	}{
		{status: gbmodels.PTZOperationSent, transport: TransportAccepted, business: BusinessPending},
		{status: gbmodels.PTZOperationAccepted, transport: TransportAccepted, business: BusinessOK},
		{status: gbmodels.PTZOperationRejected, transport: TransportRejected, business: BusinessError},
		{status: gbmodels.PTZOperationTimeout, transport: TransportTimeout, business: BusinessUnknown},
	}
	for _, test := range tests {
		result := resultFromOperation(gbmodels.GbPTZOperation{Status: test.status})
		require.Equal(t, test.transport, result.Transport)
		require.Equal(t, test.business, result.Business)
	}
}

func testService(ptzEnabled, ptzAllowed bool, executor *fakeExecutor) *Service {
	return testServiceWithVersion("2016", ptzEnabled, ptzAllowed, executor)
}

func testServiceWithVersion(version string, ptzEnabled, ptzAllowed bool, executor *fakeExecutor) *Service {
	store := fakeProjectionStore{snapshot: &repository.ProjectionSnapshot{
		Platform: model.GbCascadePlatform{ID: 1, Enabled: true, PTZEnabled: ptzEnabled, EffectiveVersion: version},
		Devices:  []model.GbCascadeDeviceProjection{{ID: 21, PlatformID: 1, SourceDeviceID: 2, PublishedDeviceID: "published-device", Active: true}},
		Channels: []model.GbCascadeChannelProjection{
			{ID: 31, PlatformID: 1, DeviceProjectionID: 21, SourceChannelID: 11, PublishedChannelID: "published-a", PTZAllowed: ptzAllowed, Active: true},
			{ID: 32, PlatformID: 2, DeviceProjectionID: 22, SourceChannelID: 12, PublishedChannelID: "published-b", PTZAllowed: true, Active: true},
		},
	}}
	if executor.target.ChannelID == 0 {
		executor.target = ptz.Target{DeviceID: 2, DeviceCode: "source-device", ChannelID: 11, ChannelCode: "source-channel",
			IP: "192.0.2.10", Port: 5060, Transport: "UDP", DeviceOnline: true, ChannelOnline: true,
			Profile: protocol.ProfileFor(protocol.Version2022)}
	}
	return NewService(store, fakeTargetLoader{target: executor.target}, executor)
}

func controlBody(version string, sn int, deviceID, raw string) []byte {
	charset := "GB2312"
	if version == "2022" {
		charset = "GB18030"
	}
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="%s"?><Control><CmdType>DeviceControl</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><PTZCmd>%s</PTZCmd></Control>`, charset, sn, deviceID, raw))
}

type fakeProjectionStore struct {
	snapshot *repository.ProjectionSnapshot
	err      error
}

func (f fakeProjectionStore) ProjectionSnapshot(context.Context, uint64) (*repository.ProjectionSnapshot, error) {
	return f.snapshot, f.err
}

type fakeTargetLoader struct {
	target ptz.Target
	err    error
}

func (f fakeTargetLoader) Load(context.Context, uint64, uint64) (ptz.Target, error) {
	return f.target, f.err
}

type fakeExecutor struct {
	target    ptz.Target
	command   ptz.Command
	operation gbmodels.GbPTZOperation
	err       error
	calls     int
}

func (f *fakeExecutor) Execute(_ context.Context, target ptz.Target, command ptz.Command) (gbmodels.GbPTZOperation, error) {
	f.calls++
	f.target = target
	f.command = command
	if f.operation.OperationID == "" {
		f.operation = gbmodels.GbPTZOperation{OperationID: "op-1", Status: gbmodels.PTZOperationSent}
	}
	return f.operation, f.err
}
