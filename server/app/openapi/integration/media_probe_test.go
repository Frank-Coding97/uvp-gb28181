//go:build openapi_live

package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

type probePlayer struct {
	bytes atomic.Int64
	done  chan struct{}
	close func()
}

func (f *mediaProbeFixture) player(t *testing.T, protocol, stream, label string) *probePlayer {
	t.Helper()
	token := f.authorize(label)
	path := fmt.Sprintf("127.0.0.1:%d/live/%s.live.flv?probe_auth=%s&bootNonce=attacker&protocol=attacker", f.tlsPort, stream, url.QueryEscape(token))
	scheme := "https://"
	if protocol == "wss-flv" {
		scheme = "wss://"
	}
	return f.playerURL(t, protocol, scheme+path)
}

func (f *mediaProbeFixture) playerURL(t *testing.T, protocol, rawURL string) *probePlayer {
	t.Helper()
	var reader io.ReadCloser
	var cleanup func()
	if protocol == "https-flv" {
		transport := &http.Transport{TLSClientConfig: f.tlsConfig.Clone(), ForceAttemptHTTP2: false, DisableKeepAlives: true, Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, ResponseHeaderTimeout: 3 * time.Second}
		client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := client.Get(rawURL)
		// Do not expose the request URL (fixture authorization) on errors.
		require.True(t, err == nil, "HTTPS fixture player must connect")
		require.Equal(t, 200, response.StatusCode)
		require.Equal(t, 1, response.ProtoMajor)
		reader = response.Body
		cleanup = func() { _ = reader.Close(); transport.CloseIdleConnections() }
	} else {
		config, err := websocket.NewConfig(rawURL, "https://127.0.0.1")
		require.NoError(t, err)
		config.TlsConfig = f.tlsConfig.Clone()
		config.Dialer = &net.Dialer{Timeout: 3 * time.Second}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, err := config.DialContext(ctx)
		cancel()
		require.True(t, err == nil, "WSS fixture player must connect")
		reader = conn
		cleanup = func() { _ = reader.Close() }
	}
	p := &probePlayer{done: make(chan struct{}), close: cleanup}
	t.Cleanup(cleanup)
	go func() {
		defer close(p.done)
		buffer := make([]byte, 32768)
		for {
			n, err := reader.Read(buffer)
			p.bytes.Add(int64(n))
			if err != nil {
				return
			}
		}
	}()
	require.Eventually(t, func() bool { return p.bytes.Load() > 4096 }, 5*time.Second, 20*time.Millisecond, "actual media payload required")
	return p
}

func TestOpenAPIProbeProtocolReportedFromSocket(t *testing.T) {
	f := newMediaProbeFixture(t)
	for _, protocol := range []struct{ player, hook string }{{"https-flv", "https"}, {"wss-flv", "wss"}} {
		t.Run(protocol.player, func(t *testing.T) {
			stream := "socket-protocol-" + protocol.player
			f.publish(t, stream)
			player := f.player(t, protocol.player, stream, protocol.player)
			var event probeEvent
			require.Eventually(t, func() bool { var ok bool; event, ok = f.event("/play", protocol.player); return ok }, time.Second, 10*time.Millisecond)
			require.Equal(t, "rtmp", event.Schema, "FLV media schema is not the connection protocol")
			require.Equal(t, protocol.hook, event.Protocol, "query cannot override actual TLS socket protocol")
			playEvent := event
			before := player.bytes.Load()
			require.Never(t, func() bool { _, ok := f.event("/flow", protocol.player); return ok }, time.Second, 20*time.Millisecond,
				"an active original player must not emit a final flow report")
			require.Greater(t, player.bytes.Load(), before, "the original player must keep receiving media during the observation")
			player.close()
			require.Eventually(t, func() bool { var ok bool; event, ok = f.event("/flow", protocol.player); return ok }, 3*time.Second, 10*time.Millisecond)
			require.Equal(t, protocol.hook, event.Protocol, "flow and play must identify the same connection protocol")
			require.Equal(t, playEvent.ID, event.ID)
			require.Equal(t, playEvent.Boot, event.Boot)
			require.Equal(t, playEvent.Schema, event.Schema)
			require.Equal(t, playEvent.VHost, event.VHost)
			require.Equal(t, playEvent.App, event.App)
			require.Equal(t, playEvent.Stream, event.Stream)
		})
	}
}

func TestOpenAPIProbeActualTLSMedia(t *testing.T) {
	f := newMediaProbeFixture(t)
	identity, err := f.client.GetRuntimeIdentity(context.Background())
	require.NoError(t, err)
	for _, protocol := range []string{"https-flv", "wss-flv"} {
		t.Run(protocol, func(t *testing.T) {
			stream := "probe-" + protocol
			f.publish(t, stream)
			generation := f.generation(stream)
			players := make([]*probePlayer, 3)
			events := make([]probeEvent, 3)
			for i, label := range []string{"A", "B", "U"} {
				label = protocol + "-" + label
				players[i] = f.player(t, protocol, stream, label)
				require.Eventually(t, func() bool { var ok bool; events[i], ok = f.event("/play", label); return ok }, time.Second, 10*time.Millisecond)
				require.Equal(t, identity.BootNonce, events[i].Boot, "query cannot override server boot identity")
				require.Equal(t, stream, events[i].Stream)
				require.Equal(t, "live", events[i].App)
				require.Equal(t, "__defaultVhost__", events[i].VHost)
			}
			require.NotEqual(t, events[0].ID, events[1].ID)
			require.NotEqual(t, events[0].ID, events[2].ID)
			require.NotEqual(t, events[1].ID, events[2].ID)
			snapshot, err := f.client.GetRuntimeMediaPlayers(context.Background(), probeTarget(stream))
			require.NoError(t, err)
			require.Equal(t, identity.BootNonce, snapshot.BootNonce)
			require.Len(t, snapshot.Players, 3)
			ids := make(map[string]bool)
			for _, player := range snapshot.Players {
				ids[player.Identifier] = true
				require.Equal(t, "127.0.0.1", player.PeerIP)
			}
			for _, event := range events {
				require.True(t, ids[event.ID], "association comes from authenticated Hook, never IP/order")
			}
			sessions, err := f.client.GetRuntimeSessions(context.Background())
			require.NoError(t, err)
			require.Equal(t, identity.BootNonce, sessions.BootNonce)
			publisherID := ""
			for _, session := range sessions.Sessions {
				if ids[session.ID] {
					require.Equal(t, "tcp", session.Type)
				}
				if session.LocalPort == f.rtmpPort {
					require.Empty(t, publisherID)
					publisherID = session.ID
				}
			}
			require.NotEmpty(t, publisherID)
			started := time.Now()
			outcome, err := f.client.KickSessionIfMatch(context.Background(), identity.BootNonce, events[0].ID)
			require.NoError(t, err)
			require.Equal(t, zlm.KickShutdownScheduled, outcome)
			select {
			case <-players[0].done:
			case <-time.After(5*time.Second - time.Since(started)):
				t.Fatal("A did not disconnect within five seconds")
			}
			elapsed := time.Since(started)
			require.Eventually(t, func() bool {
				current, err := f.client.GetRuntimeMediaPlayers(context.Background(), probeTarget(stream))
				if err != nil || current.BootNonce != identity.BootNonce || len(current.Players) != 2 {
					return false
				}
				remaining := map[string]bool{}
				for _, player := range current.Players {
					remaining[player.Identifier] = true
				}
				return !remaining[events[0].ID] && remaining[events[1].ID] && remaining[events[2].ID]
			}, 5*time.Second-time.Since(started), 20*time.Millisecond, "same-boot fresh absence must be confirmed")
			// Observe the original sockets continuously; this test contains no player
			// reconnect/retry path. Thirty seconds also exposes shared-stream teardown.
			for second := 0; second < 30; second++ {
				beforeB, beforeU := players[1].bytes.Load(), players[2].bytes.Load()
				time.Sleep(time.Second)
				for _, p := range players[1:] {
					select {
					case <-p.done:
						t.Fatal("unrelated original player disconnected")
					default:
					}
				}
				require.Greater(t, players[1].bytes.Load(), beforeB)
				require.Greater(t, players[2].bytes.Load(), beforeU)
			}
			require.Equal(t, generation, f.generation(stream), "source createStamp must not change")
			after, err := f.client.GetRuntimeSessions(context.Background())
			require.NoError(t, err)
			publisherStillPresent := false
			for _, session := range after.Sessions {
				if session.ID == publisherID {
					publisherStillPresent = true
				}
			}
			require.True(t, publisherStillPresent, "same original publisher connection required")
			require.Eventually(t, func() bool {
				event, ok := f.event("/flow", protocol+"-A")
				return ok && event.ID == events[0].ID && event.Boot == identity.BootNonce
			}, time.Second, 20*time.Millisecond)
			t.Logf("%s admitted fixture topology: Hook schema=%s; player-list schema=rtmp; A EOF=%s; B/U original sockets + publisher continuous 30s; source generation unchanged", protocol, events[0].Schema, elapsed)
		})
	}
	// A new API adapter is not a new media runtime. Only actual process exit and
	// restart may replace the nonce; no Hook count or local process ID is used.
	recreated := f.newControl()
	same, err := recreated.GetRuntimeIdentity(context.Background())
	require.NoError(t, err)
	require.Equal(t, identity, same)
	f.stop()
	f.start()
	next, err := recreated.GetRuntimeIdentity(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, identity.BootNonce, next.BootNonce)
	result, err := recreated.KickSessionIfMatch(context.Background(), identity.BootNonce, "1-9")
	require.NoError(t, err)
	require.Equal(t, zlm.KickRuntimeMismatch, result)
	t.Log("real old-process exit confirmed by Wait; new boot differs; old-boot conditional kick refused. Proxy/HTTP2/UDP topologies remain unadmitted, not inferred from direct TLS success")
}
