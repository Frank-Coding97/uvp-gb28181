package deviceupgrade

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/upgrade"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	openapiadapters "uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	deviceUpgradeTestDevice = "34020000002000000010"
	deviceUpgradeTestFW     = "fw-approved"
)

func TestUpgradeAdapterRequiresConfirmedApprovedMatchingGB2022Device(t *testing.T) {
	db := newDeviceUpgradeTestDB(t)
	device := addDeviceUpgradeTestDevice(t, db, 10, protocol.Version2022)
	addDeviceUpgradeTestFirmware(t, db, 10, deviceUpgradeTestFW, gbmodels.FirmwareStatusPublished, "Cam-.*")
	executor := &deviceUpgradeExecutorStub{operation: upgrade.Operation{
		OperationID: "upgrade-1", DeviceID: device.ID, Status: gbmodels.FirmwareUpgradeSent,
		Firmware: "v2.0", Manufacturer: "Acme", SIPStatus: http.StatusOK,
	}}
	dispatcher := NewDispatcher(db, executor)
	adapter := dispatcher.Adapter(UpgradeAdapterKey)
	require.NotNil(t, adapter)

	invocation := deviceUpgradeInvocation(http.MethodPost, UpgradeScope, `{"confirmed":true,"firmwareId":"fw-approved"}`, 41, "caller-key")
	result, err := adapter.Execute(context.Background(), invocation)
	require.NoError(t, err)
	require.Len(t, executor.calls, 1)
	call := executor.calls[0]
	require.Equal(t, deviceUpgradeTestDevice, call.target.DeviceCode)
	require.Equal(t, protocol.Version2022, string(call.target.Profile.Version))
	require.True(t, call.request.Confirmed)
	require.Equal(t, deviceUpgradeTestFW, call.request.FirmwareID)
	require.Empty(t, call.request.FileURL, "public clients may only select approved repository firmware")
	require.Equal(t, uint(10), call.request.ActorDeptID)
	require.Contains(t, call.request.IdempotencyKey, "openapi-client:41:")
	view := result.(OperationView)
	require.Equal(t, "sent", view.SubmissionStatus)
	require.Equal(t, "pending", view.DeviceStatus, "SIP 200 must not imply device accepted or upgraded")
	require.Equal(t, http.StatusOK, view.SIPStatus)
}

func TestUpgradeAdapterRejectsUnsafeOrUnsupportedInputsBeforeExecution(t *testing.T) {
	tests := []struct {
		name       string
		version    protocol.Version
		status     gbmodels.FirmwareStatus
		modelRegex string
		body       string
		want       error
	}{
		{name: "requires explicit confirmation", version: protocol.Version2022, status: gbmodels.FirmwareStatusPublished, modelRegex: "Cam-.*", body: `{"confirmed":false,"firmwareId":"fw-approved"}`, want: ErrInvalidRequest},
		{name: "rejects 2016 device", version: protocol.Version2016, status: gbmodels.FirmwareStatusPublished, modelRegex: "Cam-.*", body: `{"confirmed":true,"firmwareId":"fw-approved"}`, want: ErrProfileUnsupported},
		{name: "rejects archived firmware", version: protocol.Version2022, status: gbmodels.FirmwareStatusArchived, modelRegex: "Cam-.*", body: `{"confirmed":true,"firmwareId":"fw-approved"}`, want: ErrFirmwareUnavailable},
		{name: "rejects model mismatch", version: protocol.Version2022, status: gbmodels.FirmwareStatusPublished, modelRegex: "Other-.*", body: `{"confirmed":true,"firmwareId":"fw-approved"}`, want: ErrFirmwareNotCompatible},
		{name: "rejects arbitrary URL", version: protocol.Version2022, status: gbmodels.FirmwareStatusPublished, modelRegex: "Cam-.*", body: `{"confirmed":true,"firmwareId":"fw-approved","fileUrl":"https://attacker.invalid/fw"}`, want: ErrInvalidRequest},
		{name: "rejects unknown fields", version: protocol.Version2022, status: gbmodels.FirmwareStatusPublished, modelRegex: "Cam-.*", body: `{"confirmed":true,"firmwareId":"fw-approved","deviceId":"34020000002000000099"}`, want: ErrInvalidRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newDeviceUpgradeTestDB(t)
			addDeviceUpgradeTestDevice(t, db, 10, test.version)
			addDeviceUpgradeTestFirmware(t, db, 10, deviceUpgradeTestFW, test.status, test.modelRegex)
			executor := &deviceUpgradeExecutorStub{}
			adapter := NewDispatcher(db, executor).Adapter(UpgradeAdapterKey)
			_, err := adapter.Execute(context.Background(), deviceUpgradeInvocation(http.MethodPost, UpgradeScope, test.body, 41, "caller-key"))
			require.ErrorIs(t, err, test.want)
			require.Empty(t, executor.calls)
		})
	}
}

func TestUpgradeAdapterNamespacesIdempotencyAndScopesOperationReadByClient(t *testing.T) {
	db := newDeviceUpgradeTestDB(t)
	device := addDeviceUpgradeTestDevice(t, db, 10, protocol.Version2022)
	addDeviceUpgradeTestFirmware(t, db, 10, deviceUpgradeTestFW, gbmodels.FirmwareStatusPublished, "Cam-.*")
	executor := &deviceUpgradeExecutorStub{operation: upgrade.Operation{
		OperationID: "upgrade-1", DeviceID: device.ID, Status: gbmodels.FirmwareUpgradeAccepted,
		Firmware: "v2.0", Manufacturer: "Acme", SIPStatus: http.StatusOK,
	}}
	dispatcher := NewDispatcher(db, executor)
	adapter := dispatcher.Adapter(UpgradeAdapterKey)
	_, err := adapter.Execute(context.Background(), deviceUpgradeInvocation(http.MethodPost, UpgradeScope, `{"confirmed":true,"firmwareId":"fw-approved"}`, 41, "same-key"))
	require.NoError(t, err)
	_, err = adapter.Execute(context.Background(), deviceUpgradeInvocation(http.MethodPost, UpgradeScope, `{"confirmed":true,"firmwareId":"fw-approved"}`, 42, "same-key"))
	require.NoError(t, err)
	require.Len(t, executor.calls, 2)
	require.NotEqual(t, executor.calls[0].request.IdempotencyKey, executor.calls[1].request.IdempotencyKey)

	clientKey, err := scopedUpgradeIdempotencyKey(41, "same-key")
	require.NoError(t, err)
	require.NoError(t, db.Create(&gbmodels.GbDeviceFirmwareUpgrade{
		OperationID: "operation-41", IdempotencyKey: clientKey, DeviceID: device.ID, DeviceCode: device.DeviceID,
		Firmware: "v2.0", FileURL: "https://private.invalid/token?secret=never-return", Manufacturer: "Acme",
		FirmwareID: deviceUpgradeTestFW, SessionID: "session-41", SN: 100, ProfileVersion: protocol.Version2022,
		Status: gbmodels.FirmwareUpgradeSucceeded, SIPStatus: http.StatusOK, CurrentFirmware: "v2.0",
		CreatedAt: time.Now(), UpdatedAt: time.Now(), ActorDeptID: 10,
	}).Error)
	read := dispatcher.Adapter(ReadAdapterKey)
	request := deviceUpgradeInvocation(http.MethodGet, ReadScope, "", 41, "")
	request.Params["operationId"] = "operation-41"
	result, err := read.Execute(context.Background(), request)
	require.NoError(t, err)
	view := result.(OperationView)
	require.Equal(t, "succeeded", view.DeviceStatus)
	require.Equal(t, "v2.0", view.CurrentFirmware)
	encodedView, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(encodedView), "fileUrl", "operation DTO must never expose tokenized FileURL")
	require.NotContains(t, string(encodedView), "private.invalid")
	require.NotContains(t, string(encodedView), "secret=never-return")
	require.NotContains(t, string(encodedView), "/private/firmware.bin")

	request = deviceUpgradeInvocation(http.MethodGet, ReadScope, "", 42, "")
	request.Params["operationId"] = "operation-41"
	_, err = read.Execute(context.Background(), request)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestUpgradeAdapterRejectsDeviceOutsideClientDepartmentScope(t *testing.T) {
	db := newDeviceUpgradeTestDB(t)
	addDeviceUpgradeTestDevice(t, db, 20, protocol.Version2022)
	addDeviceUpgradeTestFirmware(t, db, 10, deviceUpgradeTestFW, gbmodels.FirmwareStatusPublished, "Cam-.*")
	executor := &deviceUpgradeExecutorStub{}
	adapter := NewDispatcher(db, executor).Adapter(UpgradeAdapterKey)
	_, err := adapter.Execute(context.Background(), deviceUpgradeInvocation(http.MethodPost, UpgradeScope, `{"confirmed":true,"firmwareId":"fw-approved"}`, 41, "key"))
	require.ErrorIs(t, err, ErrNotFound)
	require.Empty(t, executor.calls)
}

func TestDeviceUpgradeErrorsUseGatewayErrorClassification(t *testing.T) {
	for _, test := range []struct {
		name       string
		err        error
		code       string
		httpStatus int
	}{
		{name: "invalid request", err: ErrInvalidRequest, code: "INVALID_REQUEST", httpStatus: http.StatusBadRequest},
		{name: "resource absence", err: ErrFirmwareUnavailable, code: "RESOURCE_NOT_FOUND", httpStatus: http.StatusNotFound},
		{name: "incompatible firmware", err: ErrFirmwareNotCompatible, code: "CAPABILITY_NOT_SUPPORTED", httpStatus: http.StatusConflict},
		{name: "unsupported protocol", err: ErrProfileUnsupported, code: "CAPABILITY_NOT_SUPPORTED", httpStatus: http.StatusConflict},
		{name: "offline device", err: ErrDeviceOffline, code: "DEVICE_OFFLINE", httpStatus: http.StatusConflict},
		{name: "maintenance busy", err: ErrUpgradeBusy, code: "CONFLICT", httpStatus: http.StatusConflict},
		{name: "idempotency conflict", err: ErrConflict, code: "CONFLICT", httpStatus: http.StatusConflict},
		{name: "unavailable dependency", err: ErrUnavailable, code: "SERVICE_UNAVAILABLE", httpStatus: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.code, openapiadapters.ErrorCode(test.err))
			require.Equal(t, test.httpStatus, openapiadapters.HTTPStatus(test.err))
		})
	}
}

func newDeviceUpgradeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&appmodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbFirmwareRepository{}, &gbmodels.GbDeviceFirmwareUpgrade{}))
	active := int8(1)
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: 10}, Name: "OpenAPI owner", Status: &active}).Error)
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: 20}, Name: "Other owner", Status: &active}).Error)
	t.Cleanup(func() { _ = raw.Close() })
	return db
}

func addDeviceUpgradeTestDevice(t *testing.T, db *gorm.DB, deptID uint, version protocol.Version) gbmodels.GbDevice {
	t.Helper()
	device := gbmodels.GbDevice{
		DeviceID: deviceUpgradeTestDevice, OwnerDeptID: deptID, Manufacturer: "Acme", Model: "Cam-42",
		IP: "192.0.2.10", Port: 5060, Transport: "UDP", Status: gbmodels.DeviceStatusOnline,
		EffectiveVersion: string(version),
	}
	require.NoError(t, db.Create(&device).Error)
	return device
}

func addDeviceUpgradeTestFirmware(t *testing.T, db *gorm.DB, deptID uint64, firmwareID string, status gbmodels.FirmwareStatus, modelPattern string) {
	t.Helper()
	require.NoError(t, db.Create(&gbmodels.GbFirmwareRepository{
		FirmwareID: firmwareID, Version: "v2.0", Manufacturer: "Acme", ModelPattern: modelPattern,
		FileName: "firmware.bin", StoragePath: "/private/firmware.bin", Status: status, DeptID: deptID,
	}).Error)
}

func deviceUpgradeInvocation(method, scope, body string, clientID int64, key string) catalogruntime.Invocation {
	return catalogruntime.Invocation{
		Method: method, Scope: scope, Path: "/openapi/v1/devices/" + deviceUpgradeTestDevice + "/firmware-upgrades",
		Params: map[string]string{"deviceId": deviceUpgradeTestDevice}, Body: []byte(body),
		Value: openapiadapters.Request{ClientID: clientID, IdempotencyKey: key, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}},
	}
}

type deviceUpgradeCall struct {
	target  upgrade.Target
	request upgrade.Request
}

type deviceUpgradeExecutorStub struct {
	calls     []deviceUpgradeCall
	operation upgrade.Operation
	err       error
}

func (executor *deviceUpgradeExecutorStub) Execute(_ context.Context, target upgrade.Target, request upgrade.Request) (upgrade.Operation, bool, error) {
	executor.calls = append(executor.calls, deviceUpgradeCall{target: target, request: request})
	return executor.operation, false, executor.err
}
