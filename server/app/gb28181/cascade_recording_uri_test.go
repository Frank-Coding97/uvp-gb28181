package gb28181

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 上级用 `t=0 0` 表示「范围未指定」时，必须用设备录像清单把真实区间补出来。
//
// 这是 EasyGBS 3.7.451 的实际写法：早期解析直接判非法 ⇒ 每次点回放都收 488。
func TestResolveRecordingSourceFillsRangeWhenUpstreamOmitsIt(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, location)
	channelCode := "34020000001320000010"
	source := cascadeRecordingTarget{
		device:  gbmodels.GbDevice{DeviceID: "37010301021180000007", IP: "192.0.2.30", Port: 5060},
		channel: gbmodels.GbChannel{ChannelID: channelCode},
	}

	t.Run("with file uri", func(t *testing.T) {
		h := &cascadeVideoRuntime{recordingLocation: location}
		h.recordings = cascadeRecordingQueryFunc(func(_ context.Context, request recordquery.CascadeQueryRequest) (recordquery.QueryResult, error) {
			// 上级没给范围 ⇒ 查询窗口必须是「回溯窗口 .. now」，不能是零值。
			require.Equal(t, now.Add(-cascadeDefaultRecordingLookback), request.StartTime)
			require.Equal(t, now, request.EndTime)
			return recordquery.QueryResult{Records: []recordquery.Record{
				{RecordInfoItem: manscdp.RecordInfoItem{DeviceID: channelCode, FilePath: "/records/old.ps",
					StartTime: "2026-10-26T08:00:00", EndTime: "2026-10-26T09:00:00"}},
				{RecordInfoItem: manscdp.RecordInfoItem{DeviceID: channelCode, FilePath: "/records/wanted.ps",
					StartTime: "2026-10-26T10:00:00", EndTime: "2026-10-26T11:00:00"}},
			}}, nil
		})
		offer := sdp.CascadeVideoOffer{Playback: true, RangeUnknown: true, RecordingURI: channelCode + ":2"}
		got, err := h.resolveRecordingSource(context.Background(), source, 7, offer, now)
		require.NoError(t, err)
		require.True(t, got.RangeResolved)
		require.Equal(t, "http://37010301021180000007/records/wanted.ps", got.URI)
		require.Equal(t, "2026-10-26T10:00:00", got.Start.In(location).Format("2006-01-02T15:04:05"))
		require.Equal(t, "2026-10-26T11:00:00", got.End.In(location).Format("2006-01-02T15:04:05"))
	})

	t.Run("without uri picks latest recording", func(t *testing.T) {
		h := &cascadeVideoRuntime{recordingLocation: location}
		h.recordings = cascadeRecordingQueryFunc(func(_ context.Context, _ recordquery.CascadeQueryRequest) (recordquery.QueryResult, error) {
			return recordquery.QueryResult{Records: []recordquery.Record{
				{RecordInfoItem: manscdp.RecordInfoItem{DeviceID: channelCode, FilePath: "/records/old.ps",
					StartTime: "2026-10-26T08:00:00", EndTime: "2026-10-26T09:00:00"}},
				{RecordInfoItem: manscdp.RecordInfoItem{DeviceID: channelCode, FilePath: "/records/new.ps",
					StartTime: "2026-10-26T11:00:00", EndTime: "2026-10-26T11:30:00"}},
			}}, nil
		})
		got, err := h.resolveRecordingSource(context.Background(), source, 7, sdp.CascadeVideoOffer{Playback: true, RangeUnknown: true}, now)
		require.NoError(t, err)
		require.True(t, got.RangeResolved)
		require.Equal(t, "http://37010301021180000007/records/new.ps", got.URI)
		require.Equal(t, "2026-10-26T11:00:00", got.Start.In(location).Format("2006-01-02T15:04:05"))
	})

	t.Run("range unknown without any recording is rejected", func(t *testing.T) {
		h := &cascadeVideoRuntime{recordingLocation: location}
		h.recordings = cascadeRecordingQueryFunc(func(_ context.Context, _ recordquery.CascadeQueryRequest) (recordquery.QueryResult, error) {
			return recordquery.QueryResult{}, nil
		})
		_, err := h.resolveRecordingSource(context.Background(), source, 7, sdp.CascadeVideoOffer{Playback: true, RangeUnknown: true}, now)
		require.Error(t, err)
	})

	t.Run("known range without uri still skips the device query", func(t *testing.T) {
		// 有范围、无 u= 的旧路径不能被新逻辑带出一次多余的 RecordInfo 查询。
		h := &cascadeVideoRuntime{recordingLocation: location}
		h.recordings = cascadeRecordingQueryFunc(func(_ context.Context, _ recordquery.CascadeQueryRequest) (recordquery.QueryResult, error) {
			t.Fatal("device must not be queried when the upstream already sent a range and no file")
			return recordquery.QueryResult{}, nil
		})
		start := now.Add(-time.Hour)
		got, err := h.resolveRecordingSource(context.Background(), source, 7,
			sdp.CascadeVideoOffer{Playback: true, Start: start, End: now}, now)
		require.NoError(t, err)
		require.False(t, got.RangeResolved)
		require.Equal(t, start, got.Start)
		require.Equal(t, now, got.End)
	})
}
