package handler_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/handler"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/stream"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

type quotaHookLifecycle struct {
	bindCalls  int
	closeCalls int
	claims     playauth.Claims
	binding    playauth.OpenAPIViewerBinding
	report     playauth.OpenAPIFlowReport
}

type quotaHookRuntimeIdentity struct {
	bootNonce string
	err       error
}

func (r quotaHookRuntimeIdentity) ResolveOpenAPIRuntimeIdentity(context.Context, string) (string, error) {
	return r.bootNonce, r.err
}

func (l *quotaHookLifecycle) BindViewer(ctx context.Context, claims playauth.Claims, binding playauth.OpenAPIViewerBinding) error {
	if _, ok := ctx.Deadline(); !ok {
		return context.DeadlineExceeded
	}
	l.bindCalls++
	l.claims = claims
	l.binding = binding
	return nil
}

func (l *quotaHookLifecycle) CloseViewer(ctx context.Context, report playauth.OpenAPIFlowReport) error {
	if _, ok := ctx.Deadline(); !ok {
		return context.DeadlineExceeded
	}
	l.closeCalls++
	l.report = report
	return nil
}

func TestUnifiedOpenAPIPlayBindsAndReleasesAllPlaybackProtocols(t *testing.T) {
	for _, tc := range []struct{ name, protocol, schema, want string }{
		{"wsFlv", "ws", "rtmp", "ws-flv"},
		{"httpFlv", "http", "rtmp", "http-flv"},
		{"wssFlv", "wss", "rtmp", "wss-flv"},
		{"httpsFlv", "https", "rtmp", "https-flv"},
		{"wsFmp4", "ws", "fmp4", "ws-fmp4"},
		{"httpFmp4", "http", "fmp4", "http-fmp4"},
		{"wssFmp4", "wss", "fmp4", "wss-fmp4"},
		{"httpsFmp4", "https", "fmp4", "https-fmp4"},
		{"hls", "hls", "hls", "hls"},
		{"httpsHls", "https", "hls", "https-hls"},
		{"wsTs", "ws", "ts", "ws-ts"},
		{"httpTs", "http", "ts", "http-ts"},
		{"wssTs", "wss", "ts", "wss-ts"},
		{"httpsTs", "https", "ts", "https-ts"},
		{"webrtc", "rtc", "rtc", "webrtc"},
		{"webrtcs", "rtc", "rtc", "webrtc"},
		{"rtmp", "rtmp", "rtmp", "rtmp"},
		{"rtmps", "rtmps", "rtmp", "rtmps"},
		{"rtsp", "rtsp", "rtsp", "rtsp"},
		{"rtsps", "rtsps", "rtsp", "rtsps"},
		{"legacy-http", "", "http", "http-flv"},
		{"unknown-transport", "ftp", "rtmp", ""},
		{"unknown-media", "https", "unknown", ""},
		{"rtsp-mismatch", "rtsp", "rtmp", ""},
		{"rtmp-mismatch", "rtmps", "rtsp", ""},
		{"hls-mismatch", "hls", "rtmp", ""},
		{"hls-websocket", "ws", "hls", ""},
		{"rtc-mismatch", "rtc", "rtmp", ""},
		{"legacy-mismatch", "http", "wss", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setHookPlayAuth(t, true, false)
			now := time.Unix(1_800_000_000, 0).UTC()
			signer, err := playauth.NewSigner([]byte(strings.Repeat("s", 32)), playauth.WithNow(func() time.Time { return now }))
			require.NoError(t, err)
			authority := newHookDeviceAuthority(3)
			authorization := newHookAuthorization(t, signer, func() time.Time { return now }, authority)
			media := playauth.Binding{
				DeviceID: "37010301021320000018", ChannelID: "37010301021320000006", DeviceEpoch: 3,
				App: "rtp", Stream: "stream-a", MediaServerID: "node-a", MediaGeneration: 9,
				OpenAPIClientID: 7, OpenAPIGrantID: "00000000-0000-4000-8000-000000000007",
			}
			grant, err := authorization.IssueDirect(media)
			require.NoError(t, err)

			lifecycle := &quotaHookLifecycle{}
			h := handler.NewHookController(stream.NewNotifier())
			h.SetPlayAuthorizer(authorization)
			resolved := media
			resolved.OpenAPIClientID = 0
			resolved.OpenAPIGrantID = ""
			h.SetPlaybackMediaContextResolver(hookMediaResolver{binding: resolved})
			h.SetOpenAPIViewerBinder(lifecycle)
			h.SetOpenAPIFlowObserver(lifecycle)
			h.SetOpenAPIRuntimeIdentityResolver(quotaHookRuntimeIdentity{bootNonce: strings.Repeat("a", 32)})

			mediaNode := &node.Node{ID: 1, MediaServerUUID: "node-a", APISecret: "hook-secret"}
			authenticator := handler.NewHookAuthenticator()
			authenticator.SetResolver(hookAuthResolver{nodes: map[string]*node.Node{"node-a": mediaNode}})
			playCapability, err := playauth.HookCapability(mediaNode.APISecret, mediaNode.MediaServerUUID, playauth.HookOnPlay)
			require.NoError(t, err)
			flowCapability, err := playauth.HookCapability(mediaNode.APISecret, mediaNode.MediaServerUUID, playauth.HookOnFlowReport)
			require.NoError(t, err)
			engine := gin.New()
			engine.POST("/play", authenticator.Middleware(playauth.HookOnPlay, handler.HookRejectAdmission), h.OnPlay)
			engine.POST("/flow", authenticator.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), h.OnFlowReport)

			for _, params := range []string{"", "play_token=tampered"} {
				response := postJSON(t, engine, "/play?node=node-a&cap="+url.QueryEscape(playCapability), gin.H{
					"id": "session-a", "protocol": tc.protocol, "schema": tc.schema,
					"vhost": "__defaultVhost__", "app": "rtp", "stream": "stream-a", "mediaServerId": "node-a", "params": params,
				})
				assertHookCode(t, response.Code, response.Body.Bytes(), -1)
			}
			require.Zero(t, lifecycle.bindCalls)

			if tc.want == "webrtc" {
				for _, session := range [][]string{nil, {"other-session"}, {"session-a", "session-a"}} {
					response := postJSON(t, engine, "/play?node=node-a&cap="+url.QueryEscape(playCapability), gin.H{
						"id": "session-a", "protocol": tc.protocol, "schema": tc.schema, "app": "rtp", "stream": "stream-a", "vhost": "__defaultVhost__", "mediaServerId": "node-a",
						"params": url.Values{playauth.QueryParameter: {grant.Token}, "session": session}.Encode(),
					})
					assertHookCode(t, response.Code, response.Body.Bytes(), -1)
				}
				require.Zero(t, lifecycle.bindCalls)
			}
			playResponse := postJSON(t, engine, "/play?node=node-a&cap="+url.QueryEscape(playCapability), gin.H{
				"id": "session-a", "bootNonce": strings.Repeat("f", 32), "protocol": tc.protocol, "schema": tc.schema,
				"vhost": "__defaultVhost__", "app": "rtp", "stream": "stream-a", "mediaServerId": "node-a",
				"params": url.Values{playauth.QueryParameter: {grant.Token}, "session": {"session-a"}}.Encode(),
			})
			if tc.want == "" {
				assertHookCode(t, playResponse.Code, playResponse.Body.Bytes(), -1)
				require.Zero(t, lifecycle.bindCalls)
				return
			}
			assertHookCode(t, playResponse.Code, playResponse.Body.Bytes(), 0)
			require.Equal(t, 1, lifecycle.bindCalls)
			require.Equal(t, int64(7), lifecycle.claims.OpenAPIClientID)
			require.Equal(t, tc.want, lifecycle.binding.Protocol)
			require.Equal(t, "session-a", lifecycle.binding.Identifier)
			require.Equal(t, strings.Repeat("a", 32), lifecycle.binding.BootNonce, "hook payload cannot override trusted runtime identity")

			flowID := "session-a"
			if tc.schema == "rtc" {
				flowID = "ice-media-session"
			}
			flowResponse := postJSON(t, engine, "/flow?node=node-a&cap="+url.QueryEscape(flowCapability), gin.H{
				"id": flowID, "protocol": tc.protocol, "schema": tc.schema,
				"params": url.Values{"session": {"session-a"}}.Encode(),
				"vhost":  "__defaultVhost__", "app": "rtp", "stream": "stream-a", "mediaServerId": "node-a", "player": true,
			})
			assertHookCode(t, flowResponse.Code, flowResponse.Body.Bytes(), 0)
			require.Equal(t, "session-a", lifecycle.report.Identifier)
			require.Equal(t, 1, lifecycle.closeCalls)
			require.Equal(t, tc.want, lifecycle.report.Protocol)
			require.Equal(t, strings.Repeat("a", 32), lifecycle.report.BootNonce, "flow identity must use the trusted runtime nonce")
		})
	}
}

func TestOrdinaryInternalPlayDoesNotInvokeOpenAPIQuotaLifecycle(t *testing.T) {
	setHookPlayAuth(t, true, false)
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := playauth.NewSigner([]byte(strings.Repeat("s", 32)), playauth.WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	authorization := newHookAuthorization(t, signer, func() time.Time { return now }, newHookDeviceAuthority(1))
	binding := playauth.Binding{DeviceID: "37010301021320000018", ChannelID: "37010301021320000006", DeviceEpoch: 1, App: "rtp", Stream: "stream-a", MediaServerID: "node-a", MediaGeneration: 9}
	grant, err := authorization.IssueDirect(binding)
	require.NoError(t, err)
	lifecycle := &quotaHookLifecycle{}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayAuthorizer(authorization)
	h.SetPlaybackMediaContextResolver(hookMediaResolver{binding: binding})
	h.SetOpenAPIViewerBinder(lifecycle)
	engine := gin.New()
	engine.POST("/play", h.OnPlay)
	response := postJSON(t, engine, "/play", gin.H{
		"id": "session-a", "bootNonce": strings.Repeat("a", 32), "protocol": "http", "schema": "http",
		"vhost": "__defaultVhost__", "app": "rtp", "stream": "stream-a", "mediaServerId": "node-a",
		"params": url.Values{playauth.QueryParameter: {grant.Token}}.Encode(),
	})
	assertHookCode(t, response.Code, response.Body.Bytes(), 0)
	require.Zero(t, lifecycle.bindCalls)
}
