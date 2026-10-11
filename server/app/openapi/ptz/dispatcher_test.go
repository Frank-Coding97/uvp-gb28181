package ptz

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/openapi/auth"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	ptzTestDevice  = "34020000002000000010"
	ptzTestChannel = "37011200001310000010"
)

type dispatcherSender struct {
	mu     sync.Mutex
	body   []byte
	calls  int
	failAt int
}

func (s *dispatcherSender) SendMessageTracked(_ context.Context, _ string, _ string, _ string, body []byte) (uac.TrackedMessageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.body = append([]byte(nil), body...)
	if s.failAt > 0 && s.calls == s.failAt {
		return uac.TrackedMessageResult{}, fmt.Errorf("send failed at call %d", s.calls)
	}
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
	require.NoError(t, db.AutoMigrate(&appmodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZPreset{}, &gbmodels.GbPTZCruiseTrack{}, &gbmodels.GbPTZOperation{}, &gbmodels.GbDeviceTargetTrack{}))
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
	// A stale row with the same internal channel id but a different device must
	// never leak into the requested device's preset list.
	require.NoError(t, db.Create(&gbmodels.GbPTZPreset{DeviceID: 999, ChannelID: 2, PresetID: 4, Name: "其他设备", Status: gbmodels.PTZPresetActive}).Error)
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

func TestDispatcherMovesPTZAndStopsWithoutSpeedBytes(t *testing.T) {
	sender := &dispatcherSender{}
	_, dispatcher := newDispatcherFixture(t, sender)
	request := auth.PTZRequest{
		Scope: auth.PTZMoveScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "move-1",
		Body: []byte(`{"action":"left","speed":16}`),
	}
	data, err := dispatcher.Handle(context.Background(), request)
	require.NoError(t, err)
	operation := data.(controlOperationView)
	require.NotEmpty(t, operation.OperationID)
	require.Equal(t, "sent_result_unconfirmed", operation.Result)
	require.False(t, operation.DeviceAcknowledged)
	sender.mu.Lock()
	require.Contains(t, string(sender.body), "A50F0102")
	require.Equal(t, 1, sender.calls)
	sender.mu.Unlock()

	request.IdempotencyKey = "move-stop-1"
	request.Body = []byte(`{"action":"stop","speed":0}`)
	_, err = dispatcher.Handle(context.Background(), request)
	require.NoError(t, err)
	sender.mu.Lock()
	require.Contains(t, string(sender.body), "A50F0100000000B5")
	require.Equal(t, 2, sender.calls)
	sender.mu.Unlock()

	request.IdempotencyKey = "move-1"
	request.Body = []byte(`{"action":"left","speed":16}`)
	_, err = dispatcher.Handle(context.Background(), request)
	require.NoError(t, err)
	sender.mu.Lock()
	require.Equal(t, 2, sender.calls, "同一幂等键不能重复下发")
	sender.mu.Unlock()
}

func TestDispatcherRejectsMalformedPTZControlBeforeSending(t *testing.T) {
	sender := &dispatcherSender{}
	_, dispatcher := newDispatcherFixture(t, sender)
	for index, body := range []string{
		`{"action":"left","speed":16,"unknown":true}`,
		`{"action":"left","action":"right","speed":16}`,
		`{"action":"left","speed":0}`,
	} {
		_, err := dispatcher.Handle(context.Background(), auth.PTZRequest{
			Scope: auth.PTZMoveScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
			DeviceID: ptzTestDevice, ChannelID: ptzTestChannel,
			IdempotencyKey: fmt.Sprintf("invalid-%d", index), Body: []byte(body),
		})
		require.ErrorIs(t, err, auth.ErrPTZInvalid)
	}
	_, err := dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: auth.PTZMoveScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, Body: []byte(`{"action":"left","speed":16}`),
	})
	require.ErrorIs(t, err, auth.ErrPTZInvalid)
	require.Zero(t, sender.calls)
}

func TestDispatcherWiperUsesOnlyTheStandardWiperSwitchAndReportsUnknownResult(t *testing.T) {
	sender := &dispatcherSender{}
	_, dispatcher := newDispatcherFixture(t, sender)
	data, err := dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: auth.PTZWiperScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "wiper-on-1",
		Body: []byte(`{"action":"on"}`),
	})
	require.NoError(t, err)
	operation := data.(controlOperationView)
	require.Equal(t, "wiper_on", operation.Action)
	require.Equal(t, "sent_result_unconfirmed", operation.Result)
	require.False(t, operation.DeviceAcknowledged)
	sender.mu.Lock()
	require.Contains(t, string(sender.body), "A50F018C01000042")
	require.Equal(t, 1, sender.calls)
	sender.mu.Unlock()

	_, err = dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: auth.PTZWiperScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "wiper-invalid-1",
		Body: []byte(`{"action":"on","auxiliaryId":2}`),
	})
	require.ErrorIs(t, err, auth.ErrPTZInvalid)
	require.Equal(t, 1, sender.calls)
}

func TestDispatcherPreciseControlRequiresRecognized2022Profile(t *testing.T) {
	sender := &dispatcherSender{}
	db, dispatcher := newDispatcherFixture(t, sender)
	_, err := dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: auth.PTZPreciseScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "precise-2016",
		Body: []byte(`{"pan":12.5}`),
	})
	require.ErrorIs(t, err, auth.ErrPTZUnsupported)
	require.Zero(t, sender.calls)

	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", ptzTestDevice).Update("effective_version", "2022").Error)
	data, err := dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: auth.PTZPreciseScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "precise-2022",
		Body: []byte(`{"pan":12.5,"zoom":3}`),
	})
	require.NoError(t, err)
	operation := data.(controlOperationView)
	require.Equal(t, "sent_result_unconfirmed", operation.Result)
	sender.mu.Lock()
	require.Contains(t, string(sender.body), "<PTZPreciseCtrl>")
	require.Equal(t, 1, sender.calls)
	sender.mu.Unlock()
}

func TestDispatcherExtendedAndCruiseCommandsUseSingleScopedOperations(t *testing.T) {
	sender := &dispatcherSender{}
	_, dispatcher := newDispatcherFixture(t, sender)
	data, err := dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: auth.PTZExtendedScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "scan-speed-1",
		Body: []byte(`{"action":"scan_set_speed","id":2,"value":1200}`),
	})
	require.NoError(t, err)
	require.Equal(t, string(manscdp.PTZActionScanSetSpeed), data.(controlOperationView).Action)
	sender.mu.Lock()
	require.Contains(t, string(sender.body), "A50F018A02B04031")
	require.Equal(t, 1, sender.calls)
	sender.mu.Unlock()

	data, err = dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: auth.PTZCruiseControlScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "cruise-stop-1",
		Body: []byte(`{"action":"stop","trackId":4}`),
	})
	require.NoError(t, err)
	require.Equal(t, string(manscdp.PTZActionCruiseStop), data.(controlOperationView).Action)
	sender.mu.Lock()
	require.Contains(t, string(sender.body), "A50F0100000000B5")
	require.Equal(t, 2, sender.calls)
	sender.mu.Unlock()

	_, err = dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: auth.PTZCruiseControlScope, OwnerDeptID: 999, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "cruise-denied-1",
		Body: []byte(`{"action":"start","trackId":4}`),
	})
	require.ErrorIs(t, err, auth.ErrPTZNotFound)
	require.Equal(t, 2, sender.calls)
}

func TestDispatcherCreatesScopedCruiseTrackAndRequiresConfirmationForReplacement(t *testing.T) {
	sender := &dispatcherSender{}
	_, dispatcher := newDispatcherFixture(t, sender)
	request := auth.PTZRequest{
		Scope: "ptz:cruise:track:create", OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "cruise-create-1",
		Body: []byte(`{"trackId":0,"name":"north","speed":120,"dwellSec":30,"stops":[{"presetId":3}],"replaceExisting":true,"confirmed":true}`),
	}
	data, err := dispatcher.Handle(context.Background(), request)
	require.NoError(t, err)
	result, ok := data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, 0, result["trackId"])
	require.Equal(t, "sent_result_unconfirmed", result["result"])
	require.Equal(t, false, result["deviceAcknowledged"])
	steps, ok := result["steps"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, steps, 4, "replace + add stop + speed + dwell")

	_, err = dispatcher.Handle(context.Background(), request)
	require.NoError(t, err)
	sender.mu.Lock()
	require.Equal(t, 4, sender.calls, "retry with the same key must not replay the subcommands")
	require.Contains(t, string(sender.body), "A50F0187")
	sender.mu.Unlock()

	readData, err := dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: "ptz:cruise:track:read", TrackID: "0", OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel,
	})
	require.NoError(t, err)
	readResult := readData.(map[string]any)
	readTrack := readResult["track"].(cruiseTrackView)
	require.Equal(t, "platform_pending_reconcile", readTrack.Source)
	require.Equal(t, gbmodels.PTZFreshnessUnknown, readResult["freshness"])

	request.Body = []byte(`{"trackId":0,"name":"north","speed":120,"dwellSec":30,"stops":[{"presetId":3},{"presetId":4}],"replaceExisting":true,"confirmed":true}`)
	_, err = dispatcher.Handle(context.Background(), request)
	require.ErrorIs(t, err, auth.ErrPTZConflict, "the complete request must be bound to every idempotent child operation")
	sender.mu.Lock()
	require.Equal(t, 4, sender.calls)
	sender.mu.Unlock()

	request.IdempotencyKey = "cruise-create-unconfirmed"
	request.Body = []byte(`{"trackId":1,"stops":[{"presetId":3}],"replaceExisting":true,"confirmed":false}`)
	_, err = dispatcher.Handle(context.Background(), request)
	require.ErrorIs(t, err, auth.ErrPTZInvalid)
	sender.mu.Lock()
	require.Equal(t, 4, sender.calls, "unconfirmed replacement must be rejected before sending")
	sender.mu.Unlock()
}

func TestDispatcherCruiseTrackCreationReportsPartialFailure(t *testing.T) {
	sender := &dispatcherSender{failAt: 2}
	db, dispatcher := newDispatcherFixture(t, sender)
	data, err := dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: "ptz:cruise:track:create", OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "cruise-create-partial",
		Body: []byte(`{"trackId":2,"stops":[{"presetId":3},{"presetId":4}],"speed":120,"dwellSec":30}`),
	})
	require.NoError(t, err, "a mid-batch send failure is reported in the step result")
	result, ok := data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "partial_failure", result["result"])
	require.Equal(t, "add_stop", result["failedStep"])
	require.Equal(t, 1, result["completedStops"])
	steps := result["steps"].([]map[string]any)
	require.Equal(t, "sent", steps[0]["status"])
	require.Equal(t, "rejected", steps[1]["status"])
	require.Equal(t, "not_sent", steps[2]["status"])
	require.Equal(t, "not_sent", steps[3]["status"])
	sender.mu.Lock()
	require.Equal(t, 2, sender.calls, "later steps must not be sent after a failed stop")
	sender.mu.Unlock()
	var cached int64
	require.NoError(t, db.Model(&gbmodels.GbPTZCruiseTrack{}).Count(&cached).Error)
	require.Zero(t, cached, "partial configuration must not be presented as a completed cached track")
}

func TestDispatcherReadsOnlyTheScopedCruiseTrackCache(t *testing.T) {
	sender := &dispatcherSender{}
	db, dispatcher := newDispatcherFixture(t, sender)
	observedAt := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, db.Create(&gbmodels.GbPTZCruiseTrack{
		DeviceID: 1, ChannelID: 2, TrackID: 0, Name: "north",
		DetailJSON: `{"trackId":0,"name":"north","sumNum":1,"cruisePoints":[{"presetIndex":3,"stayTime":30,"speed":120}]}`,
		RawSummary: "device-reported", UpdatedAt: observedAt, CreatedAt: observedAt,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbPTZCruiseTrack{
		DeviceID: 999, ChannelID: 2, TrackID: 1, Name: "other-device",
		DetailJSON: `{}`, RawSummary: "device-reported", UpdatedAt: observedAt, CreatedAt: observedAt,
	}).Error)

	data, err := dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: "ptz:cruise:track:read", TrackID: "0", OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel,
	})
	require.NoError(t, err)
	result, ok := data.(map[string]any)
	require.True(t, ok)
	track, ok := result["track"].(cruiseTrackView)
	require.True(t, ok)
	require.Equal(t, 0, track.TrackID)
	require.Equal(t, "device_cache", track.Source)
	require.Equal(t, observedAt, *track.ObservedAt)
	require.Equal(t, gbmodels.PTZFreshnessFresh, result["freshness"])
	require.Zero(t, sender.calls, "a cache read must not issue a protocol query")

	_, err = dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: "ptz:cruise:track:read", TrackID: "1", OwnerDeptID: 999, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel,
	})
	require.ErrorIs(t, err, auth.ErrPTZNotFound)
	require.Zero(t, sender.calls)
}

func TestDispatcherHomePositionSetWaitsForDeviceApplicationResponse(t *testing.T) {
	sender := &dispatcherSender{}
	db, dispatcher := newDispatcherFixture(t, sender)
	data, err := dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: auth.PTZHomePositionSetScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "home-set-1",
		Body: []byte(`{"enabled":true,"resetTime":30,"presetId":0}`),
	})
	require.NoError(t, err)
	operation := data.(controlOperationView)
	require.Equal(t, "pending_device_response", operation.Result)
	require.False(t, operation.DeviceAcknowledged)
	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", operation.OperationID).First(&stored).Error)
	require.True(t, stored.ResponseRequired)
	require.Equal(t, gbmodels.PTZOperationQueued, stored.Status)
	require.Zero(t, sender.calls)

	_, err = dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: auth.PTZHomePositionSetScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "home-set-invalid-1",
		Body: []byte(`{"enabled":true,"resetTime":5,"presetId":0}`),
	})
	require.ErrorIs(t, err, auth.ErrPTZInvalid)
}

func TestDispatcherTargetTrackRequires2022AndExplicitReportedSupport(t *testing.T) {
	sender := &dispatcherSender{}
	db, dispatcher := newDispatcherFixture(t, sender)
	request := auth.PTZRequest{
		Scope: auth.PTZTargetTrackScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "track-2016",
		Body: []byte(`{"mode":"auto"}`),
	}
	_, err := dispatcher.Handle(context.Background(), request)
	require.ErrorIs(t, err, auth.ErrPTZUnsupported)
	require.Zero(t, sender.calls)

	capabilities := `{"target_track":true}`
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", ptzTestDevice).Update("effective_version", "2022").Error)
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", ptzTestChannel).Update("capabilities", capabilities).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{ID: 3, DeviceID: ptzTestDevice, ChannelID: "37011200001310000011", OwnerDeptID: 10, Status: gbmodels.ChannelStatusOnline}).Error)
	request.IdempotencyKey = "track-2022"
	request.Body = []byte(`{"mode":"manual","deviceId2":"37011200001310000011","area":{"length":1920,"width":1080,"midPointX":960,"midPointY":540,"lengthX":200,"lengthY":100}}`)
	data, err := dispatcher.Handle(context.Background(), request)
	require.NoError(t, err)
	response, ok := data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "sent_result_unconfirmed", response["result"])
	require.Equal(t, false, response["responseRequired"])
	require.Equal(t, false, response["deviceAcknowledged"])
	sender.mu.Lock()
	require.Contains(t, string(sender.body), "<TargetTrack>Manual</TargetTrack>")
	require.Equal(t, 1, sender.calls)
	sender.mu.Unlock()

	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", ptzTestChannel).Update("capabilities", "").Error)
	_, err = dispatcher.Handle(context.Background(), auth.PTZRequest{
		Scope: auth.PTZTargetTrackScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "track-unsupported",
		Body: []byte(`{"mode":"auto"}`),
	})
	require.ErrorIs(t, err, auth.ErrPTZUnsupported)
	require.Equal(t, 1, sender.calls, "未显式上报支持时不得发命令")
}

func TestDispatcherTargetTrackRejectsOutOfScopePanoramicChannel(t *testing.T) {
	sender := &dispatcherSender{}
	db, dispatcher := newDispatcherFixture(t, sender)
	capabilities := `{"target_track":true}`
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", ptzTestDevice).Update("effective_version", "2022").Error)
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", ptzTestChannel).Update("capabilities", capabilities).Error)
	request := auth.PTZRequest{
		Scope: auth.PTZTargetTrackScope, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment,
		DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: "track-cross-resource",
		Body: []byte(`{"mode":"auto","deviceId2":"37011200001310000999"}`),
	}
	_, err := dispatcher.Handle(context.Background(), request)
	require.ErrorIs(t, err, auth.ErrPTZNotFound)
	require.Zero(t, sender.calls)
}
