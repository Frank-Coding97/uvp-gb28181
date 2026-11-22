package trace

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func transportKeepaliveEvent(id string, at time.Time, deviceID string) StoredEvent {
	return StoredEvent{
		EventID: id, OccurredAt: at, Direction: DirectionInbound, Transport: "TCP",
		LocalAddr: "platform:5060", RemoteAddr: "device:15060",
		DeviceID: deviceID, Method: "", StatusCode: 0, CallID: "",
		BusinessCode: BusinessTransportKeepalive, BusinessType: "传输保活", BusinessConfidence: "high",
		Payload: EncryptedPayload{
			Nonce: []byte{1}, Ciphertext: []byte("\r\n\r\n"), Algorithm: EncryptionAES256GCM,
			KeyVersion: "v1", DigestSHA256: strings.Repeat("d", 64),
		},
	}
}

// TestTransportKeepaliveHiddenUnlessExplicitlyRequested 锁「传输保活默认不展示」口径：
// 它是 TCP 长连接的空包（正文只有 CRLF，没有起始行/头域），没有 Call-ID ⇒ 每条自成一条会话，
// 数量随保活周期增长，会把真实信令挤出列表（老板 2026-11-22 要求过滤）。
// ⛔ 但采集侧必须继续落库：显式按业务类型筛「传输保活」时仍要查得到，
// 否则排查长连接保活（设备掉线 / NAT 超时）就没有原始证据了。
func TestTransportKeepaliveHiddenUnlessExplicitlyRequested(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	at := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	events := []StoredEvent{
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed51", at, "device-a", "call-register", "REGISTER", 0),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed52", at.Add(time.Second), "device-a", "call-register", "REGISTER", 401),
		transportKeepaliveEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed53", at.Add(2*time.Second), "device-a"),
	}
	require.NoError(t, store.InsertBatch(t.Context(), events))

	sessionFilter := SessionFilter{From: at.Add(-time.Minute), To: at.Add(time.Minute)}
	sessions, err := store.ListSessions(t.Context(), sessionFilter)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "call-register", sessions[0].CallID)
	require.NotEqual(t, BusinessTransportKeepalive, sessions[0].BusinessCode)

	explicit := sessionFilter
	explicit.BusinessCode = BusinessTransportKeepalive
	sessions, err = store.ListSessions(t.Context(), explicit)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, BusinessTransportKeepalive, sessions[0].BusinessCode)
	require.Empty(t, sessions[0].CallID)

	// 统计口径必须跟列表一致，否则「总计」比列表实际条数多。
	stats, err := store.GetSessionStats(t.Context(), sessionFilter)
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.Total)

	messageFilter := MessageFilter{From: at.Add(-time.Minute), To: at.Add(time.Minute), Limit: 10}
	page, err := store.ListMessages(t.Context(), messageFilter)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	for _, item := range page.Items {
		require.NotEqual(t, BusinessTransportKeepalive, item.BusinessCode)
	}

	messageFilter.BusinessCode = BusinessTransportKeepalive
	page, err = store.ListMessages(t.Context(), messageFilter)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, BusinessTransportKeepalive, page.Items[0].BusinessCode)
}
