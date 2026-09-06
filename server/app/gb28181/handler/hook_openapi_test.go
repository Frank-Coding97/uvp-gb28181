package handler_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type openAPIHookBinder struct {
	calls   int
	request playauth.OpenAPIViewerBindRequest
	err     error
}

func (b *openAPIHookBinder) BindViewer(ctx context.Context, _ string, request playauth.OpenAPIViewerBindRequest) (models.Viewer, error) {
	b.calls++
	b.request = request
	_, bounded := ctx.Deadline()
	if !bounded {
		return models.Viewer{}, errors.New("missing hook deadline")
	}
	return models.Viewer{State: models.ViewerStateActive}, b.err
}

func TestOpenAPIHookRequiresAuthenticatedNodeAndDurableBinding(t *testing.T) {
	for _, protocol := range []struct{ hook, grant string }{{"https", "https-flv"}, {"wss", "wss-flv"}} {
		t.Run(protocol.grant, func(t *testing.T) {
			for _, tc := range []struct {
				name                                                                                          string
				mutate                                                                                        func(gin.H)
				unauthenticated, authOff, missingBinder, partialBinder, wrongGeneration, storeFailure, legacy bool
				wantCalls, wantCode                                                                           int
			}{
				{name: "valid", wantCalls: 1},
				{name: "no middleware", unauthenticated: true, wantCode: -1},
				{name: "auth off", authOff: true, wantCode: -1},
				{name: "missing binder", missingBinder: true, wantCode: -1},
				{name: "partial runtime", partialBinder: true, wantCode: -1},
				{name: "legacy token remains separate", legacy: true},
				{name: "stale generation", wrongGeneration: true, wantCode: -1},
				{name: "store failure", storeFailure: true, wantCalls: 1, wantCode: -1},
				{name: "missing identifier", mutate: func(b gin.H) { delete(b, "id") }, wantCode: -1},
				{name: "missing boot", mutate: func(b gin.H) { delete(b, "bootNonce") }, wantCode: -1},
				{name: "wrong boot", mutate: func(b gin.H) { b["bootNonce"] = strings.Repeat("b", 32) }, wantCode: -1},
				{name: "wrong node", mutate: func(b gin.H) { b["mediaServerId"] = "other" }, wantCode: -1},
				{name: "wrong protocol", mutate: func(b gin.H) { b["protocol"] = "http" }, wantCode: -1},
				{name: "missing protocol", mutate: func(b gin.H) { delete(b, "protocol") }, wantCode: -1},
				{name: "wrong schema", mutate: func(b gin.H) { b["schema"] = "rtsp" }, wantCode: -1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					setHookPlayAuth(t, !tc.authOff, false)
					now := time.Unix(1800000000, 0).UTC()
					signer, err := playauth.NewSigner([]byte(strings.Repeat("s", 32)), playauth.WithNow(func() time.Time { return now }))
					require.NoError(t, err)
					binding := playauth.OpenAPIBinding{GrantID: "00000000-0000-4000-8000-000000000001", ClientID: 1, ClientEpoch: 1, Scope: "play:live:apply", ScopeEpoch: 1, DeviceEpoch: 1, DeviceID: "37010301021320000014", ChannelID: "37010301021320000001", NodeUUID: "node-a", BootNonce: strings.Repeat("a", 32), Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: "0200000001", MediaGeneration: 9, Protocol: protocol.grant}
					grant, err := signer.IssueOpenAPI(binding)
					require.NoError(t, err)
					h := handler.NewHookController(stream.NewNotifier())
					legacyBinding := playauth.Binding{DeviceID: binding.DeviceID, ChannelID: binding.ChannelID, App: binding.App, Stream: binding.Stream, MediaServerID: binding.NodeUUID, MediaGeneration: binding.MediaGeneration}
					if tc.legacy {
						grant, err = signer.IssueDirect(legacyBinding)
						require.NoError(t, err)
					}
					if tc.wrongGeneration {
						legacyBinding.MediaGeneration++
					}
					h.SetPlayAuthorizer(signer)
					h.SetPlaybackMediaContextResolver(hookMediaResolver{binding: legacyBinding})
					binder := &openAPIHookBinder{}
					if tc.storeFailure {
						binder.err = errors.New("fixture-private-database-error")
					}
					if !tc.missingBinder {
						h.SetOpenAPIPlayAuthorization(signer, binder)
					}
					if tc.partialBinder {
						h.SetOpenAPIPlayAuthorization(signer, nil)
					}
					e := gin.New()
					path := "/hook"
					if tc.unauthenticated {
						e.POST(path, h.OnPlay)
					} else {
						auth := handler.NewHookAuthenticator()
						auth.SetResolver(hookAuthResolver{nodes: map[string]*node.Node{"node-a": {ID: 1, MediaServerUUID: "node-a", APISecret: "fixture-secret"}}})
						capability, err := playauth.HookCapability("fixture-secret", "node-a", playauth.HookOnPlay)
						require.NoError(t, err)
						e.POST(path, auth.Middleware(playauth.HookOnPlay, handler.HookRejectAdmission), h.OnPlay)
						path += "?node=node-a&cap=" + url.QueryEscape(capability)
					}
					body := gin.H{"app": binding.App, "stream": binding.Stream, "schema": binding.Schema, "vhost": binding.VHost, "mediaServerId": binding.NodeUUID, "bootNonce": binding.BootNonce, "id": "actual-session", "protocol": protocol.hook, "params": url.Values{playauth.QueryParameter: {grant.Token}, "protocol": {"attacker"}, "id": {"attacker"}, "bootNonce": {"attacker"}}.Encode()}
					if tc.mutate != nil {
						tc.mutate(body)
					}
					response := postJSON(t, e, path, body)
					assertHookCode(t, response.Code, response.Body.Bytes(), float64(tc.wantCode))
					require.Equal(t, tc.wantCalls, binder.calls)
					require.NotContains(t, response.Body.String(), grant.Token)
					require.NotContains(t, response.Body.String(), "fixture-private")
					if tc.wantCalls == 1 {
						require.Equal(t, "actual-session", binder.request.Identifier)
						require.Equal(t, binding.BootNonce, binder.request.BootNonce)
						require.Equal(t, protocol.grant, binder.request.Protocol)
						require.Equal(t, binding.MediaGeneration, binder.request.MediaGeneration)
					}
				})
			}
		})
	}
}
