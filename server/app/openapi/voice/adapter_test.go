package voice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/talk"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
)

func TestVoiceMediaAdapterProxiesOfferAndHidesUpstreamLocation(t *testing.T) {
	var receivedOffer string
	var receivedMethod string
	var receivedContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read offer: %v", err)
		}
		receivedOffer = string(body)
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/sdp")
		w.Header().Set("Location", "https://media.internal/private/resource")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("v=0\r\no=media answer\r\n"))
	}))
	defer server.Close()

	service := newFakeTalkService()
	service.prepareResult.URL = server.URL + "/whip?token=private"
	dispatcher := newTestDispatcher(service)
	request := createVoiceSession(t, dispatcher, ScopeTalkCreate, testRequest(41))
	request.IdempotencyKey = ""
	request.SessionID = "session-1"
	adapter := NewMediaAdapter(ScopeTalkMedia, dispatcher, talk.NewUplinkProxyClient())
	invocation := mediaInvocation(ScopeTalkMedia, request, `{"sdp":"v=0\r\no=client offer\r\n"}`)

	result, err := adapter.Execute(context.Background(), invocation)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, receivedMethod)
	require.Equal(t, "application/sdp", receivedContentType)
	require.Equal(t, "v=0\r\no=client offer\r\n", receivedOffer)
	answer, ok := result.(MediaAnswer)
	require.True(t, ok)
	require.Equal(t, "v=0\r\no=media answer\r\n", answer.SDP)
	encoded, err := json.Marshal(answer)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "media.internal")
	require.NotContains(t, string(encoded), "token=")
	require.Len(t, service.prepareCalls, 1)
}

func TestVoiceMediaAdapterRejectsOtherClientsBeforeIssuingUplink(t *testing.T) {
	service := newFakeTalkService()
	dispatcher := newTestDispatcher(service)
	request := createVoiceSession(t, dispatcher, ScopeTalkCreate, testRequest(41))
	request.ClientID = 42
	request.IdempotencyKey = ""
	request.SessionID = "session-1"
	adapter := NewMediaAdapter(ScopeTalkMedia, dispatcher, talk.NewUplinkProxyClient())

	_, err := adapter.Execute(context.Background(), mediaInvocation(ScopeTalkMedia, request, `{"sdp":"v=0\r\n"}`))
	require.ErrorIs(t, err, adapters.ErrNotFound)
	require.Empty(t, service.prepareCalls)
}

func TestVoiceMediaAdapterRejectsInvalidSDPBeforeIssuingUplink(t *testing.T) {
	service := newFakeTalkService()
	dispatcher := newTestDispatcher(service)
	request := createVoiceSession(t, dispatcher, ScopeTalkCreate, testRequest(41))
	request.IdempotencyKey = ""
	request.SessionID = "session-1"
	adapter := NewMediaAdapter(ScopeTalkMedia, dispatcher, talk.NewUplinkProxyClient())

	_, err := adapter.Execute(context.Background(), mediaInvocation(ScopeTalkMedia, request, `{"sdp":"   "}`))
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)
	require.Empty(t, service.prepareCalls)
}

func TestVoiceMediaAdapterDoesNotFollowUpstreamRedirects(t *testing.T) {
	var redirectedRequests int
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirectedRequests++ }))
	defer redirectTarget.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	service := newFakeTalkService()
	service.prepareResult.URL = redirector.URL
	dispatcher := newTestDispatcher(service)
	request := createVoiceSession(t, dispatcher, ScopeTalkCreate, testRequest(41))
	request.IdempotencyKey = ""
	request.SessionID = "session-1"
	adapter := NewMediaAdapter(ScopeTalkMedia, dispatcher, talk.NewUplinkProxyClient())

	_, err := adapter.Execute(context.Background(), mediaInvocation(ScopeTalkMedia, request, `{"sdp":"v=0\r\n"}`))
	require.ErrorIs(t, err, adapters.ErrUnavailable)
	require.Zero(t, redirectedRequests)
}

func TestBroadcastMediaUsesBroadcastScopeAndSessionOwnership(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/sdp")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("v=0\r\n"))
	}))
	defer server.Close()

	service := newFakeTalkService()
	service.prepareResult.URL = server.URL
	dispatcher := newTestDispatcher(service)
	request := createVoiceSession(t, dispatcher, ScopeBroadcastCreate, testRequest(41))
	request.IdempotencyKey = ""
	request.SessionID = "session-1"
	adapter := NewMediaAdapter(ScopeBroadcastMedia, dispatcher, talk.NewUplinkProxyClient())

	result, err := adapter.Execute(context.Background(), mediaInvocation(ScopeBroadcastMedia, request, `{"sdp":"v=0\r\n"}`))
	require.NoError(t, err)
	require.Equal(t, MediaAnswer{SDP: "v=0\r\n"}, result)
}

func TestVoiceMediaAdapterRegistrationsIncludeSeparateMediaScopes(t *testing.T) {
	registrations := NewAdapterRegistrations(newTestDispatcher(newFakeTalkService()))
	keys := make(map[string]bool, len(registrations))
	for _, registration := range registrations {
		keys[registration.Key] = true
	}
	require.True(t, keys[TalkMediaAdapterKey])
	require.True(t, keys[BroadcastMediaAdapterKey])
}

func createVoiceSession(t *testing.T, dispatcher *Dispatcher, scope string, request Request) Request {
	t.Helper()
	_, err := dispatcher.Handle(context.Background(), scope, request)
	require.NoError(t, err)
	request.SessionID = "session-1"
	return request
}

func mediaInvocation(scope string, request Request, body string) catalogruntime.Invocation {
	return catalogruntime.Invocation{
		Scope: scope, Method: http.MethodPost,
		Params: map[string]string{"deviceId": request.DeviceID, "channelId": request.ChannelID, "sessionId": request.SessionID},
		Body:   []byte(strings.TrimSpace(body)),
		Value: adapters.Request{ClientID: request.ClientID, ResourceScope: request.ResourceScope,
			DeviceID: request.DeviceID, ChannelID: request.ChannelID},
	}
}
