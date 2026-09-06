package zlm

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestOpenAPIControlConfigurationPreflight(t *testing.T) {
	for _, failure := range []string{"", "uuid", "hook", "disabled", "flow", "debug", "budget", "retry", "boot", "config-format"} {
		t.Run(failure, func(t *testing.T) {
			n := node.Node{ID: 1, Revision: 7, MediaServerUUID: "node-a", APISecret: "fixture-secret", State: node.StateActive}
			base, err := url.Parse("https://api.example.test/api/gb28181/hook")
			require.NoError(t, err)
			config := map[string]string{"general.mediaServerId": n.MediaServerUUID, "hook.enable": "1", "general.flowThreshold": "0", "api.apiDebug": "0", "hook.timeoutSec": "1.5", "hook.retry": "1", "hook.retry_delay": "0.5"}
			for _, event := range playauth.ManagedHookEvents() {
				config["hook."+string(event)], err = buildManagedHookURL(base, n.APISecret, n.MediaServerUUID, event)
				require.NoError(t, err)
			}
			switch failure {
			case "uuid":
				config["general.mediaServerId"] = "node-b"
			case "hook":
				config["hook.on_play"] += "x"
			case "disabled":
				config["hook.enable"] = "0"
			case "flow":
				config["general.flowThreshold"] = "1024"
			case "debug":
				config["api.apiDebug"] = "1"
			case "budget":
				config["hook.timeoutSec"] = "10"
			case "retry":
				config["hook.retry_delay"] = "NaN"
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, n.APISecret, r.Header.Get("secret"))
				require.Empty(t, r.URL.RawQuery, "no credentials in URL")
				require.Equal(t, "no-store", r.Header.Get("Cache-Control"))
				w.Header().Set("Cache-Control", "no-store")
				var body any
				switch strings.TrimPrefix(r.URL.Path, "/index/api/") {
				case "getServerConfig":
					body = map[string]any{"code": 0, "data": []any{config}}
					if failure == "config-format" {
						body = map[string]any{"code": 0, "data": []any{config, config}}
					}
				case "getRuntimeIdentity":
					body = map[string]any{"code": 0, "data": map[string]any{"protocolVersion": 1, "bootNonce": "000102030405060708090a0b0c0d0e0f"}}
				case "getAllSession":
					boot := "000102030405060708090a0b0c0d0e0f"
					if failure == "boot" {
						boot = "100102030405060708090a0b0c0d0e0f"
					}
					body = map[string]any{"code": 0, "bootNonce": boot, "data": []any{}}
				default:
					t.Error("preflight must not mutate config or create media")
					w.WriteHeader(404)
					return
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			control, err := NewOpenAPIRuntimeControl(n, OpenAPIControlTLS{Endpoint: server.URL + "/index/api", Roots: roots, SPKISHA256: sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)})
			require.NoError(t, err)
			defer control.Close()
			got, err := control.ProbeConfiguration(context.Background(), base.String())
			if failure == "" {
				require.NoError(t, err)
				require.EqualValues(t, 1, got.ProtocolVersion)
			} else {
				require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
				require.Empty(t, got.BootNonce)
			}
		})
	}
}
