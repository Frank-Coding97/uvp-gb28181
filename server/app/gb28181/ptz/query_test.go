package ptz

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type cancellingTrackedSender struct{ cancel context.CancelFunc }

func (s cancellingTrackedSender) SendMessageTracked(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
	s.cancel()
	return uac.TrackedMessageResult{}, errors.New("deadline")
}

func newPTZQueryTestService(t *testing.T, sender TrackedSender) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}, &gbmodels.GbPTZState{},
		&gbmodels.GbPTZPreset{}, &gbmodels.GbPTZCruiseTrack{}, &gbmodels.GbPTZHomePosition{}, &gbmodels.GbChannel{},
	))
	now := func() time.Time { return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC) }
	service, err := NewService(db, sender, now)
	require.NoError(t, err)
	return service, db
}

func TestServiceRefresh_CreatesDurableResponseQueryOperations(t *testing.T) {
	tests := []struct {
		kind    QueryKind
		trackID int
	}{
		{kind: QueryPreset},
		{kind: QueryCruiseTrackList},
		{kind: QueryCruiseTrack, trackID: 1},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			sender := &fakeTrackedSender{}
			svc, _ := newPTZQueryTestService(t, sender)

			op, err := svc.Refresh(context.Background(), testTarget(), tt.kind, tt.trackID, "refresh-"+string(tt.kind))
			require.NoError(t, err)
			require.Equal(t, gbmodels.PTZOperationQueued, op.Status)
			require.True(t, op.ResponseRequired)
			require.Equal(t, 3, op.MaxAttempts)
			require.NotNil(t, op.QueueDeadlineAt)
			require.Zero(t, sender.calls)
			body, err := buildScheduledPTZBody(op)
			require.NoError(t, err)
			head, err := manscdp.ParseHead(body)
			require.NoError(t, err)
			require.Equal(t, op.CmdType, head.CmdType)
			require.Equal(t, op.SN, headSN(*head))
			require.Equal(t, "C", head.DeviceID)
		})
	}
}

func TestServiceRefresh_RejectsOfflineBeforeSending(t *testing.T) {
	sender := &fakeTrackedSender{}
	svc, _ := newPTZQueryTestService(t, sender)
	target := testTarget()
	target.ChannelOnline = false
	_, err := svc.Refresh(context.Background(), target, QueryPreset, 0, "offline")
	require.Error(t, err)
	require.Zero(t, sender.calls)
}

func TestServiceOnPTZMessage_PersistsCompletePresetResponse(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	op, err := svc.Refresh(context.Background(), testTarget(), QueryPreset, 0, "refresh-presets")
	require.NoError(t, err)

	var items strings.Builder
	for i := 1; i <= 20; i++ {
		items.WriteString("<Item><PresetID>" + strconv.Itoa(i) + "</PresetID><PresetName>P" + strconv.Itoa(i) + "</PresetName></Item>")
	}
	body := []byte(`<Response><CmdType>PresetQuery</CmdType><SN>` + strconv.Itoa(op.SN) + `</SN><DeviceID>C</DeviceID><SumNum>20</SumNum><PresetList Num="20">` + items.String() + `</PresetList></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply-call", "2", body))

	var presets []gbmodels.GbPTZPreset
	require.NoError(t, db.Order("preset_id").Find(&presets).Error)
	require.Len(t, presets, 20)
	require.Equal(t, 20, presets[19].PresetID)
	require.Equal(t, gbmodels.PTZPresetActive, presets[19].Status)
	require.Equal(t, op.OperationID, presets[19].LastOperationID)

	stored, err := svc.GetOperation(context.Background(), op.OperationID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
}

func TestServiceRefreshPreciseStatus2022UsesPTZPosition(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	target := testTarget()
	target.Profile = protocol.ProfileFor(protocol.Version2022)
	op, err := svc.Refresh(context.Background(), target, QueryPreciseStatus, 0, "refresh-precise-2022")
	require.NoError(t, err)
	require.Equal(t, manscdp.CmdPTZPosition, op.CmdType)
	require.True(t, op.ResponseRequired)
	body := []byte("<Response><CmdType>PTZPosition</CmdType><SN>" + strconv.Itoa(op.SN) + "</SN><DeviceID>C</DeviceID><Pan>12.5</Pan></Response>")
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply", "2", body))
	var state gbmodels.GbPTZState
	require.NoError(t, db.First(&state).Error)
	require.NotNil(t, state.Pan)
	require.Equal(t, 12.5, *state.Pan)
	stored, err := svc.GetOperation(context.Background(), op.OperationID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
}

func TestServiceOnPTZMessage_LateQueryResponseDoesNotWriteCache(t *testing.T) {
	tests := []struct {
		name    string
		kind    QueryKind
		trackID int
		profile protocol.Profile
		body    func(sn int) []byte
		assert  func(t *testing.T, db *gorm.DB)
	}{
		{
			name: "preset",
			kind: QueryPreset,
			body: func(sn int) []byte {
				return []byte(`<Response><CmdType>PresetQuery</CmdType><SN>` + strconv.Itoa(sn) + `</SN><DeviceID>C</DeviceID><SumNum>1</SumNum><PresetList Num="1"><Item><PresetID>1</PresetID><PresetName>late</PresetName></Item></PresetList></Response>`)
			},
			assert: func(t *testing.T, db *gorm.DB) {
				var count int64
				require.NoError(t, db.Model(&gbmodels.GbPTZPreset{}).Count(&count).Error)
				require.Zero(t, count)
			},
		},
		{
			name: "cruise list",
			kind: QueryCruiseTrackList,
			body: func(sn int) []byte {
				return []byte(`<Response><CmdType>CruiseTrackListQuery</CmdType><SN>` + strconv.Itoa(sn) + `</SN><DeviceID>C</DeviceID><SumNum>1</SumNum><CruiseTrackList Num="1"><CruiseTrack><Number>0</Number><Name>late</Name></CruiseTrack></CruiseTrackList></Response>`)
			},
			assert: func(t *testing.T, db *gorm.DB) {
				var count int64
				require.NoError(t, db.Model(&gbmodels.GbPTZCruiseTrack{}).Count(&count).Error)
				require.Zero(t, count)
			},
		},
		{
			name:    "cruise detail",
			kind:    QueryCruiseTrack,
			trackID: 0,
			body: func(sn int) []byte {
				return []byte(`<Response><CmdType>CruiseTrackQuery</CmdType><SN>` + strconv.Itoa(sn) + `</SN><DeviceID>C</DeviceID><Number>0</Number><Name>late</Name><SumNum>1</SumNum><CruisePointList Num="1"><CruisePoint><PresetIndex>1</PresetIndex><StayTime>1</StayTime><Speed>1</Speed></CruisePoint></CruisePointList></Response>`)
			},
			assert: func(t *testing.T, db *gorm.DB) {
				var count int64
				require.NoError(t, db.Model(&gbmodels.GbPTZCruiseTrack{}).Count(&count).Error)
				require.Zero(t, count)
			},
		},
		{
			name:    "ptz position",
			kind:    QueryPreciseStatus,
			profile: protocol.ProfileFor(protocol.Version2022),
			body: func(sn int) []byte {
				return []byte(`<Response><CmdType>PTZPosition</CmdType><SN>` + strconv.Itoa(sn) + `</SN><DeviceID>C</DeviceID><Pan>12.5</Pan></Response>`)
			},
			assert: func(t *testing.T, db *gorm.DB) {
				var count int64
				require.NoError(t, db.Model(&gbmodels.GbPTZState{}).Count(&count).Error)
				require.Zero(t, count)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
			target := testTarget()
			target.Profile = tt.profile
			op, err := svc.Refresh(context.Background(), target, tt.kind, tt.trackID, "late-"+tt.name)
			require.NoError(t, err)
			before := storedOperation(t, db, op.OperationID)
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).
				Where("id = ?", op.ID).
				Update("status", gbmodels.PTZOperationTimeout).Error)

			require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "late-response", "99", tt.body(op.SN)))
			after := storedOperation(t, db, op.OperationID)
			require.Equal(t, before.OperationID, after.OperationID)
			require.Equal(t, gbmodels.PTZOperationTimeout, after.Status)
			require.Equal(t, before.ResponseAt, after.ResponseAt)
			require.Equal(t, before.ResponseCallID, after.ResponseCallID)
			require.Equal(t, before.ResponseCSeq, after.ResponseCSeq)
			tt.assert(t, db)
		})
	}
}

func TestServiceOnPTZMessage_LatePTZPositionFromOlderOperationDoesNotReplaceNewState(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	target := testTarget()
	target.Profile = protocol.ProfileFor(protocol.Version2022)
	oldOperation, err := svc.Refresh(context.Background(), target, QueryPreciseStatus, 0, "old-ptz-position")
	require.NoError(t, err)
	newOperation, err := svc.Refresh(context.Background(), target, QueryPreciseStatus, 0, "new-ptz-position")
	require.NoError(t, err)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", oldOperation.ID).
		Update("status", gbmodels.PTZOperationTimeout).Error)

	newBody := []byte(`<Response><CmdType>PTZPosition</CmdType><SN>` + strconv.Itoa(newOperation.SN) + `</SN><DeviceID>C</DeviceID><Pan>10</Pan></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "new-response", "1", newBody))
	var state gbmodels.GbPTZState
	require.NoError(t, db.First(&state).Error)
	require.NotNil(t, state.Pan)
	require.Equal(t, 10.0, *state.Pan)

	oldBody := []byte(`<Response><CmdType>PTZPosition</CmdType><SN>` + strconv.Itoa(oldOperation.SN) + `</SN><DeviceID>C</DeviceID><Pan>99</Pan></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "old-response", "2", oldBody))
	require.NoError(t, db.First(&state).Error)
	require.NotNil(t, state.Pan)
	require.Equal(t, 10.0, *state.Pan)
	require.Equal(t, gbmodels.PTZOperationTimeout, storedOperation(t, db, oldOperation.OperationID).Status)
	require.Equal(t, gbmodels.PTZOperationAccepted, storedOperation(t, db, newOperation.OperationID).Status)
}

func TestServiceOnPTZMessage_UnknownOlderPTZPositionDoesNotReplaceNewState(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	target := testTarget()
	target.Profile = protocol.ProfileFor(protocol.Version2022)
	oldOperation, err := svc.Refresh(context.Background(), target, QueryPreciseStatus, 0, "unknown-old-position")
	require.NoError(t, err)
	newOperation, err := svc.Refresh(context.Background(), target, QueryPreciseStatus, 0, "accepted-new-position")
	require.NoError(t, err)
	transportDeadline := time.Date(2026, 7, 22, 12, 1, 0, 0, time.UTC)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", oldOperation.ID).
		Updates(map[string]interface{}{"status": gbmodels.PTZOperationUnknown, "transport_deadline_at": transportDeadline}).Error)

	newBody := []byte(`<Response><CmdType>PTZPosition</CmdType><SN>` + strconv.Itoa(newOperation.SN) + `</SN><DeviceID>C</DeviceID><Pan>10</Pan></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "new-response", "1", newBody))
	oldBody := []byte(`<Response><CmdType>PTZPosition</CmdType><SN>` + strconv.Itoa(oldOperation.SN) + `</SN><DeviceID>C</DeviceID><Pan>99</Pan></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "old-response", "2", oldBody))

	var state gbmodels.GbPTZState
	require.NoError(t, db.First(&state).Error)
	require.NotNil(t, state.Pan)
	require.Equal(t, 10.0, *state.Pan)
	require.Equal(t, gbmodels.PTZOperationAccepted, storedOperation(t, db, oldOperation.OperationID).Status)
}

func TestServiceOnPTZMessage_ExpiredUnknownPositionDoesNotWriteState(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	target := testTarget()
	target.Profile = protocol.ProfileFor(protocol.Version2022)
	operation, err := svc.Refresh(context.Background(), target, QueryPreciseStatus, 0, "expired-unknown-position")
	require.NoError(t, err)
	expired := time.Date(2026, 7, 22, 11, 59, 59, 0, time.UTC)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", operation.ID).
		Updates(map[string]interface{}{"status": gbmodels.PTZOperationUnknown, "transport_deadline_at": expired}).Error)

	body := []byte(`<Response><CmdType>PTZPosition</CmdType><SN>` + strconv.Itoa(operation.SN) + `</SN><DeviceID>C</DeviceID><Pan>99</Pan></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "late", "2", body))
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZState{}).Count(&count).Error)
	require.Zero(t, count)
	require.Equal(t, gbmodels.PTZOperationUnknown, storedOperation(t, db, operation.OperationID).Status)
}

func TestServiceOnPTZMessage_AccumulatesFragmentedPresetResponse(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	require.NoError(t, db.Create(&gbmodels.GbPTZPreset{
		DeviceID: 2, ChannelID: 1, PresetID: 99, Name: "cached", Status: gbmodels.PTZPresetActive,
	}).Error)
	op, err := svc.Refresh(context.Background(), testTarget(), QueryPreset, 0, "fragmented-presets")
	require.NoError(t, err)
	fragment := func(start, end int) []byte {
		var items strings.Builder
		for i := start; i <= end; i++ {
			items.WriteString("<Item><PresetID>" + strconv.Itoa(i) + "</PresetID><PresetName>P" + strconv.Itoa(i) + "</PresetName></Item>")
		}
		return []byte(`<Response><CmdType>PresetQuery</CmdType><SN>` + strconv.Itoa(op.SN) + `</SN><DeviceID>C</DeviceID><SumNum>20</SumNum><PresetList Num="` + strconv.Itoa(end-start+1) + `">` + items.String() + `</PresetList></Response>`)
	}
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply-1", "2", fragment(1, 10)))
	var before []gbmodels.GbPTZPreset
	require.NoError(t, db.Where("channel_id = ? AND status = ?", 1, gbmodels.PTZPresetActive).Find(&before).Error)
	require.Len(t, before, 1, "半份预置位响应不能进入正式缓存")
	require.Equal(t, 99, before[0].PresetID)
	require.Equal(t, gbmodels.PTZOperationQueued, storedOperation(t, db, op.OperationID).Status)
	require.Len(t, svc.queryStages, 1)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply-2", "3", fragment(11, 20)))
	var active int64
	require.NoError(t, db.Model(&gbmodels.GbPTZPreset{}).Where("channel_id = ? AND status = ?", 1, gbmodels.PTZPresetActive).Count(&active).Error)
	require.EqualValues(t, 20, active)
	require.Empty(t, svc.queryStages)
}

func TestServiceQueryStageIsDiscardedOnTimeoutAndCancellation(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	partialBody := func(operation gbmodels.GbPTZOperation, presetID int) []byte {
		return []byte(`<Response><CmdType>PresetQuery</CmdType><SN>` + strconv.Itoa(operation.SN) + `</SN><DeviceID>C</DeviceID><SumNum>2</SumNum><PresetList Num="1"><Item><PresetID>` + strconv.Itoa(presetID) + `</PresetID><PresetName>P</PresetName></Item></PresetList></Response>`)
	}

	timedOut, err := svc.Refresh(context.Background(), testTarget(), QueryPreset, 0, "stage-timeout")
	require.NoError(t, err)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "timeout-page", "1", partialBody(timedOut, 1)))
	require.Len(t, svc.queryStages, 1)
	scheduler := NewScheduler(svc, WithSchedulerDispatcher(&schedulerManualDispatcher{capacity: 1}))
	require.NoError(t, scheduler.RunDue(time.Date(2026, 7, 22, 12, 0, 6, 0, time.UTC)))
	require.Empty(t, svc.queryStages)
	require.Equal(t, gbmodels.PTZOperationRejected, storedOperation(t, db, timedOut.OperationID).Status)

	cancelled, err := svc.Refresh(context.Background(), testTarget(), QueryPreset, 0, "stage-cancelled")
	require.NoError(t, err)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "cancel-page", "1", partialBody(cancelled, 2)))
	require.Len(t, svc.queryStages, 1)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", cancelled.ID).
		Update("status", gbmodels.PTZOperationCancelled).Error)
	require.NoError(t, svc.cleanupQueryStages(context.Background(), time.Date(2026, 7, 22, 12, 0, 1, 0, time.UTC)))
	require.Empty(t, svc.queryStages)
}

func TestServiceRestartDropsPartialStageWithoutPollutingPresetCache(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	require.NoError(t, db.Create(&gbmodels.GbPTZPreset{
		DeviceID: 2, ChannelID: 1, PresetID: 99, Name: "cached", Status: gbmodels.PTZPresetActive,
	}).Error)
	operation, err := svc.Refresh(context.Background(), testTarget(), QueryPreset, 0, "stage-restart")
	require.NoError(t, err)
	page := func(id int) []byte {
		return []byte(`<Response><CmdType>PresetQuery</CmdType><SN>` + strconv.Itoa(operation.SN) + `</SN><DeviceID>C</DeviceID><SumNum>2</SumNum><PresetList Num="1"><Item><PresetID>` + strconv.Itoa(id) + `</PresetID><PresetName>P</PresetName></Item></PresetList></Response>`)
	}
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "page-1", "1", page(1)))
	require.Len(t, svc.queryStages, 1)
	svc.Retire()
	require.Empty(t, svc.queryStages)

	reloaded, err := NewService(db, &fakeTrackedSender{}, func() time.Time {
		return time.Date(2026, 7, 22, 12, 0, 1, 0, time.UTC)
	})
	require.NoError(t, err)
	require.NoError(t, reloaded.OnPTZMessage(context.Background(), "D", "page-2", "2", page(2)))
	require.Equal(t, gbmodels.PTZOperationQueued, storedOperation(t, db, operation.OperationID).Status)
	var active []gbmodels.GbPTZPreset
	require.NoError(t, db.Where("channel_id = ? AND status = ?", 1, gbmodels.PTZPresetActive).Find(&active).Error)
	require.Len(t, active, 1)
	require.Equal(t, 99, active[0].PresetID)
}

func TestServiceOnPTZMessage_AppliesConfirmedPresetDeleteToCache(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	require.NoError(t, db.Create(&gbmodels.GbPTZPreset{DeviceID: 2, ChannelID: 1, PresetID: 3, Status: gbmodels.PTZPresetActive}).Error)
	op, err := svc.Execute(context.Background(), testTarget(), Command{
		CmdType: "DeviceControl", Action: "preset_delete", IdempotencyKey: "delete-3",
		Payload: map[string]interface{}{"id": 3},
		Build:   func(int) ([]byte, error) { return []byte("<Control/>"), nil },
	})
	require.NoError(t, err)
	body := []byte(`<Response><CmdType>DeviceControl</CmdType><SN>` + strconv.Itoa(op.SN) + `</SN><DeviceID>C</DeviceID><Result>OK</Result></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply", "2", body))
	var preset gbmodels.GbPTZPreset
	require.NoError(t, db.Where("channel_id = ? AND preset_id = ?", 1, 3).First(&preset).Error)
	require.Equal(t, gbmodels.PTZPresetDeleted, preset.Status)
	require.Equal(t, op.OperationID, preset.LastOperationID)
}

func TestServiceOnPTZMessage_PersistsHomeCruiseAndPreciseCaches(t *testing.T) {
	tests := []struct {
		name  string
		kind  QueryKind
		track int
		body  func(sn int) []byte
		check func(*testing.T, *gorm.DB)
	}{
		{
			name: "home", kind: QueryHomePosition,
			body: func(sn int) []byte {
				return []byte(`<Response><CmdType>HomePositionQuery</CmdType><SN>` + strconv.Itoa(sn) + `</SN><DeviceID>C</DeviceID><HomePosition><Enabled>0</Enabled><ResetTime>0</ResetTime><PresetIndex>0</PresetIndex></HomePosition></Response>`)
			},
			check: func(t *testing.T, db *gorm.DB) {
				var home gbmodels.GbPTZHomePosition
				require.NoError(t, db.First(&home).Error)
				require.False(t, home.Enabled)
				require.NotNil(t, home.ResetTime)
				require.Zero(t, *home.ResetTime)
				require.NotNil(t, home.PresetID)
				require.Zero(t, *home.PresetID)
				require.Equal(t, gbmodels.PTZHomePositionVerificationVerified, home.Verification)
				var preciseCount int64
				require.NoError(t, db.Model(&gbmodels.GbPTZState{}).Count(&preciseCount).Error)
				require.Zero(t, preciseCount)
			},
		},
		{
			name: "cruise list", kind: QueryCruiseTrackList,
			body: func(sn int) []byte {
				return []byte(`<Response><CmdType>CruiseTrackListQuery</CmdType><SN>` + strconv.Itoa(sn) + `</SN><DeviceID>C</DeviceID><SumNum>1</SumNum><CruiseTrackList Num="1"><CruiseTrack><Number>0</Number><Name>T0</Name></CruiseTrack></CruiseTrackList></Response>`)
			},
			check: func(t *testing.T, db *gorm.DB) {
				var track gbmodels.GbPTZCruiseTrack
				require.NoError(t, db.Where("track_id = ?", 0).First(&track).Error)
				require.Equal(t, "T0", track.Name)
			},
		},
		{
			name: "cruise detail", kind: QueryCruiseTrack, track: 0,
			body: func(sn int) []byte {
				return []byte(`<Response><CmdType>CruiseTrackQuery</CmdType><SN>` + strconv.Itoa(sn) + `</SN><DeviceID>C</DeviceID><Number>0</Number><Name>T0</Name><SumNum>1</SumNum><CruisePointList Num="1"><CruisePoint><PresetIndex>3</PresetIndex><StayTime>5</StayTime><Speed>8</Speed></CruisePoint></CruisePointList></Response>`)
			},
			check: func(t *testing.T, db *gorm.DB) {
				var track gbmodels.GbPTZCruiseTrack
				require.NoError(t, db.Where("track_id = ?", 0).First(&track).Error)
				require.Equal(t, "T0", track.Name)
				require.JSONEq(t, `{"trackId":0,"name":"T0","sumNum":1,"cruisePoints":[{"presetIndex":3,"stayTime":5,"speed":8}]}`, track.DetailJSON)
			},
		},
		{
			name: "precise", kind: QueryPreciseStatus,
			body: func(sn int) []byte {
				return []byte(`<Response><CmdType>PTZPreciseStatusQuery</CmdType><SN>` + strconv.Itoa(sn) + `</SN><DeviceID>C</DeviceID><Pan>12.5</Pan></Response>`)
			},
			check: func(t *testing.T, db *gorm.DB) {
				var state gbmodels.GbPTZState
				require.NoError(t, db.First(&state).Error)
				require.Equal(t, 12.5, *state.Pan)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
			op, err := svc.Refresh(context.Background(), testTarget(), tt.kind, tt.track, "refresh-"+tt.name)
			require.NoError(t, err)
			require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply", "2", tt.body(op.SN)))
			tt.check(t, db)
		})
	}
}

func TestServiceOnPTZMessage_CruiseListPreservesDetailAndRemovesMissing(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	enabled := true
	receivedDetail := `{"trackId":0,"name":"old","sumNum":1,"cruisePoints":[{"presetIndex":3,"stayTime":5,"speed":8}]}`
	require.NoError(t, db.Create(&gbmodels.GbPTZCruiseTrack{
		DeviceID: 2, ChannelID: 1, TrackID: 0, Name: "old", Enabled: &enabled, DetailJSON: receivedDetail,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbPTZCruiseTrack{
		DeviceID: 2, ChannelID: 1, TrackID: 1, Name: "missing",
	}).Error)

	op, err := svc.Refresh(context.Background(), testTarget(), QueryCruiseTrackList, 0, "refresh-cruise-list")
	require.NoError(t, err)
	body := []byte(`<Response><CmdType>CruiseTrackListQuery</CmdType><SN>` + strconv.Itoa(op.SN) + `</SN><DeviceID>C</DeviceID><SumNum>1</SumNum><CruiseTrackList Num="1"><CruiseTrack><Number>0</Number><Name>T0</Name></CruiseTrack></CruiseTrackList></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply", "2", body))

	var kept gbmodels.GbPTZCruiseTrack
	require.NoError(t, db.Where("channel_id = ? AND track_id = ?", 1, 0).First(&kept).Error)
	// ⛔ 名字**保持平台侧的值**(`old`),不被设备应答里的 `T0` 覆盖。
	//
	// 这不是"少更新了一列",是有意的:名字归平台所有。设备应答里那个 `<Name>` 是
	// 设备**自己编的**(本仓模拟器回"巡航 N",真实球机回出厂名),而 2022 版对云台的
	// 增强只加了**查询与应答**(标准修订说明:9.5.3、A.2.4.10~A.2.4.14、
	// A.2.6.12~A.2.6.16)—— 没有任何"平台把巡航名字/配置下发给设备"的路径。
	// 既然设备的名字不是操作员设的,用它覆盖就等于**把操作员在平台敲的名字弄丢**,
	// 表现为"我明明写了车间巡检,一秒后自己变成巡航 1"。
	//
	// 平台还没有这一行时(纯设备侧发现的轨迹)仍然采用设备名,见下一条用例。
	require.Equal(t, "old", kept.Name)
	require.Equal(t, receivedDetail, kept.DetailJSON)
	require.NotNil(t, kept.Enabled)
	// 设备这次**没报** `<Enabled>` → 保留库里原值 true,而不是被平台默认成某个值
	require.True(t, *kept.Enabled)

	var missingCount int64
	require.NoError(t, db.Model(&gbmodels.GbPTZCruiseTrack{}).Where("channel_id = ? AND track_id = ?", 1, 1).Count(&missingCount).Error)
	require.Zero(t, missingCount)
}

// ⛔ 设备回**空表**时,列表查询不得裁剪任何行。
//
// `finalizeQueryStageTx` 原来在 `len(ids) == 0` 时把 `NOT IN` 条件整个跳过,于是
// 「设备回 0 条」退化成「删掉本通道全部巡航轨迹」。而空表恰恰是**最不可信**的答案:
// 没实现这条 2022 命令的设备、被工厂复位过的设备、应答被截断的设备,回的都是一张
// 空表。这一帧的触发点现在只是操作员点了一下「同步」,拿它做**硬删除**不可逆 ——
// 预置位那边至少是软删除(status=deleted)还能改回来,巡航轨迹删了就连点位链一起没了。
func TestServiceOnPTZMessage_CruiseEmptyListKeepsPlatformTracks(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	enabled := false
	detail := `{"trackId":3,"name":"车间巡检","sumNum":1,"cruisePoints":[{"presetIndex":3,"stayTime":30,"speed":128}]}`
	for _, trackID := range []int{0, 3} {
		require.NoError(t, db.Create(&gbmodels.GbPTZCruiseTrack{
			DeviceID: 2, ChannelID: 1, TrackID: trackID, Name: "车间巡检", Enabled: &enabled, DetailJSON: detail,
		}).Error)
	}

	op, err := svc.Refresh(context.Background(), testTarget(), QueryCruiseTrackList, 0, "refresh-cruise-empty")
	require.NoError(t, err)
	body := []byte(`<Response><CmdType>CruiseTrackListQuery</CmdType><SN>` + strconv.Itoa(op.SN) +
		`</SN><DeviceID>C</DeviceID><SumNum>0</SumNum><CruiseTrackList Num="0"/></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply", "2", body))

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZCruiseTrack{}).Where("channel_id = ?", 1).Count(&count).Error)
	require.EqualValues(t, 2, count, "设备回空表不得删除平台侧已有的巡航轨迹")

	var kept gbmodels.GbPTZCruiseTrack
	require.NoError(t, db.Where("channel_id = ? AND track_id = ?", 1, 3).First(&kept).Error)
	require.Equal(t, "车间巡检", kept.Name)
	require.Equal(t, detail, kept.DetailJSON, "空表应答不得抹掉已经抓到的点位链")

	// 跳过裁剪**不能**把 operation 卡在 queued:空表是一个完整的("0 条")答案,
	// 仍按"设备已应答"收敛,否则前端会一直转圈。
	require.Equal(t, gbmodels.PTZOperationAccepted, storedOperation(t, db, op.OperationID).Status)
}

// 同上,预置位分支:空表不得把本通道预置位**全部**标记为已删除。
func TestServiceOnPTZMessage_PresetEmptyListKeepsPlatformPresets(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	for _, presetID := range []int{1, 2} {
		require.NoError(t, db.Create(&gbmodels.GbPTZPreset{
			DeviceID: 2, ChannelID: 1, PresetID: presetID, Name: "P", Status: gbmodels.PTZPresetActive,
		}).Error)
	}

	op, err := svc.Refresh(context.Background(), testTarget(), QueryPreset, 0, "refresh-presets-empty")
	require.NoError(t, err)
	body := []byte(`<Response><CmdType>PresetQuery</CmdType><SN>` + strconv.Itoa(op.SN) +
		`</SN><DeviceID>C</DeviceID><SumNum>0</SumNum><PresetList Num="0"/></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply", "2", body))

	var active int64
	require.NoError(t, db.Model(&gbmodels.GbPTZPreset{}).
		Where("channel_id = ? AND status = ?", 1, gbmodels.PTZPresetActive).Count(&active).Error)
	require.EqualValues(t, 2, active, "设备回空表不得把平台侧预置位全部标记为已删除")
	require.Equal(t, gbmodels.PTZOperationAccepted, storedOperation(t, db, op.OperationID).Status)
}

// ⛔ 预置位名字归**平台**所有:设备应答里的 `<PresetName>` 只在**新建行**时采用。
//
// 反例(修复前):对账 PresetQuery 一来就把 `name` 一起 UPSERT,操作员在平台敲的
// 「大门」被设备的出厂名 "Preset 1" 覆盖。触发点是后台异步对账,所以界面会先显示
// 正确、过一会儿自己变掉,看起来像平台把用户的输入弄丢了。
func TestServiceOnPTZMessage_PresetQueryKeepsPlatformName(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	// 平台已经有这两行(操作员创建时填过名字);设备侧无从知道这些名字:
	// 设置预置位的 PTZCmd(`0x81`)只带编号,payload 里的 name 从不进 wire。
	require.NoError(t, db.Create(&gbmodels.GbPTZPreset{
		DeviceID: 2, ChannelID: 1, PresetID: 1, Name: "大门", Status: gbmodels.PTZPresetActive,
	}).Error)
	// 第二行专门覆盖「设备回空 `<PresetName>`」这种更隐蔽的抹除(大量球机如此)。
	require.NoError(t, db.Create(&gbmodels.GbPTZPreset{
		DeviceID: 2, ChannelID: 1, PresetID: 2, Name: "后门", Status: gbmodels.PTZPresetActive,
	}).Error)

	op, err := svc.Refresh(context.Background(), testTarget(), QueryPreset, 0, "refresh-presets-name")
	require.NoError(t, err)
	body := []byte(`<Response><CmdType>PresetQuery</CmdType><SN>` + strconv.Itoa(op.SN) +
		`</SN><DeviceID>C</DeviceID><SumNum>3</SumNum><PresetList Num="3">` +
		`<Item><PresetID>1</PresetID><PresetName>Preset 1</PresetName></Item>` +
		`<Item><PresetID>2</PresetID><PresetName></PresetName></Item>` +
		`<Item><PresetID>5</PresetID><PresetName>Preset 5</PresetName></Item>` +
		`</PresetList></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply", "2", body))

	var kept gbmodels.GbPTZPreset
	require.NoError(t, db.Where("channel_id = ? AND preset_id = ?", 1, 1).First(&kept).Error)
	require.Equal(t, "大门", kept.Name, "设备报的出厂名不得覆盖操作员填的名字")

	var blanked gbmodels.GbPTZPreset
	require.NoError(t, db.Where("channel_id = ? AND preset_id = ?", 1, 2).First(&blanked).Error)
	require.Equal(t, "后门", blanked.Name, "设备回空 <PresetName> 不得把平台名字写空")

	// 设备侧发现的新行(平台还没有这一行)仍采用设备报的名字 —— 那是唯一的名字来源。
	var discovered gbmodels.GbPTZPreset
	require.NoError(t, db.Where("channel_id = ? AND preset_id = ?", 1, 5).First(&discovered).Error)
	require.Equal(t, "Preset 5", discovered.Name)
	require.Equal(t, gbmodels.PTZPresetActive, discovered.Status)
	require.Equal(t, op.OperationID, discovered.LastOperationID)

	// 名字之外的字段该更新还得更新:半份列表/空表之外的正常路径不能被这次改动连累。
	require.Equal(t, gbmodels.PTZOperationAccepted, storedOperation(t, db, op.OperationID).Status)
}

// 平台还没这一行时(纯设备侧发现的巡航轨迹),采用设备报的名字,并且**不替设备编 Enabled**。
func TestServiceOnPTZMessage_CruiseListInsertsDeviceNameAndLeavesEnabledUnreported(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	op, err := svc.Refresh(context.Background(), testTarget(), QueryCruiseTrackList, 0, "refresh-cruise-list-discovered")
	require.NoError(t, err)
	// 应答里**没有** <Enabled> —— 标准 A.2.6.13 的元素表里找不到它,本仓当扩展字段用
	body := []byte(`<Response><CmdType>CruiseTrackListQuery</CmdType><SN>` + strconv.Itoa(op.SN) +
		`</SN><DeviceID>C</DeviceID><SumNum>1</SumNum><CruiseTrackList Num="1"><CruiseTrack>` +
		`<Number>5</Number><Name>球机默认轨迹</Name></CruiseTrack></CruiseTrackList></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply", "2", body))

	var created gbmodels.GbPTZCruiseTrack
	require.NoError(t, db.Where("channel_id = ? AND track_id = ?", 1, 5).First(&created).Error)
	require.Equal(t, "球机默认轨迹", created.Name, "平台还没有这一行时采用设备报的名字")
	// ⛔ 设备没报就是"未上报",不能默认成 true。原来这里默认 true,于是同一条轨迹的
	//    启用状态会因为**读不到这个字段**而自行翻转(创建时平台乐观写 false、对账一变 true),
	//    前端还拿它渲染 tile 的启用/禁用态。
	require.Nil(t, created.Enabled, "设备未上报 <Enabled> 时必须保持 NULL,不能替它编 true")
}

// ⛔ 平台建好的轨迹跑完「列表对账 + 详情对账」之后必须仍然**点得动**。
//
// 这是上一条用例的另一半,单独写是因为它要守的不是"落库取哪个值",而是整条链路的
// 可操作性:`enabled` 一旦被平台自己写成 false,就再没有任何东西能把它翻回来 ——
// `<Enabled>` 在 A.2.6.13/A.2.6.14 的元素表里没有(本仓当扩展字段用),真机与模拟器
// 都不回,`upsertCruiseTrack` 只会走"保留库里原值"那条分支,库里那个"原值"恰恰就是
// 平台写的 false;前端 `enabled: item.enabled !== false && !pending` 于是把 tile 焊死,
// 操作员再也点不动自己刚建好的轨迹(现场表现:「巡航轨迹是禁用状态没办法调用」)。
//
// 所以断言两条:① 两轮对账跑完 `enabled` 仍是 NULL(= 设备未上报,前端判为可点);
// ② "待对账"这个状态由 `detail_json.source` 表达 —— 详情一来它就该被真实点位链替换掉,
// 不能让前端永远停在「未验证」。
func TestServiceOnPTZMessage_CruiseReconcileKeepsPlatformTrackClickable(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	// 平台创建后的那一行:`enabled` 留空(设备未上报)、尚无点位链、待对账标记在 detail_json 里
	require.NoError(t, db.Create(&gbmodels.GbPTZCruiseTrack{
		DeviceID: 2, ChannelID: 1, TrackID: 1, Name: "车间巡检",
		DetailJSON: `{"trackId":1,"name":"车间巡检","stops":[{"presetId":2}],"source":"reconcile-pending"}`,
		RawSummary: "reconcile-pending",
	}).Error)

	// ① 列表对账:设备确认这条轨迹存在,但**没有** <Enabled>
	listOp, err := svc.Refresh(context.Background(), testTarget(), QueryCruiseTrackList, 0, "reconcile-cruise-list")
	require.NoError(t, err)
	listBody := []byte(`<Response><CmdType>CruiseTrackListQuery</CmdType><SN>` + strconv.Itoa(listOp.SN) +
		`</SN><DeviceID>C</DeviceID><SumNum>1</SumNum><CruiseTrackList Num="1"><CruiseTrack><Number>1</Number><Name>巡航 1</Name></CruiseTrack></CruiseTrackList></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply", "2", listBody))

	// ② 详情对账:点位链落库(前端据此解除「未验证」),但同样不得碰 enabled
	detailOp, err := svc.Refresh(context.Background(), testTarget(), QueryCruiseTrack, 1, "reconcile-cruise-detail")
	require.NoError(t, err)
	detailBody := []byte(`<Response><CmdType>CruiseTrackQuery</CmdType><SN>` + strconv.Itoa(detailOp.SN) +
		`</SN><DeviceID>C</DeviceID><Number>1</Number><Name>巡航 1</Name><SumNum>2</SumNum>` +
		`<CruisePointList Num="2"><CruisePoint><PresetIndex>2</PresetIndex><StayTime>5</StayTime><Speed>128</Speed></CruisePoint>` +
		`<CruisePoint><PresetIndex>3</PresetIndex><StayTime>5</StayTime><Speed>128</Speed></CruisePoint></CruisePointList></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply", "2", detailBody))

	var track gbmodels.GbPTZCruiseTrack
	require.NoError(t, db.Where("channel_id = ? AND track_id = ?", 1, 1).First(&track).Error)
	require.Nil(t, track.Enabled, "设备从没报过 <Enabled>,对账跑完也不该替它下结论 —— 写 false 就等于把这条轨迹焊死")
	require.Equal(t, "车间巡检", track.Name, "对账不得用设备出厂名覆盖平台名")
	require.Contains(t, track.DetailJSON, `"cruisePoints"`, "详情对账要把点位链抓回来")
	require.Contains(t, track.DetailJSON, `"presetIndex":2`)
	require.NotContains(t, track.DetailJSON, "reconcile-pending", "设备已确认,待对账标记该被真实详情替换")
}

func TestServiceOnPTZMessage_CruisePartialListPreservesUnreturnedTracks(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	enabled := true
	for _, trackID := range []int{0, 1} {
		require.NoError(t, db.Create(&gbmodels.GbPTZCruiseTrack{
			DeviceID: 2, ChannelID: 1, TrackID: trackID, Name: "cached", Enabled: &enabled,
		}).Error)
	}

	op, err := svc.Refresh(context.Background(), testTarget(), QueryCruiseTrackList, 0, "refresh-partial-cruise-list")
	require.NoError(t, err)
	body := []byte(`<Response><CmdType>CruiseTrackListQuery</CmdType><SN>` + strconv.Itoa(op.SN) + `</SN><DeviceID>C</DeviceID><SumNum>2</SumNum><CruiseTrackList Num="1"><CruiseTrack><Number>0</Number><Name>T0</Name></CruiseTrack></CruiseTrackList></Response>`)
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply", "2", body))

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZCruiseTrack{}).Where("channel_id = ?", 1).Count(&count).Error)
	require.EqualValues(t, 2, count, "部分列表不能删除本批未返回的缓存轨迹")
	var stagedTrack gbmodels.GbPTZCruiseTrack
	require.NoError(t, db.Where("channel_id = ? AND track_id = ?", 1, 0).First(&stagedTrack).Error)
	require.Equal(t, "cached", stagedTrack.Name, "半份巡航列表不能改写正式缓存")
	require.Len(t, svc.queryStages, 1)
}

func TestServiceOnPTZMessage_AccumulatesFragmentedCruiseListResponse(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	op, err := svc.Refresh(context.Background(), testTarget(), QueryCruiseTrackList, 0, "fragmented-cruise-list")
	require.NoError(t, err)
	fragment := func(trackID int) []byte {
		return []byte(`<Response><CmdType>CruiseTrackListQuery</CmdType><SN>` + strconv.Itoa(op.SN) + `</SN><DeviceID>C</DeviceID><SumNum>2</SumNum><CruiseTrackList Num="1"><CruiseTrack><Number>` + strconv.Itoa(trackID) + `</Number><Name>T` + strconv.Itoa(trackID) + `</Name></CruiseTrack></CruiseTrackList></Response>`)
	}

	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply-1", "2", fragment(0)))
	require.Equal(t, gbmodels.PTZOperationQueued, storedOperation(t, db, op.OperationID).Status)
	var partialCount int64
	require.NoError(t, db.Model(&gbmodels.GbPTZCruiseTrack{}).Count(&partialCount).Error)
	require.Zero(t, partialCount, "半份巡航列表不能进入正式缓存")
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply-2", "3", fragment(1)))
	require.Equal(t, gbmodels.PTZOperationAccepted, storedOperation(t, db, op.OperationID).Status)
	var tracks []gbmodels.GbPTZCruiseTrack
	require.NoError(t, db.Order("track_id").Find(&tracks).Error)
	require.Len(t, tracks, 2)
	require.Equal(t, []int{0, 1}, []int{tracks[0].TrackID, tracks[1].TrackID})
	require.Empty(t, svc.queryStages)
}

func TestServiceCruiseListStagesAreIsolatedByOperation(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	first, err := svc.Refresh(context.Background(), testTarget(), QueryCruiseTrackList, 0, "cruise-stage-first")
	require.NoError(t, err)
	second, err := svc.Refresh(context.Background(), testTarget(), QueryCruiseTrackList, 0, "cruise-stage-second")
	require.NoError(t, err)
	page := func(operation gbmodels.GbPTZOperation, trackID int) []byte {
		return []byte(`<Response><CmdType>CruiseTrackListQuery</CmdType><SN>` + strconv.Itoa(operation.SN) + `</SN><DeviceID>C</DeviceID><SumNum>2</SumNum><CruiseTrackList Num="1"><CruiseTrack><Number>` + strconv.Itoa(trackID) + `</Number><Name>T</Name></CruiseTrack></CruiseTrackList></Response>`)
	}

	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "first-1", "1", page(first, 0)))
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "second-1", "1", page(second, 1)))
	require.Len(t, svc.queryStages, 2)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZCruiseTrack{}).Count(&count).Error)
	require.Zero(t, count)

	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "first-2", "2", page(first, 1)))
	require.Equal(t, gbmodels.PTZOperationAccepted, storedOperation(t, db, first.OperationID).Status)
	require.Equal(t, gbmodels.PTZOperationQueued, storedOperation(t, db, second.OperationID).Status)
	require.Len(t, svc.queryStages, 1)
}
