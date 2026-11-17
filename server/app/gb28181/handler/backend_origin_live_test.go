//go:build video_share_live

package handler_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/backendorigin"
	"uvplatform.com/uvp-gb28181/app/gb28181/handler"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/stream"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

// Opt-in isolated process test. It never contacts a business node. Lifecycle and
// device authority are fixtures; Registry, hook capability middleware, OnPlay,
// AuthorizationService, signer, media HTTP and RTMP are production components.
func TestBackendOriginStandardZLMProcess(t *testing.T) {
	binary, ffmpeg, sample := os.Getenv("VIDEO_SHARE_PROBE_ZLM"), os.Getenv("VIDEO_SHARE_PROBE_FFMPEG"), os.Getenv("VIDEO_SHARE_PROBE_SAMPLE")
	if binary == "" || ffmpeg == "" || sample == "" {
		t.Skip("requires dedicated packaged executables and synthetic media")
	}
	setHookPlayAuth(t, true, true)
	port, rtmpPort := originLivePort(t), originLivePort(t)
	n := &node.Node{ID: 1, Revision: 1, Host: "127.0.0.1", APIPort: port, APISecret: "isolated-origin-proof-secret", MediaServerUUID: "video-share-origin-live", State: node.StateActive}
	signer, err := playauth.NewSigner(bytes.Repeat([]byte{0x42}, 32))
	require.NoError(t, err)
	authorization := newHookAuthorization(t, signer, time.Now, newHookDeviceAuthority(1))
	binding := playauth.Binding{DeviceID: "34020000002000000001", ChannelID: "34020000001320000001", DeviceEpoch: 1, App: "rtp", Stream: "live", MediaServerID: n.MediaServerUUID, MediaGeneration: 1, BindClientIP: true}
	origins := backendorigin.New(4)
	defer origins.Close()
	h := handler.NewHookController(stream.NewNotifier())
	h.SetBackendOriginObserver(origins)
	h.SetPlayAuthorizer(authorization)
	h.SetPlaybackMediaContextResolver(hookMediaResolver{binding: binding})
	auth := handler.NewHookAuthenticator()
	auth.SetResolver(hookAuthResolver{nodes: map[string]*node.Node{n.MediaServerUUID: n}})
	engine := gin.New()
	engine.POST("/on_play", auth.Middleware(playauth.HookOnPlay, handler.HookRejectAdmission), h.OnPlay)
	hook := httptest.NewServer(engine)
	defer hook.Close()
	capability, err := playauth.HookCapability(n.APISecret, n.MediaServerUUID, playauth.HookOnPlay)
	require.NoError(t, err)
	hookURL := hook.URL + "/on_play?" + url.Values{"node": {n.MediaServerUUID}, "cap": {capability}}.Encode()
	root := t.TempDir()
	config := fmt.Sprintf(`[general]
listen_ip=127.0.0.1
mediaServerId=%s
[api]
secret=%s
[http]
port=%d
sslport=0
rootPath=%s/www
dirMenu=0
forwarded_ip_header=
[rtmp]
port=%d
sslport=0
[rtsp]
port=0
sslport=0
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
[hook]
enable=1
on_play=%s
[protocol]
enable_hls=0
enable_mp4=0
enable_rtsp=0
enable_rtmp=1
`, n.MediaServerUUID, n.APISecret, port, root, rtmpPort, hookURL)
	ini := filepath.Join(root, "zlm.ini")
	require.NoError(t, os.WriteFile(ini, []byte(config), 0600))
	originLiveLaunch(t, exec.Command(binary, "-c", ini, "-l", "3", "-t", "1", "--log-dir", filepath.Join(root, "logs")))
	originLiveWait(t, port)
	originLiveWait(t, rtmpPort)
	originLiveLaunch(t, exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-re", "-stream_loop", "-1", "-i", sample, "-c", "copy", "-f", "flv", fmt.Sprintf("rtmp://127.0.0.1:%d/rtp/live", rtmpPort)))
	time.Sleep(750 * time.Millisecond)
	client := zlm.NewClientForNode(n)
	initial, err := client.GetServerConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, n.MediaServerUUID, initial["general.mediaServerId"])
	fingerprint := func(config map[string]string) string {
		encoded, _ := json.Marshal(config)
		sum := sha256.Sum256(encoded)
		return hex.EncodeToString(sum[:])
	}
	target := backendorigin.Target{NodeID: 1, UUID: n.MediaServerUUID, Revision: 1, MediaGeneration: 1, EndpointFingerprint: fingerprint(initial), App: "rtp", Stream: "live"}
	validate := func(ctx context.Context, expected backendorigin.Target) error {
		actual, err := client.GetServerConfig(ctx)
		if err != nil || fingerprint(actual) != expected.EndpointFingerprint {
			return backendorigin.ErrUnavailable
		}
		return nil
	}
	raw := fmt.Sprintf("http://127.0.0.1:%d/rtp/live.live.flv", port)
	observed, err := origins.Prove(context.Background(), target, raw, validate)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", observed)
	// Prove succeeds only on a 401 response. It therefore cannot silently grant
	// access to synthetic media while discovering the origin address.
	check := func(ip string, allowed bool) {
		t.Helper()
		expected := binding
		expected.ClientIP = ip
		grant, err := authorization.IssueDirect(expected)
		require.NoError(t, err)
		response, err := (&http.Client{Timeout: 4 * time.Second}).Get(raw + "?play_token=" + url.QueryEscape(grant.Token))
		require.NoError(t, err)
		defer response.Body.Close()
		header := make([]byte, 3)
		_, readErr := io.ReadFull(response.Body, header)
		if allowed {
			require.Equal(t, 200, response.StatusCode)
			require.NoError(t, readErr)
			require.Equal(t, "FLV", string(header))
		} else {
			require.Equal(t, 401, response.StatusCode)
			require.NotEqual(t, "FLV", string(header))
		}
	}
	check(observed, true)
	check("203.0.113.42", false)
	t.Log("standard isolated ZLM: authenticated on_play + production nonce Registry => denied probe and observed 127.0.0.1; production IP-bound authorization serves FLV, wrong IP rejected")
}

func originLivePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}
func originLiveWait(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("isolated media listener failed to start")
}
func originLiveLaunch(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	log, err := os.CreateTemp(t.TempDir(), "process-*.log")
	require.NoError(t, err)
	cmd.Stdout, cmd.Stderr = log, log
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		_ = log.Close()
		data, _ := os.ReadFile(log.Name())
		if t.Failed() {
			t.Log(strings.TrimSpace(string(data)))
		}
	})
}
