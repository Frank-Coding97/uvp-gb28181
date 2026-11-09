package adapters

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

type refreshResolverStub struct{ target runtimeptz.Target }

func (s refreshResolverStub) ResolveDevice(context.Context, resource.DepartmentScope, string) (MaintenanceDevice, error) {
	return MaintenanceDevice{}, nil
}
func (s refreshResolverStub) ResolveChannel(context.Context, resource.DepartmentScope, string, string) (MaintenanceChannel, error) {
	return MaintenanceChannel{Target: s.target}, nil
}
func (s refreshResolverStub) ResolveAlarmTarget(context.Context, resource.DepartmentScope, MaintenanceChannel) (string, error) {
	return "", nil
}

type refreshExecutorStub struct {
	command runtimeptz.Command
	calls   int
}

func (s *refreshExecutorStub) Execute(_ context.Context, target runtimeptz.Target, command runtimeptz.Command) (gbmodels.GbPTZOperation, error) {
	s.command = command
	s.calls++
	return gbmodels.GbPTZOperation{OperationID: "query-1", Action: command.Action, DeviceCode: target.DeviceCode, ChannelCode: target.ChannelCode, IdempotencyKey: command.IdempotencyKey, ResponseRequired: true}, nil
}

type refreshReaderStub struct{ operation gbmodels.GbPTZOperation }

func (s refreshReaderStub) GetOperationForClient(context.Context, resource.DepartmentScope, string, string, int64) (gbmodels.GbPTZOperation, error) {
	return s.operation, nil
}

func TestChannelRefreshBuildsClientScopedQueriesAndRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	executor := &refreshExecutorStub{}
	target := runtimeptz.Target{DeviceID: 1, ChannelID: 2, DeviceCode: deviceQueryAdapterDevice, ChannelCode: deviceQueryAdapterChannel, DeviceOnline: true, ChannelOnline: true, Profile: protocol.ProfileFor(protocol.Version2022)}
	registrations := NewChannelRefreshAdapterRegistrations(refreshResolverStub{target}, executor, nil)
	request := Request{ClientID: 91, IdempotencyKey: "refresh-1", DeviceID: target.DeviceCode, ChannelID: target.ChannelCode, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}}
	for _, registration := range registrations {
		if registration.Key == ChannelQueryOperationAdapterKey {
			continue
		}
		body := `{}`
		if strings.Contains(registration.Key, "status") {
			body = `{"kind":"record"}`
		}
		invocation := catalogruntime.Invocation{Scope: map[string]string{
			"devicequery.refresh.preset.v1": "channel:ptz:preset:refresh", "devicequery.refresh.home.v1": "channel:ptz:home:refresh", "devicequery.refresh.cruise.v1": "channel:ptz:cruise:refresh", "devicequery.refresh.position.v1": "channel:ptz:position:refresh", "devicequery.refresh.storage.v1": "channel:storage-card:refresh", "devicequery.refresh.status.v1": "channel:control-status:refresh",
		}[registration.Key], Method: "POST", Params: map[string]string{"deviceId": target.DeviceCode, "channelId": target.ChannelCode}, Body: []byte(body), Value: request}
		_, err := registration.Adapter.Execute(ctx, invocation)
		require.NoError(t, err, registration.Key)
		require.Equal(t, uint(10), executor.command.ActorDeptID)
		require.True(t, strings.HasPrefix(executor.command.IdempotencyKey, clientIdempotencyPrefix(91)))
		xml, err := executor.command.Build(42)
		require.NoError(t, err)
		require.Contains(t, string(xml), "42")
		calls := executor.calls
		invocation.Body = []byte(`{"trackId":null}`)
		_, err = registration.Adapter.Execute(ctx, invocation)
		require.ErrorIs(t, err, ErrInvalidRequest)
		require.Equal(t, calls, executor.calls)
	}
}

func TestQueryOperationReadRejectsOtherClientChannelAndNonQueryAction(t *testing.T) {
	target := runtimeptz.Target{DeviceID: 1, ChannelID: 2, DeviceCode: deviceQueryAdapterDevice, ChannelCode: deviceQueryAdapterChannel}
	operation := gbmodels.GbPTZOperation{OperationID: "00000000-0000-4000-8000-000000000001", Action: "refresh_presets", ChannelID: 2, DeviceCode: target.DeviceCode, ChannelCode: target.ChannelCode, IdempotencyKey: clientIdempotencyPrefix(91) + "abc"}
	invocation := catalogruntime.Invocation{Scope: ChannelQueryOperationScope, Method: "GET", Params: map[string]string{"deviceId": target.DeviceCode, "channelId": target.ChannelCode, "operationId": operation.OperationID}, Value: Request{ClientID: 91, DeviceID: target.DeviceCode, ChannelID: target.ChannelCode, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}}}
	for _, change := range []string{"valid", "client", "channel", "action"} {
		row := operation
		switch change {
		case "client":
			row.IdempotencyKey = clientIdempotencyPrefix(92) + "abc"
		case "channel":
			row.ChannelCode = "another"
		case "action":
			row.Action = "teleboot"
		}
		registrations := NewChannelRefreshAdapterRegistrations(refreshResolverStub{target}, nil, refreshReaderStub{row})
		for _, registration := range registrations {
			if registration.Key != ChannelQueryOperationAdapterKey {
				continue
			}
			_, err := registration.Adapter.Execute(context.Background(), invocation)
			if change == "valid" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrNotFound)
			}
		}
	}
}

func TestRefreshCruiseDetailAndAlarmStatusUseTheirRealSignalTargets(t *testing.T) {
	target := runtimeptz.Target{DeviceCode: deviceQueryAdapterDevice, ChannelCode: deviceQueryAdapterChannel, Profile: protocol.ProfileFor(protocol.Version2022)}
	track := 7
	cruise, err := buildChannelRefreshCommand("cruise", target, channelRefreshInput{TrackID: &track}, "key", 10)
	require.NoError(t, err)
	require.Equal(t, "refresh_cruise_track", cruise.Action)
	xml, err := cruise.Build(12)
	require.NoError(t, err)
	require.Contains(t, string(xml), "<Number>7</Number>")
	alarm, err := buildChannelRefreshCommand("status", target, channelRefreshInput{Kind: "alarm"}, "key", 10)
	require.NoError(t, err)
	require.Equal(t, target.DeviceCode, alarm.TargetCode)
	require.Equal(t, gbmodels.ControlTargetScopeDevice, alarm.TargetScope)
	xml, err = alarm.Build(12)
	require.NoError(t, err)
	require.Contains(t, string(xml), target.DeviceCode)
	for _, body := range []string{`{"kind":"other"}`, `{"kind":"record","kind":"alarm"}`, `{"kind":null}`, `{} {}`, `{"kind":"record","confirmed":true}`} {
		_, err = decodeChannelRefreshInput([]byte(body), "status")
		require.ErrorIs(t, err, ErrInvalidRequest, body)
	}
	target.Profile = protocol.ProfileFor(protocol.Version2016)
	_, err = buildChannelRefreshCommand("storage", target, channelRefreshInput{}, "key", 10)
	require.ErrorIs(t, err, ErrCapabilityNotSupported)
}
