package adapters

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

type managementServiceStub struct {
	calls          int
	channelInput   resource.ChannelUpdate
	deviceOptions  resource.DeviceListOptions
	channelOptions resource.ChannelListOptions
}

func (s *managementServiceStub) GetDeviceStatisticsInScope(_ context.Context, _ resource.DepartmentScope, o resource.DeviceListOptions) (resource.DeviceStatistics, error) {
	s.calls++
	s.deviceOptions = o
	return resource.DeviceStatistics{Total: 3}, nil
}
func (s *managementServiceStub) GetChannelStatisticsInScope(_ context.Context, _ resource.DepartmentScope, _ string, o resource.ChannelListOptions) (resource.ChannelStatistics, error) {
	s.calls++
	s.channelOptions = o
	return resource.ChannelStatistics{Unknown: 1}, nil
}
func (s *managementServiceStub) UpdateDeviceInScope(_ context.Context, _ resource.DepartmentScope, _ string, _ resource.DeviceUpdate) (resource.Device, error) {
	s.calls++
	return resource.Device{}, nil
}
func (s *managementServiceStub) UpdateChannelInScope(_ context.Context, _ resource.DepartmentScope, _ string, _ string, i resource.ChannelUpdate) (resource.Channel, error) {
	s.calls++
	s.channelInput = i
	return resource.Channel{}, nil
}

func TestResourceManagementAdaptersRejectMalformedBodyBeforeCallingService(t *testing.T) {
	service := &managementServiceStub{}
	regs := NewResourceManagementAdapterRegistrations(service)
	var adapter catalogruntime.Adapter
	for _, r := range regs {
		if r.Key == ChannelUpdateAdapterKey {
			adapter = r.Adapter
		}
	}
	require.NotNil(t, adapter)
	req := Request{ClientID: 1, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: 3}, DeviceID: "34020000002000000010", ChannelID: "37011200001310000010"}
	inv := catalogruntime.Invocation{Scope: ChannelUpdateScope, Method: "POST", Params: map[string]string{"deviceId": req.DeviceID, "channelId": req.ChannelID}, Value: req}
	for _, body := range []string{`{}`, `{"alias":null}`, `{"alias":"x","unknown":1}`, `{"audioEnabled":0}`, `{"alias":"x","alias":"y"}`, `{"longitude":117.2}`, `{"ptzType":8}`, `{"longitude":181,"latitude":31}`, `{"streamTransport":"TCP"}`} {
		inv.Body = []byte(body)
		_, err := adapter.Execute(context.Background(), inv)
		require.ErrorIs(t, err, ErrInvalidRequest, body)
	}
	require.Zero(t, service.calls)
	inv.Body = []byte(`{"audioEnabled":false,"ptzType":0,"alias":""}`)
	_, err := adapter.Execute(context.Background(), inv)
	require.NoError(t, err)
	require.Equal(t, 1, service.calls)
	require.NotNil(t, service.channelInput.AudioEnabled)
	require.False(t, *service.channelInput.AudioEnabled)
	require.Equal(t, int8(0), *service.channelInput.PTZType)
	inv.Params["deviceId"] = "34020000002000000020"
	_, err = adapter.Execute(context.Background(), inv)
	require.ErrorIs(t, err, ErrInvalidRequest)
}
func TestResourceExtendedFilterParsingPreservesFalseAndRejectsInvalid(t *testing.T) {
	req := Request{Query: map[string]string{"manufacturer": "vendor", "model": "cam", "ptzType": "0", "audioEnabled": "false", "streamTransport": "UDP", "sort": "name", "order": "desc"}}
	opts, err := channelResourceOptions(req)
	require.NoError(t, err)
	require.Equal(t, int8(0), *opts.PTZType)
	require.False(t, *opts.AudioEnabled)
	require.Equal(t, "desc", opts.Order)
	for _, value := range []string{"0", "False", "yes", ""} {
		req.Query["audioEnabled"] = value
		_, err = channelResourceOptions(req)
		require.ErrorIs(t, err, ErrInvalidRequest)
	}
}
