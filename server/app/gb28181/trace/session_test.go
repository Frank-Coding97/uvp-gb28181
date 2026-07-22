package trace

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionSchemaUsesThirtyDayTTLAndCallIDGrouping(t *testing.T) {
	conn := &fakeClickHouseConn{}
	store, err := NewClickHouseStoreWithConn(conn, "uvp_sip_trace")
	require.NoError(t, err)
	require.NoError(t, store.EnsureSchema(t.Context()))
	// v2 schema: message table + 3 alter migrations + drop view + summary table + MV
	require.GreaterOrEqual(t, len(conn.execQueries), 3)

	// 按内容匹配,不依赖具体顺序
	var summaryDDL, viewDDL string
	for _, q := range conn.execQueries {
		if strings.Contains(q, "AggregatingMergeTree") {
			summaryDDL = q
		}
		if strings.Contains(q, "MATERIALIZED VIEW") {
			viewDDL = q
		}
	}
	require.NotEmpty(t, summaryDDL, "summary DDL not found")
	require.NotEmpty(t, viewDDL, "view DDL not found")
	require.Contains(t, summaryDDL, "uvp_sip_trace.sip_trace_session_day")
	require.Contains(t, summaryDDL, "TTL day + INTERVAL 30 DAY")
	require.Contains(t, summaryDDL, "ORDER BY (day, device_id, call_id)")
	require.Contains(t, viewDDL, "GROUP BY day, device_id, call_id")
	require.Contains(t, viewDDL, "argMaxState")
	require.Contains(t, viewDDL, "first_method_state")
}

func TestSessionQuerySupportsMultipleDevices(t *testing.T) {
	query, args, err := buildSessionListQuery("uvp_sip_trace.sip_trace_session_day", SessionFilter{
		From:      time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		DeviceIDs: []string{"device-a", "device-b"},
	})
	require.NoError(t, err)
	require.Contains(t, query, "device_id IN (?, ?)")
	require.Contains(t, args, "device-a")
	require.Contains(t, args, "device-b")
}

func TestSessionQueryParameterizesCallIDAndBoundsTime(t *testing.T) {
	_, _, err := buildSessionListQuery("uvp_sip_trace.sip_trace_session_day", SessionFilter{})
	require.ErrorIs(t, err, ErrTraceTimeRangeRequired)
	malicious := "x' OR 1=1 --"
	filter := SessionFilter{
		From:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		DeviceID: "34020000001320000001",
		CallID:   malicious,
		Anomaly:  true,
		Limit:    1000,
	}
	query, args, err := buildSessionListQuery("uvp_sip_trace.sip_trace_session_day", filter)
	require.NoError(t, err)
	require.NotContains(t, query, malicious)
	require.Contains(t, query, "device_id = ?")
	require.Contains(t, query, "call_id = ?")
	require.Contains(t, query, "final_status >= 300 OR request_count > final_response_count")
	require.Contains(t, args, malicious)
	require.Equal(t, MaxSessionPageSize, args[len(args)-1])
}

func TestDeriveSessionStateMarksExpiredMissingAndFailed(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	expired := deriveSessionState(now.Add(-8*24*time.Hour), 200, 1, 1, now)
	require.False(t, expired.OriginalAvailable)
	require.False(t, expired.Anomaly)

	missing := deriveSessionState(now.Add(-time.Hour), 0, 2, 1, now)
	require.True(t, missing.OriginalAvailable)
	require.True(t, missing.MissingResponse)
	require.True(t, missing.Anomaly)

	failed := deriveSessionState(now.Add(-time.Hour), 500, 1, 1, now)
	require.False(t, failed.MissingResponse)
	require.True(t, failed.Anomaly)
}
