//go:build windows

package launcher

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

const t18SetupTokenFragment = "/standalone-setup?bootstrap_token="

type t18Launch struct {
	cancel   context.CancelFunc
	done     chan error
	finished chan struct{}
	statuses chan Status
}

type t18HTTPClient struct {
	client      *http.Client
	baseURL     string
	origin      string
	accessToken string
}

// TestWindowsStandaloneT18InstallationHTTPFlow is an opt-in end-to-end test
// for the isolated t18-setup Windows release. The fixture must be disposable:
// this test creates an administrator and stops the owned Redis/backend/media
// processes twice.
func TestWindowsStandaloneT18InstallationHTTPFlow(t *testing.T) {
	installDir := strings.TrimSpace(os.Getenv("UVP_T18_INSTALL_DIR"))
	if installDir == "" {
		t.Skip("requires an explicitly prepared isolated t18-setup Windows fixture")
	}
	release, err := standalone.LoadRelease(installDir)
	if err != nil {
		t.Fatal("t18 fixture release is unavailable")
	}
	if release.Version != "t18-setup" {
		t.Fatal("test requires the isolated t18-setup release")
	}

	runs := make([]*t18Launch, 0, 2)
	defer func() {
		for _, run := range runs {
			run.cancel()
			t18WaitFinished(t, run, 90*time.Second)
		}
	}()

	firstURLs := make(chan string, 1)
	first := t18Start(t, installDir, firstURLs)
	runs = append(runs, first)
	firstEntry, ok := t18WaitBrowserEntry(first, firstURLs, 90*time.Second)
	if !ok {
		t.Fatal("first t18 launch did not publish a browser entry")
	}
	baseURL, origin, bootstrapToken, ok := t18BrowserEndpoint(firstEntry, true)
	if !ok {
		t.Fatal("first browser entry was not a valid loopback bootstrap URL")
	}
	client := t18HTTPClient{client: t18HTTPTransport(), baseURL: baseURL, origin: origin}

	status, headers, body := client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, "")
	if status != http.StatusOK {
		t.Fatalf("initial setup status = %d, want %d", status, http.StatusOK)
	}
	t18AssertSetupStatus(t, body, true, "pending_admin")

	password := t18RandomPassword(t)
	adminBody := t18AdminBody(t, "t18-admin", password)
	status, headers, body = client.request(t, http.MethodPost, "/api/standalone/setup/admin", "", adminBody)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, password)
	if status != http.StatusForbidden {
		t.Fatalf("administrator request without token = %d, want %d", status, http.StatusForbidden)
	}

	for _, requestCase := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/users/list"},
		{method: http.MethodPost, path: "/index/hook/on_record_mp4"},
	} {
		status, headers, body = client.request(t, requestCase.method, requestCase.path, "", nil)
		t18AssertResponseSafe(t, headers, body, bootstrapToken, password)
		if status != http.StatusServiceUnavailable {
			t.Fatalf("unallowed request = %d, want %d", status, http.StatusServiceUnavailable)
		}
	}

	status, headers, body = client.request(t, http.MethodPost, "/api/standalone/setup/admin", bootstrapToken, adminBody)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, password)
	if status != http.StatusCreated {
		t.Fatalf("administrator setup = %d, want %d", status, http.StatusCreated)
	}
	t18AssertSetupStatus(t, body, true, "pending_sip")

	status, headers, body = client.request(t, http.MethodPost, "/api/login", "", adminBody)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, password)
	var login struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &login) != nil || login.Data.AccessToken == "" {
		t.Fatal("new administrator could not log in")
	}
	client.accessToken = login.Data.AccessToken
	status, headers, body = client.request(t, http.MethodGet, "/api/gb28181/sip/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, password)
	if status != http.StatusOK {
		t.Fatalf("authenticated SIP setup status = %d", status)
	}

	status, headers, body = client.request(t, http.MethodPost, "/api/standalone/setup/admin", bootstrapToken, adminBody)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, password)
	if status != http.StatusConflict && status != http.StatusForbidden && status != http.StatusServiceUnavailable {
		t.Fatalf("replayed administrator setup = %d, want a rejection", status)
	}

	first.cancel()
	if !t18WaitFinished(t, first, 90*time.Second) {
		t.Fatal("first t18 launch did not stop cleanly")
	}

	secondURLs := make(chan string, 1)
	second := t18Start(t, installDir, secondURLs)
	runs = append(runs, second)
	secondEntry, ok := t18WaitBrowserEntry(second, secondURLs, 90*time.Second)
	if !ok {
		t.Fatal("second t18 launch did not publish a browser entry")
	}
	baseURL, origin, _, ok = t18BrowserEndpoint(secondEntry, false)
	if !ok || strings.Contains(secondEntry, "bootstrap_token=") {
		t.Fatal("second browser entry unexpectedly carried a bootstrap fragment")
	}
	client.baseURL = baseURL
	client.origin = origin
	status, headers, body = client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, "", password)
	if status != http.StatusOK {
		t.Fatalf("restarted setup status = %d, want %d", status, http.StatusOK)
	}
	t18AssertSetupStatus(t, body, true, "pending_sip")

	var mediaUUID string
	if sipIP := os.Getenv("UVP_T18_SIP_IP"); sipIP != "" {
		sipPassword := t18RandomPassword(t)
		configBody, err := json.Marshal(map[string]any{"deploymentMode": "lan", "listenIp": sipIP, "advertiseIp": sipIP, "mediaReceiveHost": sipIP, "mediaPlaybackHost": sipIP, "port": 15070, "domain": "3402000000", "serverId": "34020000002000000001", "password": sipPassword})
		if err != nil {
			t.Fatal("could not encode SIP test settings")
		}
		status, headers, body = client.request(t, http.MethodPut, "/api/gb28181/sip/setup/config", "", configBody)
		// Authorized SIP settings intentionally return the device registration
		// password for device provisioning. Bootstrap/admin secrets remain private.
		t18AssertResponseSafe(t, headers, body, bootstrapToken, password)
		var saved struct {
			Data struct {
				ReloadedOK bool `json:"reloadedOk"`
			} `json:"data"`
		}
		if status != http.StatusOK || json.Unmarshal(body, &saved) != nil || !saved.Data.ReloadedOK {
			t.Fatalf("SIP setup activation failed, HTTP=%d", status)
		}
		status, _, body = client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
		if status != http.StatusOK {
			t.Fatal("completed setup state unavailable")
		}
		t18AssertSetupStatus(t, body, true, "complete")
		if os.Getenv("UVP_T19_REQUIRE_BUSINESS_READY") == "1" {
			t19WaitBusinessReady(t, second)
			mediaUUID = t19AssertLocalMediaNode(t, client, sipIP)
		}
	}

	second.cancel()
	if !t18WaitFinished(t, second, 90*time.Second) {
		t.Fatal("second t18 launch did not stop cleanly")
	}

	if os.Getenv("UVP_T18_SIP_IP") != "" {
		thirdURLs := make(chan string, 1)
		third := t18Start(t, installDir, thirdURLs)
		runs = append(runs, third)
		entry, ok := t18WaitBrowserEntry(third, thirdURLs, 90*time.Second)
		if !ok {
			t.Fatal("completed installation did not restart")
		}
		base, origin, _, ok := t18BrowserEndpoint(entry, false)
		if !ok {
			t.Fatal("completed installation reopened bootstrap")
		}
		client.baseURL, client.origin = base, origin
		status, _, body = client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
		if status != http.StatusOK {
			t.Fatal("completed installation state unavailable after restart")
		}
		t18AssertSetupStatus(t, body, true, "complete")
		status, _, body = client.request(t, http.MethodGet, "/api/gb28181/sip/setup/status", "", nil)
		var config struct {
			Data struct {
				Config struct {
					Port int `json:"port"`
				} `json:"config"`
			} `json:"data"`
		}
		if status != http.StatusOK || json.Unmarshal(body, &config) != nil || config.Data.Config.Port != 15070 {
			t.Fatal("SIP settings did not survive restart")
		}
		if os.Getenv("UVP_T19_REQUIRE_BUSINESS_READY") == "1" {
			t19WaitBusinessReady(t, third)
			if t19AssertLocalMediaNode(t, client, os.Getenv("UVP_T18_SIP_IP")) != mediaUUID {
				t.Fatal("local media node identity changed after restart")
			}
		}
		third.cancel()
		if !t18WaitFinished(t, third, 90*time.Second) {
			t.Fatal("completed installation did not stop cleanly")
		}
	}
}

func t18Start(t *testing.T, installDir string, browserURLs chan<- string) *t18Launch {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	statuses := make(chan Status, 64)
	go func() {
		defer close(finished)
		done <- LaunchWithBrowser(ctx, installDir, "", func(status Status) {
			select {
			case statuses <- status:
			default:
			}
		}, func(entry string) {
			select {
			case browserURLs <- entry:
			default:
			}
		})
	}()
	return &t18Launch{cancel: cancel, done: done, finished: finished, statuses: statuses}
}

// This observes the launcher's signed backend probe, which in turn requires
// live ZLM configuration and an authenticated keepalive accepted by Collector.
// No synthetic Hook or direct Collector update is used by this test.
func t19WaitBusinessReady(t *testing.T, run *t18Launch) {
	t.Helper()
	started := time.Now()
	timer := time.NewTimer(100 * time.Second)
	defer timer.Stop()
	lastReason := "not_observed"
	for {
		select {
		case state := <-run.statuses:
			if state.State != Ready {
				continue
			}
			lastReason = state.BusinessReason
			if state.BusinessReady {
				if state.BusinessReason != "ready" || state.SIPState != "running" {
					t.Fatal("inconsistent business readiness state")
				}
				t.Logf("real ZLM authenticated Hook business ready after %s", time.Since(started).Round(time.Millisecond))
				return
			}
		case <-run.finished:
			t.Fatal("owned launcher exited before business readiness")
		case <-timer.C:
			t.Fatalf("real ZLM Hook readiness timed out: reason=%s", lastReason)
		}
	}
}

func t19AssertLocalMediaNode(t *testing.T, client t18HTTPClient, mediaIP string) string {
	t.Helper()
	status, _, body := client.request(t, http.MethodGet, "/api/gb28181/zlm/nodes", "", nil)
	var result struct {
		Data struct {
			List []struct {
				Host         string `json:"host"`
				ReceiveHost  string `json:"receiveHost"`
				PlaybackHost string `json:"playbackHost"`
				UUID         string `json:"mediaServerUUID"`
			} `json:"list"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &result) != nil || len(result.Data.List) != 1 {
		t.Fatal("expected exactly one persisted local media node")
	}
	n := result.Data.List[0]
	if n.Host != "127.0.0.1" || n.ReceiveHost != mediaIP || n.PlaybackHost != mediaIP || n.UUID == "" {
		t.Fatal("media addresses or identity did not match confirmed setup")
	}
	return n.UUID
}

func t18WaitBrowserEntry(run *t18Launch, browserURLs <-chan string, timeout time.Duration) (string, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case entry := <-browserURLs:
		return entry, true
	case <-run.done:
		return "", false
	case <-timer.C:
		return "", false
	}
}

func t18WaitFinished(t *testing.T, run *t18Launch, timeout time.Duration) bool {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-run.finished:
		select {
		case err := <-run.done:
			return err == nil
		default:
			return true
		}
	case <-timer.C:
		t.Errorf("t18 owned process cleanup exceeded deadline")
		return false
	}
}

func t18HTTPTransport() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (client t18HTTPClient) request(t *testing.T, method, path, token string, body []byte) (int, http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, strings.TrimRight(client.baseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal("invalid t18 setup request")
	}
	if client.origin != "" {
		request.Header.Set("Origin", client.origin)
	}
	if client.accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+client.accessToken)
	}
	if token != "" {
		request.Header.Set("X-UVP-Setup-Token", token)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.client.Do(request)
	if err != nil {
		t.Fatal("t18 setup request failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		t.Fatal("t18 setup response could not be read")
	}
	return response.StatusCode, response.Header, raw
}

func t18BrowserEndpoint(entry string, wantBootstrap bool) (baseURL, origin, token string, ok bool) {
	parsed, err := url.Parse(entry)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Opaque != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", "", "", false
	}
	if ip := net.ParseIP(parsed.Hostname()); ip == nil || !ip.IsLoopback() || parsed.Host == "" {
		return "", "", "", false
	}
	if wantBootstrap {
		if !strings.HasPrefix(parsed.Fragment, t18SetupTokenFragment) {
			return "", "", "", false
		}
		token = strings.TrimPrefix(parsed.Fragment, t18SetupTokenFragment)
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != token {
			return "", "", "", false
		}
	} else if parsed.Fragment != "" {
		return "", "", "", false
	}
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String(), parsed.Scheme + "://" + parsed.Host, token, true
}

func t18RandomPassword(t *testing.T) string {
	t.Helper()
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal("could not generate t18 test password")
	}
	return "T18!" + base64.RawURLEncoding.EncodeToString(raw[:])
}

func t18AdminBody(t *testing.T, username, password string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatal("could not encode t18 administrator request")
	}
	return body
}

func t18AssertSetupStatus(t *testing.T, raw []byte, standalone bool, phase string) {
	t.Helper()
	var status struct {
		Standalone bool   `json:"standalone"`
		Phase      string `json:"phase"`
	}
	if err := json.Unmarshal(raw, &status); err != nil || status.Standalone != standalone || status.Phase != phase {
		t.Fatalf("unexpected t18 setup status")
	}
}

func t18AssertResponseSafe(t *testing.T, headers http.Header, body []byte, token, password string) {
	t.Helper()
	values := []string{string(body)}
	for _, entries := range headers {
		values = append(values, entries...)
	}
	for _, value := range values {
		if (token != "" && strings.Contains(value, token)) || (password != "" && strings.Contains(value, password)) {
			t.Fatal("t18 setup response exposed a credential")
		}
	}
}
