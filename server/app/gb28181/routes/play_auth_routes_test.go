package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func TestSetPlayAuthorizerWiresOnPlayHook(t *testing.T) {
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	SetPlayAuthorizer(signer)
	t.Cleanup(func() { SetPlayAuthorizer(nil) })

	deviceID := "37010301021320000014"
	channelID := "37010301021320000001"
	streamID := deviceID + "_" + channelID
	grant, err := signer.IssueDirect(playauth.Binding{
		DeviceID: deviceID, ChannelID: channelID, App: "rtp",
		Stream: streamID, MediaServerID: "node-a",
	})
	if err != nil {
		t.Fatal(err)
	}

	engine := gin.New()
	RegisterHookRoutes(engine)
	body, err := json.Marshal(gin.H{
		"app": "rtp", "stream": streamID, "schema": "fmp4", "mediaServerId": "node-a",
		"params": url.Values{playauth.QueryParameter: {grant.Token}}.Encode(),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/index/hook/on_play", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"] != float64(0) {
		t.Fatalf("body=%s", response.Body.String())
	}
}
