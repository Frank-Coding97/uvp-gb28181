//go:build windows

package launcher

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
	"uvplatform.cn/uvp-gb28181/internal/standalone/winprocess"
)

const (
	t18BrowserInstallDirEnv  = "UVP_T18_BROWSER_INSTALL_DIR"
	t18BrowserReleaseVersion = "t18-setup"
)

// TestWindowsStandaloneT18InstallationBrowserFlow is an opt-in native Edge
// headless smoke test for the first-install UI. It intentionally covers only
// the browser-facing pending_admin -> login transition; SIP setup is out of
// scope. Edge is an independently owned process Job, with a temporary clean
// profile and no bootstrap credential in its command line. This is native
// headless Edge automation, not Explorer automation.
func TestWindowsStandaloneT18InstallationBrowserFlow(t *testing.T) {
	installDir := strings.TrimSpace(os.Getenv(t18BrowserInstallDirEnv))
	if installDir == "" {
		t.Skip("requires an explicitly prepared fresh t18-setup Windows fixture")
	}
	release, err := standalone.LoadRelease(installDir)
	if err != nil {
		t.Fatal("t18 browser fixture release is unavailable")
	}
	if release.Version != t18BrowserReleaseVersion {
		t.Fatal("browser test requires the isolated t18-setup release")
	}

	firstURLs := make(chan string, 1)
	run := t18Start(t, installDir, firstURLs)
	defer func() {
		run.cancel()
		t18WaitFinished(t, run, 90*time.Second)
	}()
	entry, ok := t18WaitBrowserEntry(run, firstURLs, 90*time.Second)
	if !ok {
		t.Fatal("t18 launch did not publish a browser entry")
	}
	baseURL, origin, bootstrapToken, ok := t18BrowserEndpoint(entry, true)
	if !ok {
		t.Fatal("t18 browser entry was not a valid loopback bootstrap URL")
	}
	client := t18HTTPClient{client: t18HTTPTransport(), baseURL: baseURL, origin: origin}
	status, headers, body := client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, "")
	if status != http.StatusOK {
		t.Fatal("initial setup status request failed")
	}
	t18AssertSetupStatus(t, body, true, "pending_admin")

	browserContext, cancelBrowser := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancelBrowser()
	browser, err := t18StartHeadlessEdge(browserContext)
	if err != nil {
		t.Fatal("could not start the owned Edge CDP browser")
	}
	defer func() {
		if !browser.close() {
			t.Errorf("owned Edge cleanup exceeded its deadline")
		}
	}()

	if err := browser.navigate(browserContext, entry); err != nil {
		t.Fatal("could not navigate the owned browser")
	}
	entry = ""
	bootstrapToken = ""
	if err := t18WaitSetupForm(browserContext, browser.cdp); err != nil {
		t.Fatal("first-install form did not become ready with a scrubbed URL")
	}

	username := "t18-browser-admin"
	password := t18BrowserPassword(t)
	if err := browser.fillAndSubmit(browserContext, username, password); err != nil {
		t.Fatal("first-install form submission could not be dispatched")
	}
	password = ""
	if err := t18WaitLoginRoute(browserContext, browser.cdp); err != nil {
		t.Fatal("first-install UI did not transition to the login route")
	}

	status, headers, body = client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, "", "")
	if status != http.StatusOK {
		t.Fatal("post-install setup status request failed")
	}
	t18AssertSetupStatus(t, body, true, "pending_sip")
}

func t18BrowserPassword(t *testing.T) string {
	t.Helper()
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal("could not generate browser test password")
	}
	return "T18!" + base64.RawURLEncoding.EncodeToString(raw[:])
}

type t18EdgeBrowser struct {
	job     *winprocess.Job
	process *winprocess.Process
	cdp     *t18CDP
	waited  <-chan struct{}
	profile string
}

func t18StartHeadlessEdge(ctx context.Context) (*t18EdgeBrowser, error) {
	edgePath, err := t18FindEdgeExecutable()
	if err != nil {
		return nil, err
	}
	profile, err := os.MkdirTemp("", "uvp-t18-edge-")
	if err != nil {
		return nil, errors.New("create temporary Edge profile")
	}
	job, err := winprocess.NewJob()
	if err != nil {
		_ = os.RemoveAll(profile)
		return nil, errors.New("create Edge process Job")
	}
	browser := &t18EdgeBrowser{job: job, profile: profile}
	args := []string{
		"--headless=new",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-sync",
		"--disable-gpu",
		"--disable-extensions",
		"--disable-default-apps",
		"--incognito",
		"--user-data-dir=" + profile,
		"--remote-debugging-address=127.0.0.1",
		"--remote-debugging-port=0",
		"--remote-allow-origins=*",
		"about:blank",
	}
	process, err := job.Start(winprocess.StartSpec{Path: edgePath, Dir: profile, Args: args})
	if err != nil {
		_ = job.Close()
		_ = os.RemoveAll(profile)
		return nil, errors.New("start owned Edge process")
	}
	browser.process = process
	member, err := job.Contains(process)
	if err != nil || !member {
		_ = job.Close()
		_ = process.Close()
		_ = os.RemoveAll(profile)
		return nil, errors.New("Edge process was not owned by its Job")
	}
	waited := make(chan struct{})
	browser.waited = waited
	go func() {
		_, _ = process.Wait()
		close(waited)
	}()

	port, err := t18WaitDevToolsPort(ctx, profile)
	if err != nil {
		_ = browser.close()
		return nil, err
	}
	pageURL, err := t18WaitDevToolsPage(ctx, port)
	if err != nil {
		_ = browser.close()
		return nil, err
	}
	cdp, err := t18DialCDP(ctx, pageURL)
	if err != nil {
		_ = browser.close()
		return nil, err
	}
	browser.cdp = cdp
	return browser, nil
}

func (browser *t18EdgeBrowser) close() bool {
	if browser == nil {
		return true
	}
	if browser.cdp != nil {
		_ = browser.cdp.close()
	}
	if browser.job != nil {
		_ = browser.job.Close()
	}
	if browser.waited == nil {
		return true
	}
	select {
	case <-browser.waited:
		_ = os.RemoveAll(browser.profile)
		return true
	case <-time.After(15 * time.Second):
		_ = os.RemoveAll(browser.profile)
		return false
	}
}

func (browser *t18EdgeBrowser) navigate(ctx context.Context, entry string) error {
	if browser == nil || browser.cdp == nil {
		return errors.New("browser protocol is unavailable")
	}
	if _, err := browser.cdp.call(ctx, "Page.enable", nil); err != nil {
		return err
	}
	if _, err := browser.cdp.call(ctx, "Runtime.enable", nil); err != nil {
		return err
	}
	_, err := browser.cdp.call(ctx, "Page.navigate", map[string]string{"url": entry})
	return err
}

func (browser *t18EdgeBrowser) fillAndSubmit(ctx context.Context, username, password string) error {
	usernameJSON, err := json.Marshal(username)
	if err != nil {
		return errors.New("encode browser username")
	}
	passwordJSON, err := json.Marshal(password)
	if err != nil {
		return errors.New("encode browser password")
	}
	expression := "(() => {" +
		"const form = document.querySelector('form.standalone-setup-form');" +
		"if (!form) return false;" +
		"const setValue = (selector, value) => {" +
		"const input = form.querySelector(selector);" +
		"if (!input) return false;" +
		"const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;" +
		"setter.call(input, value);" +
		"input.dispatchEvent(new Event('input', {bubbles: true}));" +
		"input.dispatchEvent(new Event('change', {bubbles: true}));" +
		"return true;};" +
		"if (!setValue('input[name=username]', " + string(usernameJSON) + ") ||" +
		"!setValue('input[name=password]', " + string(passwordJSON) + ") ||" +
		"!setValue('input[name=passwordConfirmation]', " + string(passwordJSON) + ")) return false;" +
		"const submit = form.querySelector('button[type=submit]');" +
		"if (!submit || submit.disabled) return false;" +
		"submit.click(); return true;" +
		"})()"
	return browser.cdp.evalBool(ctx, expression)
}

func t18FindEdgeExecutable() (string, error) {
	roots := []string{"ProgramFiles(x86)", "ProgramW6432", "ProgramFiles", "LOCALAPPDATA"}
	for _, rootName := range roots {
		root := strings.TrimSpace(os.Getenv(rootName))
		if root == "" {
			continue
		}
		candidate := filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe")
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("Microsoft Edge executable is unavailable")
}

func t18WaitDevToolsPort(ctx context.Context, profile string) (int, error) {
	activePortPath := filepath.Join(profile, "DevToolsActivePort")
	for {
		if data, err := os.ReadFile(activePortPath); err == nil {
			if port, ok := t18ParseDevToolsActivePort(data); ok {
				return port, nil
			}
		}
		if err := t18WaitPoll(ctx); err != nil {
			return 0, err
		}
	}
}

func t18ParseDevToolsActivePort(data []byte) (int, bool) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) < 2 || !strings.HasPrefix(strings.TrimSpace(lines[1]), "/devtools/browser/") {
		return 0, false
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return port, true
}

func t18WaitDevToolsPage(ctx context.Context, port int) (string, error) {
	client := &http.Client{
		Timeout:   time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port) + "/json/list"
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err == nil {
			response, requestErr := client.Do(request)
			if requestErr == nil {
				data, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
				_ = response.Body.Close()
				if readErr == nil && len(data) <= 64*1024 {
					if pageURL, ok := t18ParseDevToolsPage(data, port); ok {
						return pageURL, nil
					}
				}
			}
		}
		if err := t18WaitPoll(ctx); err != nil {
			return "", err
		}
	}
}

func t18ParseDevToolsPage(data []byte, port int) (string, bool) {
	var pages []struct {
		Type                 string `json:"type"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if json.Unmarshal(data, &pages) != nil {
		return "", false
	}
	for _, page := range pages {
		if page.Type != "page" || page.WebSocketDebuggerURL == "" {
			continue
		}
		parsed, err := url.Parse(page.WebSocketDebuggerURL)
		if err != nil || parsed.Scheme != "ws" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasPrefix(parsed.Path, "/devtools/page/") {
			continue
		}
		if parsed.Port() != strconv.Itoa(port) {
			continue
		}
		if ip := net.ParseIP(parsed.Hostname()); ip != nil && !ip.IsLoopback() {
			continue
		}
		if parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" && parsed.Hostname() != "::1" {
			continue
		}
		return "ws://127.0.0.1:" + strconv.Itoa(port) + parsed.Path, true
	}
	return "", false
}

func t18WaitPoll(ctx context.Context) error {
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func t18DialCDP(ctx context.Context, pageURL string) (*t18CDP, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	config, err := websocket.NewConfig(pageURL, "http://127.0.0.1")
	if err != nil {
		return nil, errors.New("prepare browser protocol connection")
	}
	config.Dialer = &net.Dialer{Timeout: time.Second}
	connection, err := websocket.DialConfig(config)
	if err != nil {
		return nil, errors.New("connect to browser protocol")
	}
	connection.MaxPayloadBytes = 4 << 20
	return &t18CDP{connection: connection, nextID: 1}, nil
}

type t18CDP struct {
	mu         sync.Mutex
	connection *websocket.Conn
	nextID     uint64
}

type t18CDPResponse struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func (cdp *t18CDP) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	cdp.mu.Lock()
	defer cdp.mu.Unlock()
	if cdp.connection == nil {
		return nil, errors.New("browser protocol is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := cdp.connection.SetDeadline(deadline); err != nil {
			return nil, errors.New("set browser protocol deadline")
		}
	} else if err := cdp.connection.SetDeadline(time.Time{}); err != nil {
		return nil, errors.New("set browser protocol deadline")
	}
	id := cdp.nextID
	cdp.nextID++
	request := struct {
		ID     uint64 `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{ID: id, Method: method, Params: params}
	if err := websocket.JSON.Send(cdp.connection, request); err != nil {
		return nil, errors.New("send browser protocol command")
	}
	for {
		var response t18CDPResponse
		if err := websocket.JSON.Receive(cdp.connection, &response); err != nil {
			return nil, errors.New("receive browser protocol response")
		}
		if response.ID != id {
			continue
		}
		if len(response.Error) != 0 && string(response.Error) != "null" {
			return nil, errors.New("browser protocol command rejected")
		}
		return response.Result, nil
	}
}

func (cdp *t18CDP) evalBool(ctx context.Context, expression string) error {
	result, err := cdp.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
	})
	if err != nil {
		return err
	}
	var envelope struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(result, &envelope) != nil {
		return errors.New("invalid browser evaluation response")
	}
	var value bool
	if json.Unmarshal(envelope.Result.Value, &value) != nil || !value {
		return errors.New("browser evaluation was not accepted")
	}
	return nil
}

func (cdp *t18CDP) evalState(ctx context.Context) (t18BrowserDOMState, error) {
	result, err := cdp.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    t18BrowserDOMStateExpression,
		"returnByValue": true,
	})
	if err != nil {
		return t18BrowserDOMState{}, err
	}
	var envelope struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(result, &envelope) != nil {
		return t18BrowserDOMState{}, errors.New("invalid browser DOM response")
	}
	var state t18BrowserDOMState
	if json.Unmarshal(envelope.Result.Value, &state) != nil {
		return t18BrowserDOMState{}, errors.New("invalid browser DOM state")
	}
	return state, nil
}

func (cdp *t18CDP) close() error {
	cdp.mu.Lock()
	defer cdp.mu.Unlock()
	if cdp.connection == nil {
		return nil
	}
	connection := cdp.connection
	cdp.connection = nil
	return connection.Close()
}

type t18BrowserDOMState struct {
	URLScrubbed bool `json:"urlScrubbed"`
	SetupForm   bool `json:"setupForm"`
	LoginRoute  bool `json:"loginRoute"`
}

const t18BrowserDOMStateExpression = `(() => {
 const href = String(location.href || "");
 const route = (String(location.pathname || "") + " " + String(location.hash || "")).toLowerCase();
 const setupForm = Boolean(document.querySelector("form.standalone-setup-form input[name=username]")) &&
   Boolean(document.querySelector("form.standalone-setup-form input[name=password]")) &&
   Boolean(document.querySelector("form.standalone-setup-form input[name=passwordConfirmation]"));
 return {
   urlScrubbed: !href.includes("bootstrap_token="),
   setupForm: setupForm,
   loginRoute: route.includes("/login")
 };
})()`

func t18WaitSetupForm(ctx context.Context, cdp *t18CDP) error {
	for {
		state, err := cdp.evalState(ctx)
		if err == nil && state.URLScrubbed && state.SetupForm {
			return nil
		}
		if err := t18WaitPoll(ctx); err != nil {
			return err
		}
	}
}

func t18WaitLoginRoute(ctx context.Context, cdp *t18CDP) error {
	for {
		state, err := cdp.evalState(ctx)
		if err == nil && state.URLScrubbed && state.LoginRoute {
			return nil
		}
		if err := t18WaitPoll(ctx); err != nil {
			return err
		}
	}
}
