package security

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAggregateStoreUpsertsWithinMinuteAndBoundsKeys(t *testing.T) {
	s := NewAggregateStore(1)
	now := time.Unix(100, 0)
	e := Event{SourceIP: "198.51.100.10", Method: "INVITE", Reason: ReasonUnknownMethod, Action: ActionDrop, Score: 2, Occurred: now}
	require.True(t, s.Record(e))
	e.Score = 3
	require.True(t, s.Record(e))
	items := s.Snapshot()
	require.Len(t, items, 1)
	require.EqualValues(t, 2, items[0].Count)
	require.EqualValues(t, 5, items[0].ScoreDelta)
	require.True(t, s.Record(Event{SourceIP: "198.51.100.11", Method: "BYE", Reason: ReasonUnknownMethod, Action: ActionDrop, Occurred: now}))
	require.EqualValues(t, 1, s.Dropped())
}

func TestAggregateStoreSeparatesDeviceAttribution(t *testing.T) {
	s := NewAggregateStore(4)
	now := time.Unix(100, 0)
	for _, deviceID := range []string{"device-a", "device-b"} {
		require.True(t, s.Record(Event{SourceIP: "198.51.100.10", DeviceID: deviceID, RiskScope: ScopeDevice, Method: "REGISTER", Reason: ReasonNonceReplay, Action: ActionSample, Occurred: now}))
	}
	items := s.Snapshot()
	require.Len(t, items, 2)
	require.NotEqual(t, items[0].DeviceID, items[1].DeviceID)
}

func TestBanStoreExpiresAndUnbanIsIdempotent(t *testing.T) {
	s := NewBanStore()
	now := time.Unix(100, 0)
	d := BanDecision{DecisionID: "d1", SourceIP: "198.51.100.10", CreatedAt: now, TTL: time.Second}
	s.Upsert(d, "auto")
	item, ok := s.Get(d.SourceIP, now.Add(2*time.Second))
	require.True(t, ok)
	require.Equal(t, BanExpired, item.Status)
	item, ok = s.Unban(d.SourceIP, "admin", now.Add(3*time.Second))
	require.True(t, ok)
	require.Equal(t, BanUnbanned, item.Status)
	item, ok = s.Unban(d.SourceIP, "other", now.Add(4*time.Second))
	require.True(t, ok)
	require.Equal(t, "admin", item.UnbannedBy)
}
