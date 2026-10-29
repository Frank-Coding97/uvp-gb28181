package deviceconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	configTestDevice  = "34020000002000000010"
	configTestChannel = "37011200001310000010"
)

type configCommandStub struct {
	readTargets   []runtimeptz.Target
	readTypes     [][]string
	readKeys      []string
	applyTargets  []runtimeptz.Target
	applyBlocks   []manscdp.DeviceConfigBlocks
	applyKeys     []string
	videoReadKeys []string
	videoItems    [][]manscdp.VideoParamItem
	videoWriteKey []string
	operations    map[string]gbmodels.GbPTZOperation
}

func (stub *configCommandStub) ReadDeviceConfigs(_ context.Context, target runtimeptz.Target, types []string, _, _ uint, key string) (gbmodels.GbPTZOperation, error) {
	stub.readTargets = append(stub.readTargets, target)
	stub.readTypes = append(stub.readTypes, append([]string(nil), types...))
	stub.readKeys = append(stub.readKeys, key)
	return configOperation("read-op", "refresh_device_configs", key), nil
}

func (stub *configCommandStub) ApplyDeviceConfig(_ context.Context, target runtimeptz.Target, blocks manscdp.DeviceConfigBlocks, _, _ uint, key string) (gbmodels.GbPTZOperation, error) {
	stub.applyTargets = append(stub.applyTargets, target)
	stub.applyBlocks = append(stub.applyBlocks, blocks)
	stub.applyKeys = append(stub.applyKeys, key)
	return configOperation("write-op", "apply_device_config", key), nil
}

func (stub *configCommandStub) ReadVideoParams(_ context.Context, _ runtimeptz.Target, _, _ uint, key string) (gbmodels.GbPTZOperation, error) {
	stub.videoReadKeys = append(stub.videoReadKeys, key)
	return configOperation("video-read-op", "refresh_video_params", key), nil
}

func (stub *configCommandStub) ApplyVideoParams(_ context.Context, _ runtimeptz.Target, items []manscdp.VideoParamItem, _, _ uint, key string) (gbmodels.GbPTZOperation, error) {
	stub.videoItems = append(stub.videoItems, append([]manscdp.VideoParamItem(nil), items...))
	stub.videoWriteKey = append(stub.videoWriteKey, key)
	return configOperation("video-write-op", "apply_video_params", key), nil
}

func (stub *configCommandStub) GetOperation(_ context.Context, id string) (gbmodels.GbPTZOperation, error) {
	operation, ok := stub.operations[id]
	if !ok {
		return gbmodels.GbPTZOperation{}, gorm.ErrRecordNotFound
	}
	return operation, nil
}

func configOperation(id, action, key string) gbmodels.GbPTZOperation {
	return gbmodels.GbPTZOperation{
		OperationID: id, IdempotencyKey: key, DeviceCode: configTestDevice, ChannelCode: configTestChannel,
		Action: action, SN: 17, Status: gbmodels.PTZOperationQueued, ResponseRequired: true,
		ProfileVersion: string(protocol.Version2022),
	}
}

func newConfigTestService(t *testing.T, version string) (*Service, *configCommandStub, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.NewString())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZOperation{}, &gbmodels.GbDeviceConfig{}, &gbmodels.GbDeviceVideoParam{}))
	require.NoError(t, db.Exec("CREATE TABLE sys_department (id integer primary key, parent_id integer, status integer, deleted_at datetime)").Error)
	require.NoError(t, db.Exec("INSERT INTO sys_department (id, status) VALUES (10, 1)").Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: configTestDevice, Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 10, EffectiveVersion: version}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: configTestDevice, ChannelID: configTestChannel, Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 10, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error)
	commands := &configCommandStub{operations: make(map[string]gbmodels.GbPTZOperation)}
	return New(db, commands), commands, db
}

func configRequest(clientID int64) Request {
	return Request{
		ClientID: clientID, Scope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment},
		DeviceID: configTestDevice, ChannelID: configTestChannel, IdempotencyKey: "same-client-key",
	}
}

func TestRefreshNamespacesIdempotencyByClientAndSelectsProtocolProfile(t *testing.T) {
	service, commands, _ := newConfigTestService(t, string(protocol.Version2022))
	first, err := service.Refresh(context.Background(), configRequest(41), []string{manscdp.ConfigTypePictureMask, manscdp.ConfigTypeFrameMirror})
	require.NoError(t, err)
	second, err := service.Refresh(context.Background(), configRequest(42), []string{manscdp.ConfigTypeFrameMirror, manscdp.ConfigTypePictureMask})
	require.NoError(t, err)
	require.Equal(t, "read-op", first.OperationID)
	require.Equal(t, "read-op", second.OperationID)
	require.Len(t, commands.readKeys, 2)
	require.NotEqual(t, commands.readKeys[0], commands.readKeys[1], "OpenAPI clients must not share PTZ idempotency keys")
	require.NotContains(t, commands.readKeys[0], "same-client-key", "raw client idempotency key must not enter PTZ operation records")
	require.Equal(t, []string{manscdp.ConfigTypePictureMask, manscdp.ConfigTypeFrameMirror}, commands.readTypes[0])
	require.Equal(t, protocol.Version(protocol.Version2022), commands.readTargets[0].Profile.Version)
}

func TestRefreshUsesProtocolSpecificDefaultTypes(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
		want    []string
	}{
		{name: "2016", version: string(protocol.Version2016), want: []string{manscdp.ConfigTypeBasicParam}},
		{name: "2022", version: string(protocol.Version2022), want: append([]string(nil), manscdp.ConfigTypeOrder[1:]...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, commands, _ := newConfigTestService(t, test.version)
			_, err := service.Refresh(context.Background(), configRequest(41), nil)
			require.NoError(t, err)
			require.Equal(t, test.want, commands.readTypes[0])
		})
	}
}

func TestRefreshRejectsUnknownOrOlderProtocolFor2022ConfigurationBeforeSending(t *testing.T) {
	service, commands, db := newConfigTestService(t, string(protocol.Version2016))
	_, err := service.Refresh(context.Background(), configRequest(41), []string{manscdp.ConfigTypePictureMask})
	require.ErrorIs(t, err, ErrCapabilityNotSupported)
	require.Empty(t, commands.readTargets)

	_, err = service.Refresh(context.Background(), configRequest(41), []string{manscdp.ConfigTypeBasicParam})
	require.NoError(t, err, "2016 BasicParam is in the shared configuration profile")
	require.Equal(t, protocol.Version(protocol.Version2016), commands.readTargets[0].Profile.Version)

	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", configTestDevice).Update("effective_version", "").Error)
	_, err = service.Refresh(context.Background(), configRequest(41), []string{manscdp.ConfigTypeBasicParam})
	require.ErrorIs(t, err, ErrCapabilityNotSupported, "unknown profile must fail closed")
	require.Len(t, commands.readTargets, 1)
}

func TestApplyRequiresObservedSupportAndReportsResponseSeparatelyFromSubmission(t *testing.T) {
	service, commands, db := newConfigTestService(t, string(protocol.Version2022))
	blocks := manscdp.DeviceConfigBlocks{FrameMirror: &manscdp.FrameMirrorBlock{Value: manscdp.FrameMirrorLeftRight}}
	_, err := service.Apply(context.Background(), configRequest(41), blocks)
	require.ErrorIs(t, err, ErrCapabilityUnknown)
	require.Empty(t, commands.applyTargets, "unknown device support must not dispatch a write")

	require.NoError(t, db.Create(&gbmodels.GbDeviceConfig{
		DeviceID: 1, TargetCode: configTestChannel, ConfigType: manscdp.ConfigTypeFrameMirror,
		PayloadJSON: `{"value":0}`, ObservedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error)
	result, err := service.Apply(context.Background(), configRequest(41), blocks)
	require.NoError(t, err)
	require.False(t, result.DeviceAcknowledged, "queued means submitted to platform, not answered by device")
	require.Equal(t, "queued", result.Status)
	require.Equal(t, "pending", result.Reconciliation.State)
	require.Len(t, commands.applyTargets, 1)
}

func TestGetConfigSnapshotUsesTypedAllowlistAndHidesSnapshotUploadCredentials(t *testing.T) {
	service, _, db := newConfigTestService(t, string(protocol.Version2022))
	upload := "https://private.example/device-upload?token=secret"
	session := "private-session-id"
	snapNum, interval := 1, 2
	require.NoError(t, db.Create(&gbmodels.GbDeviceConfig{
		DeviceID: 1, TargetCode: configTestChannel, ConfigType: manscdp.ConfigTypeSnapShotConfig,
		PayloadJSON: mustJSON(t, manscdp.SnapShotBlock{SnapNum: &snapNum, Interval: &interval, UploadURL: &upload, SessionID: &session}),
		ObservedAt:  time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceConfig{
		DeviceID: 1, TargetCode: configTestDevice, ConfigType: manscdp.ConfigTypeBasicParam,
		PayloadJSON: `{"name":"camera","longitude":"121.5","latitude":"31.2","internalId":123}`,
		ObservedAt:  time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error)

	snapshot, err := service.GetConfigSnapshot(context.Background(), configRequest(41))
	require.NoError(t, err)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), upload)
	require.NotContains(t, string(encoded), "private-session-id")
	require.NotContains(t, string(encoded), "internalId", "unrecognized persistence fields must not pass through")
	require.Contains(t, string(encoded), `"name":"camera"`)
	require.Contains(t, string(encoded), `"longitude":"121.5"`)
	for _, item := range snapshot.Items {
		if item.ConfigType == manscdp.ConfigTypeSnapShotConfig {
			require.NotContains(t, string(item.Config), "uploadUrl")
			require.NotContains(t, string(item.Config), "sessionId")
		}
	}
}

func TestGetOperationIsClientAndDepartmentScopedAndDistinguishesDeviceResponse(t *testing.T) {
	service, commands, _ := newConfigTestService(t, string(protocol.Version2022))
	key := clientIdempotencyKey(41, "same-client-key")
	now := time.Now()
	responseAt := now
	operation := configOperation("my-operation", "apply_device_config", key)
	operation.Status = gbmodels.PTZOperationAccepted
	operation.DeviceResult = "OK"
	operation.ResponseAt = &responseAt
	operation.ResponseHasData = boolPtr(true)
	commands.operations[operation.OperationID] = operation

	result, err := service.GetOperation(context.Background(), configRequest(41), "my-operation")
	require.NoError(t, err)
	require.True(t, result.DeviceAcknowledged)
	require.Equal(t, "OK", result.DeviceResult)
	require.Equal(t, "accepted", result.Status)
	_, err = service.GetOperation(context.Background(), configRequest(42), "my-operation")
	require.ErrorIs(t, err, ErrNotFound, "operation IDs must not cross OpenAPI clients")
	foreignScope := configRequest(41)
	foreignScope.Scope.OwnerDeptID = 11
	_, err = service.GetOperation(context.Background(), foreignScope, "my-operation")
	require.ErrorIs(t, err, ErrNotFound, "the current department boundary must be rechecked")
}

func TestApplyVersionAndResourceScopeAreValidatedBeforeDispatch(t *testing.T) {
	service, commands, _ := newConfigTestService(t, string(protocol.Version2016))
	request := configRequest(41)
	request.ChannelID = "37011200001310000011"
	_, err := service.Apply(context.Background(), request, manscdp.DeviceConfigBlocks{BasicParam: &manscdp.BasicParamBlock{HeartBeatInterval: intPtr(60)}})
	require.ErrorIs(t, err, ErrNotFound)
	require.Empty(t, commands.applyTargets)

	service, commands, _ = newConfigTestService(t, string(protocol.Version2016))
	_, err = service.Apply(context.Background(), configRequest(41), manscdp.DeviceConfigBlocks{OSDConfig: &manscdp.OSDConfigBlock{Length: 1920, Width: 1080}})
	require.ErrorIs(t, err, ErrCapabilityNotSupported)
	require.Empty(t, commands.applyTargets)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}

func boolPtr(value bool) *bool { return &value }
func intPtr(value int) *int    { return &value }
