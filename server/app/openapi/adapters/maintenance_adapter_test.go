package adapters

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/contracts"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	maintenanceTestDevice  = "34020000002000000010"
	maintenanceTestChannel = "34020000001320000010"
)

func TestMaintenanceRecordAdapterRequiresScopedCapabilityAndIdempotency(t *testing.T) {
	resolver := &fakeMaintenanceResolver{channel: testMaintenanceChannel(manscdp.ControlCapability{State: manscdp.CapabilitySupported})}
	service := &fakeMaintenanceService{}
	registry := maintenanceAdapterMap(t, resolver, service, &fakeMaintenanceOperationReader{})
	adapter := registry[DeviceRecordStartAdapterKey]
	require.NotNil(t, adapter)

	request := maintenanceInvocation(DeviceRecordStartScope, "record-key", []byte(`{}`))
	request.Value = Request{ClientID: 5, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}}
	_, err := adapter.Execute(context.Background(), request)
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Empty(t, service.commands)

	request = maintenanceInvocation(DeviceRecordStartScope, "record-key", []byte(`{}`))
	request.Value = Request{ClientID: 5, IdempotencyKey: "record-key", ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}}
	data, err := adapter.Execute(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, service.commands, 1)
	require.Equal(t, "record_start", service.commands[0].Action)
	require.Equal(t, protocol.Version2022, string(service.targets[0].Profile.Version))
	require.NotContains(t, service.commands[0].IdempotencyKey, "record-key")
	require.Contains(t, service.commands[0].IdempotencyKey, "openapi-client:5:")
	require.Equal(t, uint(10), service.commands[0].ActorDeptID)
	require.Equal(t, uint(0), service.commands[0].ActorID)
	require.Equal(t, map[string]any{"operationId": "op-1", "action": "record_start", "status": gbmodels.PTZOperationQueued, "sn": 12, "responseRequired": true, "deviceAcknowledged": false, "profileVersion": "2022"}, data)
}

func TestMaintenanceAdapterFailsClosedOnUnknownOrUnsupportedControl(t *testing.T) {
	tests := []struct {
		name       string
		profile    protocol.Profile
		capability manscdp.ControlCapability
	}{
		{name: "unknown capability", profile: protocol.ProfileFor(protocol.Version2016), capability: manscdp.ControlCapability{State: manscdp.CapabilityUnknown}},
		{name: "unsupported capability", profile: protocol.ProfileFor(protocol.Version2016), capability: manscdp.ControlCapability{State: manscdp.CapabilityUnsupported}},
		{name: "unknown version", profile: protocol.Profile{Version: "auto"}, capability: manscdp.ControlCapability{State: manscdp.CapabilitySupported}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := testMaintenanceChannel(test.capability)
			channel.Target.Profile = test.profile
			service := &fakeMaintenanceService{}
			adapter := maintenanceAdapterMap(t, &fakeMaintenanceResolver{channel: channel}, service, &fakeMaintenanceOperationReader{})[DeviceRecordStartAdapterKey]
			_, err := adapter.Execute(context.Background(), maintenanceInvocation(DeviceRecordStartScope, "key", []byte(`{}`)))
			require.ErrorIs(t, err, ErrCapabilityNotSupported)
			require.Empty(t, service.commands)
		})
	}
}

func TestMaintenanceDestructiveOperationsRequireConfirmationAndExplicitCardIndex(t *testing.T) {
	resolver := &fakeMaintenanceResolver{channel: testMaintenanceChannel(manscdp.ControlCapability{State: manscdp.CapabilitySupported})}
	service := &fakeMaintenanceService{}
	registry := maintenanceAdapterMap(t, resolver, service, &fakeMaintenanceOperationReader{})
	request := maintenanceInvocation(StorageCardFormatScope, "format-key", []byte(`{"confirmed":true}`))
	_, err := registry[StorageCardFormatAdapterKey].Execute(context.Background(), request)
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Empty(t, service.formattedCards)

	request = maintenanceInvocation(StorageCardFormatScope, "format-key", []byte(`{"confirmed":true,"cardIndex":0}`))
	_, err = registry[StorageCardFormatAdapterKey].Execute(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, []int{0}, service.formattedCards)
	require.Len(t, service.formatKeys, 1)

	request = maintenanceInvocation(DeviceRebootScope, "reboot-key", []byte(`{"confirmed":false}`))
	_, err = registry[DeviceRebootAdapterKey].Execute(context.Background(), request)
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Zero(t, service.rebootCalls)

	device := testMaintenanceDevice()
	device.TeleBoot = manscdp.ControlCapability{State: manscdp.CapabilitySupported}
	resolver.device = device
	request = maintenanceInvocation(DeviceRebootScope, "reboot-key", []byte(`{"confirmed":true}`))
	delete(request.Params, "channelId")
	_, err = registry[DeviceRebootAdapterKey].Execute(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, 1, service.rebootCalls)
}

func TestMaintenanceAdapterRejectsAmbiguousOrOutOfContractRequests(t *testing.T) {
	device := testMaintenanceDevice()
	device.TeleBoot = manscdp.ControlCapability{State: manscdp.CapabilitySupported}
	service := &fakeMaintenanceService{}
	adapter := maintenanceAdapterMap(t, &fakeMaintenanceResolver{device: device}, service, &fakeMaintenanceOperationReader{})[DeviceRebootAdapterKey]

	tests := []struct {
		name   string
		mutate func(*catalogruntime.Invocation)
		body   string
	}{
		{name: "missing scope", mutate: func(request *catalogruntime.Invocation) { request.Scope = "" }, body: `{"confirmed":true}`},
		{name: "case-mismatched method", mutate: func(request *catalogruntime.Invocation) { request.Method = "post" }, body: `{"confirmed":true}`},
		{name: "duplicate confirmation member", body: `{"confirmed":true,"confirmed":false}`},
		{name: "unknown field", body: `{"confirmed":true,"cardIndex":0}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := maintenanceInvocation(DeviceRebootScope, "key", []byte(test.body))
			delete(request.Params, "channelId")
			if test.mutate != nil {
				test.mutate(&request)
			}
			_, err := adapter.Execute(context.Background(), request)
			require.ErrorIs(t, err, ErrInvalidRequest)
			require.Zero(t, service.rebootCalls)
		})
	}
}

func TestStorageCardFormatRequiresGB2022AndUnambiguousCapability(t *testing.T) {
	channel := testMaintenanceChannel(manscdp.ControlCapability{State: manscdp.CapabilitySupported})
	channel.Target.Profile = protocol.ProfileFor(protocol.Version2016)
	service := &fakeMaintenanceService{}
	adapter := maintenanceAdapterMap(t, &fakeMaintenanceResolver{channel: channel}, service, &fakeMaintenanceOperationReader{})[StorageCardFormatAdapterKey]
	_, err := adapter.Execute(context.Background(), maintenanceInvocation(StorageCardFormatScope, "format-key", []byte(`{"confirmed":true,"cardIndex":0}`)))
	require.ErrorIs(t, err, ErrCapabilityNotSupported)
	require.Empty(t, service.formattedCards)

	tests := []struct {
		name string
		raw  string
		want manscdp.CapabilityState
	}{
		{name: "conflicting aliases", raw: `{"format_sd":true,"storage_card_format":false}`, want: manscdp.CapabilityUnknown},
		{name: "non boolean value", raw: `{"format_sd":"true"}`, want: manscdp.CapabilityUnknown},
		{name: "explicit unsupported", raw: `{"storage_card_format":false}`, want: manscdp.CapabilityUnsupported},
		{name: "explicit supported", raw: `{"storage_card_format":true}`, want: manscdp.CapabilitySupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capability := parseStorageCardFormatCapability(&test.raw)
			require.Equal(t, test.want, capability.State)
		})
	}
}

func TestMaintenanceRebootUsesDeviceLevelTargetAndCapability(t *testing.T) {
	device := testMaintenanceDevice()
	device.TeleBoot = manscdp.ControlCapability{State: manscdp.CapabilitySupported}
	resolver := &fakeMaintenanceResolver{device: device, channel: testMaintenanceChannel(manscdp.ControlCapability{State: manscdp.CapabilitySupported})}
	service := &fakeMaintenanceService{}
	adapter := maintenanceAdapterMap(t, resolver, service, &fakeMaintenanceOperationReader{})[DeviceRebootAdapterKey]
	request := maintenanceInvocation(DeviceRebootScope, "device-reboot", []byte(`{"confirmed":true}`))
	delete(request.Params, "channelId")

	data, err := adapter.Execute(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, 1, resolver.deviceResolves)
	require.Zero(t, resolver.channelResolves)
	require.Equal(t, 1, service.rebootCalls)
	require.Equal(t, "op-reboot", data.(map[string]any)["operationId"])
}

func TestMaintenanceRebootRejectsUnknownDeviceCapabilityBeforeSending(t *testing.T) {
	device := testMaintenanceDevice()
	device.TeleBoot = manscdp.ControlCapability{State: manscdp.CapabilityUnknown}
	resolver := &fakeMaintenanceResolver{device: device}
	service := &fakeMaintenanceService{}
	adapter := maintenanceAdapterMap(t, resolver, service, &fakeMaintenanceOperationReader{})[DeviceRebootAdapterKey]
	request := maintenanceInvocation(DeviceRebootScope, "device-reboot", []byte(`{"confirmed":true}`))
	delete(request.Params, "channelId")

	_, err := adapter.Execute(context.Background(), request)
	require.ErrorIs(t, err, ErrCapabilityNotSupported)
	require.Zero(t, service.rebootCalls)
}

func TestMaintenanceOperationReadIsBoundToClientAndDevice(t *testing.T) {
	reader := &fakeMaintenanceOperationReader{operation: gbmodels.GbPTZOperation{
		OperationID: "op-1", DeviceCode: maintenanceTestDevice, Action: "teleboot", Status: gbmodels.PTZOperationSent,
		PayloadJSON: `{"secret":"hidden"}`, IdempotencyKey: "openapi-client:5:hash",
	}}
	registry := maintenanceAdapterMap(t, &fakeMaintenanceResolver{device: testMaintenanceDevice()}, &fakeMaintenanceService{}, reader)
	request := maintenanceInvocation(MaintenanceOperationReadScope, "", nil)
	request.Method = "GET"
	request.Params["operationId"] = "op-1"
	request.Value = Request{ClientID: 5, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: maintenanceTestDevice}
	data, err := registry[MaintenanceOperationReadAdapterKey].Execute(context.Background(), request)
	require.NoError(t, err)
	require.EqualValues(t, 5, reader.clientID)
	require.Equal(t, "op-1", data.(map[string]any)["operationId"])
	require.NotContains(t, data.(map[string]any), "payloadJSON")

	request.Value = Request{ClientID: 6, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: maintenanceTestDevice}
	_, err = registry[MaintenanceOperationReadAdapterKey].Execute(context.Background(), request)
	require.ErrorIs(t, err, ErrNotFound)

	request.Value = Request{ClientID: 5, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: maintenanceTestDevice}
	request.Params["operationId"] = "../op-1"
	_, err = registry[MaintenanceOperationReadAdapterKey].Execute(context.Background(), request)
	require.ErrorIs(t, err, ErrInvalidRequest)
}

func TestMaintenanceErrorsHaveStablePublicMappings(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		code       string
		httpStatus int
	}{
		{name: "unsupported capability", err: ErrCapabilityNotSupported, code: "CAPABILITY_NOT_SUPPORTED", httpStatus: http.StatusConflict},
		{name: "device offline", err: ErrDeviceOffline, code: "DEVICE_OFFLINE", httpStatus: http.StatusConflict},
		{name: "idempotency conflict", err: ErrConflict, code: "CONFLICT", httpStatus: http.StatusConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.code, ErrorCode(test.err))
			require.Equal(t, test.httpStatus, HTTPStatus(test.err))
		})
	}
}

func TestGormMaintenanceResolverEnforcesDepartmentBoundary(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&appmodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{}))

	active := int8(1)
	rootID := uint(10)
	otherID := uint(20)
	childID := uint(11)
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: rootID}, Name: "root", Status: &active}).Error)
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: childID}, ParentID: &rootID, Name: "child", Status: &active}).Error)
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: otherID}, Name: "other", Status: &active}).Error)

	rootDevice := maintenanceTestDevice
	childDevice := "34020000002000000011"
	otherDevice := "34020000002000000012"
	for _, device := range []gbmodels.GbDevice{
		{DeviceID: rootDevice, OwnerDeptID: rootID, Status: gbmodels.DeviceStatusOnline, EffectiveVersion: string(protocol.Version2022)},
		{DeviceID: childDevice, OwnerDeptID: childID, Status: gbmodels.DeviceStatusOnline, EffectiveVersion: string(protocol.Version2022)},
		{DeviceID: otherDevice, OwnerDeptID: otherID, Status: gbmodels.DeviceStatusOnline, EffectiveVersion: string(protocol.Version2022)},
	} {
		require.NoError(t, db.Create(&device).Error)
	}
	rootChannel := maintenanceTestChannel
	childChannel := "34020000001320000011"
	foreignChannel := "34020000001320000012"
	teleBootSupported := `{"teleboot":true}`
	for _, channel := range []gbmodels.GbChannel{
		{DeviceID: rootDevice, ChannelID: rootChannel, OwnerDeptID: rootID, Status: gbmodels.ChannelStatusOnline, Capabilities: &teleBootSupported},
		{DeviceID: childDevice, ChannelID: childChannel, OwnerDeptID: childID, Status: gbmodels.ChannelStatusOnline},
		// A malformed cross-department association must not widen access.
		{DeviceID: rootDevice, ChannelID: foreignChannel, OwnerDeptID: otherID, Status: gbmodels.ChannelStatusOnline},
	} {
		require.NoError(t, db.Create(&channel).Error)
	}

	resolver := NewGormMaintenanceResolver(db)
	rootScope := resource.DepartmentScope{OwnerDeptID: rootID, DataScope: resource.DataScopeDepartment}
	childrenScope := resource.DepartmentScope{OwnerDeptID: rootID, DataScope: resource.DataScopeDepartmentAndChildren}
	_, err = resolver.ResolveDevice(context.Background(), rootScope, childDevice)
	require.ErrorIs(t, err, ErrMaintenanceTargetNotFound)
	device, err := resolver.ResolveDevice(context.Background(), rootScope, rootDevice)
	require.NoError(t, err)
	require.Equal(t, manscdp.CapabilitySupported, device.TeleBoot.State)
	_, err = resolver.ResolveDevice(context.Background(), childrenScope, childDevice)
	require.NoError(t, err)
	_, err = resolver.ResolveChannel(context.Background(), rootScope, rootDevice, foreignChannel)
	require.ErrorIs(t, err, ErrMaintenanceTargetNotFound)
	_, err = resolver.ResolveChannel(context.Background(), childrenScope, otherDevice, foreignChannel)
	require.ErrorIs(t, err, ErrMaintenanceTargetNotFound)
	channel, err := resolver.ResolveChannel(context.Background(), childrenScope, childDevice, childChannel)
	require.NoError(t, err)
	require.Equal(t, protocol.Version2022, string(channel.Target.Profile.Version))
}

func TestGormMaintenanceOperationReaderRecognizesNamespacedDeviceRebootKeys(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&appmodels.SysDepartment{}, &gbmodels.GbPTZOperation{}))
	active := int8(1)
	rootID, otherID := uint(10), uint(20)
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: rootID}, Name: "owner", Status: &active}).Error)
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: otherID}, Name: "other", Status: &active}).Error)
	keyPrefix := runtimeptz.DeviceRebootClientOperationPrefix(5)
	op := gbmodels.GbPTZOperation{
		OperationID: "reboot-op", IdempotencyKey: keyPrefix + strings.Repeat("a", 64),
		DeviceID: 1, DeviceCode: maintenanceTestDevice, Action: "teleboot", TargetScope: gbmodels.ControlTargetScopeDevice,
		Status: gbmodels.PTZOperationSent, ActorDeptID: rootID,
	}
	require.NoError(t, db.Create(&op).Error)
	reader := NewGormMaintenanceOperationReader(db)
	scope := resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}

	_, err = reader.GetOperationForClient(context.Background(), scope, maintenanceTestDevice, "reboot-op", 6)
	require.ErrorIs(t, err, ErrMaintenanceTargetNotFound)
	got, err := reader.GetOperationForClient(context.Background(), scope, maintenanceTestDevice, "reboot-op", 5)
	require.NoError(t, err)
	require.Equal(t, op.OperationID, got.OperationID)

	otherScope := resource.DepartmentScope{OwnerDeptID: otherID, DataScope: resource.DataScopeDepartment}
	_, err = reader.GetOperationForClient(context.Background(), otherScope, maintenanceTestDevice, "reboot-op", 5)
	require.ErrorIs(t, err, ErrMaintenanceTargetNotFound)
}

func TestNormalizeMaintenanceServiceErrorsPreservesDeviceOutcome(t *testing.T) {
	tests := []struct {
		name string
		code runtimeptz.ErrorCode
		want error
	}{
		{name: "offline", code: runtimeptz.ErrorCodeHomePositionDeviceOffline, want: ErrDeviceOffline},
		{name: "idempotency conflict", code: runtimeptz.ErrorCodeHomePositionIdempotencyConflict, want: ErrConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := &runtimeptz.OperationError{Code: test.code}
			require.True(t, errors.Is(normalizeMaintenanceServiceError(err), test.want))
		})
	}
}

func maintenanceInvocation(scope, key string, body []byte) catalogruntime.Invocation {
	return catalogruntime.Invocation{
		Scope: scope, Method: "POST", Params: map[string]string{"deviceId": maintenanceTestDevice, "channelId": maintenanceTestChannel},
		Body:  body,
		Value: Request{ClientID: 5, IdempotencyKey: key, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: maintenanceTestDevice, ChannelID: maintenanceTestChannel},
	}
}

func maintenanceAdapterMap(t *testing.T, resolver MaintenanceTargetResolver, service MaintenanceExecutor, reader MaintenanceOperationReader) map[string]catalogruntime.Adapter {
	t.Helper()
	registrations := NewMaintenanceAdapterRegistrations(resolver, service, reader)
	result := make(map[string]catalogruntime.Adapter, len(registrations))
	for _, registration := range registrations {
		result[registration.Key] = registration.Adapter
	}
	return result
}

func TestMaintenanceRegistrationsMatchPublishedScopes(t *testing.T) {
	expected := map[string]string{
		DeviceRebootAdapterKey:             DeviceRebootScope,
		StorageCardFormatAdapterKey:        StorageCardFormatScope,
		MaintenanceOperationReadAdapterKey: MaintenanceOperationReadScope,
	}
	for _, operation := range contracts.Operations() {
		if scope, ok := expected[operation.AdapterKey]; ok {
			require.Equal(t, scope, operation.Scope, operation.Code)
			delete(expected, operation.AdapterKey)
		}
	}
	require.Empty(t, expected, "each maintenance adapter must remain represented by the code-owned public contract")

	registrations := NewMaintenanceAdapterRegistrations(nil, nil, nil)
	registered := make(map[string]struct{}, len(registrations))
	for _, registration := range registrations {
		registered[registration.Key] = struct{}{}
	}
	for key := range map[string]struct{}{
		DeviceRebootAdapterKey:             {},
		StorageCardFormatAdapterKey:        {},
		MaintenanceOperationReadAdapterKey: {},
	} {
		_, ok := registered[key]
		require.Truef(t, ok, "maintenance adapter %q is not registered", key)
	}
}

func testMaintenanceDevice() MaintenanceDevice {
	return MaintenanceDevice{Target: runtimeptz.DeviceRebootTarget{
		DeviceID: 1, DeviceCode: maintenanceTestDevice, IP: "192.0.2.10", Port: 5060,
		Transport: "UDP", DeviceOnline: true, Profile: protocol.ProfileFor(protocol.Version2016),
	}}
}

func testMaintenanceChannel(capability manscdp.ControlCapability) MaintenanceChannel {
	target := runtimeptz.Target{
		DeviceID: 1, DeviceCode: maintenanceTestDevice, ChannelID: 2, ChannelCode: maintenanceTestChannel,
		IP: "192.0.2.10", Port: 5060, Transport: "UDP", DeviceOnline: true, ChannelOnline: true,
		Profile: protocol.ProfileFor(protocol.Version2022),
	}
	return MaintenanceChannel{Target: target, Capabilities: manscdp.ControlCapabilities{
		Record: capability, Guard: capability, AlarmReset: capability, TeleBoot: capability,
	}, StorageCardFormat: capability, AlarmTargetCode: maintenanceTestChannel}
}

type fakeMaintenanceResolver struct {
	device          MaintenanceDevice
	channel         MaintenanceChannel
	deviceResolves  int
	channelResolves int
}

func (resolver *fakeMaintenanceResolver) ResolveDevice(context.Context, resource.DepartmentScope, string) (MaintenanceDevice, error) {
	resolver.deviceResolves++
	return resolver.device, nil
}

func (resolver *fakeMaintenanceResolver) ResolveChannel(context.Context, resource.DepartmentScope, string, string) (MaintenanceChannel, error) {
	resolver.channelResolves++
	return resolver.channel, nil
}

func (resolver *fakeMaintenanceResolver) ResolveAlarmTarget(context.Context, resource.DepartmentScope, MaintenanceChannel) (string, error) {
	return resolver.channel.AlarmTargetCode, nil
}

type fakeMaintenanceService struct {
	commands       []ptzCommandCapture
	targets        []runtimeptz.Target
	formattedCards []int
	formatKeys     []string
	rebootCalls    int
}

type ptzCommandCapture struct {
	runtimeptz.Command
}

func (service *fakeMaintenanceService) Execute(_ context.Context, target runtimeptz.Target, command runtimeptz.Command) (gbmodels.GbPTZOperation, error) {
	service.targets = append(service.targets, target)
	service.commands = append(service.commands, ptzCommandCapture{Command: command})
	return gbmodels.GbPTZOperation{OperationID: "op-1", Action: command.Action, Status: gbmodels.PTZOperationQueued, SN: 12, ResponseRequired: command.ResponseRequired, ProfileVersion: string(target.Profile.Version)}, nil
}

func (service *fakeMaintenanceService) ExecuteDeviceRebootDetailed(_ context.Context, _ runtimeptz.DeviceRebootTarget, _ string, _, _ uint) (gbmodels.GbPTZOperation, bool, error) {
	service.rebootCalls++
	return gbmodels.GbPTZOperation{OperationID: "op-reboot", Action: "teleboot", Status: gbmodels.PTZOperationSent, SN: 13, ProfileVersion: string(protocol.Version2016)}, false, nil
}

func (service *fakeMaintenanceService) FormatStorageCard(_ context.Context, _ runtimeptz.Target, cardIndex int, _, _ uint, key string) (gbmodels.GbPTZOperation, error) {
	service.formattedCards = append(service.formattedCards, cardIndex)
	service.formatKeys = append(service.formatKeys, key)
	return gbmodels.GbPTZOperation{OperationID: "op-format", Action: "format_sd", Status: gbmodels.PTZOperationSent, SN: 14, ProfileVersion: string(protocol.Version2022)}, nil
}

type fakeMaintenanceOperationReader struct {
	operation gbmodels.GbPTZOperation
	clientID  int64
}

func (reader *fakeMaintenanceOperationReader) GetOperationForClient(_ context.Context, _ resource.DepartmentScope, _ string, operationID string, clientID int64) (gbmodels.GbPTZOperation, error) {
	reader.clientID = clientID
	if operationID != reader.operation.OperationID || !strings.HasPrefix(reader.operation.IdempotencyKey, "openapi-client:5:") || clientID != 5 {
		return gbmodels.GbPTZOperation{}, ErrMaintenanceTargetNotFound
	}
	return reader.operation, nil
}
