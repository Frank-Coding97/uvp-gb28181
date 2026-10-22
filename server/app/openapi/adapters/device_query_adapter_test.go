package adapters

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/devicequery"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	deviceQueryAdapterOwner   = uint(10)
	deviceQueryAdapterDevice  = "34020000002000000011"
	deviceQueryAdapterChannel = "37011200001310000011"
)

type deviceQueryReaderStub struct {
	alarms  func(context.Context, resource.DepartmentScope, string, resource.DeviceListOptions) (devicequery.AlarmPage, error)
	events  func(context.Context, resource.DepartmentScope, string, int, string) (devicequery.StatusEventPage, error)
	precise func(context.Context, resource.DepartmentScope, string, string) (devicequery.PreciseStatusSnapshot, error)
	home    func(context.Context, resource.DepartmentScope, string, string) (devicequery.HomePositionSnapshot, error)
	cruise  func(context.Context, resource.DepartmentScope, string, string) (devicequery.CruiseTrackSnapshot, error)
	target  func(context.Context, resource.DepartmentScope, string, string) (devicequery.TargetTrackSnapshot, error)

	maintenance func(context.Context, resource.DepartmentScope, string, resource.DeviceListOptions) (devicequery.MaintenanceOperationPage, error)
	mounts      func(context.Context, resource.DepartmentScope, string, string) (devicequery.ChannelMountList, error)
	timeline    func(context.Context, resource.DepartmentScope, string, string) (devicequery.ChannelTimeline, error)
}

func (stub *deviceQueryReaderStub) ListDeviceAlarmsInScope(ctx context.Context, scope resource.DepartmentScope, deviceID string, options resource.DeviceListOptions) (devicequery.AlarmPage, error) {
	if stub.alarms == nil {
		return devicequery.AlarmPage{}, errors.New("unexpected alarm query")
	}
	return stub.alarms(ctx, scope, deviceID, options)
}
func (stub *deviceQueryReaderStub) ListDeviceStatusEventsInScope(ctx context.Context, scope resource.DepartmentScope, deviceID string, pageSize int, cursor string) (devicequery.StatusEventPage, error) {
	if stub.events == nil {
		return devicequery.StatusEventPage{}, errors.New("unexpected event query")
	}
	return stub.events(ctx, scope, deviceID, pageSize, cursor)
}
func (stub *deviceQueryReaderStub) GetChannelPreciseStatusInScope(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (devicequery.PreciseStatusSnapshot, error) {
	if stub.precise == nil {
		return devicequery.PreciseStatusSnapshot{}, errors.New("unexpected precise status query")
	}
	return stub.precise(ctx, scope, deviceID, channelID)
}
func (stub *deviceQueryReaderStub) GetChannelHomePositionInScope(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (devicequery.HomePositionSnapshot, error) {
	if stub.home == nil {
		return devicequery.HomePositionSnapshot{}, errors.New("unexpected home position query")
	}
	return stub.home(ctx, scope, deviceID, channelID)
}
func (stub *deviceQueryReaderStub) ListChannelCruiseTracksInScope(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (devicequery.CruiseTrackSnapshot, error) {
	if stub.cruise == nil {
		return devicequery.CruiseTrackSnapshot{}, errors.New("unexpected cruise track query")
	}
	return stub.cruise(ctx, scope, deviceID, channelID)
}
func (stub *deviceQueryReaderStub) GetChannelTargetTrackInScope(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (devicequery.TargetTrackSnapshot, error) {
	if stub.target == nil {
		return devicequery.TargetTrackSnapshot{}, errors.New("unexpected target track query")
	}
	return stub.target(ctx, scope, deviceID, channelID)
}
func (stub *deviceQueryReaderStub) ListDeviceMaintenanceOperationsInScope(ctx context.Context, scope resource.DepartmentScope, deviceID string, options resource.DeviceListOptions) (devicequery.MaintenanceOperationPage, error) {
	if stub.maintenance == nil {
		return devicequery.MaintenanceOperationPage{}, errors.New("unexpected maintenance operation query")
	}
	return stub.maintenance(ctx, scope, deviceID, options)
}
func (stub *deviceQueryReaderStub) ListChannelMountsInScope(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (devicequery.ChannelMountList, error) {
	if stub.mounts == nil {
		return devicequery.ChannelMountList{}, errors.New("unexpected channel mount query")
	}
	return stub.mounts(ctx, scope, deviceID, channelID)
}
func (stub *deviceQueryReaderStub) GetChannelTimelineInScope(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (devicequery.ChannelTimeline, error) {
	if stub.timeline == nil {
		return devicequery.ChannelTimeline{}, errors.New("unexpected channel timeline query")
	}
	return stub.timeline(ctx, scope, deviceID, channelID)
}

func TestNewDeviceQueryAdaptersExposeFixedV1Keys(t *testing.T) {
	registrations := NewDeviceQueryAdapterRegistrations(&deviceQueryReaderStub{})
	require.Len(t, registrations, 9)
	keys := make([]string, 0, len(registrations))
	for _, registration := range registrations {
		keys = append(keys, registration.Key)
		assert.Equal(t, "v1", registration.ContractVersion)
		assert.NotNil(t, registration.Adapter)
	}
	assert.ElementsMatch(t, []string{
		DeviceAlarmsAdapterKey, DeviceStatusEventsAdapterKey, ChannelPreciseStatusAdapterKey,
		ChannelHomePositionAdapterKey, ChannelCruiseTracksAdapterKey, ChannelTargetTrackReadAdapterKey,
		DeviceMaintenanceListAdapterKey, ChannelMountsReadAdapterKey, ChannelTimelineReadAdapterKey,
	}, keys)
}

func TestDeviceQueryAdapterPassesTrustedScopePaginationAndCursor(t *testing.T) {
	wantScope := resource.DepartmentScope{OwnerDeptID: deviceQueryAdapterOwner, DataScope: resource.DataScopeDepartmentAndChildren}
	var gotScope resource.DepartmentScope
	var gotList resource.DeviceListOptions
	adapter := deviceQueryAdapterFor(t, &deviceQueryReaderStub{alarms: func(_ context.Context, scope resource.DepartmentScope, deviceID string, options resource.DeviceListOptions) (devicequery.AlarmPage, error) {
		gotScope, gotList = scope, options
		assert.Equal(t, deviceQueryAdapterDevice, deviceID)
		return devicequery.AlarmPage{Items: []devicequery.DeviceAlarm{}}, nil
	}}, DeviceAlarmsAdapterKey)
	_, err := adapter.Dispatch(context.Background(), Request{
		ResourceScope: wantScope, DeviceID: deviceQueryAdapterDevice,
		List: ListOptions{Page: 2, PageSize: 25},
	})
	require.NoError(t, err)
	assert.Equal(t, wantScope, gotScope)
	assert.Equal(t, resource.DeviceListOptions{Page: 2, PageSize: 25}, gotList)

	var gotPageSize int
	var gotCursor string
	eventAdapter := deviceQueryAdapterFor(t, &deviceQueryReaderStub{events: func(_ context.Context, scope resource.DepartmentScope, deviceID string, size int, cursor string) (devicequery.StatusEventPage, error) {
		gotScope, gotPageSize, gotCursor = scope, size, cursor
		assert.Equal(t, deviceQueryAdapterDevice, deviceID)
		return devicequery.StatusEventPage{Items: []devicequery.DeviceStatusEvent{}}, nil
	}}, DeviceStatusEventsAdapterKey)
	_, err = eventAdapter.Dispatch(context.Background(), Request{
		ResourceScope: wantScope, DeviceID: deviceQueryAdapterDevice,
		List: ListOptions{Page: 1, PageSize: 40}, Cursor: "opaque-cursor",
	})
	require.NoError(t, err)
	assert.Equal(t, wantScope, gotScope)
	assert.Equal(t, 40, gotPageSize)
	assert.Equal(t, "opaque-cursor", gotCursor)
}

func TestChannelQueryAdaptersPreserveChannelPairAndMapErrors(t *testing.T) {
	wantScope := resource.DepartmentScope{OwnerDeptID: deviceQueryAdapterOwner, DataScope: resource.DataScopeDepartment}
	var gotDeviceID, gotChannelID string
	adapter := deviceQueryAdapterFor(t, &deviceQueryReaderStub{precise: func(_ context.Context, _ resource.DepartmentScope, deviceID, channelID string) (devicequery.PreciseStatusSnapshot, error) {
		gotDeviceID, gotChannelID = deviceID, channelID
		return devicequery.PreciseStatusSnapshot{Freshness: gbmodels.PTZFreshnessUnknown}, nil
	}}, ChannelPreciseStatusAdapterKey)
	data, err := adapter.Dispatch(context.Background(), Request{ResourceScope: wantScope, DeviceID: deviceQueryAdapterDevice, ChannelID: deviceQueryAdapterChannel})
	require.NoError(t, err)
	assert.Equal(t, deviceQueryAdapterDevice, gotDeviceID)
	assert.Equal(t, deviceQueryAdapterChannel, gotChannelID)
	assert.Equal(t, gbmodels.PTZFreshnessUnknown, data.(devicequery.PreciseStatusSnapshot).Freshness)

	notFound := deviceQueryAdapterFor(t, &deviceQueryReaderStub{target: func(context.Context, resource.DepartmentScope, string, string) (devicequery.TargetTrackSnapshot, error) {
		return devicequery.TargetTrackSnapshot{}, resource.ErrResourceNotFound
	}}, ChannelTargetTrackReadAdapterKey)
	_, err = notFound.Dispatch(context.Background(), Request{ResourceScope: wantScope, DeviceID: deviceQueryAdapterDevice, ChannelID: deviceQueryAdapterChannel})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDeviceQueryAdapterRejectsInvalidScopeFiltersAndTransport(t *testing.T) {
	adapter := deviceQueryAdapterFor(t, &deviceQueryReaderStub{alarms: func(context.Context, resource.DepartmentScope, string, resource.DeviceListOptions) (devicequery.AlarmPage, error) {
		t.Fatal("query must not run")
		return devicequery.AlarmPage{}, nil
	}}, DeviceAlarmsAdapterKey)
	_, err := adapter.Dispatch(context.Background(), Request{ResourceScope: resource.DepartmentScope{OwnerDeptID: deviceQueryAdapterOwner, DataScope: 2}, DeviceID: deviceQueryAdapterDevice})
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = adapter.Dispatch(context.Background(), Request{ResourceScope: resource.DepartmentScope{OwnerDeptID: deviceQueryAdapterOwner, DataScope: resource.DataScopeDepartment}, DeviceID: deviceQueryAdapterDevice, List: ListOptions{Page: 1, PageSize: 20, Keyword: "ignored"}})
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = adapter.Execute(context.Background(), runtime.Invocation{Scope: DeviceAlarmsScope, Method: "POST", Value: Request{}})
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = adapter.Execute(context.Background(), runtime.Invocation{Scope: DeviceAlarmsScope, Method: "GET", Body: []byte("{}"), Value: Request{}})
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = adapter.Execute(context.Background(), runtime.Invocation{Scope: DeviceAlarmsScope, Method: "GET", Query: map[string]string{"unknown": "x"}, Value: Request{}})
	assert.ErrorIs(t, err, ErrInvalidRequest)
}

func deviceQueryAdapterFor(t *testing.T, reader DeviceQueryReader, key string) Adapter {
	t.Helper()
	for _, adapter := range NewDeviceQueryAdapters(reader) {
		if adapter.Key() == key {
			return adapter
		}
	}
	t.Fatalf("adapter %q is not registered", key)
	return nil
}
