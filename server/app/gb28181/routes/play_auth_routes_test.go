package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type routePlayAuthority struct{}

func (routePlayAuthority) Load(ctx context.Context, _ string) (playauth.DeviceSecurityState, error) {
	if err := ctx.Err(); err != nil {
		return playauth.DeviceSecurityState{}, err
	}
	return playauth.DeviceSecurityState{AccessEpoch: 1}, nil
}

func (routePlayAuthority) AuthorizeLegacy(ctx context.Context, _ string, _ int64) error {
	return ctx.Err()
}

func (routePlayAuthority) AuthorizeEpoch(ctx context.Context, _ string, epoch int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if epoch != 1 {
		return playauth.ErrTokenRevoked
	}
	return nil
}

func TestSetPlayAuthorizerWiresOnPlayHook(t *testing.T) {
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	authorization := playauth.NewAuthorizationService(signer,
		playauth.NewAuthorizationRegistry(),
		playauth.WithDeviceSecurityAuthority(routePlayAuthority{}))
	SetPlayAuthorizer(authorization)
	t.Cleanup(func() { SetPlayAuthorizer(nil) })
	mediaNode := &node.Node{MediaServerUUID: "node-a", APISecret: "zlm-secret"}
	installRouteHookAuth(t, mediaNode)

	deviceID := "37010301021320000014"
	channelID := "37010301021320000001"
	streamID := deviceID + "_" + channelID
	grant, err := authorization.IssueDirect(playauth.Binding{
		DeviceID: deviceID, ChannelID: channelID, App: "rtp",
		Stream: streamID, MediaServerID: "node-a", DeviceEpoch: 1,
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
	request := httptest.NewRequest(http.MethodPost, authenticatedHookPath(t, "/index/hook/on_play", mediaNode, playauth.HookOnPlay), bytes.NewReader(body))
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
