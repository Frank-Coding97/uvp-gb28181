package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/models"
)

func TestCatalogJSONGatewayRejectsInvalidTransportAndAuthenticatesBody(t *testing.T) {
	for _, kind := range []string{"valid", "tampered", "unknown", "duplicate", "chunked", "content-type", "no-scope", "missing-key", "cross-department", "replay"} {
		t.Run(kind, func(t *testing.T) {
			gate, db, secret := gatewayFixture(t)
			require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
			deviceID, channelID := "34020000002000000010", "37011200001310000010"
			dept := uint(10)
			if kind == "cross-department" {
				dept = 20
			}
			require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: deviceID, OwnerDeptID: dept}).Error)
			require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: deviceID, ChannelID: channelID, OwnerDeptID: dept}).Error)
			scope := PTZPresetCallScope
			if kind != "no-scope" {
				require.NoError(t, db.Create(&models.ClientScope{ClientID: 1, Scope: scope, Enabled: true}).Error)
			}
			spy := &catalogDispatchSpy{}
			registry := catalogruntime.NewAdapterRegistry()
			require.NoError(t, registry.Register(catalogruntime.AdapterRegistration{Key: "test.json.v1", ContractVersion: "v1", Adapter: spy}))
			runtime := catalogruntime.NewCatalogRuntime(registry, nil)
			_, report, err := runtime.Publish(context.Background(), catalogruntime.Draft{Operations: []catalogruntime.Operation{{Scope: scope, Method: "POST", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}/call", AdapterKey: "test.json.v1", ContractVersion: "v1"}}})
			require.NoError(t, err)
			require.True(t, report.Valid())
			gate.catalog = runtime
			path := "/openapi/v1/devices/" + deviceID + "/channels/" + channelID + "/ptz/presets/1/call"
			body := "{}"
			if kind == "unknown" {
				body = `{"ownerDeptId":20}`
			}
			if kind == "duplicate" {
				body = `{"a":1,"a":2}`
			}
			key := "catalog-test-key"
			if kind == "missing-key" {
				key = ""
			}
			input := SignatureInput{Method: "POST", Path: path, ContentType: "application/json", Body: []byte(body), AccessKey: fmt.Sprintf("uvp_%032x", 1), Timestamp: fmt.Sprint(time.Now().Unix()), Nonce: strings.Repeat("a", 32), IdempotencyKey: key, Audience: "test-audience"}
			// Invalid JSON is signed as a valid body then replaced, to exercise the boundary parser.
			signed := input
			if kind == "duplicate" {
				signed.Body = []byte("{}")
			}
			signature, err := Sign(signed, secret)
			require.NoError(t, err)
			if kind == "tampered" {
				body = "{ }"
			}
			request := httptest.NewRequest("POST", path, strings.NewReader(body))
			for k, v := range map[string]string{"Content-Type": "application/json", "X-UVP-Sign-Version": "1", "X-UVP-Access-Key": input.AccessKey, "X-UVP-Timestamp": input.Timestamp, "X-UVP-Nonce": input.Nonce, "X-UVP-Signature": signature, "Idempotency-Key": key} {
				if v != "" {
					request.Header.Set(k, v)
				}
			}
			if kind == "chunked" {
				request.ContentLength = -1
				request.TransferEncoding = []string{"chunked"}
			}
			if kind == "content-type" {
				request.Header.Set("Content-Type", "text/plain")
			}
			router := gin.New()
			router.POST("/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets/:presetId/call", func(c *gin.Context) {
				gate.handleCatalog(c, "json-test", scope, func(response gatewayResponse) { writeGateway(c, response) })
			})
			out := httptest.NewRecorder()
			router.ServeHTTP(out, request)
			expected := http.StatusBadRequest
			switch kind {
			case "valid", "replay":
				expected = 200
			case "tampered":
				expected = 401
			case "no-scope":
				expected = 403
			case "cross-department":
				expected = 404
			}
			require.Equal(t, expected, out.Code, out.Body.String())
			invocation := spy.Invocation()
			if expected == 200 {
				typed, ok := invocation.Value.(adapters.Request)
				require.True(t, ok)
				require.EqualValues(t, 1, typed.ClientID)
				require.Equal(t, key, typed.IdempotencyKey)
				require.Equal(t, []byte(body), invocation.Body)
				require.Equal(t, "1", invocation.Params["presetId"])
			} else {
				require.Empty(t, invocation.Scope)
			}
			if kind == "replay" {
				out = httptest.NewRecorder()
				retry := request.Clone(context.Background())
				retry.Body = io.NopCloser(strings.NewReader(body))
				router.ServeHTTP(out, retry)
				require.Equal(t, 401, out.Code, out.Body.String())
				require.Contains(t, out.Body.String(), "REQUEST_REPLAYED")
			}
		})
	}
}
