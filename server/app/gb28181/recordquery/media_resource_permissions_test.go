package recordquery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
)

func TestSnapshotStoreMediaResourceLifecycleEnforcesOwnerAndClose(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	store, err := NewResultSnapshotStore(time.Minute, func() time.Time { return now })
	require.NoError(t, err)
	start := now.Add(-time.Hour)
	end := now
	snapshot, err := store.Issue(SnapshotInput{
		OwnerUserID: 7, ChannelID: 12, DeviceCode: "device-a", ChannelCode: "channel-a", QueryID: "query-a",
		SegmentStart: start, SegmentEnd: end,
		Record: manscdp.RecordInfoItem{DeviceID: "channel-a", StartTime: "2026-09-07T11:00:00", EndTime: "2026-09-07T12:00:00", FilePath: "/private/server/record.mp4"},
	})
	require.NoError(t, err)
	require.NoError(t, store.Validate(snapshot))

	_, err = store.Resolve(ResolveRequest{RecordKey: snapshot.RecordKey, OwnerUserID: 8, ChannelID: 12, PlayFrom: start.Add(time.Minute)})
	require.ErrorIs(t, err, ErrSnapshotNotFound)
	_, err = store.Resolve(ResolveRequest{RecordKey: snapshot.RecordKey, OwnerUserID: 7, ChannelID: 12, PlayFrom: end})
	require.ErrorIs(t, err, ErrSnapshotBoundary)
	require.Equal(t, 1, store.Len())

	store.Close()
	store.Close()
	require.Zero(t, store.Len())
	_, err = store.Resolve(ResolveRequest{RecordKey: snapshot.RecordKey, OwnerUserID: 7, ChannelID: 12, PlayFrom: start.Add(time.Minute)})
	require.ErrorIs(t, err, ErrSnapshotNotFound)
	require.ErrorIs(t, store.Validate(snapshot), ErrSnapshotNotFound)
	_, err = store.Issue(SnapshotInput{
		OwnerUserID: 7, ChannelID: 12, DeviceCode: "device-a", ChannelCode: "channel-a", QueryID: "query-a",
		SegmentStart: start, SegmentEnd: end, Record: snapshot.Record,
	})
	require.ErrorIs(t, err, ErrUnavailable)
}
