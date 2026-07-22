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
		&gbmodels.GbPTZPreset{}, &gbmodels.GbPTZCruiseTrack{},
	))
	now := func() time.Time { return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC) }
	return NewService(db, sender, now), db
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

func TestServiceRefresh_RejectsOfflineAndUnknownCapabilityBeforeSending(t *testing.T) {
	sender := &fakeTrackedSender{}
	svc, _ := newPTZQueryTestService(t, sender)
	target := testTarget()
	target.ChannelOnline = false
	_, err := svc.Refresh(context.Background(), target, QueryPreset, 0, "offline")
	require.Error(t, err)
	target = testTarget()
	target.PTZType = 0
	_, err = svc.Refresh(context.Background(), target, QueryPreset, 0, "unknown-capability")
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
				return []byte(`<Response><CmdType>HomePositionQuery</CmdType><SN>` + strconv.Itoa(sn) + `</SN><DeviceID>C</DeviceID><Enabled>true</Enabled><Pan>1.5</Pan></Response>`)
			},
			check: func(t *testing.T, db *gorm.DB) {
				var state gbmodels.GbPTZState
				require.NoError(t, db.First(&state).Error)
				require.NotNil(t, state.HomeEnabled)
				require.True(t, *state.HomeEnabled)
				require.Equal(t, gbmodels.PTZFreshnessFresh, state.Freshness)
			},
		},
		{
			name: "cruise list", kind: QueryCruiseTrackList,
			body: func(sn int) []byte {
				return []byte(`<Response><CmdType>CruiseTrackListQuery</CmdType><SN>` + strconv.Itoa(sn) + `</SN><DeviceID>C</DeviceID><TrackList><Track><TrackID>7</TrackID><Name>T7</Name></Track></TrackList></Response>`)
			},
			check: func(t *testing.T, db *gorm.DB) {
				var track gbmodels.GbPTZCruiseTrack
				require.NoError(t, db.Where("track_id = ?", 7).First(&track).Error)
				require.Equal(t, "T7", track.Name)
			},
		},
		{
			name: "cruise detail", kind: QueryCruiseTrack, track: 7,
			body: func(sn int) []byte {
				return []byte(`<Response><CmdType>CruiseTrackQuery</CmdType><SN>` + strconv.Itoa(sn) + `</SN><DeviceID>C</DeviceID><Track><TrackID>7</TrackID><Name>Detail</Name></Track></Response>`)
			},
			check: func(t *testing.T, db *gorm.DB) {
				var track gbmodels.GbPTZCruiseTrack
				require.NoError(t, db.Where("track_id = ?", 7).First(&track).Error)
				require.Equal(t, "Detail", track.Name)
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
