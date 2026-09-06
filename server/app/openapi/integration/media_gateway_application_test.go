package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/media"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// Qualification and SIP/media acquisition remain explicit fakes. All external
// HMAC/admission, application/dispatcher, SQL runtime authority, v3 signing and
// authenticated Hook binding below are real implementations. This does not
// enable the production root or establish T18 deployment qualification.
func TestOpenAPIMediaGatewayApplicationComposition(t *testing.T) {
	for _, protocol := range []string{"https-flv", "wss-flv"} {
		t.Run(protocol, func(t *testing.T) {
			fixture := newBoundaryMediaFixture(t, &boundaryMediaDispatcher{}, time.Second, 200*time.Millisecond)
			db := fixture.db
			now := time.Now().UTC()
			clock := func() time.Time { return now }
			require.NoError(t, db.Exec(`CREATE TABLE meta_node (
				id INTEGER PRIMARY KEY, revision INTEGER, state TEXT, media_server_uuid TEXT,
				current_boot_nonce TEXT, runtime_epoch INTEGER, runtime_protocol_version INTEGER,
				runtime_confirmed_revision INTEGER, runtime_confirmed_at DATETIME, runtime_identity_status TEXT)`).Error)
			require.NoError(t, db.Exec("INSERT INTO meta_node VALUES(1,1,'active',?,?,1,1,1,?,'active')", mediaLifecycleNode, mediaLifecycleBoot, now).Error)
			signer, err := playauth.NewSigner(bytes.Repeat([]byte{7}, 32), playauth.WithNow(clock))
			require.NoError(t, err)
			grants, err := playauth.NewOpenAPIGrantService(db, signer, media.NewNodeAuthority(), clock)
			require.NoError(t, err)
			scheme, hookProtocol := "https", "https"
			if protocol == "wss-flv" {
				scheme, hookProtocol = "wss", "wss"
			}
			origin := scheme + "://media.example:8443"
			streamID := boundaryMediaDevice + "_" + boundaryMediaChannel
			mediaURL := origin + "/rtp/" + streamID + ".live.flv"
			provider := &mediaLifecycleQualificationProvider{ticket: media.QualificationTicket{
				QualificationID: "gateway-application", NodeID: 1, NodeUUID: mediaLifecycleNode,
				NodeRevision: 1, BootNonce: mediaLifecycleBoot, Protocol: protocol,
				MediaOrigin: origin, ExpiresAt: now.Add(10 * time.Second),
			}}
			player := &mediaLifecyclePlayer{result: &play.Result{StreamID: streamID, App: "rtp", Generation: 9,
				Node: &play.ResultNode{ID: 1, MediaServerUUID: mediaLifecycleNode, Revision: 1},
				URLs: play.PlaybackURLs{HTTPSFLV: &mediaURL, WSSFLV: &mediaURL}}}
			application := media.NewLiveApplication(provider, player, grants, true, media.WithLiveApplicationClock(clock))
			keys, err := client.NewSecretManager(bytes.Repeat([]byte{1}, 32), "test")
			require.NoError(t, err)
			fixture.gate, err = auth.NewGateway(context.Background(), db, keys, auth.GatewayConfig{
				Audience: "test-audience", Timeout: time.Second, AuditReserve: 200 * time.Millisecond, MaxInFlight: 2,
			}, auth.WithMediaDispatcher(media.NewGatewayDispatcher(application)))
			require.NoError(t, err)
			server, httpClient, _ := newBoundaryTLSServer(t, fixture, &boundaryGlobalCounters{})
			body := []byte(`{"protocol":"` + protocol + `"}`)
			request := boundarySignedRequest(t, server.URL, fixture.secret, boundaryMediaPath, body, strings.Repeat("a", 32))
			response, err := httpClient.Do(request)
			require.NoError(t, err)
			raw := readBoundaryResponse(t, response)
			require.Equal(t, http.StatusOK, response.StatusCode, raw)
			var envelope struct {
				Data auth.MediaAuthorization `json:"data"`
			}
			require.NoError(t, json.Unmarshal([]byte(raw), &envelope))
			parsed, err := url.Parse(envelope.Data.URL)
			require.NoError(t, err)
			token := parsed.Query().Get(playauth.QueryParameter)
			claims, err := signer.AuthenticateOpenAPI(token)
			require.NoError(t, err)
			require.Equal(t, int64(120), claims.ExpiresAt-claims.IssuedAt)
			require.Equal(t, int64(1), claims.ClientID)
			require.Equal(t, int64(3), claims.DeviceEpoch)
			require.Equal(t, protocol, claims.Protocol)
			require.Equal(t, claims.GrantID, envelope.Data.AuthorizationID)
			require.Equal(t, scheme, parsed.Scheme)
			require.Len(t, parsed.Query(), 1)
			var grant models.PlayGrant
			require.NoError(t, db.First(&grant, "grant_id=?", claims.GrantID).Error)
			require.Equal(t, models.GrantStateIssued, grant.State)
			nonces, audits, success, count := countBoundaryRows(t, db)
			require.Equal(t, []int64{1, 1, 1, 1}, []int64{nonces, audits, success, count})

			previousConfig := app.ConfigYml
			app.ConfigYml = &mediaLifecycleConfig{values: map[string]interface{}{gbconfig.PlayAuthEnabledConfigKey: true}}
			t.Cleanup(func() { app.ConfigYml = previousConfig })
			hookNode := &node.Node{ID: 1, MediaServerUUID: mediaLifecycleNode, APISecret: "application-hook-fixture"}
			hook := handler.NewHookController(stream.NewNotifier())
			hook.SetOpenAPIPlayAuthorization(signer, grants)
			hook.SetPlaybackMediaContextResolver(mediaLifecyclePlaybackResolver{binding: playauth.Binding{
				DeviceID: boundaryMediaDevice, ChannelID: boundaryMediaChannel, App: "rtp", Stream: streamID, MediaServerID: mediaLifecycleNode, MediaGeneration: 9,
			}})
			authenticator := handler.NewHookAuthenticator()
			authenticator.SetResolver(mediaLifecycleHookNodeResolver{node: hookNode})
			router := gin.New()
			router.POST("/hook", authenticator.Middleware(playauth.HookOnPlay, handler.HookRejectAdmission), hook.OnPlay)
			hookBody := gin.H{"id": "application-viewer", "bootNonce": mediaLifecycleBoot, "protocol": hookProtocol,
				"app": "rtp", "stream": streamID, "schema": "rtmp", "vhost": "__defaultVhost__", "mediaServerId": mediaLifecycleNode,
				"params": url.Values{playauth.QueryParameter: []string{token}}.Encode()}
			path := mediaLifecycleHookPath(t, hookNode, playauth.HookOnPlay)
			bound := postMediaLifecycleJSON(t, router, path, hookBody)
			require.Contains(t, bound.Body.String(), `"code":0`)
			require.Equal(t, models.ViewerStateActive, lifecycleViewerState(t, db, claims.GrantID))
			require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=4 WHERE device_id=?", boundaryMediaDevice).Error)
			denied := postMediaLifecycleJSON(t, router, path, hookBody)
			require.NotContains(t, denied.Body.String(), `"code":0`, "even the same Hook connection must recheck current device epoch")
			require.NoError(t, db.Exec("UPDATE gb_device SET owner_dept_id=20 WHERE device_id=?", boundaryMediaDevice).Error)
			require.NoError(t, db.Exec("UPDATE gb_channel SET owner_dept_id=20 WHERE device_id=?", boundaryMediaDevice).Error)
			request = boundarySignedRequest(t, server.URL, fixture.secret, boundaryMediaPath, body, strings.Repeat("b", 32))
			response, err = httpClient.Do(request)
			require.NoError(t, err)
			raw = readBoundaryResponse(t, response)
			require.Equal(t, http.StatusNotFound, response.StatusCode, raw)
			nonces, audits, success, count = countBoundaryRows(t, db)
			require.Equal(t, []int64{1, 1, 1, 1}, []int64{nonces, audits, success, count}, "old owner must not consume another admission or grant")
		})
	}
}
