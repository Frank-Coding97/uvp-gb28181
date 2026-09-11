package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type hookAuthConfig struct{ values map[string]interface{} }

func (*hookAuthConfig) ConfigFileChangeListen(...func())    {}
func (c *hookAuthConfig) Get(key string) interface{}        { return c.values[key] }
func (*hookAuthConfig) GetString(string) string             { return "" }
func (c *hookAuthConfig) GetBool(key string) bool           { value, _ := c.values[key].(bool); return value }
func (*hookAuthConfig) GetInt(string) int                   { return 0 }
func (*hookAuthConfig) GetInt32(string) int32               { return 0 }
func (*hookAuthConfig) GetInt64(string) int64               { return 0 }
func (*hookAuthConfig) GetFloat64(string) float64           { return 0 }
func (*hookAuthConfig) GetDuration(string) time.Duration    { return 0 }
func (*hookAuthConfig) GetStringSlice(string) []string      { return nil }
func (*hookAuthConfig) GetUintSlice(string) []uint          { return nil }
func (c *hookAuthConfig) Set(key string, value interface{}) { c.values[key] = value }
func (*hookAuthConfig) SaveConfig() error                   { return nil }

func setHookPlayAuth(t *testing.T, enabled, bindIP bool) {
	t.Helper()
	previous := app.ConfigYml
	app.ConfigYml = &hookAuthConfig{values: map[string]interface{}{
		gbconfig.PlayAuthEnabledConfigKey:      enabled,
		gbconfig.PlayAuthBindClientIPConfigKey: bindIP,
	}}
	t.Cleanup(func() { app.ConfigYml = previous })
}

type hookMediaResolver struct {
	binding     playauth.Binding
	err         error
	coldBinding playauth.Binding
	coldErr     error
}

func (r hookMediaResolver) ResolvePlaybackMediaContext(appName, streamID, mediaServerID string) (playauth.Binding, error) {
	if r.err != nil {
		return playauth.Binding{}, r.err
	}
	if r.binding.App != appName || r.binding.Stream != streamID || r.binding.MediaServerID != mediaServerID {
		return playauth.Binding{}, errors.New("not current")
	}
	return r.binding, nil
}

func (r hookMediaResolver) ResolveColdPlaybackMediaContext(_ context.Context, appName, streamID, mediaServerID string) (playauth.Binding, error) {
	if r.coldErr != nil {
		return playauth.Binding{}, r.coldErr
	}
	if r.coldBinding.App != appName || r.coldBinding.Stream != streamID || r.coldBinding.MediaServerID != mediaServerID {
		return playauth.Binding{}, errors.New("cold media is not absent")
	}
	return r.coldBinding, nil
}

func TestOnPlayAuthorizesDynamicAndFixedCurrentMedia(t *testing.T) {
	setHookPlayAuth(t, true, true)
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"), playauth.WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	for _, streamID := range []string{"0200000001", "37010301021320000014_37010301021320000001"} {
		t.Run(streamID, func(t *testing.T) {
			binding := playauth.Binding{
				DeviceID: "37010301021320000014", ChannelID: "37010301021320000001",
				App: "rtp", Stream: streamID, MediaServerID: "node-a", MediaGeneration: 9,
				BindClientIP: true, ClientIP: "203.0.113.9",
			}
			grant, issueErr := signer.IssueDirect(binding)
			if issueErr != nil {
				t.Fatal(issueErr)
			}
			h := handler.NewHookController(stream.NewNotifier())
			h.SetPlayAuthorizer(signer)
			h.SetPlaybackMediaContextResolver(hookMediaResolver{binding: binding})
			e := gin.New()
			e.POST("/index/hook/on_play", h.OnPlay)
			rr := postJSON(t, e, "/index/hook/on_play", gin.H{
				"app": "rtp", "stream": streamID, "schema": "fmp4", "mediaServerId": "node-a", "ip": "203.0.113.9",
				"params": "?" + url.Values{playauth.QueryParameter: {grant.Token}, "type": {"play"}}.Encode(),
			})
			assertHookCode(t, rr.Code, rr.Body.Bytes(), 0)

			for name, mutate := range map[string]func(gin.H){
				"missing token": func(body gin.H) { body["params"] = "" },
				"wrong stream":  func(body gin.H) { body["stream"] = "37010301021320000014_37010301021320000006" },
				"wrong node":    func(body gin.H) { body["mediaServerId"] = "node-b" },
				"wrong ip":      func(body gin.H) { body["ip"] = "203.0.113.10" },
				"duplicate token": func(body gin.H) {
					body["params"] = url.Values{playauth.QueryParameter: {grant.Token, grant.Token}}.Encode()
				},
				"bad params": func(body gin.H) { body["params"] = "%zz" },
			} {
				t.Run(name, func(t *testing.T) {
					body := gin.H{"app": "rtp", "stream": streamID, "mediaServerId": "node-a", "ip": "203.0.113.9", "params": url.Values{playauth.QueryParameter: {grant.Token}}.Encode()}
					mutate(body)
					rr := postJSON(t, e, "/index/hook/on_play", body)
					assertHookCode(t, rr.Code, rr.Body.Bytes(), -1)
					if strings.Contains(rr.Body.String(), grant.Token) {
						t.Fatal("denial response leaked token")
					}
				})
			}
		})
	}
}

func TestOnPlayAuthorizesColdFixedPreauthorizationBeforeLiveMediaExists(t *testing.T) {
	setHookPlayAuth(t, true, false)
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "37010301021320000014"
	channelID := "37010301021320000001"
	streamID := deviceID + "_" + channelID
	binding := playauth.Binding{
		DeviceID: deviceID, ChannelID: channelID,
		App: "rtp", Stream: streamID, MediaServerID: "node-a",
	}
	authorization := playauth.NewAuthorizationService(signer, playauth.NewAuthorizationRegistry())
	grant, err := authorization.IssueDirect(binding)
	if err != nil {
		t.Fatal(err)
	}

	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayAuthorizer(authorization)
	h.SetPlaybackMediaContextResolver(hookMediaResolver{err: play.ErrPlaybackMediaNotCurrent, coldBinding: binding})
	router := gin.New()
	router.POST("/index/hook/on_play", h.OnPlay)
	response := postJSON(t, router, "/index/hook/on_play", gin.H{
		"app": "rtp", "stream": streamID, "schema": "fmp4", "mediaServerId": "node-a",
		"params": "?" + url.Values{playauth.QueryParameter: {grant.Token}}.Encode(),
	})
	assertHookCode(t, response.Code, response.Body.Bytes(), 0)

	response = postJSON(t, router, "/index/hook/on_play", gin.H{
		"app": "rtp", "stream": streamID, "schema": "fmp4", "mediaServerId": "node-b",
		"params": "?" + url.Values{playauth.QueryParameter: {grant.Token}}.Encode(),
	})
	assertHookCode(t, response.Code, response.Body.Bytes(), -1)
}

func TestOnPlayRejectsColdFallbackWhenMediaAbsenceIsNotProven(t *testing.T) {
	setHookPlayAuth(t, true, false)
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "37010301021320000014"
	channelID := "37010301021320000001"
	streamID := deviceID + "_" + channelID
	binding := playauth.Binding{
		DeviceID: deviceID, ChannelID: channelID,
		App: "rtp", Stream: streamID, MediaServerID: "node-a",
	}
	authorization := playauth.NewAuthorizationService(signer, playauth.NewAuthorizationRegistry())
	grant, err := authorization.IssueDirect(binding)
	if err != nil {
		t.Fatal(err)
	}

	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayAuthorizer(authorization)
	h.SetPlaybackMediaContextResolver(hookMediaResolver{
		err:     play.ErrPlaybackMediaNotCurrent,
		coldErr: errors.New("media is online or ownership is uncertain"),
	})
	router := gin.New()
	router.POST("/index/hook/on_play", h.OnPlay)
	response := postJSON(t, router, "/index/hook/on_play", gin.H{
		"app": "rtp", "stream": streamID, "schema": "fmp4", "mediaServerId": "node-a",
		"params": "?" + url.Values{playauth.QueryParameter: {grant.Token}}.Encode(),
	})
	assertHookCode(t, response.Code, response.Body.Bytes(), -1)
}

func TestOnPlayCompatibilityAndFailClosedPolicy(t *testing.T) {
	h := handler.NewHookController(stream.NewNotifier())
	e := gin.New()
	e.POST("/index/hook/on_play", h.OnPlay)

	setHookPlayAuth(t, false, false)
	for _, body := range []gin.H{
		{"app": "rtp", "stream": "0200000001"},
		{"app": "rtp", "stream": "37010301021320000014_37010301021320000001"},
	} {
		assertHookCode(t, http.StatusOK, postJSON(t, e, "/index/hook/on_play", body).Body.Bytes(), 0)
	}

	app.ConfigYml.Set(gbconfig.PlayAuthEnabledConfigKey, true)
	assertHookCode(t, http.StatusOK, postJSON(t, e, "/index/hook/on_play", gin.H{"app": "rtp", "stream": "0200000001"}).Body.Bytes(), -1)
	assertHookCode(t, http.StatusOK, postJSON(t, e, "/index/hook/on_play", gin.H{"app": "talk", "stream": "talk-stream"}).Body.Bytes(), 0)

	malformed := httptest.NewRequest(http.MethodPost, "/index/hook/on_play", strings.NewReader("{"))
	malformed.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	e.ServeHTTP(response, malformed)
	assertHookCode(t, response.Code, response.Body.Bytes(), -1)

	oversized := postJSON(t, e, "/index/hook/on_play", gin.H{
		"app": "talk", "stream": "talk-stream", "params": strings.Repeat("x", 17<<10),
	})
	assertHookCode(t, oversized.Code, oversized.Body.Bytes(), -1)
}

func TestOnPlayLogsStableDenialReasonWithoutToken(t *testing.T) {
	setHookPlayAuth(t, true, false)
	core, observed := observer.New(zap.DebugLevel)
	previousLogger := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previousLogger })

	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayAuthorizer(&rejectingPlayAuthorizer{})
	h.SetPlaybackMediaContextResolver(hookMediaResolver{binding: playauth.Binding{
		DeviceID: "37010301021320000014", ChannelID: "37010301021320000001",
		App: "rtp", Stream: "dynamic-stream", MediaServerID: "node-a", MediaGeneration: 9,
	}})
	router := gin.New()
	router.POST("/index/hook/on_play", h.OnPlay)
	response := postJSON(t, router, "/index/hook/on_play", gin.H{
		"app": "rtp", "stream": "dynamic-stream", "mediaServerId": "node-a",
		"params": "?play_token=must-not-appear-in-log",
	})
	assertHookCode(t, response.Code, response.Body.Bytes(), -1)

	entries := observed.FilterMessage("播放鉴权 Hook 已拒绝").All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.Equal(t, "tampered", fields["reason"])
	encoded, err := json.Marshal(fields)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "must-not-appear-in-log")
}

type rejectingPlayAuthorizer struct{}

func (*rejectingPlayAuthorizer) Verify(string, playauth.Binding) (playauth.Claims, error) {
	return playauth.Claims{}, playauth.ErrTokenInvalid
}

func assertHookCode(t *testing.T, status int, raw []byte, want float64) {
	t.Helper()
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["code"] != want {
		t.Fatalf("code=%v want=%v body=%s", body["code"], want, raw)
	}
}
