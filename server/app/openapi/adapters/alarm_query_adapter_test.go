package adapters

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/devicequery"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

type alarmReaderSpy struct {
	filter     devicequery.AlarmFilter
	device, id string
}

func (r *alarmReaderSpy) SearchDeviceAlarmsInScope(_ context.Context, _ resource.DepartmentScope, device string, filter devicequery.AlarmFilter) (devicequery.AlarmPage, error) {
	r.filter = filter
	r.device = device
	return devicequery.AlarmPage{}, nil
}
func (r *alarmReaderSpy) GetDeviceAlarmInScope(_ context.Context, _ resource.DepartmentScope, device, id string) (devicequery.DeviceAlarm, error) {
	r.device = device
	r.id = id
	return devicequery.DeviceAlarm{ID: id}, nil
}
func TestAlarmDetailAndFiltersRejectMalformedTransport(t *testing.T) {
	reader := &alarmReaderSpy{}
	adapter := NewDeviceAlarmAdapterRegistrations(reader)[0].Adapter
	request := Request{ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: deviceQueryAdapterDevice}
	invocation := catalogruntime.Invocation{Scope: DeviceAlarmDetailScope, Method: "GET", Params: map[string]string{"deviceId": deviceQueryAdapterDevice, "alarmId": "123"}, Value: request}
	_, err := adapter.Execute(context.Background(), invocation)
	require.NoError(t, err)
	require.Equal(t, "123", reader.id)
	invocation.Params["deviceId"] = "other"
	_, err = adapter.Execute(context.Background(), invocation)
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = alarmFilterFromRequest(Request{Query: map[string]string{"priority": "bad"}})
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = alarmFilterFromRequest(Request{Query: map[string]string{"alarmFrom": "yesterday"}})
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = alarmFilterFromRequest(Request{Query: map[string]string{"unknown": "x"}})
	require.ErrorIs(t, err, ErrInvalidRequest)
	filter, err := alarmFilterFromRequest(Request{Query: map[string]string{"sourceCode": "123", "priority": "2", "method": "4", "alarmType": "1", "alarmFrom": "2026-11-09T00:00:00Z"}, List: ListOptions{Page: 2, PageSize: 10, Keyword: "烟雾"}})
	require.NoError(t, err)
	require.Equal(t, 2, *filter.Priority)
	require.Equal(t, "烟雾", filter.Keyword)
	require.NotNil(t, filter.AlarmFrom)
}

type filteredDeviceQueryReader struct {
	*deviceQueryReaderStub
	*alarmReaderSpy
}

func TestDeviceAlarmAdapterDispatchesValidatedRichFilters(t *testing.T) {
	spy := &alarmReaderSpy{}
	reader := &filteredDeviceQueryReader{deviceQueryReaderStub: &deviceQueryReaderStub{}, alarmReaderSpy: spy}
	adapter := deviceQueryAdapterFor(t, reader, DeviceAlarmsAdapterKey)
	request := Request{DeviceID: deviceQueryAdapterDevice, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, List: ListOptions{Page: 2, PageSize: 20, Keyword: "烟雾"}, Query: map[string]string{"keyword": "烟雾", "priority": "1", "alarmTo": "2026-11-09T01:00:00Z"}}
	_, err := adapter.Execute(context.Background(), catalogruntime.Invocation{Scope: DeviceAlarmsScope, Method: "GET", Params: map[string]string{"deviceId": request.DeviceID}, Query: request.Query, Value: request})
	require.NoError(t, err)
	require.Equal(t, "烟雾", spy.filter.Keyword)
	require.Equal(t, 2, spy.filter.Page)
	require.Equal(t, 1, *spy.filter.Priority)
	require.NotNil(t, spy.filter.AlarmTo)
}
