package recordquery

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
)

func TestCascadeQueryDoesNotIssueOrInvalidateUserPlaybackKeys(t *testing.T) {
	var service *Service
	service = newTestService(t, senderFunc(func(_ context.Context, device, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		sn, channel := sentQuery(t, body)
		service.Accept(device, &manscdp.RecordInfoResponse{SN: sn, DeviceID: channel, SumNum: 1,
			Items: []manscdp.RecordInfoItem{record("record", "/a", "2026-08-02T08:00:00", "2026-08-02T08:10:00")}})
		return uac.TrackedMessageResult{StatusCode: 200}, nil
	}), nil)
	request := queryFixture()
	userResult, err := service.Query(context.Background(), request)
	require.NoError(t, err)
	result, err := service.QueryForCascade(context.Background(), CascadeQueryRequest{
		ChannelID: request.ChannelID, DeviceCode: request.DeviceCode, ChannelCode: request.ChannelCode,
		Destination: request.Destination, Transport: request.Transport, StartTime: request.StartTime, EndTime: request.EndTime,
	})
	require.NoError(t, err)
	require.Equal(t, QueryStatusComplete, result.Status)
	require.Len(t, result.Records, 1)
	require.Empty(t, result.Records[0].RecordKey)
	_, err = service.Snapshots().Resolve(ResolveRequest{RecordKey: userResult.Records[0].RecordKey,
		OwnerUserID: request.OwnerUserID, ChannelID: request.ChannelID, PlayFrom: request.StartTime})
	require.NoError(t, err)
	request.OwnerUserID = 0
	_, err = service.Query(context.Background(), request)
	require.ErrorIs(t, err, ErrInvalidArgument)
}
