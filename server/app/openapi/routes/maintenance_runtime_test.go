package routes

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

func TestMaintenanceRuntimeExposesFailClosedForwardersAndRebindsService(t *testing.T) {
	_, err := NewMaintenanceRuntime(nil)
	require.ErrorIs(t, err, adapters.ErrUnavailable)

	runtime, err := NewMaintenanceRuntime(newCatalogRuntimeTestDB(t))
	require.NoError(t, err)
	forwarders := runtime.Forwarders()
	require.Len(t, forwarders, 8)

	registry := catalogruntime.NewAdapterRegistry()
	for key, adapter := range forwarders {
		require.NoError(t, registry.Register(catalogruntime.AdapterRegistration{
			Key: key, ContractVersion: adapters.ResourceAdapterContractVersion, Adapter: adapter,
		}))
	}
	require.True(t, registry.Has(adapters.DeviceRebootAdapterKey, adapters.ResourceAdapterContractVersion))

	request := catalogruntime.Invocation{Scope: adapters.MaintenanceOperationReadScope}
	_, err = forwarders[adapters.MaintenanceOperationReadAdapterKey].Execute(context.Background(), request)
	require.ErrorIs(t, err, catalogruntime.ErrAdapterNotFound, "unbound generations must fail closed")

	require.ErrorIs(t, runtime.Replace(nil), adapters.ErrUnavailable)
	require.NoError(t, runtime.Replace(maintenanceRuntimeExecutorStub{}))
	_, err = forwarders[adapters.MaintenanceOperationReadAdapterKey].Execute(context.Background(), request)
	require.ErrorIs(t, err, adapters.ErrInvalidRequest, "the forwarder must reach the bound operation adapter")

	runtime.Clear()
	_, err = forwarders[adapters.MaintenanceOperationReadAdapterKey].Execute(context.Background(), request)
	require.ErrorIs(t, err, catalogruntime.ErrAdapterNotFound)
}

func TestMaintenanceRuntimeForwardersUseLatestSIPGeneration(t *testing.T) {
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(&appmodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	active := int8(1)
	deptID := uint(10)
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: deptID}, Name: "OpenAPI owner", Status: &active}).Error)
	deviceCode := "34020000002000000010"
	channelCode := "34020000001320000010"
	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID: deviceCode, OwnerDeptID: deptID, IP: "192.0.2.10", Port: 5060, Transport: "UDP",
		Status: gbmodels.DeviceStatusOnline, EffectiveVersion: string(protocol.Version2022),
	}).Error)
	capabilities := `{"teleboot":true}`
	require.NoError(t, db.Create(&gbmodels.GbChannel{
		DeviceID: deviceCode, ChannelID: channelCode, OwnerDeptID: deptID,
		Status: gbmodels.ChannelStatusOnline, Capabilities: &capabilities,
	}).Error)
	runtime, err := NewMaintenanceRuntime(db)
	require.NoError(t, err)
	forwarder := runtime.Forwarders()[adapters.DeviceRebootAdapterKey]

	firstCalls, secondCalls := 0, 0
	first := maintenanceRuntimeExecutorStub{rebootCalls: &firstCalls}
	second := maintenanceRuntimeExecutorStub{rebootCalls: &secondCalls}
	require.NoError(t, runtime.Replace(first))
	_, err = forwarder.Execute(context.Background(), maintenanceRebootInvocation(deviceCode, "reboot-1"))
	require.NoError(t, err)
	require.Equal(t, 1, firstCalls)

	require.NoError(t, runtime.Replace(second))
	_, err = forwarder.Execute(context.Background(), maintenanceRebootInvocation(deviceCode, "reboot-2"))
	require.NoError(t, err)
	require.Equal(t, 1, firstCalls, "replaced SIP generation must stop receiving maintenance calls")
	require.Equal(t, 1, secondCalls, "existing stable forwarder must resolve the current SIP generation")

	runtime.Clear()
	_, err = forwarder.Execute(context.Background(), maintenanceRebootInvocation(deviceCode, "reboot-3"))
	require.ErrorIs(t, err, catalogruntime.ErrAdapterNotFound)
}

func maintenanceRebootInvocation(deviceCode, key string) catalogruntime.Invocation {
	return catalogruntime.Invocation{
		Scope: adapters.DeviceRebootScope, Method: "POST", Params: map[string]string{"deviceId": deviceCode},
		Body: []byte(`{"confirmed":true}`),
		Value: adapters.Request{ClientID: 5, IdempotencyKey: key,
			ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}},
	}
}

type maintenanceRuntimeExecutorStub struct {
	rebootCalls *int
}

func (maintenanceRuntimeExecutorStub) Execute(context.Context, runtimeptz.Target, runtimeptz.Command) (gbmodels.GbPTZOperation, error) {
	return gbmodels.GbPTZOperation{}, nil
}

func (executor maintenanceRuntimeExecutorStub) ExecuteDeviceRebootDetailed(context.Context, runtimeptz.DeviceRebootTarget, string, uint, uint) (gbmodels.GbPTZOperation, bool, error) {
	if executor.rebootCalls != nil {
		(*executor.rebootCalls)++
	}
	return gbmodels.GbPTZOperation{}, false, nil
}

func (maintenanceRuntimeExecutorStub) FormatStorageCard(context.Context, runtimeptz.Target, int, uint, uint, string) (gbmodels.GbPTZOperation, error) {
	return gbmodels.GbPTZOperation{}, nil
}

var _ adapters.MaintenanceExecutor = maintenanceRuntimeExecutorStub{}
