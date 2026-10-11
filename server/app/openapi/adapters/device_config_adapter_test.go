package adapters

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/deviceconfig"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	deviceConfigAdapterTestDevice  = "34020000002000000010"
	deviceConfigAdapterTestChannel = "37011200001310000010"
)

type deviceConfigServiceStub struct {
	refreshRequest        deviceconfig.Request
	refreshTypes          []string
	applyRequest          deviceconfig.Request
	applyBlocks           manscdp.DeviceConfigBlocks
	videoRefreshRequest   deviceconfig.Request
	videoApplyRequest     deviceconfig.Request
	videoItems            []manscdp.VideoParamItem
	configSnapshotRequest deviceconfig.Request
	videoSnapshotRequest  deviceconfig.Request
	operationRequest      deviceconfig.Request
	operationID           string
	operationAction       string
	refreshCalls          int
	applyCalls            int
	videoRefreshCalls     int
	videoApplyCalls       int
	configSnapshotCalls   int
	videoSnapshotCalls    int
	operationCalls        int
}

func (stub *deviceConfigServiceStub) Refresh(_ context.Context, request deviceconfig.Request, types []string) (deviceconfig.OperationResult, error) {
	stub.refreshCalls++
	stub.refreshRequest, stub.refreshTypes = request, append([]string(nil), types...)
	return deviceconfig.OperationResult{OperationID: "refresh-op"}, nil
}

func (stub *deviceConfigServiceStub) Apply(_ context.Context, request deviceconfig.Request, blocks manscdp.DeviceConfigBlocks) (deviceconfig.OperationResult, error) {
	stub.applyCalls++
	stub.applyRequest, stub.applyBlocks = request, blocks
	return deviceconfig.OperationResult{OperationID: "apply-op"}, nil
}

func (stub *deviceConfigServiceStub) RefreshVideoParams(_ context.Context, request deviceconfig.Request) (deviceconfig.OperationResult, error) {
	stub.videoRefreshCalls++
	stub.videoRefreshRequest = request
	return deviceconfig.OperationResult{OperationID: "video-refresh-op"}, nil
}

func (stub *deviceConfigServiceStub) ApplyVideoParams(_ context.Context, request deviceconfig.Request, items []manscdp.VideoParamItem) (deviceconfig.OperationResult, error) {
	stub.videoApplyCalls++
	stub.videoApplyRequest, stub.videoItems = request, append([]manscdp.VideoParamItem(nil), items...)
	return deviceconfig.OperationResult{OperationID: "video-apply-op"}, nil
}

func (stub *deviceConfigServiceStub) GetConfigSnapshot(_ context.Context, request deviceconfig.Request) (deviceconfig.ConfigSnapshot, error) {
	stub.configSnapshotCalls++
	stub.configSnapshotRequest = request
	return deviceconfig.ConfigSnapshot{DeviceID: request.DeviceID, ChannelID: request.ChannelID}, nil
}

func (stub *deviceConfigServiceStub) GetVideoParamSnapshot(_ context.Context, request deviceconfig.Request) (deviceconfig.VideoParamSnapshot, error) {
	stub.videoSnapshotCalls++
	stub.videoSnapshotRequest = request
	return deviceconfig.VideoParamSnapshot{DeviceID: request.DeviceID, ChannelID: request.ChannelID}, nil
}

func (stub *deviceConfigServiceStub) GetOperation(_ context.Context, request deviceconfig.Request, operationID string) (deviceconfig.OperationResult, error) {
	stub.operationCalls++
	stub.operationRequest, stub.operationID = request, operationID
	return deviceconfig.OperationResult{OperationID: operationID, Action: stub.operationAction}, nil
}

func deviceConfigInvocation(scope, method string, body string, params map[string]string, query map[string]string) runtime.Invocation {
	return runtime.Invocation{
		Scope: scope, Method: method, Params: params, Query: query, Body: []byte(body),
		Value: Request{
			ClientID: 91, IdempotencyKey: "config-change-once",
			ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment},
			DeviceID:      deviceConfigAdapterTestDevice, ChannelID: deviceConfigAdapterTestChannel,
		},
	}
}

func TestDeviceConfigAdaptersRouteRefreshReadAndOperationWithVerifiedIdentity(t *testing.T) {
	service := &deviceConfigServiceStub{operationAction: "refresh_device_configs"}
	registrations := NewDeviceConfigAdapterRegistrations(service)
	require.Len(t, registrations, 7)
	byKey := make(map[string]runtime.Adapter, len(registrations))
	for _, registration := range registrations {
		byKey[registration.Key] = registration.Adapter
		require.Equal(t, ResourceAdapterContractVersion, registration.ContractVersion)
	}
	params := map[string]string{"deviceId": deviceConfigAdapterTestDevice, "channelId": deviceConfigAdapterTestChannel}
	request := deviceConfigInvocation(DeviceConfigRefreshScope, "POST", `{"configTypes":["FrameMirror"]}`, params, nil)
	data, err := byKey[DeviceConfigRefreshAdapterKey].Execute(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "refresh-op", data.(deviceconfig.OperationResult).OperationID)
	require.Equal(t, int64(91), service.refreshRequest.ClientID)
	require.Equal(t, resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, service.refreshRequest.Scope)
	require.Equal(t, deviceConfigAdapterTestDevice, service.refreshRequest.DeviceID)
	require.Equal(t, deviceConfigAdapterTestChannel, service.refreshRequest.ChannelID)
	require.Equal(t, "config-change-once", service.refreshRequest.IdempotencyKey)
	require.Equal(t, []string{"FrameMirror"}, service.refreshTypes)

	request = deviceConfigInvocation(DeviceConfigReadScope, "GET", "", params, nil)
	_, err = byKey[DeviceConfigReadAdapterKey].Execute(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, 1, service.configSnapshotCalls)

	params["operationId"] = "refresh-op"
	request = deviceConfigInvocation(DeviceConfigOperationReadScope, "GET", "", params, nil)
	_, err = byKey[DeviceConfigOperationReadAdapterKey].Execute(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "refresh-op", service.operationID)
	require.Equal(t, int64(91), service.operationRequest.ClientID)
}

func TestDeviceConfigOperationReadHidesOperationsFromOtherActionFamilies(t *testing.T) {
	service := &deviceConfigServiceStub{operationAction: "ptz_move"}
	registrations := NewDeviceConfigAdapterRegistrations(service)
	var adapter runtime.Adapter
	for _, registration := range registrations {
		if registration.Key == DeviceConfigOperationReadAdapterKey {
			adapter = registration.Adapter
		}
	}
	_, err := adapter.Execute(context.Background(), deviceConfigInvocation(
		DeviceConfigOperationReadScope, "GET", "",
		map[string]string{"deviceId": deviceConfigAdapterTestDevice, "channelId": deviceConfigAdapterTestChannel, "operationId": "other-op"}, nil,
	))
	require.ErrorIs(t, err, ErrNotFound)
	require.Equal(t, 1, service.operationCalls)
}

func TestDeviceConfigAdaptersValidateWritesAndVideoParamOperations(t *testing.T) {
	service := &deviceConfigServiceStub{}
	registrations := NewDeviceConfigAdapterRegistrations(service)
	byKey := make(map[string]runtime.Adapter, len(registrations))
	for _, registration := range registrations {
		byKey[registration.Key] = registration.Adapter
	}
	params := map[string]string{"deviceId": deviceConfigAdapterTestDevice, "channelId": deviceConfigAdapterTestChannel}
	request := deviceConfigInvocation(DeviceConfigApplyScope, "PUT", `{"blocks":{"frameMirror":{"value":1}}}`, params, nil)
	_, err := byKey[DeviceConfigApplyAdapterKey].Execute(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, service.applyBlocks.FrameMirror)
	require.Equal(t, manscdp.FrameMirrorLeftRight, service.applyBlocks.FrameMirror.Value)
	require.Equal(t, "config-change-once", service.applyRequest.IdempotencyKey)

	request = deviceConfigInvocation(VideoParamsRefreshScope, "POST", `{}`, params, nil)
	_, err = byKey[VideoParamsRefreshAdapterKey].Execute(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, 1, service.videoRefreshCalls)

	request = deviceConfigInvocation(VideoParamsReadScope, "GET", "", params, nil)
	_, err = byKey[VideoParamsReadAdapterKey].Execute(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, 1, service.videoSnapshotCalls)

	request = deviceConfigInvocation(VideoParamsApplyScope, "PUT", `{"items":[{"streamNumber":0,"videoFormat":"2","resolution":"1920x1080","frameRate":"25","bitRateType":"1","videoBitRate":"2048"}]}`, params, nil)
	_, err = byKey[VideoParamsApplyAdapterKey].Execute(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, service.videoItems, 1)
	require.Equal(t, 0, service.videoItems[0].StreamNumber)
	require.Equal(t, "1920x1080", service.videoItems[0].Resolution)
	require.Equal(t, int64(91), service.videoApplyRequest.ClientID)
}

func TestDeviceConfigAdapterRejectsScopePathBodyAndDepartmentIdentityMismatch(t *testing.T) {
	service := &deviceConfigServiceStub{}
	registrations := NewDeviceConfigAdapterRegistrations(service)
	adapter := registrations[0].Adapter
	validParams := map[string]string{"deviceId": deviceConfigAdapterTestDevice, "channelId": deviceConfigAdapterTestChannel}
	validBody := `{"configTypes":["FrameMirror"]}`
	tests := []struct {
		name       string
		invocation runtime.Invocation
	}{
		{name: "wrong scope", invocation: deviceConfigInvocation("channel:video-params:apply", "POST", validBody, validParams, nil)},
		{name: "wrong method", invocation: deviceConfigInvocation(DeviceConfigRefreshScope, "GET", validBody, validParams, nil)},
		{name: "extra path parameter", invocation: deviceConfigInvocation(DeviceConfigRefreshScope, "POST", validBody, map[string]string{"deviceId": deviceConfigAdapterTestDevice, "channelId": deviceConfigAdapterTestChannel, "operationId": "other"}, nil)},
		{name: "untrusted path differs from metadata", invocation: func() runtime.Invocation {
			value := deviceConfigInvocation(DeviceConfigRefreshScope, "POST", validBody, validParams, nil)
			request := value.Value.(Request)
			request.ChannelID = "37011200001310000011"
			value.Value = request
			return value
		}()},
		{name: "invalid json", invocation: deviceConfigInvocation(DeviceConfigRefreshScope, "POST", `{`, validParams, nil)},
		{name: "non object body", invocation: deviceConfigInvocation(DeviceConfigRefreshScope, "POST", `null`, validParams, nil)},
		{name: "null config types", invocation: deviceConfigInvocation(DeviceConfigRefreshScope, "POST", `{"configTypes":null}`, validParams, nil)},
		{name: "unknown body property", invocation: deviceConfigInvocation(DeviceConfigRefreshScope, "POST", `{"configTypes":["FrameMirror"],"deviceId":"34020000002000000011"}`, validParams, nil)},
		{name: "unexpected query", invocation: deviceConfigInvocation(DeviceConfigRefreshScope, "POST", validBody, validParams, map[string]string{"refresh": "true"})},
		{name: "missing idempotency", invocation: func() runtime.Invocation {
			value := deviceConfigInvocation(DeviceConfigRefreshScope, "POST", validBody, validParams, nil)
			request := value.Value.(Request)
			request.IdempotencyKey = ""
			value.Value = request
			return value
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := adapter.Execute(context.Background(), test.invocation)
			require.ErrorIs(t, err, ErrInvalidRequest)
		})
	}
	require.Zero(t, service.refreshCalls, "malformed or mismatched input must be rejected before domain dispatch")
}

func TestDeviceConfigAdapterRejectsInvalidConfigAndVideoParameters(t *testing.T) {
	service := &deviceConfigServiceStub{}
	registrations := NewDeviceConfigAdapterRegistrations(service)
	byKey := make(map[string]runtime.Adapter, len(registrations))
	for _, registration := range registrations {
		byKey[registration.Key] = registration.Adapter
	}
	params := map[string]string{"deviceId": deviceConfigAdapterTestDevice, "channelId": deviceConfigAdapterTestChannel}
	badConfig := deviceConfigInvocation(DeviceConfigApplyScope, "PUT", `{"blocks":{"frameMirror":{"value":99}}}`, params, nil)
	_, err := byKey[DeviceConfigApplyAdapterKey].Execute(context.Background(), badConfig)
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Zero(t, service.applyCalls)

	badVideo := deviceConfigInvocation(VideoParamsApplyScope, "PUT", `{"items":[{"streamNumber":0,"videoFormat":"2","resolution":"1920*1080","frameRate":"25","bitRateType":"1","videoBitRate":"2048"}]}`, params, nil)
	_, err = byKey[VideoParamsApplyAdapterKey].Execute(context.Background(), badVideo)
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Zero(t, service.videoApplyCalls)
}

func TestDeviceConfigAdapterRejectsReadOnlyInternalAndCallerSelectedSnapshotFields(t *testing.T) {
	service := &deviceConfigServiceStub{}
	registrations := NewDeviceConfigAdapterRegistrations(service)
	adapter := registrations[2].Adapter
	params := map[string]string{"deviceId": deviceConfigAdapterTestDevice, "channelId": deviceConfigAdapterTestChannel}
	for _, body := range []string{
		`{"blocks":{"videoParamOpt":{"resolution":"1/2"}}}`,
		`{"blocks":{"basicParam":{"positionCapability":1}}}`,
		`{"blocks":{"videoRecordPlan":{"additionalStreams":1}}}`,
		`{"blocks":{"videoAlarmRecord":{"additionalStreams":1}}}`,
		`{"blocks":{"snapShot":{"snapNum":1,"uploadUrl":"https://caller.example/upload","sessionId":"01234567890123456789012345678901"}}}`,
	} {
		request := deviceConfigInvocation(DeviceConfigApplyScope, "PUT", body, params, nil)
		_, err := adapter.Execute(context.Background(), request)
		require.ErrorIs(t, err, ErrInvalidRequest, body)
	}
	require.Zero(t, service.applyCalls, "untrusted upload destinations and non-writable fields must not reach the domain service")
}
