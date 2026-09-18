package handler_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type openAPIFlowObserverFixture struct {
	calls     atomic.Int32
	deadlined atomic.Bool
	mu        sync.Mutex
	report    playauth.OpenAPIFlowReport
	err       error
}

func (f *openAPIFlowObserverFixture) ObserveFlow(ctx context.Context, report playauth.OpenAPIFlowReport) error {
	f.calls.Add(1)
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= time.Second && time.Until(deadline) > 0 {
		f.deadlined.Store(true)
	}
	f.mu.Lock()
	f.report = report
	f.mu.Unlock()
	return f.err
}

func (f *openAPIFlowObserverFixture) lastReport() playauth.OpenAPIFlowReport {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.report
}

func TestOpenAPIFlowHookCallsObserverWithoutTrafficRuntime(t *testing.T) {
	nodeFixture := &node.Node{ID: 7, MediaServerUUID: "node-flow", APISecret: "flow-secret"}
	observer := &openAPIFlowObserverFixture{}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetOpenAPIFlowObserver(observer)
	e, path := authenticatedFlowEngine(t, h, nodeFixture)

	response := postJSON(t, e, path, validOpenAPIFlowHookBody())
	assertHookCode(t, response.Code, response.Body.Bytes(), 0)
	require.EqualValues(t, 1, observer.calls.Load())
	require.True(t, observer.deadlined.Load(), "observer must receive a deadline no longer than one second")
	require.Equal(t, playauth.OpenAPIFlowReport{
		NodeUUID: "node-flow", BootNonce: strings.Repeat("a", 32), Identifier: "flow-session",
		Protocol: "https-flv", Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-a", Player: true,
	}, observer.lastReport())
}

func TestOpenAPIFlowHookDoesNotCallObserverForLegacyOrMismatchedReports(t *testing.T) {
	cases := []struct {
		name       string
		withAuth   bool
		nodeID     int64
		mutateBody func(gin.H)
		payloadID  string
	}{
		{name: "legacy unauthenticated", withAuth: false, nodeID: 7},
		{name: "authenticated node id zero", withAuth: true, nodeID: 0},
		{name: "payload node mismatch", withAuth: true, nodeID: 7, payloadID: "other-node"},
		{name: "publisher", withAuth: true, nodeID: 7, mutateBody: func(body gin.H) { body["player"] = false }},
		{name: "missing boot", withAuth: true, nodeID: 7, mutateBody: func(body gin.H) { delete(body, "bootNonce") }},
		{name: "invalid boot", withAuth: true, nodeID: 7, mutateBody: func(body gin.H) { body["bootNonce"] = strings.Repeat("g", 32) }},
		{name: "missing id", withAuth: true, nodeID: 7, mutateBody: func(body gin.H) { delete(body, "id") }},
		{name: "invalid id", withAuth: true, nodeID: 7, mutateBody: func(body gin.H) { body["id"] = " flow-session" }},
		{name: "wrong protocol", withAuth: true, nodeID: 7, mutateBody: func(body gin.H) { body["protocol"] = "http" }},
		{name: "missing protocol", withAuth: true, nodeID: 7, mutateBody: func(body gin.H) { delete(body, "protocol") }},
		{name: "missing tuple", withAuth: true, nodeID: 7, mutateBody: func(body gin.H) { delete(body, "stream") }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			nodeFixture := &node.Node{ID: test.nodeID, MediaServerUUID: "node-flow", APISecret: "flow-secret"}
			observer := &openAPIFlowObserverFixture{}
			h := handler.NewHookController(stream.NewNotifier())
			h.SetOpenAPIFlowObserver(observer)
			var e *gin.Engine
			path := "/hook"
			if test.withAuth {
				e, path = authenticatedFlowEngine(t, h, nodeFixture)
			} else {
				e = gin.New()
				e.POST(path, h.OnFlowReport)
			}
			body := validOpenAPIFlowHookBody()
			if test.payloadID != "" {
				body["mediaServerId"] = test.payloadID
			}
			if test.mutateBody != nil {
				test.mutateBody(body)
			}
			response := postJSON(t, e, path, body)
			assertHookCode(t, response.Code, response.Body.Bytes(), 0)
			require.Zero(t, observer.calls.Load())
		})
	}
}

func TestOpenAPIFlowHookFailureIsFailOpenAndRedacted(t *testing.T) {
	secret := "fixture-openapi-flow-secret"
	for _, failure := range []struct {
		name string
		err  error
	}{
		{name: "ordinary error", err: errors.New(secret)},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "cancel", err: context.Canceled},
	} {
		t.Run(failure.name, func(t *testing.T) {
			nodeFixture := &node.Node{ID: 7, MediaServerUUID: "node-flow", APISecret: "flow-secret"}
			observer := &openAPIFlowObserverFixture{err: failure.err}
			h := handler.NewHookController(stream.NewNotifier())
			h.SetOpenAPIFlowObserver(observer)
			e, path := authenticatedFlowEngine(t, h, nodeFixture)
			response := postJSON(t, e, path, validOpenAPIFlowHookBody())
			assertHookCode(t, response.Code, response.Body.Bytes(), 0)
			require.EqualValues(t, 1, observer.calls.Load())
			require.NotContains(t, response.Body.String(), secret)
		})
	}
}

func TestOpenAPIFlowHookPreservesExistingTrafficFlow(t *testing.T) {
	nodeFixture := &node.Node{ID: 7, MediaServerUUID: "node-flow", APISecret: "flow-secret"}
	observer := &openAPIFlowObserverFixture{}
	collector := &mockFlowCollector{}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetOpenAPIFlowObserver(observer)
	h.SetFlowRuntime(mockFlowNodeResolver{node: nodeFixture}, collector)
	e, path := authenticatedFlowEngine(t, h, nodeFixture)

	response := postJSON(t, e, path, validOpenAPIFlowHookBody())
	assertHookCode(t, response.Code, response.Body.Bytes(), 0)
	require.EqualValues(t, 1, observer.calls.Load())
	require.EqualValues(t, 1, collector.calls.Load())
}

func authenticatedFlowEngine(t *testing.T, h *handler.HookController, mediaNode *node.Node) (*gin.Engine, string) {
	t.Helper()
	authenticator := handler.NewHookAuthenticator()
	authenticator.SetResolver(hookAuthResolver{nodes: map[string]*node.Node{mediaNode.MediaServerUUID: mediaNode}})
	capability, err := playauth.HookCapability(mediaNode.APISecret, mediaNode.MediaServerUUID, playauth.HookOnFlowReport)
	require.NoError(t, err)
	e := gin.New()
	e.POST("/hook", authenticator.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), h.OnFlowReport)
	return e, "/hook?node=" + url.QueryEscape(mediaNode.MediaServerUUID) + "&cap=" + url.QueryEscape(capability)
}

func validOpenAPIFlowHookBody() gin.H {
	return gin.H{
		"id": "flow-session", "mediaServerId": "node-flow", "bootNonce": strings.Repeat("a", 32),
		"protocol": "https", "schema": "rtmp", "vhost": "__defaultVhost__", "app": "rtp", "stream": "stream-a", "player": true,
	}
}
