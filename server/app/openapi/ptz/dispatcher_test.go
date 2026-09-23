package ptz

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

const (
	ptzTestDevice  = "34020000002000000010"
	ptzTestChannel = "37011200001310000010"
)

type dispatcherSender struct {
	mu    sync.Mutex
	body  []byte
	calls int
}

func (s *dispatcherSender) SendMessageTracked(_ context.Context, _ string, _ string, _ string, body []byte) (uac.TrackedMessageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.body = append([]byte(nil), body...)
	return uac.TrackedMessageResult{CallID: "call-1", CSeq: "1", StatusCode: 200}, nil
}

func newDispatcherFixture(t *testing.T, sender ptz.TrackedSender) (*gorm.DB, *Dispatcher) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	active := int8(1)
	require.NoError(t, db.AutoMigrate(&appmodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZPreset{}, &gbmodels.GbPTZOperation{}))
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: 10}, Name: "root", Status: &active}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{BaseModel: appmodels.BaseModel{ID: 1}, DeviceID: ptzTestDevice, OwnerDeptID: 10, Status: gbmodels.DeviceStatusOnline, IP: "192.0.2.10", Port: 5060, Transport: "UDP"}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{ID: 2, DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, OwnerDeptID: 10, Status: gbmodels.ChannelStatusOnline}).Error)
	service, err := ptz.NewService(db, sender, time.Now)
	require.NoError(t, err)
	return db, NewDispatcher(db, service)
}

func TestDispatcherListsOnlyScopedPresets(t *testing.T) {
	db, dispatcher := newDispatcherFixture(t, nil)
	require.NoError(t, db.Create(&gbmodels.GbPTZPreset{DeviceID: 1, ChannelID: 2, PresetID: 3, Name: "入口", Status: gbmodels.PTZPresetActive}).Error)
	data, err := dispatcher.Handle(context.Background(), auth.PTZRequest{Scope: auth.PTZPresetListScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment, DeviceID: ptzTestDevice, ChannelID: ptzTestChannel})
	require.NoError(t, err)
	result, ok := data.(map[string]any)
	require.True(t, ok)
	items, ok := result["items"].([]presetView)
	require.True(t, ok)
	require.Len(t, items, 1)
	require.Equal(t, 3, items[0].PresetID)
}

func TestDispatcherRejectsUnknownPresetFieldsAndReusesPTZService(t *testing.T) {
	sender := &dispatcherSender{}
	_, dispatcher := newDispatcherFixture(t, sender)
	_, err := dispatcher.Handle(context.Background(), auth.PTZRequest{Scope: auth.PTZPresetSaveScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment, DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "preset-1", Body: []byte(`{"presetId":3,"unexpected":true}`)})
	require.ErrorIs(t, err, auth.ErrPTZInvalid)
	data, err := dispatcher.Handle(context.Background(), auth.PTZRequest{Scope: auth.PTZPresetSaveScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment, DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "preset-1", Body: []byte(`{"presetId":3,"name":"入口"}`)})
	require.NoError(t, err)
	operation := data.(operationView)
	require.NotEmpty(t, operation.OperationID)
	require.Equal(t, "preset_set", operation.Action)
	sender.mu.Lock()
	require.Equal(t, 1, sender.calls)
	require.Contains(t, string(sender.body), "A50F0181")
	sender.mu.Unlock()
}

func TestDispatcherRejectsOperationFromAnotherChannel(t *testing.T) {
	db, dispatcher := newDispatcherFixture(t, nil)
	require.NoError(t, db.Create(&gbmodels.GbPTZOperation{OperationID: "op-other", IdempotencyKey: "key-other", DeviceID: 1, DeviceCode: ptzTestDevice, ChannelID: 999, ChannelCode: "37011200001310000999", CmdType: "DeviceControl", Action: "preset_set", Status: gbmodels.PTZOperationQueued, CreatedAt: time.Now()}).Error)
	_, err := dispatcher.Handle(context.Background(), auth.PTZRequest{Scope: auth.PTZOperationReadScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment, DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, OperationID: "op-other"})
	require.ErrorIs(t, err, auth.ErrPTZNotFound)
}
