//go:build openapi_live

package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestOpenAPIProbeDelayedHookAndLostKickResponse(t *testing.T) {
	f := newMediaProbeFixture(t)
	f.publish(t, "fault-probe")
	identity, err := f.client.GetRuntimeIdentity(context.Background())
	require.NoError(t, err)
	// Actual TLS negotiation is observed, not inferred from our HTTP client.
	tlsConfig := f.tlsConfig.Clone()
	tlsConfig.NextProtos = []string{"h2", "http/1.1"}
	connection, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", fmt.Sprintf("127.0.0.1:%d", f.tlsPort), tlsConfig)
	require.NoError(t, err)
	require.NotEqual(t, "h2", connection.ConnectionState().NegotiatedProtocol, "this gate only admits one HTTP1 viewer per TCP connection")
	_ = connection.Close()

	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHook := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseHook)
	f.mu.Lock()
	f.delays = map[string]<-chan struct{}{"delayed": release}
	f.mu.Unlock()
	token := f.authorize("delayed")
	transport := &http.Transport{TLSClientConfig: f.tlsConfig.Clone(), Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second}
	receivedMedia := make(chan bool, 1)
	go func() {
		response, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/live/fault-probe.live.flv?probe_auth=%s", f.tlsPort, url.QueryEscape(token)))
		if err != nil {
			receivedMedia <- false
			return
		}
		defer response.Body.Close()
		first := make([]byte, 3)
		_, err = io.ReadFull(response.Body, first)
		receivedMedia <- err == nil && string(first) == "FLV"
	}()
	var delayed probeEvent
	require.Eventually(t, func() bool { var ok bool; delayed, ok = f.event("/play", "delayed"); return ok }, 2*time.Second, 10*time.Millisecond)
	outcome, err := f.client.KickSessionIfMatch(context.Background(), identity.BootNonce, delayed.ID)
	require.NoError(t, err)
	require.Equal(t, zlm.KickShutdownScheduled, outcome)
	releaseHook()
	select {
	case media := <-receivedMedia:
		require.False(t, media, "late successful Hook reply must not revive a closed TCP viewer")
	case <-time.After(5 * time.Second):
		t.Fatal("pending viewer did not finish")
	}
	require.Eventually(t, func() bool {
		sessions, err := f.client.GetRuntimeSessions(context.Background())
		if err != nil || sessions.BootNonce != identity.BootNonce {
			return false
		}
		for _, session := range sessions.Sessions {
			if session.ID == delayed.ID {
				return false
			}
		}
		return true
	}, 2*time.Second, 20*time.Millisecond)

	// The real node accepts the kick, but this controlled failure injector drops
	// its response. The adapter must return unknown/error, not invent completion.
	player := f.player(t, "https-flv", "fault-probe", "lost-response")
	event, ok := f.event("/play", "lost-response")
	require.True(t, ok)
	forwarded := make(chan bool, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/kick_session_if_match" || r.Header.Get("secret") != f.secret {
			w.WriteHeader(403)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			forwarded <- false
			return
		}
		request, err := http.NewRequestWithContext(r.Context(), "POST", fmt.Sprintf("http://127.0.0.1:%d/index/api/kick_session_if_match", f.apiPort), bytes.NewReader(body))
		if err != nil {
			w.WriteHeader(503)
			return
		}
		request.Header.Set("secret", f.secret)
		request.Header.Set("Content-Type", "application/json")
		response, err := (&http.Client{Timeout: time.Second}).Do(request)
		accepted := false
		if err == nil {
			var reply struct {
				Code int `json:"code"`
				Data struct {
					Result string `json:"result"`
				} `json:"data"`
			}
			accepted = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&reply) == nil && reply.Code == 0 && reply.Data.Result == "shutdown_scheduled"
			_ = response.Body.Close()
		}
		forwarded <- accepted
		<-r.Context().Done()
	}))
	defer proxy.Close()
	endpoint, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(endpoint.Port())
	require.NoError(t, err)
	faultClient := zlm.NewClientForNode(&node.Node{Host: "127.0.0.1", APIPort: port, APISecret: f.secret})
	result, err := faultClient.KickSessionIfMatch(context.Background(), identity.BootNonce, event.ID)
	require.ErrorIs(t, err, zlm.ErrRuntimeControlUnavailable)
	require.Empty(t, result, "dropped response must not return closed/scheduled")
	require.True(t, <-forwarded, "the actual node must have accepted the forwarded kick")
	select {
	case <-player.done:
	case <-time.After(time.Second):
		t.Fatal("forwarded real kick did not close the player")
	}
	sessions, err := f.client.GetRuntimeSessions(context.Background())
	require.NoError(t, err)
	require.Equal(t, identity.BootNonce, sessions.BootNonce)
	for _, session := range sessions.Sessions {
		require.NotEqual(t, event.ID, session.ID)
	}
	t.Log("delayed Hook cannot revive kicked session; lost real-kick response returns unavailable until same-boot fresh observation; HTTP2 not negotiated; proxy multiplex/UDP/unknown-ID remain unadmitted")
}
