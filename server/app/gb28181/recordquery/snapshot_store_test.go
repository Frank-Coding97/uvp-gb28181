package recordquery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

func TestSnapshotStoreRandomBindingAndValidation(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	store, err := NewResultSnapshotStore(time.Minute, func() time.Time { return now })
	require.NoError(t, err)
	item := record("record", "/server/private/path", "2026-08-02T08:00:00", "2026-08-02T09:00:00")
	start := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	input := SnapshotInput{OwnerUserID: 7, ChannelID: 12, DeviceCode: "device-a", ChannelCode: "channel-a", QueryID: "query-a", SegmentStart: start, SegmentEnd: end, Record: item}

	first, err := store.Issue(input)
	require.NoError(t, err)
	second, err := store.Issue(input)
	require.NoError(t, err)
	require.NotEqual(t, first.RecordKey, second.RecordKey)
	require.NotEqual(t, "", first.MetadataDigest)

	resolved, err := store.Resolve(ResolveRequest{RecordKey: first.RecordKey, OwnerUserID: 7, ChannelID: 12, PlayFrom: start.Add(time.Minute)})
	require.NoError(t, err)
	require.Equal(t, input.DeviceCode, resolved.DeviceCode)
	require.Equal(t, input.ChannelCode, resolved.ChannelCode)
	require.Equal(t, item.FilePath, resolved.Record.FilePath, "private metadata comes only from the server snapshot")

	_, err = store.Resolve(ResolveRequest{RecordKey: first.RecordKey, OwnerUserID: 8, ChannelID: 12, PlayFrom: start})
	require.ErrorIs(t, err, ErrSnapshotNotFound)
	_, err = store.Resolve(ResolveRequest{RecordKey: first.RecordKey, OwnerUserID: 7, ChannelID: 13, PlayFrom: start})
	require.ErrorIs(t, err, ErrSnapshotNotFound)
	_, err = store.Resolve(ResolveRequest{RecordKey: first.RecordKey, OwnerUserID: 7, ChannelID: 12, PlayFrom: start.Add(-time.Nanosecond)})
	require.ErrorIs(t, err, ErrSnapshotBoundary)
	_, err = store.Resolve(ResolveRequest{RecordKey: first.RecordKey, OwnerUserID: 7, ChannelID: 12, PlayFrom: end})
	require.ErrorIs(t, err, ErrSnapshotBoundary)

	tampered := resolved
	tampered.SegmentEnd = tampered.SegmentEnd.Add(time.Second)
	require.ErrorIs(t, store.Validate(tampered), ErrSnapshotTampered)
	tampered = resolved
	tampered.QueryID = "forged-query"
	require.ErrorIs(t, store.Validate(tampered), ErrSnapshotTampered)
	tampered = resolved
	tampered.Record.FilePath = "/client/forged"
	require.ErrorIs(t, store.Validate(tampered), ErrSnapshotTampered)
}

func TestSnapshotStoreTTLNewQueryAndRebuildInvalidation(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	store, err := NewResultSnapshotStore(time.Minute, func() time.Time { return now })
	require.NoError(t, err)
	issue := func(queryID string) Snapshot {
		snapshot, issueErr := store.Issue(SnapshotInput{
			OwnerUserID: 7, ChannelID: 12, DeviceCode: "device-a", ChannelCode: "channel-a", QueryID: queryID,
			SegmentStart: now, SegmentEnd: now.Add(time.Hour), Record: manscdp.RecordInfoItem{DeviceID: "channel-a", StartTime: "2026-08-02T10:00:00", EndTime: "2026-08-02T11:00:00"},
		})
		require.NoError(t, issueErr)
		return snapshot
	}

	old := issue("old-query")
	store.InvalidateOwnerChannel(7, 12)
	_, err = store.Resolve(ResolveRequest{RecordKey: old.RecordKey, OwnerUserID: 7, ChannelID: 12, PlayFrom: now})
	require.ErrorIs(t, err, ErrSnapshotExpired)

	current := issue("current-query")
	now = now.Add(time.Minute)
	_, err = store.Resolve(ResolveRequest{RecordKey: current.RecordKey, OwnerUserID: 7, ChannelID: 12, PlayFrom: now})
	require.ErrorIs(t, err, ErrSnapshotExpired)
	require.Equal(t, 0, store.Len())

	rebuilt, err := NewResultSnapshotStore(time.Minute, func() time.Time { return now })
	require.NoError(t, err)
	_, err = rebuilt.Resolve(ResolveRequest{RecordKey: current.RecordKey, OwnerUserID: 7, ChannelID: 12, PlayFrom: now})
	require.ErrorIs(t, err, ErrSnapshotNotFound)
}

func TestSnapshotStoreRejectsOlderOverlappingQueryGeneration(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	store, err := NewResultSnapshotStore(time.Minute, func() time.Time { return now })
	require.NoError(t, err)
	require.NoError(t, store.BeginQuery(7, 12, "older"))
	require.NoError(t, store.BeginQuery(7, 12, "newer"))
	_, err = store.Issue(SnapshotInput{
		OwnerUserID: 7, ChannelID: 12, DeviceCode: "device-a", ChannelCode: "channel-a", QueryID: "older",
		SegmentStart: now, SegmentEnd: now.Add(time.Hour), Record: record("old", "/old", "2026-08-02T10:00:00", "2026-08-02T11:00:00"),
	})
	require.ErrorIs(t, err, ErrSnapshotExpired)
	newer, err := store.Issue(SnapshotInput{
		OwnerUserID: 7, ChannelID: 12, DeviceCode: "device-a", ChannelCode: "channel-a", QueryID: "newer",
		SegmentStart: now, SegmentEnd: now.Add(time.Hour), Record: record("new", "/new", "2026-08-02T10:00:00", "2026-08-02T11:00:00"),
	})
	require.NoError(t, err)
	require.NotEmpty(t, newer.RecordKey)
}

func TestServiceNewQueryInvalidatesPriorKeys(t *testing.T) {
	var service *Service
	service = newTestService(t, senderFunc(func(_ context.Context, sender, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		sn, channelCode := sentQuery(t, body)
		service.Accept(sender, &manscdp.RecordInfoResponse{SN: sn, DeviceID: channelCode, SumNum: 1, Items: []manscdp.RecordInfoItem{
			record("one", "/a", "2026-08-02T08:00:00", "2026-08-02T08:10:00"),
		}})
		return uac.TrackedMessageResult{StatusCode: 200}, nil
	}), nil)

	first, err := service.Query(context.Background(), queryFixture())
	require.NoError(t, err)
	second, err := service.Query(context.Background(), queryFixture())
	require.NoError(t, err)
	playFrom := queryFixture().StartTime.Add(time.Minute)
	_, err = service.Snapshots().Resolve(ResolveRequest{RecordKey: first.Records[0].RecordKey, OwnerUserID: 7, ChannelID: 12, PlayFrom: playFrom})
	require.ErrorIs(t, err, ErrSnapshotExpired)
	_, err = service.Snapshots().Resolve(ResolveRequest{RecordKey: second.Records[0].RecordKey, OwnerUserID: 7, ChannelID: 12, PlayFrom: playFrom})
	require.NoError(t, err)
}
