//go:build openapi_live

package integration

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// Dedicated processes, generated credentials and loopback listeners only.
// These are three independent fixture authorizations, not product OpenAPI
// clients; the product/real-device acceptance remains T17.
type mediaProbeFixture struct {
	t                          *testing.T
	dir, binary, secret        string
	apiPort, tlsPort, rtmpPort int
	tlsConfig                  *tls.Config
	client                     *zlm.OpenAPIRuntimeControl
	controlPin                 [32]byte
	process                    *exec.Cmd
	processDone                chan error
	hooks                      *httptest.Server
	mu                         sync.Mutex
	tokens                     map[string]string
	delays                     map[string]<-chan struct{}
	events                     []probeEvent
}
type probeEvent struct {
	Kind, Label, ID, Boot, Schema, VHost, App, Stream string
	Protocol                                          string
}

func newMediaProbeFixture(t *testing.T) *mediaProbeFixture {
	t.Helper()
	binary := os.Getenv("UVP_OPENAPI_TEST_ZLM_BINARY")
	if binary == "" {
		if os.Getenv("UVP_OPENAPI_INTEGRATION_REQUIRED") == "1" {
			t.Fatal("dedicated MediaServer binary is required")
		}
		t.Skip("requires a dedicated MediaServer binary; no existing node is contacted")
	}
	require.True(t, filepath.IsAbs(binary), "binary path must be absolute")
	content, err := os.ReadFile(binary)
	require.NoError(t, err)
	sum := sha256.Sum256(content)
	t.Logf("MediaServer sha256=%x; topology=direct loopback TLS/HTTP1.1, no proxy or multiplexing", sum)
	f := &mediaProbeFixture{t: t, dir: t.TempDir(), binary: binary, secret: probeRandom(t), apiPort: probePort(t), tlsPort: probePort(t), rtmpPort: probePort(t), tokens: make(map[string]string)}
	f.hooks = httptest.NewServer(http.HandlerFunc(f.hook))
	t.Cleanup(f.hooks.Close)
	f.writeCertificate()
	config := fmt.Sprintf("[api]\nsecret=%s\napiDebug=0\n[general]\nlisten_ip=127.0.0.1\nmediaServerId=openapi-isolated-probe\nflowThreshold=0\n[http]\nport=%d\nsslport=%d\nallow_ip_range=127.0.0.1\n[rtmp]\nport=%d\nsslport=0\n[rtsp]\nport=0\nsslport=0\n[rtp_proxy]\nport=0\n[rtc]\nport=0\ntcpPort=0\n[srt]\nport=0\n[shell]\nport=0\n[onvif]\nport=0\n[hook]\nenable=1\non_play=%s/play\non_flow_report=%s/flow\n", f.secret, f.apiPort, f.tlsPort, f.rtmpPort, f.hooks.URL, f.hooks.URL)
	require.NoError(t, os.WriteFile(filepath.Join(f.dir, "probe.ini"), []byte(config), 0600))
	f.client = f.newControl()
	t.Cleanup(f.stop)
	f.start()
	return f
}

func (f *mediaProbeFixture) newControl() *zlm.OpenAPIRuntimeControl {
	control, err := zlm.NewOpenAPIRuntimeControl(node.Node{ID: 1, Revision: 1, MediaServerUUID: "openapi-isolated-probe", APISecret: f.secret}, zlm.OpenAPIControlTLS{
		Endpoint: fmt.Sprintf("https://127.0.0.1:%d/index/api", f.tlsPort), Roots: f.tlsConfig.RootCAs, SPKISHA256: f.controlPin,
	})
	require.NoError(f.t, err)
	f.t.Cleanup(control.Close)
	return control
}

func probeRandom(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return hex.EncodeToString(b)
}
func probePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}
func (f *mediaProbeFixture) writeCertificate() {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(f.t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(f.t, err)
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "isolated-loopback"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	require.NoError(f.t, err)
	parsed, err := x509.ParseCertificate(der)
	require.NoError(f.t, err)
	f.controlPin = sha256.Sum256(parsed.RawSubjectPublicKeyInfo)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(f.t, err)
	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	combined := append(append([]byte{}, pemCert...), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})...)
	require.NoError(f.t, os.WriteFile(filepath.Join(f.dir, "server.pem"), combined, 0600))
	pool := x509.NewCertPool()
	require.True(f.t, pool.AppendCertsFromPEM(pemCert))
	f.tlsConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}
}
func (f *mediaProbeFixture) start() {
	f.process = exec.Command(f.binary, "-c", filepath.Join(f.dir, "probe.ini"), "-s", filepath.Join(f.dir, "server.pem"), "--threads", "2", "--log-dir", f.dir)
	f.process.Dir = f.dir
	f.process.Stdout, f.process.Stderr = io.Discard, io.Discard
	require.NoError(f.t, f.process.Start())
	f.processDone = make(chan error, 1)
	go func(cmd *exec.Cmd, done chan error) { done <- cmd.Wait(); close(done) }(f.process, f.processDone)
	require.Eventually(f.t, func() bool {
		select {
		case <-f.processDone:
			return false
		default:
		}
		_, err := f.client.GetRuntimeIdentity(context.Background())
		return err == nil
	}, 12*time.Second, 50*time.Millisecond, "isolated process must become ready")
}
func (f *mediaProbeFixture) stop() {
	if f.process == nil {
		return
	}
	select {
	case <-f.processDone:
		f.process = nil
		return
	default:
	}
	_ = f.process.Process.Signal(os.Interrupt)
	select {
	case <-f.processDone:
	case <-time.After(5 * time.Second):
		_ = f.process.Process.Kill()
		<-f.processDone
	}
	f.process = nil
}
func (f *mediaProbeFixture) publish(t *testing.T, stream string) {
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-re", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10", "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-g", "10", "-f", "flv", fmt.Sprintf("rtmp://127.0.0.1:%d/live/%s", f.rtmpPort, stream))
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	require.NoError(f.t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	require.Eventually(f.t, func() bool {
		_, err := f.client.GetRuntimeMediaPlayers(context.Background(), probeTarget(stream))
		return err == nil
	}, 10*time.Second, 50*time.Millisecond)
}
func probeTarget(stream string) zlm.StreamTarget {
	return zlm.StreamTarget{Schema: "rtmp", VHost: "__defaultVhost__", App: "live", Stream: stream}
}
func (f *mediaProbeFixture) authorize(label string) string {
	token := probeRandom(f.t)
	f.mu.Lock()
	f.tokens[token] = label
	f.mu.Unlock()
	return token
}
func (f *mediaProbeFixture) hook(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Boot     string `json:"bootNonce"`
		Params   string `json:"params"`
		Schema   string `json:"schema"`
		VHost    string `json:"vhost"`
		App      string `json:"app"`
		Stream   string `json:"stream"`
		Protocol string `json:"protocol"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&body) != nil {
		w.WriteHeader(400)
		return
	}
	params, _ := url.ParseQuery(body.Params)
	f.mu.Lock()
	label := f.tokens[params.Get("probe_auth")]
	delay := f.delays[label]
	allowed := label != "" && body.ID != "" && len(body.Boot) == 32 && body.App == "live"
	if allowed {
		f.events = append(f.events, probeEvent{Kind: r.URL.Path, Label: label, ID: body.ID, Boot: body.Boot, Schema: body.Schema, VHost: body.VHost, App: body.App, Stream: body.Stream, Protocol: body.Protocol})
	}
	f.mu.Unlock()
	if allowed && r.URL.Path == "/play" && delay != nil {
		select {
		case <-delay:
		case <-r.Context().Done():
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if allowed {
		_, _ = io.WriteString(w, `{"code":0}`)
	} else {
		_, _ = io.WriteString(w, `{"code":-1,"msg":"fixture authorization denied"}`)
	}
}
func (f *mediaProbeFixture) event(kind, label string) (probeEvent, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, event := range f.events {
		if event.Kind == kind && event.Label == label {
			return event, true
		}
	}
	return probeEvent{}, false
}
func (f *mediaProbeFixture) generation(stream string) int64 {
	// Read-only fixture observation; never used by product authority decisions.
	request, err := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(f.apiPort)+"/index/api/getMediaInfo?schema=rtmp&vhost=__defaultVhost__&app=live&stream="+url.QueryEscape(stream), nil)
	require.NoError(f.t, err)
	request.Header.Set("secret", f.secret)
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	require.NoError(f.t, err)
	defer response.Body.Close()
	var data struct {
		Code        int   `json:"code"`
		CreateStamp int64 `json:"createStamp"`
	}
	require.NoError(f.t, json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&data))
	require.Zero(f.t, data.Code)
	require.Positive(f.t, data.CreateStamp)
	return data.CreateStamp
}
