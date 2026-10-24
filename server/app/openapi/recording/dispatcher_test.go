package recording

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	testDeviceCode  = "37011200001310000001"
	testChannelCode = "37011200001310000002"
)

type targetResolverFunc func(context.Context, resource.DepartmentScope, string, string) (Target, error)

func (f targetResolverFunc) Resolve(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (Target, error) {
	return f(ctx, scope, deviceID, channelID)
}

type queryStub struct {
	request recordquery.QueryRequest
	result  recordquery.QueryResult
	err     error
	calls   int
	current bool
}

func (s *queryStub) Query(_ context.Context, request recordquery.QueryRequest) (recordquery.QueryResult, error) {
	s.calls++
	s.request = request
	return s.result, s.err
}

func (s *queryStub) IsClientQueryCurrent(clientID int64, channelID uint, queryID string) bool {
	return s.current && clientID == s.request.OwnerClientID && channelID == s.request.ChannelID && queryID == s.result.QueryID
}

func (*queryStub) Snapshots() *recordquery.ResultSnapshotStore { return nil }

type snapshotStub struct {
	snapshot recordquery.Snapshot
	err      error
	request  recordquery.ResolveRequest
}

func (s *snapshotStub) Resolve(request recordquery.ResolveRequest) (recordquery.Snapshot, error) {
	s.request = request
	if request.OwnerClientID != s.snapshot.OwnerClientID || request.ChannelID != s.snapshot.ChannelID || request.RecordKey != s.snapshot.RecordKey {
		return recordquery.Snapshot{}, recordquery.ErrSnapshotNotFound
	}
	return s.snapshot, s.err
}

type playbackStub struct {
	createRequests []gbplayback.CreateRequest
	createResult   gbplayback.CreateResult
	peek           *gbplayback.Session
	peekOwner      string
	get            *gbplayback.Session
	getOwner       string
	actionOwner    string
	actionRequest  gbplayback.ActionRequest
	stopID         string
	stopOwner      string
	err            error
}

func (s *playbackStub) Create(_ context.Context, request gbplayback.CreateRequest) (gbplayback.CreateResult, error) {
	s.createRequests = append(s.createRequests, request)
	return s.createResult, s.err
}

func (s *playbackStub) PeekForOwner(sessionID, ownerID string) (*gbplayback.Session, bool) {
	s.peekOwner = ownerID
	if s.peek == nil || s.peek.ID != sessionID || s.peek.OwnerID != ownerID {
		return nil, false
	}
	return s.peek, true
}

func (s *playbackStub) GetForOwner(sessionID, ownerID string) (*gbplayback.Session, bool) {
	s.getOwner = ownerID
	if s.get == nil || s.get.ID != sessionID || s.get.OwnerID != ownerID {
		return nil, false
	}
	return s.get, true
}

func (s *playbackStub) Action(_ context.Context, sessionID, ownerID string, request gbplayback.ActionRequest) (*gbplayback.Session, error) {
	s.actionOwner, s.actionRequest = ownerID, request
	if s.peek == nil || s.peek.ID != sessionID || s.peek.OwnerID != ownerID {
		return nil, gbplayback.ErrPlaybackNotFound
	}
	return s.peek, s.err
}

func (s *playbackStub) StopForOwner(_ context.Context, sessionID, ownerID, _ string) error {
	s.stopID, s.stopOwner = sessionID, ownerID
	return s.err
}

func testTarget() Target {
	return Target{
		Authorization: gbplayback.AuthorizationSnapshot{
			DevicePK: 51, DeviceEpoch: 3, CleanupCompletedEpoch: 3, DeviceCode: testDeviceCode,
			ChannelPK: 71, ChannelCode: testChannelCode,
		},
		DeviceCode: testDeviceCode, ChannelCode: testChannelCode, DeviceIP: "192.0.2.10", DevicePort: 5060,
		Transport: "UDP", StreamTransport: "TCP-Passive", PreferredNodeID: 8, DeviceOnline: true, ChannelOnline: true, EffectiveVersion: "2022",
	}
}

func testDispatcher(resolver TargetResolver, query *queryStub, snapshots SnapshotResolver, plays *playbackStub) *Dispatcher {
	return NewDispatcher(resolver, query, snapshots, plays, Config{
		Location: time.FixedZone("CST", 8*60*60), MaxRangeHours: 24, DefaultProtocol: "webrtc", Secure: true,
	})
}

func TestRecordingSearchUsesClientOwnerAndHidesInternalFileMetadata(t *testing.T) {
	var resolvedScope resource.DepartmentScope
	resolver := targetResolverFunc(func(_ context.Context, scope resource.DepartmentScope, deviceID, channelID string) (Target, error) {
		resolvedScope = scope
		require.Equal(t, testDeviceCode, deviceID)
		require.Equal(t, testChannelCode, channelID)
		return testTarget(), nil
	})
	query := &queryStub{result: recordquery.QueryResult{
		QueryID: "internal-query", Status: recordquery.QueryStatusComplete, DeclaredTotal: 1, ReceivedCount: 1,
		Records: []recordquery.Record{{
			RecordInfoItem: manscdp.RecordInfoItem{
				DeviceID: testChannelCode, Name: "front", FilePath: "/private/zlm/record.mp4", Address: "sip:192.0.2.10",
				StartTime: "2026-10-11T10:00:00", EndTime: "2026-10-11T10:05:00", Secrecy: 0, Type: manscdp.RecordInfoItemType("time"),
			}, RecordKey: "opaque-record-key",
		}},
	}, current: true}
	dispatcher := testDispatcher(resolver, query, &snapshotStub{}, &playbackStub{})
	data, err := dispatcher.Handle(context.Background(), ScopeRecordQuery, Request{
		ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartmentAndChildren},
		DeviceID: testDeviceCode, ChannelID: testChannelCode,
		Body: []byte(`{"startTime":"2026-10-11T10:00:00+08:00","endTime":"2026-10-11T11:00:00+08:00","type":"all"}`),
	})
	require.NoError(t, err)
	require.Equal(t, resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartmentAndChildren}, resolvedScope)
	require.EqualValues(t, 31, query.request.OwnerClientID)
	require.Zero(t, query.request.OwnerUserID)
	require.EqualValues(t, 71, query.request.ChannelID)
	require.Equal(t, testDeviceCode, query.request.DeviceCode)
	require.Equal(t, testChannelCode, query.request.ChannelCode)

	body, err := json.Marshal(data)
	require.NoError(t, err)
	require.Contains(t, string(body), "opaque-record-key")
	require.NotContains(t, string(body), "/private/zlm")
	require.NotContains(t, string(body), "192.0.2.10")
	require.NotContains(t, string(body), "internal-query")
}

func TestDownloadCreateUsesDownloadModeAndValidSpeed(t *testing.T) {
	query := &queryStub{}
	snapshots := &snapshotStub{snapshot: recordquery.Snapshot{
		RecordKey: "record-key", OwnerClientID: 31, ChannelID: 71, DeviceCode: testDeviceCode,
		ChannelCode: testChannelCode, SegmentStart: time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC),
		SegmentEnd: time.Date(2026, 10, 11, 10, 5, 0, 0, time.UTC),
	}}
	plays := &playbackStub{createResult: gbplayback.CreateResult{Session: &gbplayback.Session{
		ID: "download-1", OwnerID: clientOwner(31), DeviceID: testDeviceCode,
		ChannelID: "71", Mode: gbplayback.ModeDownload, State: gbplayback.StateCreating,
		Authorization: testTarget().Authorization, CreatedAt: time.Now(), Deadline: time.Now().Add(time.Minute),
	}}}
	dispatcher := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) {
		return testTarget(), nil
	}), query, snapshots, plays)
	data, err := dispatcher.Handle(context.Background(), ScopeDownloadCreate, Request{
		ClientID: 31, IdempotencyKey: "download-key", ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment},
		DeviceID: testDeviceCode, ChannelID: testChannelCode,
		Body: []byte(`{"recordKey":"record-key","playFrom":"2026-10-11T10:00:00Z","downloadSpeed":4}`),
	})
	require.NoError(t, err)
	view := data.(PlaybackView)
	require.Equal(t, "download", view.Mode)
	require.Equal(t, gbplayback.ModeDownload, plays.createRequests[0].Mode)
	require.EqualValues(t, 4, plays.createRequests[0].DownloadSpeed)
}

func TestDownloadReadAndCancelStayBoundToClientAndChannel(t *testing.T) {
	target := testTarget()
	session := &gbplayback.Session{ID: "download-1", OwnerID: clientOwner(31), DeviceID: testDeviceCode, ChannelID: "71", Mode: gbplayback.ModeDownload, State: gbplayback.StatePlaying, Authorization: target.Authorization}
	plays := &playbackStub{peek: session, get: session}
	dispatcher := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) { return target, nil }), &queryStub{}, &snapshotStub{}, plays)
	base := Request{ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: testDeviceCode, ChannelID: testChannelCode, SessionID: session.ID}
	data, err := dispatcher.Handle(context.Background(), ScopeDownloadRead, base)
	require.NoError(t, err)
	require.Equal(t, "download", data.(PlaybackView).Mode)
	_, err = dispatcher.Handle(context.Background(), ScopeDownloadCancel, base)
	require.NoError(t, err)
	require.Equal(t, clientOwner(31), plays.stopOwner)
	base.ClientID = 32
	_, err = dispatcher.Handle(context.Background(), ScopeDownloadRead, base)
	require.ErrorIs(t, err, adapters.ErrNotFound)
}

func TestRecordingSearchReportsTheNegotiatedProtocolVersion(t *testing.T) {
	for _, version := range []string{"2016", "2022"} {
		t.Run(version, func(t *testing.T) {
			target := testTarget()
			target.EffectiveVersion = version
			query := &queryStub{result: recordquery.QueryResult{Status: recordquery.QueryStatusEmpty}}
			dispatcher := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) {
				return target, nil
			}), query, &snapshotStub{}, &playbackStub{})
			data, err := dispatcher.Handle(context.Background(), ScopeRecordQuery, Request{
				ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment},
				DeviceID: testDeviceCode, ChannelID: testChannelCode,
				Body: []byte(`{"startTime":"2026-10-11T10:00:00Z","endTime":"2026-10-11T11:00:00Z"}`),
			})
			require.NoError(t, err)
			page := data.(RecordPage)
			require.Equal(t, version, page.ProtocolVersion)
			require.Equal(t, 1, query.calls)
			require.Equal(t, version, string(query.request.ProtocolVersion))
		})
	}
}

func TestRecordingSearchReturnsStableClientBoundCursorPages(t *testing.T) {
	start := time.Date(2026, 10, 11, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	makeRecord := func(index int, offset time.Duration) recordquery.Record {
		return recordquery.Record{
			RecordInfoItem: manscdp.RecordInfoItem{
				DeviceID: testChannelCode, Name: "record", StartTime: start.Add(offset).Format("2006-01-02T15:04:05"),
				EndTime: start.Add(offset + time.Minute).Format("2006-01-02T15:04:05"),
			}, RecordKey: fmt.Sprintf("record-key-%d", index),
		}
	}
	query := &queryStub{current: true, result: recordquery.QueryResult{
		QueryID: "query-1", Status: recordquery.QueryStatusComplete, DeclaredTotal: 3,
		Records: []recordquery.Record{makeRecord(1, 0), makeRecord(2, time.Minute), makeRecord(3, 2*time.Minute)},
	}}
	dispatcher := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) {
		return testTarget(), nil
	}), query, &snapshotStub{}, &playbackStub{})
	base := Request{
		ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment},
		DeviceID: testDeviceCode, ChannelID: testChannelCode,
	}
	base.Body = []byte(`{"startTime":"2026-10-11T10:00:00+08:00","endTime":"2026-10-11T11:00:00+08:00"}`)
	base.Query = map[string]string{"pageSize": "2"}
	firstData, err := dispatcher.Handle(context.Background(), ScopeRecordQuery, base)
	require.NoError(t, err)
	first, ok := firstData.(RecordPage)
	require.True(t, ok)
	require.Equal(t, []string{"record-key-1", "record-key-2"}, recordKeys(first.Items))
	require.NotEmpty(t, first.NextCursor)
	require.NotContains(t, first.NextCursor, "query-1", "cursor must not expose the internal query identifier")
	require.Equal(t, 1, query.calls)

	base.Body = nil
	base.Query = map[string]string{"cursor": first.NextCursor}
	secondData, err := dispatcher.Handle(context.Background(), ScopeRecordQuery, base)
	require.NoError(t, err)
	second, ok := secondData.(RecordPage)
	require.True(t, ok)
	require.Equal(t, []string{"record-key-3"}, recordKeys(second.Items))
	require.Empty(t, second.NextCursor)
	require.Equal(t, 1, query.calls, "continuation must not send another device query")
	require.Equal(t, 2, second.PageSize)
}

func TestRecordingCursorRejectsOtherClientsExpiredQueriesAndChangedScope(t *testing.T) {
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	query := &queryStub{current: true, result: recordquery.QueryResult{
		QueryID: "query-1", Status: recordquery.QueryStatusComplete, Records: []recordquery.Record{
			{RecordInfoItem: manscdp.RecordInfoItem{DeviceID: testChannelCode, StartTime: "2026-10-11T10:00:00", EndTime: "2026-10-11T10:01:00"}, RecordKey: "key-1"},
			{RecordInfoItem: manscdp.RecordInfoItem{DeviceID: testChannelCode, StartTime: "2026-10-11T10:01:00", EndTime: "2026-10-11T10:02:00"}, RecordKey: "key-2"},
		}}}
	resolver := targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) {
		return testTarget(), nil
	})
	dispatcher := NewDispatcher(resolver, query, &snapshotStub{}, &playbackStub{}, Config{
		Location: time.UTC, MaxRangeHours: 24, CursorTTL: time.Minute, Now: func() time.Time { return now },
	})
	base := Request{ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: testDeviceCode, ChannelID: testChannelCode}
	base.Body = []byte(`{"startTime":"2026-10-11T10:00:00Z","endTime":"2026-10-11T11:00:00Z"}`)
	base.Query = map[string]string{"pageSize": "1"}
	data, err := dispatcher.Handle(context.Background(), ScopeRecordQuery, base)
	require.NoError(t, err)
	cursor := data.(RecordPage).NextCursor
	require.NotEmpty(t, cursor)

	base.ClientID = 32
	base.Body = nil
	base.Query = map[string]string{"cursor": cursor}
	_, err = dispatcher.Handle(context.Background(), ScopeRecordQuery, base)
	require.ErrorIs(t, err, adapters.ErrNotFound)
	require.Equal(t, 1, query.calls)

	base.ClientID = 31
	base.ResourceScope.OwnerDeptID = 11
	_, err = dispatcher.Handle(context.Background(), ScopeRecordQuery, base)
	require.ErrorIs(t, err, adapters.ErrNotFound)
	base.ResourceScope.OwnerDeptID = 10

	query.current = false
	_, err = dispatcher.Handle(context.Background(), ScopeRecordQuery, base)
	require.ErrorIs(t, err, adapters.ErrNotFound, "a newer query invalidates the prior cursor")
	query.current = true

	base.Body = []byte(`{"startTime":"2026-10-11T10:00:00Z","endTime":"2026-10-11T11:00:00Z"}`)
	base.Query = map[string]string{"pageSize": "1"}
	data, err = dispatcher.Handle(context.Background(), ScopeRecordQuery, base)
	require.NoError(t, err)
	cursor = data.(RecordPage).NextCursor
	base.Body = nil
	base.Query = map[string]string{"cursor": cursor}
	now = now.Add(2 * time.Minute)
	_, err = dispatcher.Handle(context.Background(), ScopeRecordQuery, base)
	require.ErrorIs(t, err, adapters.ErrNotFound, "expired cursors must not return cached records")
}

func TestRecordingAdapterRegistrationsMapInvocationIntoTypedDispatcher(t *testing.T) {
	query := &queryStub{current: true, result: recordquery.QueryResult{QueryID: "query-1", Status: recordquery.QueryStatusEmpty}}
	dispatcher := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) {
		return testTarget(), nil
	}), query, &snapshotStub{}, &playbackStub{})
	registrations := NewAdapterRegistrations(dispatcher)
	require.Len(t, registrations, 9)
	registry := catalogruntime.NewAdapterRegistry()
	for _, registration := range registrations {
		require.NoError(t, registry.Register(registration))
	}
	adapter, err := registry.Resolve(RecordQueryAdapterKey, "v1")
	require.NoError(t, err)
	data, err := adapter.Execute(context.Background(), catalogruntime.Invocation{
		Scope: ScopeRecordQuery, Method: "POST", Path: "/openapi/v1/devices/" + testDeviceCode + "/channels/" + testChannelCode + "/record-query",
		Params: map[string]string{"deviceId": testDeviceCode, "channelId": testChannelCode},
		Query:  map[string]string{"pageSize": "50"},
		Value:  adapters.Request{ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: testDeviceCode, ChannelID: testChannelCode},
		Body:   []byte(`{"startTime":"2026-10-11T10:00:00Z","endTime":"2026-10-11T11:00:00Z"}`),
	})
	require.NoError(t, err)
	require.Equal(t, testDeviceCode, query.request.DeviceCode)
	require.Equal(t, testChannelCode, query.request.ChannelCode)
	require.EqualValues(t, 31, query.request.OwnerClientID)
	require.Equal(t, 50, data.(RecordPage).PageSize, "the signed query parameter must reach the typed adapter")
}

func TestPlaybackActionAdapterMapsAuthenticatedPathAndSignedBody(t *testing.T) {
	target := testTarget()
	session := &gbplayback.Session{
		ID: "session-one", OwnerID: "openapi-client:31", DeviceID: testDeviceCode,
		ChannelID: "71", Authorization: target.Authorization,
	}
	plays := &playbackStub{peek: session}
	dispatcher := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) {
		return target, nil
	}), &queryStub{}, &snapshotStub{}, plays)
	var actionAdapter catalogruntime.Adapter
	for _, registration := range NewAdapterRegistrations(dispatcher) {
		if registration.Key == PlaybackActionAdapterKey {
			actionAdapter = registration.Adapter
		}
	}
	require.NotNil(t, actionAdapter)
	_, err := actionAdapter.Execute(context.Background(), catalogruntime.Invocation{
		Scope: ScopePlaybackAction, Method: "POST", Params: map[string]string{"sessionId": "session-one"},
		Value: adapters.Request{
			ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment},
			DeviceID: testDeviceCode, ChannelID: testChannelCode,
		},
		Body: []byte(`{"action":"pause","positionSeconds":0,"scale":0}`),
	})
	require.NoError(t, err)
	require.Equal(t, "openapi-client:31", plays.actionOwner)
	require.Equal(t, "pause", plays.actionRequest.Action)
	require.Equal(t, "session-one", plays.peek.ID)
}

func recordKeys(items []RecordItem) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.RecordKey)
	}
	return keys
}

func TestRecordingSearchRejectsInvalidRangeAndUnknownBodyFieldsBeforeDeviceQuery(t *testing.T) {
	query := &queryStub{}
	dispatcher := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) {
		return testTarget(), nil
	}), query, &snapshotStub{}, &playbackStub{})
	base := Request{ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: testDeviceCode, ChannelID: testChannelCode}

	base.Body = []byte(`{"startTime":"2026-10-11T11:00:00Z","endTime":"2026-10-11T10:00:00Z"}`)
	_, err := dispatcher.Handle(context.Background(), ScopeRecordQuery, base)
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)
	base.Body = []byte(`{"startTime":"2026-10-11T10:00:00Z","endTime":"2026-10-11T10:01:00Z","unexpected":true}`)
	_, err = dispatcher.Handle(context.Background(), ScopeRecordQuery, base)
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)
	require.Zero(t, query.calls)
}

func TestRecordingSearchRejectsMalformedSignedPaginationQuery(t *testing.T) {
	query := &queryStub{}
	dispatcher := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) {
		return testTarget(), nil
	}), query, &snapshotStub{}, &playbackStub{})
	request := Request{
		ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment},
		DeviceID: testDeviceCode, ChannelID: testChannelCode,
		Body: []byte(`{"startTime":"2026-10-11T10:00:00Z","endTime":"2026-10-11T11:00:00Z"}`),
	}
	for _, queryValues := range []map[string]string{
		{"unknown": "value"}, {"pageSize": "0"}, {"pageSize": "101"}, {"cursor": ""},
	} {
		request.Query = queryValues
		_, err := dispatcher.Handle(context.Background(), ScopeRecordQuery, request)
		require.ErrorIs(t, err, adapters.ErrInvalidRequest, "query=%v", queryValues)
	}
	require.Zero(t, query.calls)
}

func TestPlaybackCreateBindsSnapshotAndSessionToOpenAPIClient(t *testing.T) {
	start := time.Date(2026, 10, 11, 2, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)
	snapshots := &snapshotStub{snapshot: recordquery.Snapshot{
		OwnerClientID: 31, ChannelID: 71, RecordKey: "opaque-record-key", DeviceCode: testDeviceCode, ChannelCode: testChannelCode,
		SegmentStart: start, SegmentEnd: end,
	}}
	plays := &playbackStub{createResult: gbplayback.CreateResult{Session: &gbplayback.Session{
		ID: "session-one", OwnerID: "openapi-client:31", DeviceID: testDeviceCode, ChannelID: "71",
		RecordKey: "opaque-record-key", Mode: gbplayback.ModePlayback, State: gbplayback.StatePlaying,
		Authorization: testTarget().Authorization, SegmentStart: start, SegmentEnd: end, PlayFrom: start,
	}}}
	dispatcher := testDispatcher(targetResolverFunc(func(_ context.Context, _ resource.DepartmentScope, deviceID, channelID string) (Target, error) {
		require.Equal(t, testDeviceCode, deviceID)
		require.Equal(t, testChannelCode, channelID)
		return testTarget(), nil
	}), &queryStub{}, snapshots, plays)
	data, err := dispatcher.Handle(context.Background(), ScopePlaybackCreate, Request{
		ClientID: 31, IdempotencyKey: "idem-1", ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment},
		DeviceID: testDeviceCode, ChannelID: testChannelCode,
		Body: []byte(`{"recordKey":"opaque-record-key","playFrom":"2026-10-11T10:01:00+08:00"}`),
	})
	require.NoError(t, err)
	require.EqualValues(t, 31, snapshots.request.OwnerClientID)
	require.Zero(t, snapshots.request.OwnerUserID)
	require.Len(t, plays.createRequests, 1)
	create := plays.createRequests[0]
	require.Equal(t, "openapi-client:31", create.OwnerID)
	require.Equal(t, "idem-1", create.IdempotencyKey)
	require.Equal(t, gbplayback.ModePlayback, create.Mode)
	require.Equal(t, testTarget().Authorization, create.Authorization)
	require.Equal(t, "192.0.2.10:5060", create.Destination)
	require.True(t, create.TCPMode)
	body, err := json.Marshal(data)
	require.NoError(t, err)
	require.Contains(t, string(body), "session-one")
	require.NotContains(t, string(body), "CallID")
}

func TestPlaybackSessionOperationsRequireClientOwnershipAndCurrentDepartmentScope(t *testing.T) {
	target := testTarget()
	session := &gbplayback.Session{ID: "session-one", OwnerID: "openapi-client:31", DeviceID: testDeviceCode, ChannelID: "71", Authorization: target.Authorization}
	plays := &playbackStub{peek: session, get: session}
	resolveCalls := 0
	dispatcher := testDispatcher(targetResolverFunc(func(_ context.Context, scope resource.DepartmentScope, deviceID, channelID string) (Target, error) {
		resolveCalls++
		require.Equal(t, resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, scope)
		require.Equal(t, testDeviceCode, deviceID)
		require.Equal(t, testChannelCode, channelID)
		return target, nil
	}), &queryStub{}, &snapshotStub{}, plays)

	_, err := dispatcher.Handle(context.Background(), ScopePlaybackRead, Request{
		ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: testDeviceCode, ChannelID: testChannelCode, SessionID: "session-one",
	})
	require.NoError(t, err)
	require.Equal(t, 1, resolveCalls)
	require.Equal(t, "openapi-client:31", plays.getOwner)

	plays.peek = nil
	_, err = dispatcher.Handle(context.Background(), ScopePlaybackAction, Request{
		ClientID: 32, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, DeviceID: testDeviceCode, ChannelID: testChannelCode, SessionID: "session-one",
		Body: []byte(`{"action":"pause"}`),
	})
	require.ErrorIs(t, err, adapters.ErrNotFound)
	require.Equal(t, "", plays.actionOwner, "the domain action service must not receive a cross-client session")
	require.Equal(t, 1, resolveCalls, "cross-client session is rejected before target lookup")
}

func TestPlaybackRejectsTargetMovedOutsideClientScopeBeforeTouchOrAction(t *testing.T) {
	target := testTarget()
	session := &gbplayback.Session{ID: "session-one", OwnerID: "openapi-client:31", DeviceID: testDeviceCode, Authorization: target.Authorization}
	plays := &playbackStub{peek: session, get: session}
	dispatcher := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) {
		return Target{}, resource.ErrResourceNotFound
	}), &queryStub{}, &snapshotStub{}, plays)
	_, err := dispatcher.Handle(context.Background(), ScopePlaybackAction, Request{
		ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 20, DataScope: resource.DataScopeDepartment}, DeviceID: testDeviceCode, ChannelID: testChannelCode, SessionID: "session-one",
		Body: []byte(`{"action":"pause"}`),
	})
	require.ErrorIs(t, err, adapters.ErrNotFound)
	require.Empty(t, plays.actionOwner)
	require.Empty(t, plays.getOwner, "failed scope revalidation must not refresh the playback idle lease")
}

func TestRecordingErrorsPreserveExplicitOfflineAndBusyCauses(t *testing.T) {
	require.ErrorIs(t, mapQueryError(recordquery.ErrBusy), adapters.ErrConflict)
	require.ErrorIs(t, mapQueryError(errors.Join(recordquery.ErrSendFailed, errors.New("sip timeout"))), adapters.ErrUnavailable)
}

func TestRecordingTargetReadinessAndProtocolVersionFailClosed(t *testing.T) {
	query := &queryStub{}
	base := Request{
		ClientID: 31, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment},
		DeviceID: testDeviceCode, ChannelID: testChannelCode,
		Body: []byte(`{"startTime":"2026-10-11T10:00:00Z","endTime":"2026-10-11T11:00:00Z"}`),
	}
	for _, test := range []struct {
		name   string
		mutate func(*Target)
		want   error
	}{
		{name: "offline", mutate: func(target *Target) { target.DeviceOnline = false }, want: adapters.ErrDeviceOffline},
		{name: "unknown protocol", mutate: func(target *Target) { target.EffectiveVersion = "" }, want: adapters.ErrCapabilityNotSupported},
		{name: "missing endpoint", mutate: func(target *Target) { target.DeviceIP = "" }, want: adapters.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := testTarget()
			test.mutate(&target)
			dispatcher := testDispatcher(targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) {
				return target, nil
			}), query, &snapshotStub{}, &playbackStub{})
			_, err := dispatcher.Handle(context.Background(), ScopeRecordQuery, base)
			require.ErrorIs(t, err, test.want)
		})
	}
	require.Zero(t, query.calls)
}

// The options endpoint exists so a caller can prepare a search before the
// channel comes back online: it must answer for an offline target and must
// never open a SIP query.
func TestRecordQueryOptionsAnswerForOfflineChannelWithoutQueryingDevice(t *testing.T) {
	resolver := targetResolverFunc(func(_ context.Context, scope resource.DepartmentScope, deviceID, channelID string) (Target, error) {
		require.Equal(t, testDeviceCode, deviceID)
		require.Equal(t, testChannelCode, channelID)
		target := testTarget()
		target.DeviceName, target.ChannelName = "front-device", "front-channel"
		target.DeviceOnline, target.ChannelOnline = false, false
		return target, nil
	})
	query := &queryStub{}
	dispatcher := testDispatcher(resolver, query, &snapshotStub{}, &playbackStub{})
	scope := resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}

	data, err := dispatcher.Handle(context.Background(), ScopeRecordQueryOptions, Request{
		ClientID: 31, ResourceScope: scope, DeviceID: testDeviceCode, ChannelID: testChannelCode,
	})
	require.NoError(t, err)
	options, ok := data.(RecordQueryOptions)
	require.True(t, ok)
	assert.Equal(t, testDeviceCode, options.Device.ID)
	assert.Equal(t, "front-device", options.Device.Name)
	assert.False(t, options.Device.Online, "offline is reported, not rejected")
	assert.Equal(t, testChannelCode, options.Channel.ID)
	assert.Equal(t, "CST", options.Timezone)
	assert.Equal(t, 24, options.MaxRangeHours)
	assert.Equal(t, []string{"all", "manual", "alarm"}, options.SupportedTypes)
	assert.NotEmpty(t, options.ServerNow)
	_, err = time.Parse(time.RFC3339, options.ServerNow)
	require.NoError(t, err, "serverNow must stay RFC3339 so a caller can echo the window")
	require.Zero(t, query.calls, "reading options must never reach the device")

	_, err = dispatcher.Handle(context.Background(), ScopeRecordQueryOptions, Request{
		ClientID: 31, ResourceScope: scope, DeviceID: testDeviceCode, ChannelID: testChannelCode,
		Body: []byte(`{}`),
	})
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)

	_, err = dispatcher.Handle(context.Background(), ScopeRecordQueryOptions, Request{
		ClientID: 31, ResourceScope: scope, DeviceID: testDeviceCode, ChannelID: testChannelCode,
		Query: map[string]string{"pageSize": "10"},
	})
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)

	missing := targetResolverFunc(func(context.Context, resource.DepartmentScope, string, string) (Target, error) {
		return Target{}, adapters.ErrNotFound
	})
	_, err = testDispatcher(missing, query, &snapshotStub{}, &playbackStub{}).Handle(context.Background(), ScopeRecordQueryOptions, Request{
		ClientID: 31, ResourceScope: scope, DeviceID: testDeviceCode, ChannelID: testChannelCode,
	})
	require.ErrorIs(t, err, adapters.ErrNotFound)
}
