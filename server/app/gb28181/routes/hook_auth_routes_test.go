package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type routeHookAuthResolver struct {
	node *node.Node
}

func (r routeHookAuthResolver) GetByUUID(id string) (*node.Node, bool) {
	if r.node == nil || r.node.MediaServerUUID != id {
		return nil, false
	}
	return r.node, true
}

func installRouteHookAuth(t *testing.T, mediaNode *node.Node) {
	t.Helper()
	SetHookAuthResolver(routeHookAuthResolver{node: mediaNode})
	t.Cleanup(func() { SetHookAuthResolver(nil) })
}

func authenticatedHookPath(t *testing.T, path string, mediaNode *node.Node, event playauth.HookEvent) string {
	t.Helper()
	capability, err := playauth.HookCapability(mediaNode.APISecret, mediaNode.MediaServerUUID, event)
	if err != nil {
		t.Fatal(err)
	}
	query := url.Values{"node": {mediaNode.MediaServerUUID}, "cap": {capability}}
	return path + "?" + query.Encode()
}

func TestRegisterHookRoutesUsesEventSpecificNodeCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mediaNode := &node.Node{ID: 7, MediaServerUUID: "node-a", APISecret: "zlm-secret"}
	installRouteHookAuth(t, mediaNode)
	engine := gin.New()
	RegisterHookRoutes(engine)
	body, err := json.Marshal(gin.H{"app": "rtp", "stream": "stream-a", "mediaServerId": "node-a"})
	require.NoError(t, err)

	serve := func(path string) map[string]interface{} {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		engine.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
		return response
	}

	require.Equal(t, float64(-1), serve("/index/hook/on_publish")["code"])
	crossEvent := authenticatedHookPath(t, "/index/hook/on_publish", mediaNode, playauth.HookOnPlay)
	require.Equal(t, float64(-1), serve(crossEvent)["code"])
	valid := authenticatedHookPath(t, "/index/hook/on_publish", mediaNode, playauth.HookOnPublish)
	require.Equal(t, float64(0), serve(valid)["code"])
}
