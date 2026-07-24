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

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
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
		&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZState{},
		&gbmodels.GbPTZPreset{}, &gbmodels.GbPTZCruiseTrack{}, &gbmodels.GbPTZHomePosition{}, &gbmodels.GbChannel{},
	))
	now := func() time.Time { return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC) }
	service, err := NewService(db, sender, now)
	require.NoError(t, err)
	return service, db
}

func TestServiceRefresh_CreatesTrackedQueryOperation(t *testing.T) {
	sender := &fakeTrackedSender{}
	svc, _ := newPTZQueryTestService(t, sender)

	op, err := svc.Refresh(context.Background(), testTarget(), QueryPreset, 0, "refresh-1")
	require.NoError(t, err)
	require.Equal(t, "PresetQuery", op.CmdType)
	require.Equal(t, "refresh_presets", op.Action)
	require.Equal(t, gbmodels.PTZOperationSent, op.Status)
	require.Equal(t, 1, sender.calls)
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

func TestServiceRefresh_ContextTimeoutPersistsTimeoutOperation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	svc, db := newPTZQueryTestService(t, cancellingTrackedSender{cancel: cancel})
	op, err := svc.Refresh(ctx, testTarget(), QueryPreset, 0, "timeout")
	require.Error(t, err)
	require.Equal(t, gbmodels.PTZOperationTimeout, op.Status)
	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", op.OperationID).First(&stored).Error)
	require.Equal(t, gbmodels.PTZOperationTimeout, stored.Status)
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

func TestServiceOnPTZMessage_AccumulatesFragmentedPresetResponse(t *testing.T) {
	svc, db := newPTZQueryTestService(t, &fakeTrackedSender{})
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
	require.NoError(t, svc.OnPTZMessage(context.Background(), "D", "reply-2", "3", fragment(11, 20)))
	var active int64
	require.NoError(t, db.Model(&gbmodels.GbPTZPreset{}).Where("channel_id = ? AND status = ?", 1, gbmodels.PTZPresetActive).Count(&active).Error)
	require.EqualValues(t, 20, active)
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
}
