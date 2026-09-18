//go:build openapi_live

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	openapiclient "uvplatform.cn/uvp-gb28181/app/openapi/client"
	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
	"uvplatform.cn/uvp-gb28181/app/openapi/media"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/routes"
)

type productMediaKey struct{ ak, sk string }
type productMediaFixture struct {
	*activeRevocationFixture
	api                                  *httptest.Server
	http                                 *http.Client
	bareURL, backgroundURL, stream, boot string
}

func newProductMediaFixture(t *testing.T, protocol string) *productMediaFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var hookRouter atomic.Pointer[gin.Engine]
	f := newActiveRevocationFixtureWithAdmission(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router := hookRouter.Load()
		if router == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		router.ServeHTTP(w, r)
	}))
	prepareProductMediaResources(t, f)
	bindings, err := media.LoadNodeControlBindings(f.bindingsPath)
	require.NoError(t, err)
	runtime, err := media.NewTrustedRevocationFactory(liveRevocationRegistry{f.node}, bindings, openapiconfig.NewNodeRuntimeStore(f.db, nil)).Resolve(context.Background(), f.node.MediaServerUUID)
	require.NoError(t, err)
	defer runtime.Release()
	signer, err := playauth.NewSigner(bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)
	grants, err := playauth.NewOpenAPIGrantService(f.db, signer, media.NewNodeAuthority(), time.Now)
	require.NoError(t, err)
	streamID := boundaryMediaDevice + "_" + boundaryMediaChannel
	binding := playauth.Binding{DeviceID: boundaryMediaDevice, ChannelID: boundaryMediaChannel, DeviceEpoch: 3,
		App: "rtp", Stream: streamID, MediaServerID: f.node.MediaServerUUID, MediaGeneration: 9}
	background := playauth.NewAuthorizationService(signer, playauth.NewAuthorizationRegistry(), playauth.WithDeviceSecurityAuthority(playauth.NewDeviceSecurityStore(f.db)))
	hook := handler.NewHookController(stream.NewNotifier())
	hook.SetOpenAPIPlayAuthorization(signer, grants)
	hook.SetOpenAPIFlowObserver(grants)
	hook.SetPlayAuthorizer(background)
	hook.SetPlaybackMediaContextResolver(mediaLifecyclePlaybackResolver{binding: binding})
	authenticator := handler.NewHookAuthenticator()
	authenticator.SetResolver(mediaLifecycleHookNodeResolver{node: &f.node})
	router := gin.New()
	router.POST("/hook/on_play", authenticator.Middleware(playauth.HookOnPlay, handler.HookRejectAdmission), hook.OnPlay)
	router.POST("/hook/on_flow_report", authenticator.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), hook.OnFlowReport)
	previousConfig := app.ConfigYml
	app.ConfigYml = &mediaLifecycleConfig{values: map[string]interface{}{gbconfig.PlayAuthEnabledConfigKey: true}}
	t.Cleanup(func() {
		f.probe.stop()
		f.hookServer.Close() // join every real Hook before restoring the global
		app.ConfigYml = previousConfig
	})
	hookRouter.Store(router)
	f.probe.publishApp(t, "rtp", streamID)
	scheme := "https"
	if protocol == "wss-flv" {
		scheme = "wss"
	}
	origin := fmt.Sprintf("%s://127.0.0.1:%d", scheme, f.probe.tlsPort)
	bareURL := origin + "/rtp/" + streamID + ".live.flv"
	// Only the acquisition/qualification adapter remains synthetic: the
	// already published source substitutes for SIP/physical device startup.
	provider := &mediaLifecycleQualificationProvider{ticket: media.QualificationTicket{
		QualificationID: "isolated-product-fixture", NodeID: f.node.ID, NodeUUID: f.node.MediaServerUUID,
		NodeRevision: f.node.Revision, BootNonce: runtime.CurrentBootNonce, Protocol: protocol,
		MediaOrigin: origin, ExpiresAt: time.Now().Add(time.Minute),
	}}
	player := &mediaLifecyclePlayer{result: &play.Result{StreamID: streamID, App: "rtp", Generation: binding.MediaGeneration,
		Node: &play.ResultNode{ID: f.node.ID, MediaServerUUID: f.node.MediaServerUUID, Revision: f.node.Revision},
		URLs: play.PlaybackURLs{HTTPSFLV: &bareURL, WSSFLV: &bareURL}}}
	application := media.NewLiveApplication(provider, player, grants, true)
	keys, err := openapiclient.NewSecretManager(bytes.Repeat([]byte{0xA5}, 32), "isolated-revocation")
	require.NoError(t, err)
	gate, err := auth.NewGateway(context.Background(), f.db, keys, auth.GatewayConfig{Audience: "test-audience", Timeout: 3 * time.Second, AuditReserve: time.Second, MaxInFlight: 4}, auth.WithMediaDispatcher(media.NewGatewayDispatcher(application)))
	require.NoError(t, err)
	root := gin.New()
	require.NoError(t, routes.InstallPublicBoundary(root, gate, nil))
	api := httptest.NewTLSServer(root)
	t.Cleanup(api.Close)
	httpClient := api.Client()
	httpClient.Timeout = 5 * time.Second
	uGrant, err := background.IssueDirectContext(context.Background(), binding)
	require.NoError(t, err)
	return &productMediaFixture{activeRevocationFixture: f, api: api, http: httpClient, bareURL: bareURL,
		backgroundURL: bareURL + "?" + url.Values{playauth.QueryParameter: {uGrant.Token}}.Encode(), stream: streamID, boot: runtime.CurrentBootNonce}
}

func prepareProductMediaResources(t *testing.T, f *activeRevocationFixture) {
	t.Helper()
	require.NoError(t, f.db.AutoMigrate(&models.Nonce{}))
	for _, sql := range []string{
		"ALTER TABLE meta_node ADD COLUMN state TEXT NOT NULL DEFAULT 'active'",
		"CREATE TABLE sys_department (id INTEGER PRIMARY KEY, status INTEGER, deleted_at DATETIME NULL)",
		"INSERT INTO sys_department (id,status) VALUES (10,1)",
		`CREATE TABLE gb_device (id INTEGER PRIMARY KEY AUTOINCREMENT, device_id TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL DEFAULT '', alias TEXT NOT NULL DEFAULT '', manufacturer TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
		status INTEGER NOT NULL DEFAULT 1, owner_dept_id INTEGER NOT NULL, access_epoch INTEGER NOT NULL DEFAULT 1,
		cleanup_completed_epoch INTEGER NOT NULL DEFAULT 1, legacy_revoked_before DATETIME NULL, deleted_at DATETIME NULL)`,
		`CREATE TABLE gb_channel (id INTEGER PRIMARY KEY AUTOINCREMENT, device_id TEXT NOT NULL, channel_id TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '', alias TEXT NOT NULL DEFAULT '', manufacturer TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
		status INTEGER NOT NULL DEFAULT 1, ptz_type INTEGER NOT NULL DEFAULT 0, owner_dept_id INTEGER NOT NULL, deleted_at DATETIME NULL, UNIQUE(device_id,channel_id))`,
	} {
		require.NoError(t, f.db.Exec(sql).Error)
	}
	require.NoError(t, f.db.Exec("INSERT INTO gb_device (device_id,owner_dept_id,access_epoch,cleanup_completed_epoch) VALUES (?,10,3,3)", boundaryMediaDevice).Error)
	require.NoError(t, f.db.Exec("INSERT INTO gb_channel (device_id,channel_id,owner_dept_id) VALUES (?,?,10)", boundaryMediaDevice, boundaryMediaChannel).Error)
}

func (f *productMediaFixture) machine(t *testing.T, label string) (openapiclient.ClientView, productMediaKey) {
	t.Helper()
	c := f.client(t, label)
	material, err := f.service.LoadVerificationMaterial(context.Background(), c.AK)
	require.NoError(t, err)
	return c, productMediaKey{ak: c.AK, sk: material.SecretKey}
}

func (f *productMediaFixture) request(t *testing.T, key productMediaKey, method, path string, body []byte) (int, []byte) {
	t.Helper()
	input := auth.SignatureInput{Method: method, Path: path, Body: body, AccessKey: key.ak,
		Timestamp: fmt.Sprint(time.Now().Unix()), Nonce: probeRandom(t), Audience: "test-audience"}
	if method == http.MethodPost {
		input.ContentType = "application/json"
	}
	signature, err := auth.Sign(input, key.sk)
	require.NoError(t, err)
	r, err := http.NewRequest(method, f.api.URL+path, bytes.NewReader(body))
	require.NoError(t, err)
	for k, v := range map[string]string{"Content-Type": input.ContentType, "X-UVP-Sign-Version": "1", "X-UVP-Access-Key": key.ak,
		"X-UVP-Timestamp": input.Timestamp, "X-UVP-Nonce": input.Nonce, "X-UVP-Signature": signature} {
		if v != "" {
			r.Header.Set(k, v)
		}
	}
	response, err := f.http.Do(r)
	require.True(t, err == nil, "signed isolated API request must complete")
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	require.NoError(t, err)
	return response.StatusCode, raw
}

func (f *productMediaFixture) rejectMedia(t *testing.T, rawURL, protocol string) {
	t.Helper()
	if protocol == "wss-flv" {
		config, err := websocket.NewConfig(rawURL, "https://127.0.0.1")
		require.NoError(t, err)
		config.TlsConfig = f.probe.tlsConfig.Clone()
		config.Dialer = &net.Dialer{Timeout: 2 * time.Second}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		conn, err := config.DialContext(ctx)
		require.True(t, err == nil, "this ZLM upgrades WSS before asynchronous Hook admission")
		defer conn.Close()
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		// HttpSession upgrades first, then emits a CLOSE frame on denied
		// admission. A completed upgrade is not permission to receive media.
		buffer := make([]byte, 1024)
		n, readErr := conn.Read(buffer)
		require.Zero(t, n, "unauthorized WSS must receive no application payload")
		require.ErrorIs(t, readErr, io.EOF, "actual peer closure required, not a silent timeout")
		return
	}
	transport := &http.Transport{TLSClientConfig: f.probe.tlsConfig.Clone(), ForceAttemptHTTP2: false, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	response, err := client.Get(rawURL)
	require.True(t, err == nil, "denial must be an actual HTTP response, not a timeout")
	defer response.Body.Close()
	require.NotEqual(t, http.StatusOK, response.StatusCode)
}

func (f *productMediaFixture) checkPlayers(t *testing.T, a, b string, count int) {
	t.Helper()
	players, err := f.probe.client.GetRuntimeMediaPlayers(context.Background(), zlm.StreamTarget{Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: f.stream})
	require.NoError(t, err)
	require.Equal(t, f.boot, players.BootNonce)
	require.Len(t, players.Players, count)
	ids := make([]string, 0, count)
	for _, p := range players.Players {
		ids = append(ids, p.Identifier)
	}
	if a != "" {
		require.Contains(t, ids, a)
	}
	require.Contains(t, ids, b)
}
