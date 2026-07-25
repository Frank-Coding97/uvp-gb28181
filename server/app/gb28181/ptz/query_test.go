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
	require.Equal(t, "T0", kept.Name)
	require.Equal(t, receivedDetail, kept.DetailJSON)
	require.NotNil(t, kept.Enabled)
	require.True(t, *kept.Enabled)

	var missingCount int64
	require.NoError(t, db.Model(&gbmodels.GbPTZCruiseTrack{}).Where("channel_id = ? AND track_id = ?", 1, 1).Count(&missingCount).Error)
	require.Zero(t, missingCount)
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
