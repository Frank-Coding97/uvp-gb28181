package gb28181

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/backendorigin"
	"uvplatform.com/uvp-gb28181/app/gb28181/play"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

type videoShareOriginMediaResolver struct{ generation atomic.Uint64 }

func (r *videoShareOriginMediaResolver) ResolvePlaybackMediaContext(app, stream, uuid string) (playauth.Binding, error) {
	if app != "rtp" || stream != "stream" || uuid != "node" {
		return playauth.Binding{}, backendorigin.ErrUnavailable
	}
	return playauth.Binding{App: app, Stream: stream, MediaServerID: uuid, MediaGeneration: r.generation.Load()}, nil
}

func TestVideoShareBackendOriginUsesStandardZLMAndRejectsChanges(t *testing.T) {
	for _, kind := range []string{"standard-zlm", "revision", "config", "forwarded-header", "uuid", "generation", "post-receipt-generation"} {
		t.Run(kind, func(t *testing.T) {
			previousRegistry, previousOrigins := zlmRegistry, videoShareBackendOrigins
			t.Cleanup(func() {
				if videoShareBackendOrigins != nil {
					videoShareBackendOrigins.Close()
				}
				zlmRegistry, videoShareBackendOrigins = previousRegistry, previousOrigins
			})
			var probed atomic.Bool
			var runtimeCalls atomic.Int32
			media := &videoShareOriginMediaResolver{}
			media.generation.Store(1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/index/api/getRuntimeIdentity":
					runtimeCalls.Add(1)
					w.WriteHeader(404)
				case "/index/api/getServerConfig":
					config := map[string]string{"general.mediaServerId": "node", "http.port": "80", "hook.on_play": "original", "http.forwarded_ip_header": ""}
					if kind == "config" && probed.Load() {
						config["hook.on_play"] = "changed"
					}
					if kind == "forwarded-header" && probed.Load() {
						config["http.forwarded_ip_header"] = "X-Forwarded-For"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]string{config}})
				case "/rtp/stream.live.flv":
					probed.Store(true)
					n, _ := zlmRegistry.Get(1)
					if kind == "revision" {
						n.PlaybackHost = "changed"
						require.NoError(t, zlmRegistry.Update(r.Context(), *n))
					}
					if kind == "generation" {
						media.generation.Store(2)
					}
					uuid := "node"
					if kind == "uuid" {
						uuid = "other"
					}
					err := videoShareBackendOrigins.Observe(r.Context(), r.URL.Query().Get(backendorigin.QueryParameter), 1, uuid, "rtp", "stream", "203.0.113.42")
					if kind == "standard-zlm" || kind == "post-receipt-generation" {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
					if kind == "post-receipt-generation" {
						media.generation.Store(2)
					}
					w.WriteHeader(http.StatusUnauthorized)
				default:
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			host, portRaw, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
			require.NoError(t, err)
			port, err := strconv.Atoi(portRaw)
			require.NoError(t, err)
			zlmRegistry = node.NewRegistry(newSeedRepo())
			_, err = zlmRegistry.Add(context.Background(), node.Node{Host: host, APIPort: port, APISecret: "secret", MediaServerUUID: "node", State: node.StateActive})
			require.NoError(t, err)
			resolve := configureVideoShareBackendOrigin(media)
			ip, err := resolve(context.Background(), &play.Result{App: "rtp", StreamID: "stream", Generation: 1, Node: &play.ResultNode{ID: 1}})
			if kind == "standard-zlm" {
				require.NoError(t, err)
				require.Equal(t, "203.0.113.42", ip)
			} else {
				require.Error(t, err)
				require.Empty(t, ip)
			}
			require.Zero(t, runtimeCalls.Load(), "source proof must work on standard ZLM without enhanced APIs")
		})
	}
}
