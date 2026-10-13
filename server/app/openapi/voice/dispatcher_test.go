package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/talk"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

type targetResolverFunc func(context.Context, resource.DepartmentScope, string, string) (Target, error)

func (resolver targetResolverFunc) Resolve(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (Target, error) {
	return resolver(ctx, scope, deviceID, channelID)
}

type fakeTalkService struct {
	now           time.Time
	createCalls   []talk.CreateRequest
	sessions      map[string]*gbmodels.GbTalkSession
	idempotency   map[string]string
	cleanupCalls  []string
	prepareCalls  []string
	prepareResult *talk.UplinkTarget
}

func newFakeTalkService() *fakeTalkService {
	return &fakeTalkService{now: time.Date(2026, 10, 11, 2, 0, 0, 0, time.UTC), sessions: map[string]*gbmodels.GbTalkSession{}, idempotency: map[string]string{},
		prepareResult: &talk.UplinkTarget{URL: "https://media.internal/whip?token=server-only", ContentType: "application/sdp"}}
}

func (service *fakeTalkService) Create(_ context.Context, request talk.CreateRequest) (*talk.CreateResult, error) {
	service.createCalls = append(service.createCalls, request)
	key := openAPIIdempotencyIdentity(request.OpenAPIClientID, request.OpenAPIIdempotencyKey)
	if sessionID := service.idempotency[key]; sessionID != "" {
		session := service.sessions[sessionID]
		if session.OpenAPIRequestHash != request.OpenAPIRequestHash {
			return nil, talk.ErrOpenAPIIdempotencyConflict
		}
		return createResult(session), nil
	}
	sessionID := "session-" + string(rune('0'+len(service.createCalls)))
	keyValue := request.OpenAPIIdempotencyKey
	session := &gbmodels.GbTalkSession{SessionID: sessionID, ChannelID: request.Channel.ID, DeviceID: request.Device.DeviceID,
		TargetID: request.Channel.ChannelID, OpenAPIClientID: request.OpenAPIClientID, OpenAPIIdempotencyKey: &keyValue,
		OpenAPIRequestHash: request.OpenAPIRequestHash, Mode: request.Mode, State: gbmodels.TalkSessionReserved, ExpiresAt: service.now.Add(time.Minute)}
	service.sessions[sessionID] = session
	service.idempotency[key] = sessionID
	return createResult(session), nil
}

func createResult(session *gbmodels.GbTalkSession) *talk.CreateResult {
	return &talk.CreateResult{SessionID: session.SessionID, Mode: session.Mode, State: string(session.State), ExpiresAt: session.ExpiresAt,
		Uplink: talk.UplinkDescriptor{Protocol: "whip", Path: "/talk-sessions/" + session.SessionID + "/uplink", ContentType: "application/sdp",
			ICEServers: []talk.ICEServer{{URLs: []string{"stun:media.example.test:3478"}}}}}
}

func (service *fakeTalkService) GetForOpenAPIClient(_ context.Context, clientID int64, sessionID string) (*gbmodels.GbTalkSession, error) {
	session := service.sessions[sessionID]
	if session == nil || session.OpenAPIClientID != clientID {
		return nil, nil
	}
	copy := *session
	return &copy, nil
}

func (service *fakeTalkService) CleanupForOpenAPIClient(_ context.Context, clientID int64, sessionID string, terminal gbmodels.TalkSessionState, _ string) error {
	session := service.sessions[sessionID]
	if session == nil || session.OpenAPIClientID != clientID {
		return talk.ErrTalkSessionNotFound
	}
	service.cleanupCalls = append(service.cleanupCalls, sessionID)
	session.State = terminal
	return nil
}

func (service *fakeTalkService) PrepareUplinkForOpenAPIClient(_ context.Context, clientID int64, sessionID string) (*talk.UplinkTarget, error) {
	service.prepareCalls = append(service.prepareCalls, sessionID)
	session := service.sessions[sessionID]
	if session == nil || session.OpenAPIClientID != clientID {
		return nil, talk.ErrTalkSessionNotFound
	}
	return service.prepareResult, nil
}

func openAPIIdempotencyIdentity(clientID int64, key string) string {
	return fmt.Sprintf("%d\x00%s", clientID, key)
}

func dereference(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func testTarget() Target {
	device := gbmodels.GbDevice{DeviceID: "34020000001320000001", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 10}
	device.ID = 7
	channel := gbmodels.GbChannel{ID: 71, DeviceID: device.DeviceID, ChannelID: "34020000001320000002", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 10}
	return Target{Device: device, Channel: channel}
}

func otherTestTarget() Target {
	device := gbmodels.GbDevice{DeviceID: "34020000001320000003", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 10}
	device.ID = 8
	channel := gbmodels.GbChannel{ID: 72, DeviceID: device.DeviceID, ChannelID: "34020000001320000004", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 10}
	return Target{Device: device, Channel: channel}
}

func newTestDispatcher(service *fakeTalkService) *Dispatcher {
	return NewDispatcher(targetResolverFunc(func(_ context.Context, scope resource.DepartmentScope, deviceID, channelID string) (Target, error) {
		if scope.OwnerDeptID != 10 {
			return Target{}, adapters.ErrNotFound
		}
		for _, target := range []Target{testTarget(), otherTestTarget()} {
			if deviceID == target.Device.DeviceID && channelID == target.Channel.ChannelID {
				return target, nil
			}
		}
		return Target{}, adapters.ErrNotFound
	}), service)
}

func testRequest(clientID int64) Request {
	target := testTarget()
	return Request{ClientID: clientID, IdempotencyKey: "voice-create-1",
		ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment},
		DeviceID:      target.Device.DeviceID, ChannelID: target.Channel.ChannelID}
}

func TestVoiceCreateUsesClientIdentityAndClientScopedIdempotency(t *testing.T) {
	service := newFakeTalkService()
	dispatcher := newTestDispatcher(service)
	request := testRequest(41)

	firstData, err := dispatcher.Handle(context.Background(), ScopeTalkCreate, request)
	require.NoError(t, err)
	first := firstData.(SessionView)
	require.Equal(t, "talk", string(first.Mode))
	require.NotEmpty(t, first.Media.Path)
	require.EqualValues(t, 41, service.createCalls[0].OpenAPIClientID)
	require.EqualValues(t, 0, service.createCalls[0].ActorID, "OpenAPI client IDs must not be impersonated as user IDs")
	require.Equal(t, "voice-create-1", service.createCalls[0].OpenAPIIdempotencyKey)
	require.NotEmpty(t, service.createCalls[0].OpenAPIRequestHash)

	secondData, err := dispatcher.Handle(context.Background(), ScopeTalkCreate, request)
	require.NoError(t, err)
	second := secondData.(SessionView)
	require.Equal(t, first.SessionID, second.SessionID, "same client/key/payload reuses the durable talk session")

	otherClient := testRequest(42)
	otherTarget := otherTestTarget()
	otherClient.DeviceID = otherTarget.Device.DeviceID
	otherClient.ChannelID = otherTarget.Channel.ChannelID
	thirdData, err := dispatcher.Handle(context.Background(), ScopeTalkCreate, otherClient)
	require.NoError(t, err)
	third := thirdData.(SessionView)
	require.NotEqual(t, first.SessionID, third.SessionID, "an identical idempotency key is isolated by client")
	require.EqualValues(t, 42, service.createCalls[2].OpenAPIClientID)
}

func TestVoiceReadAndEndRequireClientAndDepartmentOwnedSession(t *testing.T) {
	service := newFakeTalkService()
	dispatcher := newTestDispatcher(service)
	request := testRequest(41)
	created, err := dispatcher.Handle(context.Background(), ScopeTalkCreate, request)
	require.NoError(t, err)
	sessionID := created.(SessionView).SessionID

	otherClient := request
	otherClient.ClientID = 42
	otherClient.SessionID = sessionID
	otherClient.IdempotencyKey = ""
	_, err = dispatcher.Handle(context.Background(), ScopeTalkRead, otherClient)
	require.ErrorIs(t, err, adapters.ErrNotFound)
	_, err = dispatcher.Handle(context.Background(), ScopeTalkEnd, otherClient)
	require.ErrorIs(t, err, adapters.ErrNotFound)
	require.Empty(t, service.cleanupCalls, "an unauthorized client must not reach cleanup")

	ownerRequest := request
	ownerRequest.SessionID = sessionID
	ownerRequest.IdempotencyKey = ""
	readData, err := dispatcher.Handle(context.Background(), ScopeTalkRead, ownerRequest)
	require.NoError(t, err)
	require.Equal(t, sessionID, readData.(SessionView).SessionID)
	endData, err := dispatcher.Handle(context.Background(), ScopeTalkEnd, ownerRequest)
	require.NoError(t, err)
	require.Equal(t, string(gbmodels.TalkSessionEnded), endData.(SessionView).State)
	require.Equal(t, []string{sessionID}, service.cleanupCalls)

	moved := ownerRequest
	moved.ResourceScope.OwnerDeptID = 20
	_, err = dispatcher.Handle(context.Background(), ScopeTalkRead, moved)
	require.ErrorIs(t, err, adapters.ErrNotFound)
}

func TestBroadcastHasSeparateCreateReadAndEndScopes(t *testing.T) {
	service := newFakeTalkService()
	dispatcher := newTestDispatcher(service)
	request := testRequest(41)
	data, err := dispatcher.Handle(context.Background(), ScopeBroadcastCreate, request)
	require.NoError(t, err)
	created := data.(SessionView)
	require.Equal(t, gbmodels.TalkSessionModeBroadcast, created.Mode)
	require.Equal(t, gbmodels.TalkSessionModeBroadcast, service.createCalls[0].Mode)

	request.SessionID = created.SessionID
	request.IdempotencyKey = ""
	_, err = dispatcher.Handle(context.Background(), ScopeTalkRead, request)
	require.ErrorIs(t, err, adapters.ErrNotFound, "talk scope cannot read a broadcast session")
	_, err = dispatcher.Handle(context.Background(), ScopeBroadcastRead, request)
	require.NoError(t, err)
	_, err = dispatcher.Handle(context.Background(), ScopeBroadcastEnd, request)
	require.NoError(t, err)
}

func TestVoiceMediaResourceRequiresClientOwnershipAndNeverSerializesOneUseURL(t *testing.T) {
	service := newFakeTalkService()
	dispatcher := newTestDispatcher(service)
	request := testRequest(41)
	data, err := dispatcher.Handle(context.Background(), ScopeTalkCreate, request)
	require.NoError(t, err)
	view := data.(SessionView)
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "media.internal")
	require.NotContains(t, string(encoded), "token=")
	require.NotContains(t, string(encoded), "publish")

	otherClient := request
	otherClient.ClientID = 42
	otherClient.SessionID = view.SessionID
	otherClient.IdempotencyKey = ""
	_, err = dispatcher.PrepareMedia(context.Background(), ScopeTalkRead, otherClient)
	require.ErrorIs(t, err, adapters.ErrNotFound)
	require.Empty(t, service.prepareCalls, "ownership must be checked before rotating media credentials")

	ownerRequest := request
	ownerRequest.SessionID = view.SessionID
	ownerRequest.IdempotencyKey = ""
	media, err := dispatcher.PrepareMedia(context.Background(), ScopeTalkRead, ownerRequest)
	require.NoError(t, err)
	require.Equal(t, "https://media.internal/whip?token=server-only", media.URL, "only the media proxy may consume this internal target")
	require.Len(t, service.prepareCalls, 1)
}

func TestVoiceCreateRejectsInvalidScopeTargetAndIdempotency(t *testing.T) {
	service := newFakeTalkService()
	dispatcher := newTestDispatcher(service)
	request := testRequest(41)
	request.IdempotencyKey = "bad key"
	_, err := dispatcher.Handle(context.Background(), ScopeTalkCreate, request)
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)

	request = testRequest(41)
	request.ChannelID = "other-channel"
	_, err = dispatcher.Handle(context.Background(), ScopeTalkCreate, request)
	require.ErrorIs(t, err, adapters.ErrNotFound)

	request = testRequest(0)
	_, err = dispatcher.Handle(context.Background(), ScopeTalkCreate, request)
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)

	_, err = dispatcher.Handle(context.Background(), "device:voice-admin", testRequest(41))
	require.ErrorIs(t, err, adapters.ErrUnavailable)
	require.False(t, errors.Is(err, adapters.ErrConflict))
}

func TestVoiceAdapterChecksScopeAndPathParameters(t *testing.T) {
	adapter := NewAdapter(ScopeTalkRead, newTestDispatcher(newFakeTalkService()))
	_, err := adapter.Execute(context.Background(), adapterInvocation(ScopeBroadcastRead, Request{}))
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)
	invocation := adapterInvocation(ScopeTalkRead, testRequest(41))
	invocation.Params["sessionId"] = "other-session"
	_, err = adapter.Execute(context.Background(), invocation)
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)
	_, err = adapter.Execute(context.Background(), adapterInvocation(ScopeTalkRead, Request{ClientID: 41}))
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)
}

func TestVoiceAdapterAcceptsGatewayRequestAndEmptyJSONBody(t *testing.T) {
	service := newFakeTalkService()
	dispatcher := newTestDispatcher(service)
	request := testRequest(41)
	invocation := catalogruntime.Invocation{
		Scope: ScopeTalkCreate, Method: methodForScope(ScopeTalkCreate), Body: []byte(`{}`),
		Params: map[string]string{"deviceId": request.DeviceID, "channelId": request.ChannelID},
		Value: adapters.Request{
			ClientID: request.ClientID, IdempotencyKey: request.IdempotencyKey, ResourceScope: request.ResourceScope,
		},
	}
	_, err := NewAdapter(ScopeTalkCreate, dispatcher).Execute(context.Background(), invocation)
	require.NoError(t, err)
	require.Len(t, service.createCalls, 1)
	require.EqualValues(t, request.ClientID, service.createCalls[0].OpenAPIClientID)

	invocation.Body = []byte(`{"unexpected":true}`)
	_, err = NewAdapter(ScopeTalkCreate, dispatcher).Execute(context.Background(), invocation)
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)
	require.Len(t, service.createCalls, 1, "invalid body must be rejected before the talk service")
}

func adapterInvocation(scope string, request Request) catalogruntime.Invocation {
	params := map[string]string{"deviceId": request.DeviceID, "channelId": request.ChannelID}
	if scope != ScopeTalkCreate && scope != ScopeBroadcastCreate {
		params["sessionId"] = request.SessionID
	}
	return catalogruntime.Invocation{Scope: scope, Method: methodForScope(scope), Params: params, Value: request}
}

func TestVoiceViewDoesNotEmbedDomainErrorOrInternalIDs(t *testing.T) {
	service := newFakeTalkService()
	dispatcher := newTestDispatcher(service)
	request := testRequest(41)
	data, err := dispatcher.Handle(context.Background(), ScopeTalkCreate, request)
	require.NoError(t, err)
	view := data.(SessionView)
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "channelId\":71")
	require.NotContains(t, string(encoded), "deviceId\":\"34020000001320000001\"")
	require.False(t, strings.Contains(string(encoded), "sourceStream"))
}
