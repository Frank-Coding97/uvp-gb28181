package gb28181

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbzlmrepo "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
	gbzlmsched "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

func TestSchedulerLogRepoAdapter_MapsFilteredContract(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	nodeID := int64(9)
	filter := schedulerLogFilterToRepo(gbzlmsched.SchedulerLogFilter{
		From: fromPtr(from), To: fromPtr(to), NodeID: &nodeID,
		Policy: "weighted", Result: gbzlmsched.SchedulerLogResultError,
		StreamID: "stream-1", Limit: 37,
	})
	require.Equal(t, from, *filter.From)
	require.Equal(t, to, *filter.To)
	require.Equal(t, nodeID, *filter.NodeID)
	require.Equal(t, "weighted", filter.Algorithm)
	require.Equal(t, "weighted", filter.Policy)
	require.Equal(t, "error", filter.Result)
	require.Equal(t, "stream-1", filter.StreamID)
	require.Equal(t, 37, filter.Limit)

	rows := schedulerLogRowsToDomain([]gbzlmrepo.SchedulerLogRow{{
		ID: 4, HappenedAt: from, Algorithm: "weighted", NodeID: nodeID,
		NodeName: "n", StreamID: "stream-1", DeviceID: "d", ChannelID: "c",
		ErrorMessage: "failed",
	}})
	require.Len(t, rows, 1)
	require.Equal(t, int64(4), rows[0].ID)
	require.Equal(t, "weighted", rows[0].Algorithm)
	require.Equal(t, "failed", rows[0].ErrorMessage)
}

func fromPtr(value time.Time) *time.Time { return &value }
