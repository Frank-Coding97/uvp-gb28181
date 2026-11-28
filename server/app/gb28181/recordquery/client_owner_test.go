package recordquery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
)

func TestSnapshotClientOwnershipIsSeparateFromUserOwnership(t *testing.T) {
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	store, err := NewResultSnapshotStore(time.Minute, func() time.Time { return now })
	require.NoError(t, err)
	input := SnapshotInput{
		OwnerClientID: 41, ChannelID: 12, DeviceCode: "device-a", ChannelCode: "channel-a", QueryID: "client-query",
		SegmentStart: now, SegmentEnd: now.Add(time.Hour),
		Record: manscdp.RecordInfoItem{DeviceID: "channel-a", StartTime: "2026-10-11T10:00:00", EndTime: "2026-10-11T11:00:00"},
	}
	require.NoError(t, store.BeginQueryForClient(41, 12, "client-query"))
	snapshot, err := store.Issue(input)
	require.NoError(t, err)
	require.EqualValues(t, 41, snapshot.OwnerClientID)

	_, err = store.Resolve(ResolveRequest{RecordKey: snapshot.RecordKey, OwnerClientID: 41, ChannelID: 12, PlayFrom: now})
	require.NoError(t, err)
	_, err = store.Resolve(ResolveRequest{RecordKey: snapshot.RecordKey, OwnerClientID: 42, ChannelID: 12, PlayFrom: now})
	require.ErrorIs(t, err, ErrSnapshotNotFound)
	_, err = store.Resolve(ResolveRequest{RecordKey: snapshot.RecordKey, OwnerUserID: 41, ChannelID: 12, PlayFrom: now})
	require.ErrorIs(t, err, ErrSnapshotNotFound, "numeric client ID must never act as a user ID")
}

func TestClientQueryGenerationTracksSupersessionAndExpiry(t *testing.T) {
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	store, err := NewResultSnapshotStore(time.Minute, func() time.Time { return now })
	require.NoError(t, err)
	require.NoError(t, store.BeginQueryForClient(41, 12, "query-one"))
	require.True(t, store.IsCurrentClientQuery(41, 12, "query-one"))
	require.False(t, store.IsCurrentClientQuery(42, 12, "query-one"))
	require.False(t, store.IsCurrentClientQuery(41, 13, "query-one"))
	require.NoError(t, store.BeginQueryForClient(41, 12, "query-two"))
	require.False(t, store.IsCurrentClientQuery(41, 12, "query-one"), "a new query invalidates the old cursor generation")
	require.True(t, store.IsCurrentClientQuery(41, 12, "query-two"))
	now = now.Add(time.Minute)
	require.False(t, store.IsCurrentClientQuery(41, 12, "query-two"), "query generations expire with their result snapshots")
}

func TestRecordQueryAcceptsClientPrincipalWithoutUserID(t *testing.T) {
	var service *Service
	service = newTestService(t, senderFunc(func(_ context.Context, sender, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		sn, channelCode := sentQuery(t, body)
		service.Accept(sender, &manscdp.RecordInfoResponse{SN: sn, DeviceID: channelCode, SumNum: 1, Items: []manscdp.RecordInfoItem{
			{DeviceID: channelCode, StartTime: "2026-08-02T08:00:00", EndTime: "2026-08-02T08:05:00"},
		}})
		return uac.TrackedMessageResult{StatusCode: 200}, nil
	}), nil)

	request := queryFixture()
	request.OwnerUserID = 0
	request.OwnerClientID = 41
	result, err := service.Query(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, result.Records, 1)
	require.NotEmpty(t, result.Records[0].RecordKey)
	_, err = service.Snapshots().Resolve(ResolveRequest{
		RecordKey: result.Records[0].RecordKey, OwnerClientID: 41, ChannelID: request.ChannelID, PlayFrom: request.StartTime,
	})
	require.NoError(t, err)
	_, err = service.Snapshots().Resolve(ResolveRequest{
		RecordKey: result.Records[0].RecordKey, OwnerUserID: 41, ChannelID: request.ChannelID, PlayFrom: request.StartTime,
	})
	require.ErrorIs(t, err, ErrSnapshotNotFound)
}
