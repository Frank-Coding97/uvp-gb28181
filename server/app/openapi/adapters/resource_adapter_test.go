package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	adapterOwnerDeptID = uint(10)
	adapterDeviceID    = "34020000002000000011"
	adapterChannelID   = "37011200001310000011"
	adapterRootDevice  = "34020000002000000010"
	adapterOtherDevice = "34020000002000000012"
	adapterRootChannel = "37011200001310000010"
)

type resourceReaderStub struct {
	listDevices      func(context.Context, resource.DepartmentScope, resource.DeviceListOptions) (resource.DevicePage, error)
	getDevice        func(context.Context, resource.DepartmentScope, string) (resource.Device, error)
	getDeviceStatus  func(context.Context, resource.DepartmentScope, string) (resource.DeviceStatus, error)
	listChannels     func(context.Context, resource.DepartmentScope, string, resource.ChannelListOptions) (resource.ChannelPage, error)
	getChannel       func(context.Context, resource.DepartmentScope, string, string) (resource.Channel, error)
	getChannelStatus func(context.Context, resource.DepartmentScope, string, string) (resource.ChannelStatus, error)
}

func (stub *resourceReaderStub) ListDevicesInScope(ctx context.Context, scope resource.DepartmentScope, options resource.DeviceListOptions) (resource.DevicePage, error) {
	if stub.listDevices == nil {
		return resource.DevicePage{}, errors.New("unexpected ListDevicesInScope")
	}
	return stub.listDevices(ctx, scope, options)
}

func (stub *resourceReaderStub) GetDeviceInScope(ctx context.Context, scope resource.DepartmentScope, deviceID string) (resource.Device, error) {
	if stub.getDevice == nil {
		return resource.Device{}, errors.New("unexpected GetDeviceInScope")
	}
	return stub.getDevice(ctx, scope, deviceID)
}

func (stub *resourceReaderStub) GetDeviceStatusInScope(ctx context.Context, scope resource.DepartmentScope, deviceID string) (resource.DeviceStatus, error) {
	if stub.getDeviceStatus == nil {
		return resource.DeviceStatus{}, errors.New("unexpected GetDeviceStatusInScope")
	}
	return stub.getDeviceStatus(ctx, scope, deviceID)
}

func (stub *resourceReaderStub) ListChannelsInScope(ctx context.Context, scope resource.DepartmentScope, deviceID string, options resource.ChannelListOptions) (resource.ChannelPage, error) {
	if stub.listChannels == nil {
		return resource.ChannelPage{}, errors.New("unexpected ListChannelsInScope")
	}
	return stub.listChannels(ctx, scope, deviceID, options)
}

func (stub *resourceReaderStub) GetChannelInScope(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (resource.Channel, error) {
	if stub.getChannel == nil {
		return resource.Channel{}, errors.New("unexpected GetChannelInScope")
	}
	return stub.getChannel(ctx, scope, deviceID, channelID)
}

func (stub *resourceReaderStub) GetChannelStatusInScope(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (resource.ChannelStatus, error) {
	if stub.getChannelStatus == nil {
		return resource.ChannelStatus{}, errors.New("unexpected GetChannelStatusInScope")
	}
	return stub.getChannelStatus(ctx, scope, deviceID, channelID)
}

func resourceAdapterFor(t *testing.T, reader ResourceReader, key string) Adapter {
	t.Helper()
	for _, adapter := range NewDeviceChannelAdapters(reader) {
		if adapter.Key() == key {
			return adapter
		}
	}
	t.Fatalf("adapter %q is not registered", key)
	return nil
}

func newDeviceChannelAdapterService(t *testing.T) *resource.Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&appmodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{}))

	active := int8(1)
	rootID := adapterOwnerDeptID
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: rootID}, Name: "root", Status: &active}).Error)
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: 11}, ParentID: &rootID, Name: "child", Status: &active}).Error)
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: 20}, Name: "other", Status: &active}).Error)
	for _, device := range []gbmodels.GbDevice{
		{DeviceID: adapterRootDevice, Name: "root-device", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: rootID},
		{DeviceID: adapterDeviceID, Name: "child-device", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 11},
		{DeviceID: adapterOtherDevice, Name: "other-device", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 20},
	} {
		require.NoError(t, db.Create(&device).Error)
	}
	for _, channel := range []gbmodels.GbChannel{
		{DeviceID: adapterRootDevice, ChannelID: adapterRootChannel, Name: "root-channel", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: rootID},
		{DeviceID: adapterDeviceID, ChannelID: adapterChannelID, Name: "child-channel", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 11, PTZType: 1},
	} {
		require.NoError(t, db.Create(&channel).Error)
	}
	return resource.New(db)
}

func TestDeviceChannelAdaptersPreserveTrustedScopeAndStableIdentifiers(t *testing.T) {
	var gotScope resource.DepartmentScope
	var gotOptions resource.DeviceListOptions
	reader := &resourceReaderStub{
		listDevices: func(_ context.Context, scope resource.DepartmentScope, options resource.DeviceListOptions) (resource.DevicePage, error) {
			gotScope = scope
			gotOptions = options
			return resource.DevicePage{
				Items: []resource.Device{{
					DeviceID: adapterDeviceID,
					Name:     "child-device",
					Status:   "online",
				}},
				Page: 2, PageSize: 25, Total: 1,
			}, nil
		},
	}
	adapter := resourceAdapterFor(t, reader, DeviceListAdapterKey)

	data, err := adapter.Dispatch(context.Background(), Request{
		ResourceScope: resource.DepartmentScope{OwnerDeptID: adapterOwnerDeptID, DataScope: resource.DataScopeDepartmentAndChildren},
		List:          ListOptions{Page: 2, PageSize: 25, Keyword: "child", Status: "online"},
	})
	require.NoError(t, err)
	page, ok := data.(resource.DevicePage)
	require.True(t, ok)
	require.Equal(t, resource.DepartmentScope{OwnerDeptID: adapterOwnerDeptID, DataScope: resource.DataScopeDepartmentAndChildren}, gotScope)
	require.Equal(t, resource.DeviceListOptions{Page: 2, PageSize: 25, Keyword: "child", Status: "online"}, gotOptions)
	require.Equal(t, adapterDeviceID, page.Items[0].DeviceID)

	body, err := json.Marshal(page)
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[{"deviceId":"34020000002000000011","name":"child-device","alias":"","manufacturer":"","model":"","status":"online"}],"page":2,"pageSize":25,"total":1}`, string(body))

	assert.Equal(t, DeviceListAdapterKey, adapter.Key())
	assert.Equal(t, http.StatusOK, HTTPStatus(nil))
}

func TestDeviceChannelAdaptersPassOnlyStableChannelTarget(t *testing.T) {
	var gotScope resource.DepartmentScope
	var gotDeviceID, gotChannelID string
	reader := &resourceReaderStub{
		getChannel: func(_ context.Context, scope resource.DepartmentScope, deviceID, channelID string) (resource.Channel, error) {
			gotScope, gotDeviceID, gotChannelID = scope, deviceID, channelID
			return resource.Channel{DeviceID: deviceID, ChannelID: channelID, Name: "camera", Status: "online", PTZType: 1}, nil
		},
	}
	adapter := resourceAdapterFor(t, reader, ChannelDetailAdapterKey)

	data, err := adapter.Dispatch(context.Background(), Request{
		ResourceScope: resource.DepartmentScope{OwnerDeptID: adapterOwnerDeptID, DataScope: resource.DataScopeDepartment},
		DeviceID:      adapterDeviceID,
		ChannelID:     adapterChannelID,
	})
	require.NoError(t, err)
	channel, ok := data.(resource.Channel)
	require.True(t, ok)
	assert.Equal(t, resource.DepartmentScope{OwnerDeptID: adapterOwnerDeptID, DataScope: resource.DataScopeDepartment}, gotScope)
	assert.Equal(t, adapterDeviceID, gotDeviceID)
	assert.Equal(t, adapterChannelID, gotChannelID)
	assert.Equal(t, resource.Channel{DeviceID: adapterDeviceID, ChannelID: adapterChannelID, Name: "camera", Status: "online", PTZType: 1}, channel)
}

func TestDeviceChannelAdaptersNormalizeReadFailuresForUnifiedDispatch(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		serviceErr error
		wantErr    error
		wantCode   string
		wantStatus int
	}{
		{name: "resource not found", serviceErr: resource.ErrResourceNotFound, wantErr: ErrNotFound, wantCode: "RESOURCE_NOT_FOUND", wantStatus: http.StatusNotFound},
		{name: "invalid list", serviceErr: resource.ErrInvalidListOptions, wantErr: ErrInvalidRequest, wantCode: "INVALID_REQUEST", wantStatus: http.StatusBadRequest},
		{name: "database unavailable", serviceErr: resource.ErrResourceUnavailable, wantErr: ErrUnavailable, wantCode: "SERVICE_UNAVAILABLE", wantStatus: http.StatusServiceUnavailable},
		{name: "unknown dependency", serviceErr: errors.New("database driver failed"), wantErr: ErrUnavailable, wantCode: "SERVICE_UNAVAILABLE", wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			reader := &resourceReaderStub{
				listDevices: func(context.Context, resource.DepartmentScope, resource.DeviceListOptions) (resource.DevicePage, error) {
					return resource.DevicePage{}, testCase.serviceErr
				},
			}
			adapter := resourceAdapterFor(t, reader, DeviceListAdapterKey)

			_, err := adapter.Dispatch(context.Background(), Request{
				ResourceScope: resource.DepartmentScope{OwnerDeptID: adapterOwnerDeptID, DataScope: resource.DataScopeDepartment},
			})
			require.ErrorIs(t, err, testCase.wantErr)
			assert.Equal(t, testCase.wantCode, ErrorCode(err))
			assert.Equal(t, testCase.wantStatus, HTTPStatus(err))
		})
	}
}

func TestDeviceChannelAdaptersRejectInvalidTrustedScopeBeforeReader(t *testing.T) {
	called := false
	reader := &resourceReaderStub{
		listDevices: func(context.Context, resource.DepartmentScope, resource.DeviceListOptions) (resource.DevicePage, error) {
			called = true
			return resource.DevicePage{}, nil
		},
	}
	adapter := resourceAdapterFor(t, reader, DeviceListAdapterKey)

	_, err := adapter.Dispatch(context.Background(), Request{
		ResourceScope: resource.DepartmentScope{OwnerDeptID: adapterOwnerDeptID, DataScope: 2},
	})
	require.ErrorIs(t, err, ErrInvalidRequest)
	assert.False(t, called)

	_, err = adapter.Dispatch(context.Background(), Request{ResourceScope: resource.DepartmentScope{DataScope: resource.DataScopeDepartment}})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, http.StatusNotFound, HTTPStatus(err))
}

func TestDeviceChannelAdaptersCloseTheActualScopedResourceReadLoop(t *testing.T) {
	service := newDeviceChannelAdapterService(t)
	listAdapter := resourceAdapterFor(t, service, DeviceListAdapterKey)
	access := resource.DepartmentScope{OwnerDeptID: adapterOwnerDeptID, DataScope: resource.DataScopeDepartmentAndChildren}

	data, err := listAdapter.Execute(context.Background(), catalogruntime.Invocation{
		Scope: DeviceListScope, Method: "GET", Params: map[string]string{"deviceId": adapterDeviceID}, Value: Request{ResourceScope: access},
	})
	require.NoError(t, err)
	page, ok := data.(resource.DevicePage)
	require.True(t, ok)
	require.Equal(t, []resource.Device{
		{DeviceID: adapterRootDevice, Name: "root-device", Status: "online"},
		{DeviceID: adapterDeviceID, Name: "child-device", Status: "online"},
	}, page.Items)

	channelAdapter := resourceAdapterFor(t, service, ChannelDetailAdapterKey)
	data, err = channelAdapter.Execute(context.Background(), catalogruntime.Invocation{
		Scope: ChannelDetailScope, Method: "GET", Params: map[string]string{"deviceId": adapterDeviceID, "channelId": adapterChannelID}, Value: Request{ResourceScope: access, DeviceID: adapterDeviceID, ChannelID: adapterChannelID},
	})
	require.NoError(t, err)
	channel, ok := data.(resource.Channel)
	require.True(t, ok)
	require.Equal(t, resource.Channel{DeviceID: adapterDeviceID, ChannelID: adapterChannelID, Name: "child-channel", Status: "online", PTZType: 1}, channel)
	channelJSON, err := json.Marshal(channel)
	require.NoError(t, err)
	assert.JSONEq(t, `{"deviceId":"34020000002000000011","channelId":"37011200001310000011","name":"child-channel","alias":"","manufacturer":"","model":"","status":"online","ptzType":1}`, string(channelJSON))

	_, err = resourceAdapterFor(t, service, DeviceDetailAdapterKey).Dispatch(context.Background(), Request{ResourceScope: access, DeviceID: adapterOtherDevice})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, http.StatusNotFound, HTTPStatus(err))
}

func TestDeviceChannelAdaptersRegisterWithUnifiedCatalogDispatch(t *testing.T) {
	reader := &resourceReaderStub{
		getDevice: func(_ context.Context, scope resource.DepartmentScope, deviceID string) (resource.Device, error) {
			return resource.Device{DeviceID: deviceID, Status: "online"}, nil
		},
	}
	registry := catalogruntime.NewAdapterRegistry()
	for _, registration := range NewDeviceChannelAdapterRegistrations(reader) {
		require.NoError(t, registry.Register(registration))
	}
	catalog := catalogruntime.NewCatalogRuntime(registry, nil)
	_, report, err := catalog.Publish(context.Background(), catalogruntime.Draft{Operations: []catalogruntime.Operation{{
		Scope: "device:detail", Method: "GET", ExternalPath: "/openapi/v1/devices/{deviceId}",
		AdapterKey: DeviceDetailAdapterKey, ContractVersion: ResourceAdapterContractVersion,
	}}})
	require.NoError(t, err)
	require.True(t, report.Valid())

	data, err := catalog.Dispatch(context.Background(), "device:detail", catalogruntime.Invocation{
		Method: "GET",
		Value:  Request{ResourceScope: resource.DepartmentScope{OwnerDeptID: adapterOwnerDeptID, DataScope: resource.DataScopeDepartment}, DeviceID: adapterDeviceID},
	})
	require.NoError(t, err)
	assert.Equal(t, resource.Device{DeviceID: adapterDeviceID, Status: "online"}, data)
}

func TestDeviceChannelAdaptersRejectUntypedRuntimeInvocation(t *testing.T) {
	adapter := resourceAdapterFor(t, &resourceReaderStub{}, DeviceListAdapterKey)
	_, err := adapter.Execute(context.Background(), catalogruntime.Invocation{Scope: "device:list", Method: "GET"})
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = adapter.Execute(context.Background(), catalogruntime.Invocation{Scope: "device:list", Method: "POST", Value: Request{}})
	require.ErrorIs(t, err, ErrInvalidRequest)
}

func TestDeviceChannelAdaptersMapRuntimePanicsToUnavailable(t *testing.T) {
	reader := &resourceReaderStub{
		listDevices: func(context.Context, resource.DepartmentScope, resource.DeviceListOptions) (resource.DevicePage, error) {
			panic("resource dependency crashed")
		},
	}
	adapter := resourceAdapterFor(t, reader, DeviceListAdapterKey)
	_, err := adapter.Execute(context.Background(), catalogruntime.Invocation{
		Scope:  DeviceListScope,
		Method: "get",
		Value:  Request{ResourceScope: resource.DepartmentScope{OwnerDeptID: adapterOwnerDeptID, DataScope: resource.DataScopeDepartment}},
	})
	require.ErrorIs(t, err, ErrUnavailable)
	require.Equal(t, http.StatusServiceUnavailable, HTTPStatus(err))
}
