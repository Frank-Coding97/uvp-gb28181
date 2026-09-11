package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

type startedNotifier struct {
	calls atomic.Int32
	last  atomic.Int64
}

func (n *startedNotifier) OnNodeStarted(nodeID int64) {
	n.calls.Add(1)
	n.last.Store(nodeID)
}

func newServerStartedEngine(h *handler.HookController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/index/hook/on_server_started", h.OnServerStarted)
	return engine
}

func postServerStarted(engine *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/index/hook/on_server_started", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder
}

func TestOnServerStartedUsesFlatMediaServerIDAndIgnoresSecrets(t *testing.T) {
	notifier := &startedNotifier{}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetMultiNode(mockNodeResolver{uuid: "node-uuid", nodeID: 42}, nil)
	h.SetRestartStartedNotifier(notifier)

	recorder := postServerStarted(newServerStartedEngine(h), `{
		"general.mediaServerId":"node-uuid",
		"api.secret":"must-never-escape",
		"unknown":{"value":"ignored"}
	}`)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"code":0,"msg":"success"}`, recorder.Body.String())
	require.Equal(t, int32(1), notifier.calls.Load())
	require.Equal(t, int64(42), notifier.last.Load())
	require.NotContains(t, recorder.Body.String(), "must-never-escape")
}

func TestOnServerStartedRejectsNestedOrUnknownNodeIdentity(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "nested key is not the ZLM wire contract", body: `{"general":{"mediaServerId":"node-uuid"}}`},
		{name: "unknown uuid", body: `{"general.mediaServerId":"unknown"}`},
		{name: "empty uuid", body: `{"general.mediaServerId":""}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			notifier := &startedNotifier{}
			h := handler.NewHookController(stream.NewNotifier())
			h.SetMultiNode(mockNodeResolver{uuid: "node-uuid", nodeID: 42}, nil)
			h.SetRestartStartedNotifier(notifier)

			recorder := postServerStarted(newServerStartedEngine(h), test.body)

			require.Equal(t, http.StatusOK, recorder.Code)
			require.Zero(t, notifier.calls.Load())
		})
	}
}

func TestOnServerStartedFailsSafelyForMalformedAndOversizedBodies(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "malformed", body: `{"general.mediaServerId":"must-never-escape"`, wantStatus: http.StatusBadRequest},
		{name: "multiple documents", body: `{"general.mediaServerId":"node-uuid"}{"api.secret":"must-never-escape"}`, wantStatus: http.StatusBadRequest},
		{name: "oversized", body: `{"padding":"` + strings.Repeat("must-never-escape", 5000) + `","general.mediaServerId":"node-uuid"}`, wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			notifier := &startedNotifier{}
			h := handler.NewHookController(stream.NewNotifier())
			h.SetMultiNode(mockNodeResolver{uuid: "node-uuid", nodeID: 42}, nil)
			h.SetRestartStartedNotifier(notifier)

			recorder := postServerStarted(newServerStartedEngine(h), test.body)

			require.Equal(t, test.wantStatus, recorder.Code)
			require.Zero(t, notifier.calls.Load())
			require.NotContains(t, recorder.Body.String(), "must-never-escape")
		})
	}
}

func TestOnServerStartedAllowsUnassembledRestartNotifier(t *testing.T) {
	h := handler.NewHookController(stream.NewNotifier())
	h.SetMultiNode(mockNodeResolver{uuid: "node-uuid", nodeID: 42}, nil)

	recorder := postServerStarted(newServerStartedEngine(h), `{"general.mediaServerId":"node-uuid"}`)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"code":0,"msg":"success"}`, recorder.Body.String())
}
