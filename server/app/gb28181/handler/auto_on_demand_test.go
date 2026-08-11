package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type fakeAutoNodeResolver struct {
	node *node.Node
}

func (f *fakeAutoNodeResolver) ResolveAutoOnDemandNode(uuid string) (*node.Node, bool) {
	if f.node == nil || uuid != f.node.MediaServerUUID {
		return nil, false
	}
	copy := *f.node
	return &copy, true
}

type fakeAutoTargetValidator struct {
	err   error
	calls int
}

func (f *fakeAutoTargetValidator) ValidateAutoOnDemandTarget(_ context.Context, _, _ string) error {
	f.calls++
	return f.err
}

type fakeAutoDispatcher struct {
	mu        sync.Mutex
	err       error
	available bool
	requests  []play.Request
}

func (f *fakeAutoDispatcher) Available() bool { return f.available }

func (f *fakeAutoDispatcher) Submit(request play.Request) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, request)
	return f.err
}

func (f *fakeAutoDispatcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

type autoOnDemandFixture struct {
	controller *handler.HookController
	engine     *gin.Engine
	resolver   *fakeAutoNodeResolver
	validator  *fakeAutoTargetValidator
	dispatcher *fakeAutoDispatcher
	signer     *playauth.Signer
	settings   gbconfig.FixedAddressPlaybackSettings
	deviceID   string
	channelID  string
	streamID   string
	capability string
	token      string
	path       string
	peer       string
	body       map[string]interface{}
	raw        []byte
	xff        string
}

func newAutoOnDemandFixture(t *testing.T) *autoOnDemandFixture {
	t.Helper()
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := playauth.NewSigner(
		[]byte("0123456789abcdef0123456789abcdef"),
		playauth.WithNow(func() time.Time { return now }),
	)
	require.NoError(t, err)
	deviceID := "37010301021320000014"
	channelID := "37010301021320000001"
	streamID := deviceID + "_" + channelID
	mediaNode := &node.Node{
		ID: 7, Host: "192.0.2.1", APISecret: "node-api-secret",
		MediaServerUUID: "node-a", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 30100,
	}
	grant, err := signer.IssueDirect(playauth.Binding{
		DeviceID: deviceID, ChannelID: channelID, App: "rtp",
		Stream: streamID, MediaServerID: mediaNode.MediaServerUUID,
	})
	require.NoError(t, err)
	capability, err := playauth.CallbackCapability(mediaNode.APISecret, mediaNode.MediaServerUUID)
	require.NoError(t, err)

	resolver := &fakeAutoNodeResolver{node: mediaNode}
	validator := &fakeAutoTargetValidator{}
	dispatcher := &fakeAutoDispatcher{available: true}
	settings := gbconfig.FixedAddressPlaybackSettings{FixedAddressEnabled: true, AutoOnDemandEnabled: true}
	controller := handler.NewHookController(stream.NewNotifier())
	controller.SetPlayAuthorizer(signer)
	controller.SetAutoOnDemandSettingsProvider(func() gbconfig.FixedAddressPlaybackSettings { return settings })
	controller.SetAutoOnDemandRuntime(resolver, validator, dispatcher)
	engine := gin.New()
	engine.POST("/index/hook/on_stream_not_found", controller.OnStreamNotFound)

	fixture := &autoOnDemandFixture{
		controller: controller, engine: engine, resolver: resolver,
		validator: validator, dispatcher: dispatcher, signer: signer,
		settings: settings, deviceID: deviceID, channelID: channelID,
		streamID: streamID, capability: capability, token: grant.Token,
		peer: "192.0.2.1:1234",
	}
	fixture.path = "/index/hook/on_stream_not_found?cap=" + url.QueryEscape(capability)
	fixture.body = map[string]interface{}{
		"mediaServerId": mediaNode.MediaServerUUID,
		"vhost":         "__defaultVhost__",
		"app":           "rtp",
		"schema":        "fmp4",
		"stream":        streamID,
		"params":        url.Values{playauth.QueryParameter: {grant.Token}}.Encode(),
	}
	controller.SetAutoOnDemandSettingsProvider(func() gbconfig.FixedAddressPlaybackSettings { return fixture.settings })
	return fixture
}

func (f *autoOnDemandFixture) serve(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	raw := f.raw
	if raw == nil {
		var err error
		raw, err = json.Marshal(f.body)
		require.NoError(t, err)
	}
	request := httptest.NewRequest(http.MethodPost, f.path, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = f.peer
	if f.xff != "" {
		request.Header.Set("X-Forwarded-For", f.xff)
	}
	response := httptest.NewRecorder()
	f.engine.ServeHTTP(response, request)
	return response
}

func TestOnStreamNotFoundAcceptsAuthorizedFixedRequest(t *testing.T) {
	fixture := newAutoOnDemandFixture(t)
	response := fixture.serve(t)
	assertHookCode(t, response.Code, response.Body.Bytes(), 0)

	var payload struct {
		Close bool `json:"close"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.False(t, payload.Close)
	require.Equal(t, 1, fixture.validator.calls)
	require.Equal(t, 1, fixture.dispatcher.count())
	require.Equal(t, play.Request{
		DeviceID: fixture.deviceID, ChannelID: fixture.channelID,
		Trigger: "on_stream_not_found", RequiredNode: fixture.resolver.node.ID,
	}, fixture.dispatcher.requests[0])
}

func TestOnStreamNotFoundFailsClosedBeforeDispatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*autoOnDemandFixture)
	}{
		{name: "malformed json", mutate: func(f *autoOnDemandFixture) { f.raw = []byte("{") }},
		{name: "trailing json", mutate: func(f *autoOnDemandFixture) {
			raw, _ := json.Marshal(f.body)
			f.raw = append(raw, []byte(` {}`)...)
		}},
		{name: "body over limit", mutate: func(f *autoOnDemandFixture) {
			f.body["params"] = strings.Repeat("x", 17<<10)
		}},
		{name: "wrong vhost", mutate: func(f *autoOnDemandFixture) { f.body["vhost"] = "other" }},
		{name: "wrong app", mutate: func(f *autoOnDemandFixture) { f.body["app"] = "live" }},
		{name: "wrong schema", mutate: func(f *autoOnDemandFixture) { f.body["schema"] = "file" }},
		{name: "invalid fixed stream", mutate: func(f *autoOnDemandFixture) { f.body["stream"] = "0200000001" }},
		{name: "unknown node", mutate: func(f *autoOnDemandFixture) { f.body["mediaServerId"] = "unknown" }},
		{name: "node host is not literal ip", mutate: func(f *autoOnDemandFixture) { f.resolver.node.Host = "zlm.local" }},
		{name: "peer mismatch", mutate: func(f *autoOnDemandFixture) { f.peer = "192.0.2.99:1234" }},
		{name: "forwarded header ignored", mutate: func(f *autoOnDemandFixture) {
			f.peer = "192.0.2.99:1234"
			f.xff = f.resolver.node.Host
		}},
		{name: "fixed disabled", mutate: func(f *autoOnDemandFixture) { f.settings.FixedAddressEnabled = false }},
		{name: "auto disabled", mutate: func(f *autoOnDemandFixture) { f.settings.AutoOnDemandEnabled = false }},
		{name: "cap missing", mutate: func(f *autoOnDemandFixture) { f.path = "/index/hook/on_stream_not_found" }},
		{name: "cap duplicated", mutate: func(f *autoOnDemandFixture) { f.path += "&cap=" + url.QueryEscape(f.capability) }},
		{name: "cap invalid", mutate: func(f *autoOnDemandFixture) { f.path = "/index/hook/on_stream_not_found?cap=invalid" }},
		{name: "play token missing", mutate: func(f *autoOnDemandFixture) { f.body["params"] = "" }},
		{name: "play token duplicated", mutate: func(f *autoOnDemandFixture) {
			f.body["params"] = url.Values{playauth.QueryParameter: {f.token, f.token}}.Encode()
		}},
		{name: "play token invalid", mutate: func(f *autoOnDemandFixture) {
			f.body["params"] = url.Values{playauth.QueryParameter: {"invalid"}}.Encode()
		}},
		{name: "target unknown", mutate: func(f *autoOnDemandFixture) { f.validator.err = errors.New("not found") }},
		{name: "runtime missing", mutate: func(f *autoOnDemandFixture) {
			f.controller.SetAutoOnDemandRuntime(nil, nil, nil)
		}},
		{name: "dispatcher stopped", mutate: func(f *autoOnDemandFixture) { f.dispatcher.available = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAutoOnDemandFixture(t)
			test.mutate(fixture)
			response := fixture.serve(t)
			assertHookCode(t, response.Code, response.Body.Bytes(), -1)
			require.Zero(t, fixture.dispatcher.count())
		})
	}
}

func TestOnStreamNotFoundRejectsDispatcherAdmissionError(t *testing.T) {
	fixture := newAutoOnDemandFixture(t)
	fixture.dispatcher.err = errors.New("queue full")
	response := fixture.serve(t)
	assertHookCode(t, response.Code, response.Body.Bytes(), -1)
	require.Equal(t, 1, fixture.dispatcher.count())
}

func TestOnStreamNotFoundGlobalLimiterBoundsPreAuthenticationWork(t *testing.T) {
	fixture := newAutoOnDemandFixture(t)
	accepted := 0
	for i := 0; i < 40; i++ {
		response := fixture.serve(t)
		var payload struct {
			Code int `json:"code"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
		if payload.Code == 0 {
			accepted++
		}
	}
	require.Equal(t, 32, accepted)
	require.Equal(t, 32, fixture.dispatcher.count())
}

type blockingAutoEnsurer struct {
	started chan play.Request
	release chan struct{}
}

func (e *blockingAutoEnsurer) EnsureLive(ctx context.Context, request play.Request) (*play.Result, error) {
	e.started <- request
	select {
	case <-e.release:
		return &play.Result{StreamID: request.DeviceID + "_" + request.ChannelID}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestOnStreamNotFoundReturnsBeforeBackgroundEnsureLiveCompletes(t *testing.T) {
	fixture := newAutoOnDemandFixture(t)
	ensurer := &blockingAutoEnsurer{started: make(chan play.Request, 1), release: make(chan struct{})}
	dispatcher := play.NewAutoStartDispatcher(ensurer, play.AutoStartDispatcherOptions{
		KnownNodeIDs:    []int64{fixture.resolver.node.ID},
		ServiceDeadline: time.Second,
	})
	t.Cleanup(func() {
		close(ensurer.release)
		require.NoError(t, dispatcher.Stop())
	})
	fixture.controller.SetAutoOnDemandRuntime(fixture.resolver, fixture.validator, dispatcher)

	startedAt := time.Now()
	response := fixture.serve(t)
	require.Less(t, time.Since(startedAt), 200*time.Millisecond)
	assertHookCode(t, response.Code, response.Body.Bytes(), 0)
	select {
	case request := <-ensurer.started:
		require.Equal(t, fixture.resolver.node.ID, request.RequiredNode)
	case <-time.After(time.Second):
		t.Fatal("background EnsureLive was not started")
	}
}

func TestOnStreamNotFoundLogsStableReasonWithoutCredentials(t *testing.T) {
	core, observed := observer.New(zap.DebugLevel)
	previousLogger := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previousLogger })

	fixture := newAutoOnDemandFixture(t)
	fixture.path = "/index/hook/on_stream_not_found?cap=do-not-log-capability"
	response := fixture.serve(t)
	assertHookCode(t, response.Code, response.Body.Bytes(), -1)

	entries := observed.FilterMessage("自动点播 Hook 已拒绝").All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.Equal(t, "callback-auth-invalid", fields["reason"])
	encoded, err := json.Marshal(fields)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "do-not-log-capability")
	require.NotContains(t, string(encoded), fixture.token)
	require.NotContains(t, string(encoded), fixture.capability)
}
