package recording

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

type listPlaybackStub struct {
	playbackStub
	sessions []*gbplayback.Session
	owner    string
}

func (s *listPlaybackStub) ListForOwner(owner string) []*gbplayback.Session {
	s.owner = owner
	return s.sessions
}

func TestMediaSessionListsRevalidateOwnerTargetModeAndPagination(t *testing.T) {
	target := testTarget()
	now := time.Now().UTC()
	base := gbplayback.Session{ID: "one", OwnerID: clientOwner(31), DeviceID: testDeviceCode, ChannelID: "71", Authorization: target.Authorization, Mode: gbplayback.ModePlayback, CreatedAt: now, State: gbplayback.StatePlaying}
	newer := base
	newer.ID = "two"
	newer.CreatedAt = now.Add(time.Minute)
	otherOwner := base
	otherOwner.ID = "other-owner"
	otherOwner.OwnerID = clientOwner(32)
	otherChannel := base
	otherChannel.ID = "other-channel"
	otherChannel.Authorization.ChannelCode = "37011200001310000003"
	stale := base
	stale.ID = "old-epoch"
	stale.Authorization.DeviceEpoch--
	download := base
	download.ID = "download"
	download.Mode = gbplayback.ModeDownload
	plays := &listPlaybackStub{sessions: []*gbplayback.Session{&base, &otherOwner, &otherChannel, &stale, &download, &newer}}
	d := NewDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) { return target, nil }), nil, nil, plays, Config{})
	request := Request{ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: testDeviceCode, ChannelID: testChannelCode, Query: map[string]string{"page": "1", "pageSize": "1"}}
	result, err := d.Handle(context.Background(), ScopePlaybackList, request)
	require.NoError(t, err)
	page := result.(SessionPage)
	require.EqualValues(t, 2, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, "two", page.Items[0].SessionID)
	require.Equal(t, clientOwner(31), plays.owner)
	request.Query["page"] = "2"
	result, err = d.Handle(context.Background(), ScopePlaybackList, request)
	require.NoError(t, err)
	require.Equal(t, "one", result.(SessionPage).Items[0].SessionID)
	request.Query = nil
	result, err = d.Handle(context.Background(), ScopeDownloadList, request)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.(SessionPage).Total)
	require.Equal(t, "download", result.(SessionPage).Items[0].SessionID)
	request.Query = map[string]string{"pageSize": "101"}
	_, err = d.Handle(context.Background(), ScopePlaybackList, request)
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)
}

func TestPlaybackViewReportsRealFailureAndTransferFactsWithoutInternalPaths(t *testing.T) {
	now := time.Now().UTC()
	session := &gbplayback.Session{ID: "one", DeviceID: testDeviceCode, Authorization: testTarget().Authorization, Mode: gbplayback.ModeDownload, RecordKey: "opaque-record", StreamID: "stream-one", SSRC: "0200000001", DownloadSpeed: 4, DefaultProtocol: "webrtc", EndReason: "device-ended", ErrorCode: "SIP_REJECTED", ErrorStage: "invite", RecordingURI: "/private/media/file.mp4", LastActivityAt: now, IdleDeadline: now.Add(time.Minute)}
	body, err := json.Marshal(playbackView(session, "2022", false))
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(body, &fields))
	for _, field := range []string{"recordKey", "streamId", "ssrc", "downloadSpeed", "defaultProtocol", "endReason", "errorCode", "errorStage", "lastActivityAt", "idleDeadline", "existing"} {
		require.Contains(t, fields, field)
	}
	require.Equal(t, float64(4), fields["downloadSpeed"])
	require.Equal(t, false, fields["existing"])
	require.NotContains(t, string(body), "/private/media")
}

func TestRecordQueryOptionsSeparatesDeviceAndChannelOnlineFacts(t *testing.T) {
	target := testTarget()
	target.ChannelOnline = false
	d := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) { return target, nil }), &queryStub{}, nil, &playbackStub{})
	result, err := d.Handle(context.Background(), ScopeRecordQueryOptions, Request{ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: testDeviceCode, ChannelID: testChannelCode})
	require.NoError(t, err)
	body, err := json.Marshal(result)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(body, &fields))
	require.Equal(t, true, fields["device"].(map[string]any)["online"])
	require.Equal(t, false, fields["channel"].(map[string]any)["online"])
	require.Equal(t, testDeviceCode, fields["deviceId"])
	require.Equal(t, testChannelCode, fields["channelId"])
}

func TestRecordSearchCursorAcceptsAnEmptyProjectedJSONObject(t *testing.T) {
	target := testTarget()
	query := &queryStub{current: true, result: recordquery.QueryResult{QueryID: "query", Status: recordquery.QueryStatusComplete}}
	d := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) { return target, nil }), query, nil, &playbackStub{})
	state := recordCursor{clientID: 31, resourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, deviceID: testDeviceCode, channelID: testChannelCode, channelPK: 71, ownerDeptID: target.OwnerDeptID, authorization: target.Authorization, queryID: "query", items: []RecordItem{{RecordKey: "one"}, {RecordKey: "two"}}, page: RecordPage{PageSize: 1}, offset: 1, pageSize: 1, expiresAt: time.Now().Add(time.Minute)}
	token, err := d.storeCursor(state)
	require.NoError(t, err)
	query.request = recordquery.QueryRequest{OwnerClientID: 31, ChannelID: 71}
	result, err := d.Handle(context.Background(), ScopeRecordQuery, Request{ClientID: 31, ResourceScope: state.resourceScope, DeviceID: testDeviceCode, ChannelID: testChannelCode, Query: map[string]string{"cursor": token}, Body: []byte(`{}`)})
	require.NoError(t, err)
	require.Equal(t, "two", result.(RecordPage).Items[0].RecordKey)
	require.Zero(t, query.calls)
}

func TestPlaybackScopesRejectDownloadSessionsBeforeTouchOrControl(t *testing.T) {
	target := testTarget()
	session := &gbplayback.Session{ID: "download-one", OwnerID: clientOwner(31), DeviceID: testDeviceCode, ChannelID: "71", Authorization: target.Authorization, Mode: gbplayback.ModeDownload}
	for _, scope := range []string{ScopePlaybackRead, ScopePlaybackAction, ScopePlaybackStop} {
		t.Run(scope, func(t *testing.T) {
			plays := &playbackStub{peek: session, get: session}
			d := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) { return target, nil }), nil, nil, plays)
			request := Request{ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: testDeviceCode, ChannelID: testChannelCode, SessionID: session.ID}
			if scope == ScopePlaybackAction {
				request.Body = []byte(`{"action":"pause"}`)
			}
			_, err := d.Handle(context.Background(), scope, request)
			require.ErrorIs(t, err, adapters.ErrNotFound)
			require.Empty(t, plays.getOwner, "rejected mode must not renew the idle lease")
			require.Empty(t, plays.actionOwner)
			require.Empty(t, plays.stopOwner)
		})
	}
}
