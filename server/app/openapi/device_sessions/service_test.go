package devicesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/devicecapture"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	captureTestDevice  = "34020000002000000010"
	captureTestChannel = "37011200001310000010"
)

type captureCommandStub struct {
	commands   []runtimeptz.Command
	targets    []runtimeptz.Target
	byID       map[string]gbmodels.GbPTZOperation
	executeErr error
}

func (stub *captureCommandStub) Execute(_ context.Context, target runtimeptz.Target, command runtimeptz.Command) (gbmodels.GbPTZOperation, error) {
	stub.commands = append(stub.commands, command)
	stub.targets = append(stub.targets, target)
	if stub.executeErr != nil {
		return gbmodels.GbPTZOperation{}, stub.executeErr
	}
	op := gbmodels.GbPTZOperation{OperationID: "operation-1", Status: gbmodels.PTZOperationQueued, Action: command.Action}
	if stub.byID == nil {
		stub.byID = make(map[string]gbmodels.GbPTZOperation)
	}
	stub.byID[op.OperationID] = op
	return op, nil
}

func (stub *captureCommandStub) GetOperation(_ context.Context, id string) (gbmodels.GbPTZOperation, error) {
	op, ok := stub.byID[id]
	if !ok {
		return gbmodels.GbPTZOperation{}, gorm.ErrRecordNotFound
	}
	return op, nil
}

func newCaptureTestService(t *testing.T) (*Service, *captureCommandStub, *devicecapture.Registry, *gorm.DB, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.NewString())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbChannelSnapshot{},
		&CaptureSessionRecord{},
	))
	require.NoError(t, db.Exec("CREATE TABLE sys_department (id integer primary key, parent_id integer, status integer, deleted_at datetime)").Error)
	require.NoError(t, db.Exec("INSERT INTO sys_department (id, status) VALUES (10, 1)").Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID: captureTestDevice, Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 10, EffectiveVersion: "2022",
	}).Error)
	var device gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", captureTestDevice).First(&device).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{
		DeviceID: captureTestDevice, ChannelID: captureTestChannel, Status: gbmodels.ChannelStatusOnline,
		OwnerDeptID: 10, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error)
	root := t.TempDir()
	registry := devicecapture.NewRegistry(root)
	commands := &captureCommandStub{}
	service := NewService(Config{
		DB: db, Captures: registry, Commands: commands,
		UploadURL: func(_ context.Context, token string) (string, error) {
			return "https://platform.example/uploads/" + token, nil
		},
	})
	return service, commands, registry, db, root
}

func captureScope() resource.DepartmentScope {
	return resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}
}

func TestCreateCaptureUsesNamespacedClientOwnerAndDeviceIdempotency(t *testing.T) {
	service, commands, registry, _, _ := newCaptureTestService(t)
	request := CaptureRequest{ClientID: 91, Scope: captureScope(), DeviceID: captureTestDevice, ChannelID: captureTestChannel, IdempotencyKey: "capture-1", SnapNum: 2, Interval: 3}
	created, err := service.CreateCapture(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "queued", created.CommandStatus)
	require.Equal(t, "waiting", created.State)
	require.NotEmpty(t, created.SessionID)
	require.Len(t, commands.commands, 1)
	require.Equal(t, 1, commands.commands[0].MaxAttempts, "snapshot configuration is non-idempotent at device level")
	require.True(t, commands.commands[0].ResponseRequired)
	require.NotContains(t, commands.commands[0].IdempotencyKey, "capture-1", "raw client idempotency material must not leak into protocol operation keys")
	session, ok := registry.GetForOwner(created.registrySessionID, "openapi-client:91")
	require.True(t, ok)
	require.NotEmpty(t, session.UploadToken)
	require.NotContains(t, string(mustJSON(t, created)), session.UploadToken, "upload token is device-only")

	replayed, err := service.CreateCapture(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, created.SessionID, replayed.SessionID)
	require.Len(t, commands.commands, 1, "same client/key must not issue another SnapShotConfig")
}

func TestCaptureSessionAndImageAccessAreClientAndDepartmentScoped(t *testing.T) {
	service, _, registry, db, root := newCaptureTestService(t)
	created, err := service.CreateCapture(context.Background(), CaptureRequest{
		ClientID: 91, Scope: captureScope(), DeviceID: captureTestDevice, ChannelID: captureTestChannel,
		IdempotencyKey: "capture-scope", SnapNum: 1, Interval: 1,
	})
	require.NoError(t, err)

	_, err = service.GetCapture(context.Background(), 92, captureScope(), captureTestDevice, captureTestChannel, created.SessionID)
	require.ErrorIs(t, err, ErrNotFound, "a different OpenAPI client must not learn that the session exists")
	_, err = service.GetCapture(context.Background(), 91, resource.DepartmentScope{OwnerDeptID: 11, DataScope: resource.DataScopeDepartment}, captureTestDevice, captureTestChannel, created.SessionID)
	require.ErrorIs(t, err, ErrNotFound, "current data scope must still authorize the saved target")

	registrySession, ok := registry.GetForOwner(created.registrySessionID, "openapi-client:91")
	require.True(t, ok)
	jpg := []byte{0xff, 0xd8, 0xff, 0xd9}
	file, err := registry.Upload(registrySession.UploadToken, "capture.jpg", "image/jpeg", bytes.NewReader(jpg))
	require.NoError(t, err)
	snapshot := gbmodels.GbChannelSnapshot{
		DeviceID: registrySessionDeviceID(t, db), ChannelID: registrySessionChannelID(t, db),
		ChannelCode: captureTestChannel, FileName: file.Name, RelPath: file.RelPath, Size: file.Size, MD5: file.MD5,
		CapturedAt: file.ReceivedAt, Source: gbmodels.SnapshotSourceDevice, SessionID: &registrySession.ID,
		CreatedAt: file.ReceivedAt, UpdatedAt: file.ReceivedAt,
	}
	require.NoError(t, db.Create(&snapshot).Error)

	content, err := service.ReadImage(context.Background(), 91, captureScope(), captureTestDevice, captureTestChannel, created.SessionID, file.Name)
	require.NoError(t, err)
	require.Equal(t, jpg, content.Bytes)
	service.captures = devicecapture.NewRegistry(root)
	view, err := service.GetCapture(context.Background(), 91, captureScope(), captureTestDevice, captureTestChannel, created.SessionID)
	require.NoError(t, err)
	require.Equal(t, CaptureStateUnknown, view.State, "a process restart loses transient device-session state")
	content, err = service.ReadImage(context.Background(), 91, captureScope(), captureTestDevice, captureTestChannel, created.SessionID, file.Name)
	require.NoError(t, err, "persisted image access should survive transient registry expiry")
	require.Equal(t, jpg, content.Bytes)
	_, err = service.ReadImage(context.Background(), 92, captureScope(), captureTestDevice, captureTestChannel, created.SessionID, file.Name)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = service.ReadImage(context.Background(), 91, captureScope(), captureTestDevice, captureTestChannel, created.SessionID, "../capture.jpg")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestCaptureIdempotencyRejectsChangedPayloadAndStopDoesNotClaimDeviceStopped(t *testing.T) {
	service, commands, _, _, _ := newCaptureTestService(t)
	request := CaptureRequest{ClientID: 91, Scope: captureScope(), DeviceID: captureTestDevice, ChannelID: captureTestChannel, IdempotencyKey: "same-key", SnapNum: 1, Interval: 1}
	created, err := service.CreateCapture(context.Background(), request)
	require.NoError(t, err)
	request.Interval++
	_, err = service.CreateCapture(context.Background(), request)
	require.ErrorIs(t, err, ErrConflict)
	require.Len(t, commands.commands, 1)

	stopped, err := service.StopCapture(context.Background(), 91, captureScope(), captureTestDevice, captureTestChannel, created.SessionID)
	require.ErrorIs(t, err, ErrStopUnsupported)
	require.Empty(t, stopped.State, "no local cancellation may be presented as a device stop")
}

func TestCreateCaptureClassifiesOfflineAndUnsupportedTargets(t *testing.T) {
	request := CaptureRequest{ClientID: 91, Scope: captureScope(), DeviceID: captureTestDevice, ChannelID: captureTestChannel, IdempotencyKey: "capture-target-state", SnapNum: 1, Interval: 1}
	t.Run("offline device", func(t *testing.T) {
		service, _, _, db, _ := newCaptureTestService(t)
		require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", captureTestDevice).Update("status", gbmodels.DeviceStatusOffline).Error)
		_, err := service.CreateCapture(context.Background(), request)
		require.ErrorIs(t, err, ErrDeviceOffline)
	})
	t.Run("offline channel", func(t *testing.T) {
		service, _, _, db, _ := newCaptureTestService(t)
		require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", captureTestChannel).Update("status", gbmodels.ChannelStatusOffline).Error)
		_, err := service.CreateCapture(context.Background(), request)
		require.ErrorIs(t, err, ErrDeviceOffline)
	})
	t.Run("unsupported protocol version", func(t *testing.T) {
		service, _, _, db, _ := newCaptureTestService(t)
		require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", captureTestDevice).Update("effective_version", "2016").Error)
		_, err := service.CreateCapture(context.Background(), request)
		require.ErrorIs(t, err, ErrCapabilityNotSupported)
	})
}

func TestCreateCaptureMapsTypedCommandErrors(t *testing.T) {
	tests := []struct {
		name       string
		commandErr error
		want       error
	}{
		{name: "idempotency conflict", commandErr: &runtimeptz.OperationError{Code: runtimeptz.ErrorCodeHomePositionIdempotencyConflict}, want: ErrConflict},
		{name: "device offline", commandErr: &runtimeptz.OperationError{Code: runtimeptz.ErrorCodeHomePositionDeviceOffline}, want: ErrDeviceOffline},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, commands, _, _, _ := newCaptureTestService(t)
			commands.executeErr = test.commandErr
			_, err := service.CreateCapture(context.Background(), CaptureRequest{
				ClientID: 91, Scope: captureScope(), DeviceID: captureTestDevice, ChannelID: captureTestChannel,
				IdempotencyKey: "capture-command-error", SnapNum: 1, Interval: 1,
			})
			require.ErrorIs(t, err, test.want)
		})
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

func registrySessionDeviceID(t *testing.T, db *gorm.DB) uint {
	t.Helper()
	var device gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", captureTestDevice).First(&device).Error)
	return device.ID
}

func registrySessionChannelID(t *testing.T, db *gorm.DB) uint {
	t.Helper()
	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", captureTestChannel).First(&channel).Error)
	return channel.ID
}
