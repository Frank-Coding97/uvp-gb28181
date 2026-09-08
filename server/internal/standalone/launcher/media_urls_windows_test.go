//go:build windows

package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

const (
	mediaURLsRootEnv         = "UVP_MEDIA_URLS_ROOT"
	mediaURLsPrivateOutEnv   = "UVP_MEDIA_URLS_PRIVATE_OUT"
	mediaURLsContinueFileEnv = "UVP_MEDIA_URLS_CONTINUE"
	mediaURLsExternalTimeout = 2 * time.Minute
)

type mediaURLsNode struct {
	ID           int64  `json:"id"`
	Host         string `json:"host"`
	PlaybackHost string `json:"playbackHost"`
}

type mediaURLsPlayResult struct {
	StreamID string `json:"streamId"`
	Node     *struct {
		ID int64 `json:"id"`
	} `json:"node"`
	URLs struct {
		WSFlvURL   string `json:"wsFlv"`
		HTTPFlvURL string `json:"httpFlv"`
		WebRTCURL  string `json:"webrtc"`
	} `json:"urls"`
}

type mediaURLsWebRTCObservation struct {
	OK          bool    `json:"ok"`
	Reason      string  `json:"reason"`
	VideoWidth  int     `json:"videoWidth"`
	CurrentTime float64 `json:"currentTime"`
}

// TestWindowsStandaloneMediaURLsAndWebRTC is an opt-in acceptance test for a
// completed t18 Windows fixture. The external device simulator is owned by
// the caller; this test owns only the launcher, one play request, and Edge.
// HTTP-FLV and WS-FLV are checked for the configured playback host. WebRTC is
// exercised through a real Edge RTCPeerConnection and only passes after a
// decoded video track has non-zero dimensions and advances past two seconds.
func TestWindowsStandaloneMediaURLsAndWebRTC(t *testing.T) {
	root := strings.TrimSpace(os.Getenv(mediaURLsRootEnv))
	if root == "" {
		t.Skip("requires UVP_MEDIA_URLS_ROOT for an isolated completed t18 fixture")
	}
	privatePath := strings.TrimSpace(os.Getenv(corePrivateFileEnv))
	if privatePath == "" {
		t.Fatal("requires UVP_CORE_PRIVATE_FILE for private login credentials")
	}
	var credentials corePrivateCredentials
	raw, err := os.ReadFile(privatePath)
	if err != nil || json.Unmarshal(raw, &credentials) != nil || strings.TrimSpace(credentials.Username) == "" || credentials.Password == "" {
		t.Fatal("private login unavailable")
	}
	release, err := standalone.LoadRelease(root)
	if err != nil || release.Version != "t18-setup" {
		t.Fatal("media URL test requires the isolated t18-setup release")
	}

	browserURLs := make(chan string, 1)
	run := t18Start(t, root, browserURLs)
	defer func() {
		run.cancel()
		if !t18WaitFinished(t, run, 90*time.Second) {
			t.Error("owned launcher cleanup exceeded its deadline")
		}
	}()
	entry, ok := t18WaitBrowserEntry(run, browserURLs, 90*time.Second)
	if !ok {
		t.Fatal("owned launcher did not publish a browser entry")
	}
	baseURL, origin, _, ok := t18BrowserEndpoint(entry, false)
	if !ok {
		t.Fatal("completed fixture did not publish a valid loopback browser entry")
	}
	t19WaitBusinessReady(t, run)

	client := t18HTTPClient{client: t18HTTPTransport(), baseURL: baseURL, origin: origin}
	loginBody := t18AdminBody(t, credentials.Username, credentials.Password)
	status, headers, body := client.request(t, http.MethodPost, "/api/login", "", loginBody)
	t18AssertResponseSafe(t, headers, body, "", credentials.Password)
	accessToken, ok := coreAccessToken(body)
	if status != http.StatusOK || !ok {
		t.Fatal("existing administrator login failed")
	}
	client.accessToken = accessToken

	deviceID, channelID := mediaURLsFindChannel(t, &client, 5*time.Minute)
	nodes := mediaURLsReadNodes(t, &client)
	playPath := "/api/gb28181/play/" + url.PathEscape(deviceID) + "/" + url.PathEscape(channelID)
	status, headers, body = client.request(t, http.MethodPost, playPath, "", nil)
	t18AssertResponseSafe(t, headers, body, accessToken, credentials.Password)
	var playResult mediaURLsPlayResult
	if status != http.StatusOK || !coreResponseData(body, &playResult) {
		t.Fatal("real conventional device play request failed")
	}
	if playResult.StreamID == "" {
		t.Fatal("real play response did not contain a stream ID")
	}
	defer func() {
		stopPath := "/api/gb28181/play/" + url.PathEscape(playResult.StreamID)
		stopStatus, stopHeaders, stopBody := client.request(t, http.MethodDelete, stopPath, "", nil)
		t18AssertResponseSafe(t, stopHeaders, stopBody, accessToken, credentials.Password)
		if stopStatus != http.StatusOK {
			t.Errorf("owned play cleanup returned HTTP %d", stopStatus)
		}
	}()

	node := mediaURLsNodeForResult(t, nodes, playResult.Node)
	playbackHost := strings.TrimSpace(node.PlaybackHost)
	if mediaURLsForbiddenHost(playbackHost) {
		t.Fatalf("configured PlaybackHost is a management or loopback address: %q", playbackHost)
	}
	mediaURLsAssertHost(t, "httpFlv", playResult.URLs.HTTPFlvURL, playbackHost, "http", "https")
	mediaURLsAssertHost(t, "wsFlv", playResult.URLs.WSFlvURL, playbackHost, "ws", "wss")
	mediaURLsAssertHost(t, "webrtc", playResult.URLs.WebRTCURL, playbackHost, "http", "https")

	browserContext, cancelBrowser := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancelBrowser()
	browser, err := t18StartHeadlessEdge(browserContext)
	if err != nil {
		t.Fatal("could not start the owned Edge CDP browser")
	}
	defer func() {
		if !browser.close() {
			t.Error("owned Edge cleanup exceeded its deadline")
		}
	}()
	if err := browser.navigate(browserContext, baseURL); err != nil {
		t.Fatal("could not open the installed loopback origin in Edge")
	}
	if err := t19WaitEval(browserContext, browser.cdp, `document.readyState === 'complete'`); err != nil {
		t.Fatal("installed loopback origin did not become ready in Edge")
	}

	webrtcContext, cancelWebRTC := context.WithTimeout(browserContext, 120*time.Second)
	defer cancelWebRTC()
	observation, err := mediaURLsVerifyWebRTC(webrtcContext, browser.cdp, playResult.URLs.WebRTCURL)
	if err != nil {
		t.Fatal("Edge WebRTC evaluation was unavailable")
	}
	if !observation.OK {
		t.Fatalf("real WebRTC playback failed: %s", observation.Reason)
	}
	if observation.VideoWidth <= 0 || observation.CurrentTime <= 2 {
		t.Fatalf("WebRTC did not provide decoded video: width=%d currentTime=%.3f", observation.VideoWidth, observation.CurrentTime)
	}
	t.Logf("MEDIA_URLS_HOSTS_CONFIRMED playbackHost=%s; WEBRTC_VIDEO_DECODED width=%d currentTime=%.3f", playbackHost, observation.VideoWidth, observation.CurrentTime)
	mediaURLsMaybeWaitExternal(t, playResult, privatePath)
}

func mediaURLsMaybeWaitExternal(t *testing.T, result mediaURLsPlayResult, privatePath string) {
	t.Helper()
	outputPath := strings.TrimSpace(os.Getenv(mediaURLsPrivateOutEnv))
	if outputPath == "" {
		return
	}
	continuePath := strings.TrimSpace(os.Getenv(mediaURLsContinueFileEnv))
	if continuePath == "" {
		t.Fatal("UVP_MEDIA_URLS_CONTINUE is required when UVP_MEDIA_URLS_PRIVATE_OUT is set")
	}
	if _, err := os.Stat(continuePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("external media acceptance requires a fresh continue marker path")
	}
	if strings.EqualFold(filepath.Clean(outputPath), filepath.Clean(privatePath)) || strings.EqualFold(filepath.Clean(outputPath), filepath.Clean(continuePath)) {
		t.Fatal("external URL output path must be distinct from credentials and continue marker")
	}
	snapshot := struct {
		HTTPFlv string `json:"httpFlv"`
		WSFlv   string `json:"wsFlv"`
		WebRTC  string `json:"webrtc"`
	}{
		HTTPFlv: result.URLs.HTTPFlvURL,
		WSFlv:   result.URLs.WSFlvURL,
		WebRTC:  result.URLs.WebRTCURL,
	}
	if err := coreWriteJSONFile(outputPath, snapshot); err != nil {
		t.Fatal("could not write the private external media URL file")
	}
	if err := os.Chmod(outputPath, 0o600); err != nil {
		t.Fatal("could not secure the private external media URL file")
	}
	if !coreWaitForFile(continuePath, mediaURLsExternalTimeout) {
		t.Fatal("external media URL acceptance marker was not observed within two minutes")
	}
}

func mediaURLsFindChannel(t *testing.T, client *t18HTTPClient, timeout time.Duration) (string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		status, _, body, err := client.coreRequest(ctx, http.MethodGet, "/api/gb28181/device/list?page=1&pageSize=200", nil)
		if err == nil {
			if status == http.StatusUnauthorized || status == http.StatusForbidden {
				t.Fatal("device list authorization was rejected")
			}
			if status == http.StatusNotFound {
				t.Fatal("device list route was not found")
			}
			if status == http.StatusOK {
				var devices struct {
					List []coreDevice `json:"list"`
				}
				if !coreResponseData(body, &devices) {
					t.Fatal("device list response was invalid")
				}
				for _, device := range devices.List {
					if !device.Online || device.DeviceID == "" {
						continue
					}
					channelPath := "/api/gb28181/device/" + url.PathEscape(device.DeviceID) + "/channels"
					channelStatus, _, channelBody, channelErr := client.coreRequest(ctx, http.MethodGet, channelPath, nil)
					if channelErr != nil {
						continue
					}
					if channelStatus == http.StatusUnauthorized || channelStatus == http.StatusForbidden {
						t.Fatal("channel list authorization was rejected")
					}
					if channelStatus == http.StatusNotFound {
						t.Fatal("channel list route was not found")
					}
					if channelStatus != http.StatusOK {
						continue
					}
					var channels struct {
						List []coreChannel `json:"list"`
					}
					if !coreResponseData(channelBody, &channels) {
						t.Fatal("channel list response was invalid")
					}
					for _, channel := range channels.List {
						if channel.ChannelID == "" || channel.DeviceID != "" && channel.DeviceID != device.DeviceID {
							continue
						}
						return device.DeviceID, channel.ChannelID
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("no online device channel became available")
		case <-ticker.C:
		}
	}
}

func mediaURLsReadNodes(t *testing.T, client *t18HTTPClient) []mediaURLsNode {
	t.Helper()
	status, headers, body := client.request(t, http.MethodGet, "/api/gb28181/zlm/nodes", "", nil)
	t18AssertResponseSafe(t, headers, body, client.accessToken, "")
	if status != http.StatusOK {
		t.Fatalf("media node list returned HTTP %d", status)
	}
	var response struct {
		List []mediaURLsNode `json:"list"`
	}
	if !coreResponseData(body, &response) || len(response.List) == 0 {
		t.Fatal("media node list did not contain an active node")
	}
	return response.List
}

func mediaURLsNodeForResult(t *testing.T, nodes []mediaURLsNode, resultNode *struct {
	ID int64 `json:"id"`
}) mediaURLsNode {
	t.Helper()
	if resultNode != nil && resultNode.ID != 0 {
		for _, node := range nodes {
			if node.ID == resultNode.ID {
				return node
			}
		}
		t.Fatal("play result node was missing from the configured media node list")
	}
	if len(nodes) == 1 {
		return nodes[0]
	}
	t.Fatal("play result did not identify one configured media node")
	return mediaURLsNode{}
}

func mediaURLsAssertHost(t *testing.T, label, rawURL, expectedHost string, schemes ...string) {
	t.Helper()
	if strings.TrimSpace(rawURL) == "" {
		t.Fatalf("%s URL was missing from the real play response", label)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		t.Fatalf("%s URL was invalid", label)
	}
	allowedScheme := false
	for _, scheme := range schemes {
		if strings.EqualFold(parsed.Scheme, scheme) {
			allowedScheme = true
			break
		}
	}
	if !allowedScheme {
		t.Fatalf("%s URL used an unexpected scheme", label)
	}
	if parsed.Port() == "" {
		t.Fatalf("%s URL did not include a playback port", label)
	}
	host := strings.TrimSuffix(strings.TrimSpace(parsed.Hostname()), ".")
	expected := strings.TrimSuffix(strings.TrimSpace(expectedHost), ".")
	if mediaURLsForbiddenHost(host) {
		t.Fatalf("%s URL used localhost or a loopback management host", label)
	}
	if !strings.EqualFold(host, expected) {
		t.Fatalf("%s URL host did not match configured PlaybackHost: got %q want %q", label, host, expected)
	}
}

func mediaURLsForbiddenHost(host string) bool {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func mediaURLsVerifyWebRTC(ctx context.Context, cdp *t18CDP, rawURL string) (mediaURLsWebRTCObservation, error) {
	encodedURL, err := json.Marshal(rawURL)
	if err != nil {
		return mediaURLsWebRTCObservation{}, errors.New("encode WebRTC URL for browser")
	}
	expression := "(async () => {" +
		"let pc = null; let video = null; let stage = 'create';" +
		"const failed = reason => ({ok:false, reason});" +
		"try {" +
		"const source = " + string(encodedURL) + ";" +
		"video = document.createElement('video');" +
		"video.muted = true; video.autoplay = true; video.playsInline = true;" +
		"video.setAttribute('muted', ''); video.setAttribute('autoplay', ''); video.setAttribute('playsinline', '');" +
		"document.body.appendChild(video);" +
		"pc = new RTCPeerConnection();" +
		"pc.addTransceiver('video', {direction:'recvonly'});" +
		"const trackReady = new Promise((resolve, reject) => {" +
		"const timer = window.setTimeout(() => reject(new Error('track_timeout')), 90000);" +
		"pc.ontrack = event => {" +
		"if (!event.track || event.track.kind !== 'video') return;" +
		"window.clearTimeout(timer);" +
		"const stream = event.streams && event.streams[0] ? event.streams[0] : new MediaStream([event.track]);" +
		"video.srcObject = stream;" +
		"const playback = video.play(); if (playback && typeof playback.catch === 'function') playback.catch(() => {});" +
		"resolve();};" +
		"});" +
		"const offer = await pc.createOffer();" +
		"await pc.setLocalDescription(offer);" +
		"stage = 'ice';" +
		"if (pc.iceGatheringState !== 'complete') await new Promise((resolve, reject) => {" +
		"const timer = window.setTimeout(() => reject(new Error('ice_timeout')), 15000);" +
		"const done = () => { if (pc.iceGatheringState !== 'complete') return; window.clearTimeout(timer); pc.removeEventListener('icegatheringstatechange', done); resolve(); };" +
		"pc.addEventListener('icegatheringstatechange', done); done();" +
		"});" +
		"const offerSDP = pc.localDescription && pc.localDescription.sdp;" +
		"if (!offerSDP) return failed('webrtc_offer_missing');" +
		"stage = 'fetch';" +
		"let response;" +
		"try { response = await fetch(source, {method:'POST', headers:{'Content-Type':'text/plain;charset=utf-8'}, body:offerSDP}); }" +
		"catch (_) { return failed('webrtc_fetch_cors_or_network_error'); }" +
		"if (!response.ok) return failed('webrtc_http_error');" +
		"stage = 'answer';" +
		"let answer;" +
		"try { answer = await response.json(); } catch (_) { return failed('webrtc_answer_not_json'); }" +
		"if (!answer || answer.code !== 0 || typeof answer.sdp !== 'string' || !answer.sdp) return failed('webrtc_answer_rejected_or_missing');" +
		"await pc.setRemoteDescription({type:'answer', sdp:answer.sdp});" +
		"stage = 'track';" +
		"await trackReady;" +
		"stage = 'decode';" +
		"const deadline = performance.now() + 90000;" +
		"while (!(video.videoWidth > 0 && video.currentTime > 2)) {" +
		"if (performance.now() >= deadline) return failed('webrtc_video_decode_timeout');" +
		"await new Promise(resolve => window.setTimeout(resolve, 250));" +
		"}" +
		"return {ok:true, reason:'', videoWidth:video.videoWidth, currentTime:video.currentTime};" +
		"} catch (_) { return failed(stage === 'fetch' ? 'webrtc_fetch_cors_or_network_error' : 'webrtc_browser_exchange_error'); }" +
		"finally { if (pc) { try { pc.close(); } catch (_) {} } if (video) { try { video.pause(); video.srcObject = null; video.remove(); } catch (_) {} } }" +
		"})()"
	result, err := cdp.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"awaitPromise":  true,
		"returnByValue": true,
		"userGesture":   true,
	})
	if err != nil {
		return mediaURLsWebRTCObservation{}, errors.New("browser WebRTC evaluation failed")
	}
	var envelope struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if json.Unmarshal(result, &envelope) != nil || len(envelope.ExceptionDetails) != 0 && string(envelope.ExceptionDetails) != "null" {
		return mediaURLsWebRTCObservation{}, errors.New("browser WebRTC evaluation returned an exception")
	}
	var observation mediaURLsWebRTCObservation
	if json.Unmarshal(envelope.Result.Value, &observation) != nil {
		return mediaURLsWebRTCObservation{}, errors.New("browser WebRTC evaluation returned an invalid result")
	}
	return observation, nil
}
