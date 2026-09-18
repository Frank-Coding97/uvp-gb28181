package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

type fakeTalkPublishAuthorizer struct {
	allowed bool
	calls   atomic.Int32
	request handler.TalkPublishRequest
}

func (f *fakeTalkPublishAuthorizer) AuthorizeTalkPublish(_ context.Context, request handler.TalkPublishRequest) (bool, error) {
	f.calls.Add(1)
	f.request = request
	return f.allowed, nil
}

type fakeTalkStreamObserver struct {
	calls      atomic.Int32
	registered atomic.Bool
	received   chan struct{}
}

func (f *fakeTalkStreamObserver) ObserveTalkStream(_ context.Context, _ int64, _ string, _ string, registered bool) error {
	f.calls.Add(1)
	f.registered.Store(registered)
	select {
	case <-f.received:
	default:
		close(f.received)
	}
	return nil
}

func newTalkHookEngine(h *handler.HookController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/index/hook/on_publish", h.OnPublish)
	engine.POST("/index/hook/on_stream_changed", h.OnStreamChanged)
	return engine
}

func TestTalkOnPublishAuthorizesOnlyTalkApp(t *testing.T) {
	authorizer := &fakeTalkPublishAuthorizer{allowed: true}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetTalk(recordMP4Resolver{"node-1": 11}, authorizer, nil)
	engine := newTalkHookEngine(h)

	ordinary := postJSON(t, engine, "/index/hook/on_publish", gin.H{"app": "live", "stream": "camera"})
	require.Equal(t, http.StatusOK, ordinary.Code)
	require.Zero(t, authorizer.calls.Load())

	talkPublish := postJSON(t, engine, "/index/hook/on_publish", gin.H{
		"app": "talk", "stream": "source-1", "params": "?token=secret", "id": "pub-1", "mediaServerId": "node-1",
	})
	require.Equal(t, http.StatusOK, talkPublish.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(talkPublish.Body.Bytes(), &response))
	require.EqualValues(t, 0, response["code"])
	require.EqualValues(t, 11, authorizer.request.NodeID)
	require.Equal(t, "secret", authorizer.request.PublishToken)
	require.Equal(t, "pub-1", authorizer.request.PublishID)
}

func TestTalkOnPublishRejectsForgedOrUnknownNode(t *testing.T) {
	authorizer := &fakeTalkPublishAuthorizer{allowed: false}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetTalk(recordMP4Resolver{"node-1": 11}, authorizer, nil)
	engine := newTalkHookEngine(h)

	for _, payload := range []gin.H{
		{"app": "talk", "stream": "source-1", "params": "?token=forged", "id": "pub-1", "mediaServerId": "node-1"},
		{"app": "talk", "stream": "source-1", "params": "?token=secret", "id": "pub-2", "mediaServerId": "unknown"},
	} {
		response := postJSON(t, engine, "/index/hook/on_publish", payload)
		var body map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.EqualValues(t, -1, body["code"])
	}
	require.EqualValues(t, 1, authorizer.calls.Load())
}

func TestTalkStreamChangedDispatchesDedicatedObserver(t *testing.T) {
	observer := &fakeTalkStreamObserver{received: make(chan struct{})}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetTalk(recordMP4Resolver{"node-1": 11}, nil, observer)
	engine := newTalkHookEngine(h)

	response := postJSON(t, engine, "/index/hook/on_stream_changed", gin.H{
		"app": "talk", "stream": "source-1", "regist": true, "mediaServerId": "node-1",
	})
	require.Equal(t, http.StatusOK, response.Code)
	select {
	case <-observer.received:
	case <-time.After(time.Second):
		t.Fatal("talk observer was not called")
	}
	require.True(t, observer.registered.Load())
}

func TestTalkStreamUnregisteredDispatchesCleanupObserver(t *testing.T) {
	observer := &fakeTalkStreamObserver{received: make(chan struct{})}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetTalk(recordMP4Resolver{"node-1": 11}, nil, observer)
	engine := newTalkHookEngine(h)

	response := postJSON(t, engine, "/index/hook/on_stream_changed", gin.H{
		"app": "talk", "stream": "source-1", "regist": false, "mediaServerId": "node-1",
	})
	require.Equal(t, http.StatusOK, response.Code)
	select {
	case <-observer.received:
	case <-time.After(time.Second):
		t.Fatal("talk cleanup observer was not called")
	}
	require.False(t, observer.registered.Load())
}
