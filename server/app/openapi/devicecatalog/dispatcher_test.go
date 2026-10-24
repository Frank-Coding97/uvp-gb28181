package devicecatalog

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/catalogprogress"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	catalogDept      = uint(10)
	catalogOtherDept = uint(20)
	catalogDevice    = "34020000002000000031"
	catalogOtherDev  = "34020000002000000032"
)

type triggerCall struct {
	deviceID  string
	dest      string
	transport string
}

type fakeTrigger struct{ calls []triggerCall }

func (f *fakeTrigger) Trigger(_ context.Context, deviceID, dest, transport string) {
	f.calls = append(f.calls, triggerCall{deviceID: deviceID, dest: dest, transport: transport})
}

func catalogDB(t *testing.T) (*gorm.DB, resource.DepartmentScope) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&appmodels.SysDepartment{}, &gbmodels.GbDevice{}))
	active := int8(1)
	for _, department := range []appmodels.SysDepartment{
		{BaseModel: appmodels.BaseModel{ID: catalogDept}, Name: "root", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: catalogOtherDept}, Name: "other", Status: &active},
	} {
		require.NoError(t, db.Create(&department).Error)
	}
	for _, device := range []gbmodels.GbDevice{
		{DeviceID: catalogDevice, Name: "online", OwnerDeptID: catalogDept, Status: gbmodels.DeviceStatusOnline, IP: "192.0.2.31", Port: 5060, Transport: "UDP"},
		{DeviceID: catalogOtherDev, Name: "foreign", OwnerDeptID: catalogOtherDept, Status: gbmodels.DeviceStatusOnline, IP: "192.0.2.32", Port: 5060, Transport: "UDP"},
	} {
		require.NoError(t, db.Create(&device).Error)
	}
	return db, resource.DepartmentScope{OwnerDeptID: catalogDept, DataScope: resource.DataScopeDepartment}
}

func isolatedTracker(t *testing.T) *catalogprogress.Tracker {
	t.Helper()
	tracker := catalogprogress.NewTracker(time.Minute, time.Second)
	SetTracker(tracker)
	t.Cleanup(func() { SetTracker(nil) })
	return tracker
}

func TestRefreshDeduplicatesWhileAnExchangeIsInFlight(t *testing.T) {
	db, scope := catalogDB(t)
	isolatedTracker(t)
	trigger := &fakeTrigger{}
	dispatcher := NewDispatcher(db, trigger)
	request := adapters.Request{ClientID: 7, ResourceScope: scope, DeviceID: catalogDevice}

	first, err := dispatcher.refreshCatalog(context.Background(), request)
	require.NoError(t, err)
	require.False(t, first.Deduplicated)
	require.Equal(t, catalogDevice, first.DeviceID)
	require.Len(t, trigger.calls, 1)
	assert.Equal(t, "192.0.2.31:5060", trigger.calls[0].dest)
	assert.Equal(t, "UDP", trigger.calls[0].transport)

	second, err := dispatcher.refreshCatalog(context.Background(), request)
	require.NoError(t, err)
	require.True(t, second.Deduplicated, "a second refresh must reuse the in-flight operation")
	assert.Equal(t, first.OperationID, second.OperationID)
	require.Len(t, trigger.calls, 1, "no second SIP exchange")
}

func TestRefreshRefusesDevicesItCannotReach(t *testing.T) {
	db, scope := catalogDB(t)
	isolatedTracker(t)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", catalogDevice).Update("status", gbmodels.DeviceStatusOffline).Error)
	dispatcher := NewDispatcher(db, &fakeTrigger{})

	_, err := dispatcher.refreshCatalog(context.Background(), adapters.Request{ClientID: 7, ResourceScope: scope, DeviceID: catalogDevice})
	require.ErrorIs(t, err, adapters.ErrDeviceOffline)

	_, err = dispatcher.refreshCatalog(context.Background(), adapters.Request{ClientID: 7, ResourceScope: scope, DeviceID: catalogOtherDev})
	require.ErrorIs(t, err, adapters.ErrNotFound, "another department's device stays invisible")

	// An online device without a signalling endpoint cannot be queried either.
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", catalogDevice).Updates(map[string]any{"status": gbmodels.DeviceStatusOnline, "ip": ""}).Error)
	_, err = dispatcher.refreshCatalog(context.Background(), adapters.Request{ClientID: 7, ResourceScope: scope, DeviceID: catalogDevice})
	require.ErrorIs(t, err, adapters.ErrUnavailable)
}

func TestProgressIsBoundToTheRequestingDevice(t *testing.T) {
	db, scope := catalogDB(t)
	tracker := isolatedTracker(t)
	dispatcher := NewDispatcher(db, &fakeTrigger{})

	started := tracker.Start(catalogOtherDev)
	_, err := dispatcher.progress(context.Background(), adapters.Request{ClientID: 7, ResourceScope: scope, DeviceID: catalogDevice}, started.Snapshot.OperationID)
	require.ErrorIs(t, err, adapters.ErrNotFound, "the tracker is process-wide, so the device binding is the only guard")

	own := tracker.Start(catalogDevice)
	progress, err := dispatcher.progress(context.Background(), adapters.Request{ClientID: 7, ResourceScope: scope, DeviceID: catalogDevice}, own.Snapshot.OperationID)
	require.NoError(t, err)
	assert.Equal(t, catalogDevice, progress.DeviceID)
	assert.Equal(t, string(catalogprogress.StatusWaiting), progress.Status)

	_, err = dispatcher.progress(context.Background(), adapters.Request{ClientID: 7, ResourceScope: scope, DeviceID: catalogDevice}, "unknown-operation")
	require.ErrorIs(t, err, adapters.ErrNotFound)
}

func TestAdaptersRejectForeignScopesAndMethods(t *testing.T) {
	db, _ := catalogDB(t)
	isolatedTracker(t)
	registry := catalogruntime.NewAdapterRegistry()
	for _, registration := range NewAdapterRegistrations(NewDispatcher(db, &fakeTrigger{})) {
		require.NoError(t, registry.Register(registration))
		require.Equal(t, AdapterVersion, registration.ContractVersion)
	}

	refresh, err := registry.Resolve(RefreshAdapterKey, AdapterVersion)
	require.NoError(t, err)
	_, err = refresh.Execute(context.Background(), catalogruntime.Invocation{Scope: RefreshReadScope, Method: "POST", Value: adapters.Request{}})
	require.ErrorIs(t, err, adapters.ErrInvalidRequest, "the trigger scope and the read scope are not interchangeable")

	read, err := registry.Resolve(RefreshReadAdapterKey, AdapterVersion)
	require.NoError(t, err)
	_, err = read.Execute(context.Background(), catalogruntime.Invocation{Scope: RefreshReadScope, Method: "POST", Value: adapters.Request{}})
	require.ErrorIs(t, err, adapters.ErrInvalidRequest, "polling progress is a GET")
	_, err = read.Execute(context.Background(), catalogruntime.Invocation{Scope: RefreshReadScope, Method: "GET", Params: map[string]string{"operationId": ""}, Value: adapters.Request{}})
	require.ErrorIs(t, err, adapters.ErrInvalidRequest, "an operation id is mandatory")
}
