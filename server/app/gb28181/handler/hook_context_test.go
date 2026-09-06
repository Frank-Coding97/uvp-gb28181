package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

// hookDeviceAuthority is an in-memory test authority. It deliberately does
// not model a persistent SQL authority; the production service receives the
// authority through its interface and the tests only exercise its contract.
type hookDeviceAuthority struct {
	mu           sync.Mutex
	state        playauth.DeviceSecurityState
	epochCalls   int
	blockAt      int
	epochStarted chan struct{}
	startOnce    sync.Once
	canceled     atomic.Bool
}

func newHookDeviceAuthority(epoch int64) *hookDeviceAuthority {
	return &hookDeviceAuthority{state: playauth.DeviceSecurityState{AccessEpoch: epoch}}
}

func (a *hookDeviceAuthority) setEpoch(epoch int64) {
	a.mu.Lock()
	a.state.AccessEpoch = epoch
	a.mu.Unlock()
}

func (a *hookDeviceAuthority) blockEpochAt(call int, started chan struct{}) {
	a.mu.Lock()
	a.blockAt = call
	a.epochStarted = started
	a.mu.Unlock()
}

func (a *hookDeviceAuthority) Load(ctx context.Context, _ string) (playauth.DeviceSecurityState, error) {
	if err := ctx.Err(); err != nil {
		return playauth.DeviceSecurityState{}, err
	}
	a.mu.Lock()
	state := a.state
	a.mu.Unlock()
	return state, nil
}

func (a *hookDeviceAuthority) AuthorizeLegacy(ctx context.Context, _ string, _ int64) error {
	return ctx.Err()
}

func (a *hookDeviceAuthority) AuthorizeEpoch(ctx context.Context, _ string, epoch int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	a.epochCalls++
	call := a.epochCalls
	blockAt := a.blockAt
	started := a.epochStarted
	state := a.state
	a.mu.Unlock()
	if call == blockAt {
		if started != nil {
			a.startOnce.Do(func() { close(started) })
		}
		select {
		case <-ctx.Done():
			a.canceled.Store(true)
			return ctx.Err()
		}
	}
	if epoch != state.AccessEpoch {
		return playauth.ErrTokenRevoked
	}
	return nil
}

func newHookAuthorization(t *testing.T, signer *playauth.Signer, now func() time.Time, authority *hookDeviceAuthority) *playauth.AuthorizationService {
	t.Helper()
	return playauth.NewAuthorizationService(
		signer,
		playauth.NewAuthorizationRegistry(playauth.WithAuthorizationRegistryNow(now)),
		playauth.WithDeviceSecurityAuthority(authority),
	)
}

func postJSONWithContext(t *testing.T, engine http.Handler, path string, ctx context.Context, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	return response
}

func waitHookAuthorityCall(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("device authority was not reached")
	}
}

func TestOnPlayRawSignerDoesNotSatisfyProductionAuthorizer(t *testing.T) {
	signer, err := playauth.NewSigner([]byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	var candidate any = signer
	_, ok := candidate.(handler.PlayAuthorizer)
	require.False(t, ok, "raw signer must not satisfy the production context authorizer")
}

func TestOnPlayRejectsOldDeviceEpoch(t *testing.T) {
	setHookPlayAuth(t, true, false)
	now := time.Unix(1_700_000_000, 0).UTC()
	signer, err := playauth.NewSigner([]byte("01234567890123456789012345678901"), playauth.WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	authority := newHookDeviceAuthority(1)
	authorization := newHookAuthorization(t, signer, func() time.Time { return now }, authority)
	binding := playauth.Binding{
		DeviceID: "34020000001110000001", ChannelID: "34020000001320000001",
		App: "rtp", Stream: "stream-1", MediaServerID: "ms-1", MediaGeneration: 3,
		DeviceEpoch: 1,
	}
	grant, err := authorization.IssueDirect(binding)
	require.NoError(t, err)
	authority.setEpoch(2)

	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayAuthorizer(authorization)
	h.SetPlaybackMediaContextResolver(hookMediaResolver{binding: binding})
	engine := gin.New()
	engine.POST("/play", h.OnPlay)
	response := postJSON(t, engine, "/play", gin.H{
		"app": binding.App, "stream": binding.Stream, "mediaServerId": binding.MediaServerID,
		"params": url.Values{playauth.QueryParameter: []string{grant.Token}, "client_ip": []string{"192.0.2.10"}}.Encode(),
	})
	assertHookCode(t, response.Code, response.Body.Bytes(), -1)
}

func TestOnPlayPassesRequestContextToDeviceAuthority(t *testing.T) {
	setHookPlayAuth(t, true, false)
	now := time.Unix(1_700_000_100, 0).UTC()
	signer, err := playauth.NewSigner([]byte("01234567890123456789012345678901"), playauth.WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	authority := newHookDeviceAuthority(1)
	authorization := newHookAuthorization(t, signer, func() time.Time { return now }, authority)
	binding := playauth.Binding{
		DeviceID: "34020000001110000001", ChannelID: "34020000001320000001",
		App: "rtp", Stream: "stream-1", MediaServerID: "ms-1", MediaGeneration: 3,
		DeviceEpoch: 1,
	}
	grant, err := authorization.IssueDirect(binding)
	require.NoError(t, err)
	started := make(chan struct{})
	authority.blockEpochAt(2, started)

	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayAuthorizer(authorization)
	h.SetPlaybackMediaContextResolver(hookMediaResolver{binding: binding})
	engine := gin.New()
	engine.POST("/play", h.OnPlay)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- postJSONWithContext(t, engine, "/play", ctx, gin.H{
			"app": binding.App, "stream": binding.Stream, "mediaServerId": binding.MediaServerID,
			"params": url.Values{playauth.QueryParameter: []string{grant.Token}, "client_ip": []string{"192.0.2.10"}}.Encode(),
		})
	}()
	waitHookAuthorityCall(t, started)
	cancel()
	response := <-result
	assertHookCode(t, response.Code, response.Body.Bytes(), -1)
	require.True(t, authority.canceled.Load())
}

func TestOnPlayMarkPassesRequestContextToDeviceAuthority(t *testing.T) {
	setHookPlayAuth(t, true, true)
	now := time.Unix(1_700_000_200, 0).UTC()
	signer, err := playauth.NewSigner([]byte("01234567890123456789012345678901"), playauth.WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	authority := newHookDeviceAuthority(1)
	authorization := newHookAuthorization(t, signer, func() time.Time { return now }, authority)
	binding := playauth.Binding{
		DeviceID: "34020000001110000001", ChannelID: "34020000001320000001",
		App: "rtp", Stream: "stream-1", MediaServerID: "ms-1", BindClientIP: true,
		ClientIP: "192.0.2.10", DeviceEpoch: 1,
	}
	grant, err := authorization.IssueDirect(binding)
	require.NoError(t, err)
	started := make(chan struct{})
	// IssueDirect and VerifyContext consume the first two epoch checks; the
	// third is MarkVerifiedClientSourceContext.
	authority.blockEpochAt(3, started)

	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayAuthorizer(authorization)
	h.SetPlaybackMediaContextResolver(hookMediaResolver{binding: binding})
	engine := gin.New()
	engine.POST("/play", h.OnPlay)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- postJSONWithContext(t, engine, "/play", ctx, gin.H{
			"app": binding.App, "stream": binding.Stream, "mediaServerId": binding.MediaServerID, "ip": binding.ClientIP,
			"params": url.Values{playauth.QueryParameter: []string{grant.Token}, "client_ip": []string{binding.ClientIP}}.Encode(),
		})
	}()
	waitHookAuthorityCall(t, started)
	cancel()
	response := <-result
	assertHookCode(t, response.Code, response.Body.Bytes(), -1)
	require.True(t, authority.canceled.Load())
}

type v2AutoStartAuthorizer struct{ claims playauth.Claims }

func (a v2AutoStartAuthorizer) VerifyContext(context.Context, string, playauth.Binding) (playauth.Claims, error) {
	return a.claims, nil
}

func (a v2AutoStartAuthorizer) VerifyForAutoStartContext(context.Context, string, playauth.Binding) (playauth.Claims, error) {
	return a.claims, nil
}

func TestOnStreamNotFoundPreservesV2ZeroDeviceEpoch(t *testing.T) {
	fixture := newAutoOnDemandFixture(t)
	fixture.controller.SetPlayAuthorizer(v2AutoStartAuthorizer{claims: playauth.Claims{
		Version: 2, DeviceID: fixture.deviceID, ChannelID: fixture.channelID,
		AuthorizationGeneration: "legacy", DeviceEpoch: 0,
	}})
	fixture.token = "v2-test-token"
	fixture.body["params"] = url.Values{playauth.QueryParameter: []string{fixture.token}}.Encode()

	response := fixture.serve(t)
	assertHookCode(t, response.Code, response.Body.Bytes(), 0)
	require.Len(t, fixture.dispatcher.requests, 1)
	require.Zero(t, fixture.dispatcher.requests[0].DeviceEpoch)
}

func TestOnStreamNotFoundRejectsOldDeviceEpoch(t *testing.T) {
	fixture := newAutoOnDemandFixture(t)
	fixture.authority.setEpoch(fixture.deviceEpoch + 1)

	response := fixture.serve(t)
	assertHookCode(t, response.Code, response.Body.Bytes(), -1)
	require.Empty(t, fixture.dispatcher.requests)
}
