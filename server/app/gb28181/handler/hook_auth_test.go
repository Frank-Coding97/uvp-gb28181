package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type hookAuthResolver struct {
	nodes map[string]*node.Node
}

func (r hookAuthResolver) GetByUUID(id string) (*node.Node, bool) {
	n, ok := r.nodes[id]
	return n, ok
}

func TestHookAuthenticatorAcceptsValidCapabilityAcrossNAT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mediaNode := &node.Node{MediaServerUUID: "node-a", Host: "192.168.10.220", APISecret: "secret-a"}
	auth := handler.NewHookAuthenticator()
	auth.SetResolver(hookAuthResolver{nodes: map[string]*node.Node{"node-a": mediaNode}})
	capability, err := playauth.HookCapability(mediaNode.APISecret, mediaNode.MediaServerUUID, playauth.HookOnFlowReport)
	require.NoError(t, err)

	called := 0
	engine := gin.New()
	engine.POST("/hook", auth.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), func(c *gin.Context) {
		called++
		resolved, ok := handler.AuthenticatedHookNode(c)
		require.True(t, ok)
		require.Equal(t, "node-a", resolved.MediaServerUUID)
		c.JSON(http.StatusOK, gin.H{"code": 0})
	})
	req := httptest.NewRequest(http.MethodPost, "/hook?node=node-a&cap="+capability, nil)
	req.RemoteAddr = "10.8.0.2:45678"
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, called)
}

func TestHookAuthenticatorRejectsInvalidRequestsWithoutCallingHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mediaNode := &node.Node{MediaServerUUID: "node-a", Host: "192.168.10.220", APISecret: "secret-a"}
	auth := handler.NewHookAuthenticator()
	auth.SetResolver(hookAuthResolver{nodes: map[string]*node.Node{"node-a": mediaNode}})
	valid, err := playauth.HookCapability(mediaNode.APISecret, mediaNode.MediaServerUUID, playauth.HookOnFlowReport)
	require.NoError(t, err)
	wrongEvent, err := playauth.HookCapability(mediaNode.APISecret, mediaNode.MediaServerUUID, playauth.HookOnPlay)
	require.NoError(t, err)

	tests := []struct {
		name, query string
	}{
		{name: "missing node", query: "cap=" + valid},
		{name: "missing cap", query: "node=node-a"},
		{name: "duplicate node", query: "node=node-a&node=node-b&cap=" + valid},
		{name: "duplicate cap", query: "node=node-a&cap=" + valid + "&cap=other"},
		{name: "unknown node", query: "node=node-b&cap=" + valid},
		{name: "invalid cap", query: "node=node-a&cap=invalid"},
		{name: "cross event", query: "node=node-a&cap=" + wrongEvent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := 0
			engine := gin.New()
			engine.POST("/hook", auth.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), func(c *gin.Context) { called++ })
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/hook?"+tc.query, nil))
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, 0, called)
		})
	}
}

func TestHookAuthenticatorFailsClosedWhenResolverIsUnavailable(t *testing.T) {
	auth := handler.NewHookAuthenticator()
	called := 0
	engine := gin.New()
	engine.POST("/hook", auth.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), func(c *gin.Context) { called++ })
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/hook?node=node-a&cap=anything", nil))
	require.Equal(t, 0, called)
}

func TestHookAuthenticatorUsesSafeRejectResponses(t *testing.T) {
	tests := []struct {
		name  string
		mode  handler.HookRejectMode
		code  float64
		close *bool
	}{
		{name: "notification", mode: handler.HookRejectNotification, code: 0},
		{name: "admission", mode: handler.HookRejectAdmission, code: -1},
		{name: "none reader", mode: handler.HookRejectNoneReader, code: 0, close: boolPtr(false)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auth := handler.NewHookAuthenticator()
			engine := gin.New()
			engine.POST("/hook", auth.Middleware(playauth.HookOnPlay, tc.mode), func(c *gin.Context) { t.Fatal("handler must not run") })
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/hook", nil))
			var body map[string]interface{}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, tc.code, body["code"])
			if tc.close != nil {
				require.Equal(t, *tc.close, body["close"])
			}
		})
	}
}

func TestHookAuthenticatorDoesNotLogCapability(t *testing.T) {
	core, observed := observer.New(zap.WarnLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })
	auth := handler.NewHookAuthenticator()
	engine := gin.New()
	engine.POST("/hook", auth.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), func(c *gin.Context) {})
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/hook?node=node-a&cap=must-not-appear", nil))

	encoded, err := json.Marshal(observed.All())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "must-not-appear")
}

func boolPtr(value bool) *bool { return &value }
