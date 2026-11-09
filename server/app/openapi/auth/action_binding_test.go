package auth

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestActionBindingUsesTypedTargetsWithoutChangingSignedBytes(t *testing.T) {
	body := []byte(`{"deviceId":"34020000001320000001","channelId":"34020000001320000002","action":"stop","speed":0}`)
	q := gatewayRequest{scope: PTZMoveScope, method: "POST", body: body}
	require.NoError(t, bindActionRequest(&q))
	require.Equal(t, body, q.body)
	require.Equal(t, "34020000001320000001", q.deviceID)
	require.Equal(t, "34020000001320000002", q.channelID)
	require.JSONEq(t, `{"action":"stop","speed":0}`, string(q.businessBody))
	for _, invalid := range []string{
		`{"deviceId":"34020000001320000001","channelId":"34020000001320000002","ownerDeptId":1}`,
		`{"deviceId":"34020000001320000001","deviceId":"34020000001320000003","channelId":"34020000001320000002"}`,
		`{"deviceId":34020000001320000001,"channelId":"34020000001320000002"}`,
		`{"deviceId":"34020000001320000001"}`,
	} {
		bad := gatewayRequest{scope: PTZMoveScope, method: "POST", body: []byte(invalid)}
		require.Error(t, bindActionRequest(&bad), invalid)
	}
}

func TestActionQueryBindingRejectsDuplicateUnknownAndWrongSourceTargets(t *testing.T) {
	q := gatewayRequest{scope: "channel:detail", method: "GET", rawQuery: "channelId=34020000001320000002&deviceId=34020000001320000001"}
	require.NoError(t, bindActionRequest(&q))
	require.Equal(t, "34020000001320000002", q.params["channelId"])
	for _, query := range []string{
		"deviceId=34020000001320000001&channelId=34020000001320000002&deviceId=34020000001320000003",
		"deviceId=34020000001320000001&channelId=34020000001320000002&ownerDeptId=2",
		"deviceId=&channelId=34020000001320000002",
	} {
		bad := gatewayRequest{scope: "channel:detail", method: "GET", rawQuery: query}
		require.Error(t, bindActionRequest(&bad), query)
	}
	bad := gatewayRequest{scope: PTZMoveScope, method: "POST", rawQuery: q.rawQuery, body: []byte(`{"action":"stop"}`)}
	require.Error(t, bindActionRequest(&bad))
}

func TestRecordingSearchPaginationIsSignedInJSONAndProjectedToDomainQuery(t *testing.T) {
	body := []byte(`{"deviceId":"34020000002000000001","channelId":"34020000001320000001","startTime":"2026-11-09T08:00:00Z","endTime":"2026-11-09T09:00:00Z","pageSize":20}`)
	q := gatewayRequest{scope: "device:record-query", method: "POST", body: body}
	require.NoError(t, bindActionRequest(&q))
	require.Equal(t, body, q.body, "HMAC must cover original JSON including pagination")
	require.Equal(t, "pageSize=20", q.businessQuery)
	require.JSONEq(t, `{"startTime":"2026-11-09T08:00:00Z","endTime":"2026-11-09T09:00:00Z"}`, string(q.businessBody))
	metadata, err := parseMetadata(q, 10)
	require.NoError(t, err)
	require.Empty(t, metadata.cursor)
	require.Equal(t, 20, metadata.size)
	require.Equal(t, "34020000002000000001", metadata.deviceID)
	for _, invalid := range []string{
		`{"deviceId":"34020000002000000001","channelId":"34020000001320000001","page":1}`,
		`{"deviceId":"34020000002000000001","channelId":"34020000001320000001","pageSize":101}`,
		`{"deviceId":"34020000002000000001","channelId":"34020000001320000001","cursor":20}`,
	} {
		bad := gatewayRequest{scope: q.scope, method: "POST", body: []byte(invalid)}
		require.Error(t, bindActionRequest(&bad))
	}
}

func TestRecordingSearchCursorContinuationHasNoBusinessBody(t *testing.T) {
	body := []byte(`{"deviceId":"34020000002000000001","channelId":"34020000001320000001","cursor":"query-cursor","pageSize":20}`)
	q := gatewayRequest{scope: "device:record-query", method: "POST", body: body}
	require.NoError(t, bindActionRequest(&q))
	require.Equal(t, body, q.body)
	require.Empty(t, q.businessBody, "continuation must reach the domain with no new query parameters")
	require.Equal(t, "cursor=query-cursor&pageSize=20", q.businessQuery)
	for _, fields := range []string{
		`"cursor":"query-cursor","startTime":"2026-11-09T08:00:00Z","endTime":"2026-11-09T09:00:00Z"`,
		`"cursor":"query-cursor","type":"alarm"`,
		`"cursor":""`,
		`"pageSize":20`,
		`"startTime":"2026-11-09T08:00:00Z"`,
	} {
		bad := gatewayRequest{scope: q.scope, method: "POST", body: []byte(`{"deviceId":"34020000002000000001","channelId":"34020000001320000001",` + fields + `}`)}
		require.Error(t, bindActionRequest(&bad), fields)
	}
}
