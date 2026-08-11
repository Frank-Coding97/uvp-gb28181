package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

func TestOnPlayUsesDirectAuthorizationForFixedStreams(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"), playauth.WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "37010301021320000014"
	channelID := "37010301021320000001"
	streamID := deviceID + "_" + channelID
	binding := playauth.Binding{DeviceID: deviceID, ChannelID: channelID, App: "rtp", Stream: streamID, MediaServerID: "node-a"}
	grant, err := signer.IssueDirect(binding)
	if err != nil {
		t.Fatal(err)
	}

	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayAuthorizer(signer)
	e := gin.New()
	e.POST("/index/hook/on_play", h.OnPlay)
	rr := postJSON(t, e, "/index/hook/on_play", gin.H{
		"app": "rtp", "stream": streamID, "schema": "fmp4", "mediaServerId": "node-a",
		"params": url.Values{playauth.QueryParameter: {grant.Token}}.Encode(),
	})
	assertHookCode(t, rr.Code, rr.Body.Bytes(), 0)

	for name, body := range map[string]gin.H{
		"missing token": {"app": "rtp", "stream": streamID, "mediaServerId": "node-a"},
		"wrong stream":  {"app": "rtp", "stream": deviceID + "_37010301021320000006", "mediaServerId": "node-a", "params": url.Values{playauth.QueryParameter: {grant.Token}}.Encode()},
		"wrong node":    {"app": "rtp", "stream": streamID, "mediaServerId": "node-b", "params": url.Values{playauth.QueryParameter: {grant.Token}}.Encode()},
		"duplicate token": {"app": "rtp", "stream": streamID, "mediaServerId": "node-a", "params": url.Values{
			playauth.QueryParameter: {grant.Token, grant.Token},
		}.Encode()},
		"bad params": {"app": "rtp", "stream": streamID, "mediaServerId": "node-a", "params": "%zz"},
	} {
		t.Run(name, func(t *testing.T) {
			rr := postJSON(t, e, "/index/hook/on_play", body)
			assertHookCode(t, rr.Code, rr.Body.Bytes(), -1)
		})
	}
}

func TestOnPlayFailsClosedForFixedStreamWhenAuthorizerMissing(t *testing.T) {
	h := handler.NewHookController(stream.NewNotifier())
	e := gin.New()
	e.POST("/index/hook/on_play", h.OnPlay)

	fixed := postJSON(t, e, "/index/hook/on_play", gin.H{
		"app": "rtp", "stream": "37010301021320000014_37010301021320000001", "mediaServerId": "node-a",
	})
	assertHookCode(t, fixed.Code, fixed.Body.Bytes(), -1)

	dynamic := postJSON(t, e, "/index/hook/on_play", gin.H{"app": "rtp", "stream": "0200000001"})
	assertHookCode(t, dynamic.Code, dynamic.Body.Bytes(), 0)

	malformed := httptest.NewRequest(http.MethodPost, "/index/hook/on_play", strings.NewReader("{"))
	malformed.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	e.ServeHTTP(response, malformed)
	assertHookCode(t, response.Code, response.Body.Bytes(), -1)
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
