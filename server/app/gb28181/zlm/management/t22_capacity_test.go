package management

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const (
	t22NodeCount      = 20
	t22StreamsPerNode = 500
	t22SessionCount   = 20_000
)

func TestT22CapacityBoundsTwentyThousandSessionPage(t *testing.T) {
	sessions := t22Sessions()
	service := NewSessionService(SessionServiceDependencies{
		Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}},
		Runtime:  &t9RuntimeReader{sessions: map[int64][]zlm.Session{1: sessions}},
	})

	page, err := service.ListNetworkSessions(context.Background(), NetworkSessionListRequest{
		NodeID: 1,
		Page:   PageRequest{Page: 1, PageSize: MaxPageSize},
	})
	require.NoError(t, err)
	require.Len(t, page.List, MaxPageSize)
	require.Equal(t, int64(MaxResponseItems), page.Total)
	require.True(t, page.Truncated)
	require.LessOrEqual(t, cap(page.List), MaxPageSize,
		"a response page must not retain the 20,000-item source backing array")
}

func TestT22CapacityBoundsTenThousandStreamsAndTwentyNodeWorkers(t *testing.T) {
	nodes, media := t22OverviewData()
	block := make(chan struct{})
	tracker := &overviewCallTracker{block: block, reached: make(chan struct{})}
	service := NewOverviewService(OverviewDependencies{
		Registry: overviewRegistryFake{nodes: nodes},
		Runtime:  &overviewRuntimeFake{},
		Media: &overviewMediaFake{
			tracker: tracker,
			media:   media,
			errors: map[int64]error{
				t22NodeCount: errors.New("upstream user:password@example.invalid?token=t22-secret"),
			},
		},
	})

	type outcome struct {
		result StreamDistribution
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := service.ListStreams(context.Background(), StreamFilter{}, PageRequest{Page: 1, PageSize: MaxPageSize})
		done <- outcome{result: result, err: err}
	}()

	select {
	case <-tracker.reached:
		require.Equal(t, DefaultOverviewWorkers, tracker.activeNodeCount())
		require.LessOrEqual(t, tracker.maxDistinctNodes(), DefaultOverviewWorkers)
	case <-time.After(time.Second):
		t.Fatal("overview workers did not reach the bounded parallelism gate")
	}
	close(block)

	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.Len(t, got.result.SuccessfulNodeIDs, t22NodeCount-1)
		require.Equal(t, []int64{t22NodeCount}, got.result.FailedNodeIDs)
		require.True(t, got.result.Partial)
		require.Equal(t, int64(MaxResponseItems), got.result.Total)
		require.Len(t, got.result.List, MaxPageSize)
		require.True(t, got.result.Truncated)
		require.LessOrEqual(t, cap(got.result.List), MaxPageSize,
			"a response page must not retain the 10,000-item aggregate backing array")
		encoded, err := json.Marshal(got.result)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "password")
		require.NotContains(t, string(encoded), "t22-secret")
		require.NotContains(t, string(encoded), "example.invalid")
	case <-time.After(3 * time.Second):
		t.Fatal("overview aggregation did not complete")
	}
}

func TestT22CapacityBoundsTenThousandStreamManagementRows(t *testing.T) {
	nodes, media := t22OverviewData()
	runtime := &t9RuntimeReader{media: media}
	service := NewStreamService(StreamServiceDependencies{
		Registry: &t9NodeRegistry{nodes: nodes},
		Runtime:  runtime,
	})

	page, err := service.ListStreams(context.Background(), StreamListRequest{
		Page: PageRequest{Page: 1, PageSize: MaxPageSize},
	})
	require.NoError(t, err)
	require.Equal(t, t22NodeCount, runtime.listCalls)
	require.Equal(t, int64(MaxResponseItems), page.Total)
	require.Len(t, page.List, MaxPageSize)
	require.True(t, page.Truncated)
	require.LessOrEqual(t, cap(page.List), MaxPageSize,
		"the stream management response must not retain the 10,000-item source backing array")
}

func BenchmarkT22CapacityTwentyThousandSessions(b *testing.B) {
	service := NewSessionService(SessionServiceDependencies{
		Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}},
		Runtime:  &t9RuntimeReader{sessions: map[int64][]zlm.Session{1: t22Sessions()}},
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page, err := service.ListNetworkSessions(context.Background(), NetworkSessionListRequest{
			NodeID: 1,
			Page:   PageRequest{Page: 1, PageSize: MaxPageSize},
		})
		if err != nil || len(page.List) != MaxPageSize || !page.Truncated {
			b.Fatalf("unexpected bounded page: len=%d truncated=%t err=%v", len(page.List), page.Truncated, err)
		}
	}
}

func BenchmarkT22CapacityTenThousandStreamsTwentyNodes(b *testing.B) {
	nodes, media := t22OverviewData()
	service := NewOverviewService(OverviewDependencies{
		Registry: overviewRegistryFake{nodes: nodes},
		Runtime:  &overviewRuntimeFake{},
		Media:    &overviewMediaFake{media: media},
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page, err := service.ListStreams(context.Background(), StreamFilter{}, PageRequest{Page: 1, PageSize: MaxPageSize})
		if err != nil || len(page.List) != MaxPageSize || !page.Truncated {
			b.Fatalf("unexpected bounded page: len=%d truncated=%t err=%v", len(page.List), page.Truncated, err)
		}
	}
}

func BenchmarkT22CapacityTenThousandStreamManagementRows(b *testing.B) {
	nodes, media := t22OverviewData()
	service := NewStreamService(StreamServiceDependencies{
		Registry: &t9NodeRegistry{nodes: nodes},
		Runtime:  &t9RuntimeReader{media: media},
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page, err := service.ListStreams(context.Background(), StreamListRequest{
			Page: PageRequest{Page: 1, PageSize: MaxPageSize},
		})
		if err != nil || len(page.List) != MaxPageSize || !page.Truncated {
			b.Fatalf("unexpected bounded page: len=%d truncated=%t err=%v", len(page.List), page.Truncated, err)
		}
	}
}

func t22Sessions() []zlm.Session {
	sessions := make([]zlm.Session, t22SessionCount)
	for index := range sessions {
		sessions[index] = zlm.Session{
			ID:         fmt.Sprintf("session-%05d", index),
			Identifier: fmt.Sprintf("connection-%05d", index),
			PeerIP:     "192.0.2.10",
			PeerPort:   10_000 + index%50_000,
			LocalIP:    "0.0.0.0",
			LocalPort:  1935,
			Type:       "tcp",
			TypeID:     "TcpSession",
		}
	}
	return sessions
}

func t22OverviewData() ([]*node.Node, map[int64][]zlm.MediaInfo) {
	nodes := make([]*node.Node, 0, t22NodeCount)
	media := make(map[int64][]zlm.MediaInfo, t22NodeCount)
	for nodeIndex := 1; nodeIndex <= t22NodeCount; nodeIndex++ {
		nodeID := int64(nodeIndex)
		nodes = append(nodes, t9Node(nodeID, node.StateActive))
		items := make([]zlm.MediaInfo, t22StreamsPerNode)
		for streamIndex := range items {
			items[streamIndex] = zlm.MediaInfo{
				Online: true,
				Schema: "rtsp",
				VHost:  "__defaultVhost__",
				App:    "live",
				Stream: fmt.Sprintf("node-%02d-stream-%03d", nodeIndex, streamIndex),
			}
		}
		media[nodeID] = items
	}
	return nodes, media
}
