package controllers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

func TestZLMNodeControllerT13_ImpactPreflightAndFingerprintRace(t *testing.T) {
	r, svc := setupRouter(t)
	_, resp := do(t, r, "POST", "/api/gb28181/zlm/nodes", service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})
	id := int64(resp["data"].(map[string]any)["id"].(float64))

	impact := service.NodeImpact{Streams: 2, Recordings: 1, Sessions: 3}
	svc.SetNodeImpactProbe(func(context.Context, *node.Node) (service.NodeImpact, error) {
		return impact, nil
	})
	idPath := "/api/gb28181/zlm/nodes/" + pathInt(id)
	preflightResponse, preflightBody := do(t, r, "DELETE", idPath, map[string]any{"force": true})
	require.Equal(t, 200, preflightResponse.Code)
	preflight := preflightBody["data"].(map[string]any)
	require.Equal(t, "delete", preflight["action"])
	require.Equal(t, float64(2), preflight["impact"].(map[string]any)["streams"])
	fingerprint := preflight["fingerprint"].(string)

	// The provider changes after confirmation was displayed. The request body
	// is ignored; only the explicit query/header fingerprint can authorize it.
	impact = service.NodeImpact{Streams: 3, Recordings: 1, Sessions: 3}
	failedResponse, failedBody := do(t, r, "DELETE", idPath+"?fingerprint="+fingerprint, nil)
	require.Equal(t, float64(1), failedBody["code"])
	require.Contains(t, []int{200, 409}, failedResponse.Code, "the shared response handler may encode business failures as 200")
	_, stillPresent := svc.Get(context.Background(), id)
	require.NoError(t, stillPresent)
}
