package dashboard

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestBuildMediaDashboardDeduplicatesSchemasAndSumsLiveValues(t *testing.T) {
	asOf := time.Date(2026, 9, 2, 4, 0, 0, 0, time.UTC)
	result := management.OverviewResult{
		AsOf: asOf,
		Streams: []management.RuntimeMedia{
			{NodeID: 2, Media: management.MediaIdentity{Schema: "rtsp", Vhost: "__defaultVhost__", App: "rtp", Stream: "stream-1"}, Online: true, BytesSpeed: 100, ReaderCount: 2, TotalReaderCount: 8, RecordingMP4: true},
			{NodeID: 2, Media: management.MediaIdentity{Schema: "rtmp", Vhost: "__defaultVhost__", App: "rtp", Stream: "stream-1"}, Online: true, BytesSpeed: 200, ReaderCount: 3, TotalReaderCount: 9},
			{NodeID: 3, Media: management.MediaIdentity{Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "stream-2"}, Online: true, BytesSpeed: 50, ReaderCount: 1},
		},
		Metrics: management.OverviewMetrics{NetworkSessionCount: 7, NetThreadLoadAvg: 0.42},
		Nodes: []management.NodeRuntimeView{
			{NodeID: 2, State: node.StateActive, MetricsComplete: true, Metrics: management.NodeRuntimeMetrics{EventThreadLoads: []management.RuntimeThreadLoad{{Name: "event-0", Load: 45}, {Name: "event-1", Load: 91}}}},
			{NodeID: 3, State: node.StateMaintenance},
			{NodeID: 4, State: node.StateOffline},
		},
		MetricsSampledNodeIDs: []int64{2},
		MediaSampledNodeIDs:   []int64{2, 3},
	}

	summary := BuildMediaDashboard(result)
	require.Equal(t, asOf, summary.AsOf)
	require.EqualValues(t, 2, summary.Runtime.Streams)
	require.EqualValues(t, 6, summary.Runtime.Viewers)
	require.EqualValues(t, 350, summary.Runtime.BytesPerSecond)
	require.EqualValues(t, 7, summary.Runtime.NetworkSessions)
	require.EqualValues(t, 1, summary.Runtime.Recording)
	require.Equal(t, 1, summary.Health.Active)
	require.Equal(t, 1, summary.Health.Maintenance)
	require.Equal(t, 1, summary.Health.Offline)
	require.Equal(t, 91, summary.Health.MaxEventThreadLoad)
	require.Equal(t, 1, summary.Health.HighLoadThreads)
	require.Len(t, summary.Streams, 2)
	require.Equal(t, "stream-1", summary.Streams[0].Stream)
	require.Equal(t, []string{"rtmp", "rtsp"}, summary.Streams[0].Schemas)
	require.Equal(t, 5, summary.Streams[0].Viewers)
	require.EqualValues(t, 300, summary.Streams[0].BytesPerSecond)
}

func TestBuildMediaDashboardMarksPartialAndUsesDeterministicRanking(t *testing.T) {
	result := management.OverviewResult{
		Partial: true,
		Streams: []management.RuntimeMedia{
			{NodeID: 2, Media: management.MediaIdentity{Schema: "rtsp", Vhost: "v", App: "a", Stream: "b"}, ReaderCount: 1, BytesSpeed: 10},
			{NodeID: 1, Media: management.MediaIdentity{Schema: "rtsp", Vhost: "v", App: "a", Stream: "a"}, ReaderCount: 1, BytesSpeed: 10},
		},
	}
	summary := BuildMediaDashboard(result)
	require.Equal(t, CoveragePartial, summary.Coverage)
	require.Equal(t, "a", summary.Streams[0].Stream)
	require.Equal(t, "b", summary.Streams[1].Stream)
}

func TestBuildMediaDashboardDoesNotTreatHLSOutputAsRecording(t *testing.T) {
	result := management.OverviewResult{
		Streams: []management.RuntimeMedia{
			{NodeID: 1, Media: management.MediaIdentity{Schema: "hls", Vhost: "v", App: "rtp", Stream: "stream-1"}, Online: true, RecordingHLS: true},
		},
	}

	summary := BuildMediaDashboard(result)
	require.Zero(t, summary.Runtime.Recording)
	require.False(t, summary.Streams[0].Recording)
}
