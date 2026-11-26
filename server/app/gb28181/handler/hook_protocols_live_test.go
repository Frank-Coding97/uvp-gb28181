//go:build openapi_live

package handler_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"
	"uvplatform.com/uvp-gb28181/app/gb28181/handler"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/stream"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

// This opt-in test starts its own loopback-only ZLM and synthetic publisher.
// It uses production token verification and authenticated hook handlers; only
// durable viewer bookkeeping/device authority are in-memory fixtures. RTC is
// deliberately outside this matrix: successful SDP is not proof of media.
func TestOpenAPIPlaybackProtocolMediaIsolated(t *testing.T) {
	binary := os.Getenv("UVP_OPENAPI_TEST_ZLM_BINARY")
	if binary == "" {
		t.Skip("requires UVP_OPENAPI_TEST_ZLM_BINARY; no existing service is contacted")
	}
	require.True(t, filepath.IsAbs(binary), "dedicated ZLM binary must use an absolute path")
	ffmpeg := os.Getenv("UVP_OPENAPI_TEST_FFMPEG_BINARY")
	if ffmpeg == "" {
		var err error
		ffmpeg, err = exec.LookPath("ffmpeg")
		require.NoError(t, err)
	}
	setHookPlayAuth(t, true, false)
	root := t.TempDir()
	ports := make([]int, 6)
	for i := range ports {
		ports[i] = protocolLivePort(t)
	}
	certificate, roots := protocolLiveCertificate(t, root)
	signer, err := playauth.NewSigner(bytes.Repeat([]byte{0x46}, 32))
	require.NoError(t, err)
	authority := newHookAuthorization(t, signer, time.Now, newHookDeviceAuthority(1))
	binding := playauth.Binding{DeviceID: "34020000002000000001", ChannelID: "34020000001320000001", DeviceEpoch: 1, App: "rtp", Stream: "matrix", MediaServerID: "isolated-openapi-matrix", MediaGeneration: 1}
	lifecycle := &protocolLiveLifecycle{bindings: make(map[string][]playauth.OpenAPIViewerBinding), closes: make(map[string]playauth.OpenAPIFlowReport)}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayAuthorizer(authority)
	h.SetPlaybackMediaContextResolver(hookMediaResolver{binding: binding})
	h.SetOpenAPIViewerBinder(lifecycle)
	h.SetOpenAPIFlowObserver(lifecycle)
	h.SetOpenAPIRuntimeIdentityResolver(quotaHookRuntimeIdentity{bootNonce: strings.Repeat("a", 32)})
	n := &node.Node{ID: 1, Revision: 1, Host: "127.0.0.1", APIPort: ports[0], APISecret: uuid.NewString(), MediaServerUUID: binding.MediaServerID, State: node.StateActive}
	authenticator := handler.NewHookAuthenticator()
	authenticator.SetResolver(hookAuthResolver{nodes: map[string]*node.Node{n.MediaServerUUID: n}})
	engine := gin.New()
	engine.POST("/play", authenticator.Middleware(playauth.HookOnPlay, handler.HookRejectAdmission), h.OnPlay)
	engine.POST("/flow", authenticator.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), h.OnFlowReport)
	hooks := httptest.NewServer(engine)
	defer hooks.Close()
	hookURL := func(path string, event playauth.HookEvent) string {
		cap, err := playauth.HookCapability(n.APISecret, n.MediaServerUUID, event)
		require.NoError(t, err)
		return hooks.URL + path + "?" + url.Values{"node": {n.MediaServerUUID}, "cap": {cap}}.Encode()
	}
	config := fmt.Sprintf(`[general]
listen_ip=127.0.0.1
mediaServerId=%s
flowThreshold=0
[api]
secret=%s
apiDebug=0
[http]
port=%d
sslport=%d
rootPath=%s/www
dirMenu=0
forwarded_ip_header=
[rtmp]
port=%d
sslport=%d
[rtsp]
port=%d
sslport=%d
[rtc]
port=0
tcpPort=0
signalingPort=0
signalingSslPort=0
icePort=0
iceTcpPort=0
[srt]
port=0
[shell]
port=0
[rtp_proxy]
port=0
[onvif]
port=0
[hook]
enable=1
on_play=%s
on_flow_report=%s
[protocol]
enable_hls=1
enable_mp4=0
enable_rtsp=1
enable_rtmp=1
enable_ts=1
enable_fmp4=1
hls_demand=0
rtsp_demand=0
rtmp_demand=0
ts_demand=0
fmp4_demand=0
[hls]
segDur=1
segNum=3
segKeep=0
`, n.MediaServerUUID, n.APISecret, ports[0], ports[1], root, ports[2], ports[3], ports[4], ports[5], hookURL("/play", playauth.HookOnPlay), hookURL("/flow", playauth.HookOnFlowReport))
	ini := filepath.Join(root, "matrix.ini")
	require.NoError(t, os.WriteFile(ini, []byte(config), 0600))
	protocolLiveLaunch(t, exec.Command(binary, "-c", ini, "-s", certificate, "-l", "3", "--threads", "2", "--log-dir", filepath.Join(root, "logs")))
	for _, port := range ports {
		protocolLiveWait(t, port)
	}
	publishArgs := []string{"-nostdin", "-hide_banner", "-loglevel", "error"}
	if sample := os.Getenv("UVP_OPENAPI_TEST_SAMPLE"); sample != "" {
		publishArgs = append(publishArgs, "-stream_loop", "-1", "-re", "-i", sample, "-c", "copy")
	} else {
		publishArgs = append(publishArgs, "-re", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10", "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-g", "10")
	}
	publishArgs = append(publishArgs, "-f", "flv", fmt.Sprintf("rtmp://127.0.0.1:%d/rtp/matrix", ports[2]))
	publish := exec.Command(ffmpeg, publishArgs...)
	protocolLiveLaunch(t, publish)
	api := zlm.NewClientForNode(n)
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := api.GetMediaPlayerList(ctx, "rtmp", "__defaultVhost__", "rtp", "matrix")
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "dedicated synthetic stream must become ready")
	tlsConfig := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	type protocolCase struct {
		name, scheme, suffix, family string
		port                         int
	}
	cases := []protocolCase{}
	for _, family := range []struct{ name, suffix string }{{"Flv", ".live.flv"}, {"Fmp4", ".live.mp4"}, {"Ts", ".live.ts"}} {
		for _, transport := range []struct {
			prefix, scheme string
			port           int
		}{{"http", "http", ports[0]}, {"https", "https", ports[1]}, {"ws", "ws", ports[0]}, {"wss", "wss", ports[1]}} {
			cases = append(cases, protocolCase{transport.prefix + family.name, transport.scheme, family.suffix, strings.ToLower(family.name), transport.port})
		}
	}
	cases = append(cases, protocolCase{"hls", "http", "/hls.m3u8", "hls", ports[0]}, protocolCase{"httpsHls", "https", "/hls.m3u8", "hls", ports[1]}, protocolCase{"rtmp", "rtmp", "", "rtmp", ports[2]}, protocolCase{"rtmps", "rtmps", "", "rtmp", ports[3]}, protocolCase{"rtsp", "rtsp", "", "rtsp", ports[4]}, protocolCase{"rtsps", "rtsps", "", "rtsp", ports[5]})
	var completed []playauth.OpenAPIViewerBinding
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			media := binding
			media.OpenAPIClientID = 7
			media.OpenAPIGrantID = uuid.NewString()
			grant, err := authority.IssueDirect(media)
			require.NoError(t, err)
			target := fmt.Sprintf("%s://127.0.0.1:%d/rtp/matrix%s", tc.scheme, tc.port, tc.suffix)
			for _, negative := range []struct{ name, token string }{{"missing", ""}, {"invalid", "not-a-valid-token"}} {
				t.Run(negative.name, func(t *testing.T) {
					protocolLiveProbe(t, ffmpeg, certificate, tlsConfig, target, tc.family, negative.token, false)
				})
			}
			protocolLiveProbe(t, ffmpeg, certificate, tlsConfig, target, tc.family, grant.Token, true)
			var observed playauth.OpenAPIViewerBinding
			require.Eventually(t, func() bool {
				lifecycle.mu.Lock()
				defer lifecycle.mu.Unlock()
				rows := lifecycle.bindings[media.OpenAPIGrantID]
				if len(rows) == 0 {
					return false
				}
				observed = rows[0]
				return true
			}, 2*time.Second, 20*time.Millisecond, "media access must pass the production OpenAPI hook")
			require.Equal(t, binding.MediaGeneration, observed.MediaGeneration)
			require.Equal(t, binding.MediaServerID, observed.NodeUUID)
			require.Equal(t, "rtp", observed.App)
			require.Equal(t, "matrix", observed.Stream)
			lifecycle.mu.Lock()
			count := len(lifecycle.bindings[media.OpenAPIGrantID])
			lifecycle.mu.Unlock()
			require.Equal(t, 1, count, "HLS refresh/segments must reuse its admitted cookie viewer")
			completed = append(completed, observed)
		})
	}
	// HLS keeps a viewer until its cookie is idle for 60 seconds. Waiting for
	// all final flow reports also checks that HTTP/RTMP/RTSP identifiers match.
	require.Eventually(t, func() bool {
		lifecycle.mu.Lock()
		defer lifecycle.mu.Unlock()
		for _, viewer := range completed {
			report, ok := lifecycle.closes[viewer.Identifier]
			if !ok || report.Schema != viewer.Schema || report.Protocol != viewer.Protocol || report.Stream != viewer.Stream {
				return false
			}
		}
		return true
	}, 75*time.Second, 100*time.Millisecond, "all admitted viewers must emit a matching production flow callback")
	t.Logf("verified %d independent protocol authorizations; RTC media is not covered by this fixture", len(completed))
}

// Failure messages intentionally omit command lines/URLs (they contain tokens).
func protocolLiveProbe(t *testing.T, ffmpeg, certificate string, tlsConfig *tls.Config, target, family, token string, allowed bool) {
	t.Helper()
	if token != "" {
		target += "?" + url.Values{playauth.QueryParameter: {token}}.Encode()
	}
	parsed, err := url.Parse(target)
	require.NoError(t, err)
	if family == "rtmp" || family == "rtsp" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		args := []string{"-nostdin", "-hide_banner", "-loglevel", "error"}
		if parsed.Scheme == "rtmps" || parsed.Scheme == "rtsps" {
			bridge := protocolLiveTLSBridge(t, parsed.Host, tlsConfig)
			parsed.Host = bridge
			parsed.Scheme = family
			target = parsed.String()
		}
		if family == "rtsp" {
			args = append(args, "-rtsp_transport", "tcp")
		}
		args = append(args, "-i", target, "-t", "0.4", "-map", "0:v:0", "-c", "copy", "-an", "-f", "mpegts", "pipe:1")
		data, runErr := exec.CommandContext(ctx, ffmpeg, args...).Output()
		if allowed {
			require.True(t, runErr == nil, "isolated protocol player must receive media")
			require.Greater(t, len(data), 188)
			require.Equal(t, byte(0x47), data[0])
		} else {
			require.True(t, runErr != nil, "missing or wrong authorization must reject protocol playback")
			require.Empty(t, data)
		}
		return
	}
	if parsed.Scheme == "ws" || parsed.Scheme == "wss" {
		cfg, err := websocket.NewConfig(target, "http://127.0.0.1")
		require.NoError(t, err)
		cfg.TlsConfig = tlsConfig.Clone()
		cfg.Dialer = &net.Dialer{Timeout: 3 * time.Second}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		conn, dialErr := cfg.DialContext(ctx)
		if !allowed && dialErr != nil {
			return
		}
		require.True(t, dialErr == nil, "isolated WebSocket player must connect")
		defer conn.Close()
		require.NoError(t, conn.SetDeadline(time.Now().Add(4*time.Second)))
		data := make([]byte, 512)
		n, readErr := io.ReadFull(conn, data)
		if allowed {
			require.True(t, readErr == nil, "WebSocket upgrade must be followed by actual media")
			protocolLiveMediaHeader(t, family, data[:n])
		} else {
			require.True(t, readErr != nil, "unauthorized WebSocket must not deliver media")
			require.Zero(t, n)
		}
		return
	}
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	transport := &http.Transport{TLSClientConfig: tlsConfig.Clone(), Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Jar: jar, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(target)
	require.True(t, err == nil, "isolated HTTP player must answer")
	if !allowed {
		defer response.Body.Close()
		require.Equal(t, http.StatusUnauthorized, response.StatusCode)
		return
	}
	require.Equal(t, http.StatusOK, response.StatusCode)
	if family != "hls" {
		defer response.Body.Close()
		data := make([]byte, 512)
		_, err = io.ReadFull(response.Body, data)
		require.True(t, err == nil, "HTTP player must deliver actual media")
		protocolLiveMediaHeader(t, family, data)
		return
	}
	// Preserve the cookie on playlist refresh and relative TS requests. A new
	// TCP connection for each request must remain the same logical HLS viewer.
	for round := 0; round < 2; round++ {
		if round > 0 {
			response, err = client.Get(target)
			require.True(t, err == nil, "HLS playlist refresh must answer")
			require.Equal(t, http.StatusOK, response.StatusCode)
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 128<<10))
		response.Body.Close()
		require.NoError(t, readErr)
		require.True(t, strings.HasPrefix(string(raw), "#EXTM3U"), "HLS must return a playlist")
		segment := ""
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				segment = line
				break
			}
		}
		require.NotEmpty(t, segment, "HLS playlist must reference actual media")
		relative, parseErr := url.Parse(segment)
		require.NoError(t, parseErr)
		mediaURL := parsed.ResolveReference(relative)
		require.Equal(t, parsed.Host, mediaURL.Host, "fixture must not follow external media addresses")
		media, mediaErr := client.Get(mediaURL.String())
		require.True(t, mediaErr == nil, "HLS segment must answer")
		require.Equal(t, http.StatusOK, media.StatusCode)
		data := make([]byte, 188)
		_, readErr = io.ReadFull(media.Body, data)
		media.Body.Close()
		require.True(t, readErr == nil, "HLS must deliver an actual TS segment")
		require.Equal(t, byte(0x47), data[0])
	}
}

func protocolLiveMediaHeader(t *testing.T, family string, data []byte) {
	t.Helper()
	switch family {
	case "flv":
		require.Equal(t, "FLV", string(data[:3]))
	case "fmp4":
		require.Equal(t, "ftyp", string(data[4:8]))
	case "ts":
		require.Equal(t, byte(0x47), data[0])
	default:
		t.Fatal("unknown synthetic media family")
	}
}

type protocolLiveLifecycle struct {
	mu       sync.Mutex
	bindings map[string][]playauth.OpenAPIViewerBinding
	closes   map[string]playauth.OpenAPIFlowReport
}

func (l *protocolLiveLifecycle) BindViewer(ctx context.Context, claims playauth.Claims, b playauth.OpenAPIViewerBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bindings[claims.OpenAPIGrantID] = append(l.bindings[claims.OpenAPIGrantID], b)
	return nil
}
func (l *protocolLiveLifecycle) CloseViewer(ctx context.Context, r playauth.OpenAPIFlowReport) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closes[r.Identifier] = r
	return nil
}
func protocolLivePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}
func protocolLiveWait(t *testing.T, port int) {
	t.Helper()
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			conn.Close()
		}
		return err == nil
	}, 6*time.Second, 30*time.Millisecond, "isolated media listener must start")
}
func protocolLiveLaunch(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		select {
		case <-done:
			return
		default:
		}
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
}
func protocolLiveCertificate(t *testing.T, root string) (string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "isolated-loopback"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	data := append(append([]byte{}, certPEM...), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})...)
	path := filepath.Join(root, "server.pem")
	require.NoError(t, os.WriteFile(path, data, 0600))
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(certPEM))
	return path, pool
}

// Packaged ffmpeg intentionally lacks TLS. This loopback bridge validates the
// fixture CA and forwards native RTMP/RTSP bytes over a real TLS ZLM socket.
func protocolLiveTLSBridge(t *testing.T, upstream string, config *tls.Config) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			local, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer local.Close()
				remote, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", upstream, config.Clone())
				if err != nil {
					return
				}
				defer remote.Close()
				remote.SetDeadline(time.Now().Add(10 * time.Second))
				go func() { io.Copy(remote, local); remote.Close() }()
				io.Copy(local, remote)
			}()
		}
	}()
	return listener.Addr().String()
}
