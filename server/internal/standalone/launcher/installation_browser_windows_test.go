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
	t19BrowserSIPIPEnv       = "UVP_T19_BROWSER_SIP_IP"
)

// TestWindowsStandaloneT18InstallationBrowserFlow is an opt-in native Edge
// headless smoke test for the first-install UI. It covers the browser-facing
// pending_admin -> login -> required SIP setup transition. When
// UVP_T19_BROWSER_SIP_IP is set, it also completes the required SIP setup
// through the browser. Edge is an independently owned process Job, with a
// temporary clean profile and no bootstrap credential in its command line.
// This is native headless Edge automation, not Explorer automation.
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
		t.Logf("initial setup status HTTP status=%d", status)
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
		t18LogBrowserState(t, browser.cdp)
		t.Fatal("could not navigate the owned browser")
	}
	entry = ""
	bootstrapToken = ""
	if err := t18WaitSetupForm(browserContext, browser.cdp); err != nil {
		t18LogBrowserState(t, browser.cdp)
		t.Fatal("first-install form did not become ready with a scrubbed URL")
	}

	username := "t18-browser-admin"
	password := t18BrowserPassword(t)
	if err := browser.fillAndSubmit(browserContext, username, password); err != nil {
		t18LogBrowserState(t, browser.cdp)
		t.Fatal("first-install form submission could not be dispatched")
	}
	if err := t18WaitLoginRoute(browserContext, browser.cdp); err != nil {
		t18LogBrowserState(t, browser.cdp)
		t.Fatal("first-install UI did not transition to the login route")
	}
	if err := browser.loginAndSubmit(browserContext, username, password); err != nil {
		t18LogBrowserState(t, browser.cdp)
		t.Fatal("administrator login form submission could not be dispatched")
	}
	password = ""
	if err := t18WaitSIPRequired(browserContext, browser.cdp); err != nil {
		t18LogBrowserState(t, browser.cdp)
		t.Fatal("SIP required setup modal did not become visible")
	}
	status, headers, body = client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, "", "")
	if status != http.StatusOK {
		t18LogBrowserState(t, browser.cdp)
		t.Logf("post-login setup status HTTP status=%d", status)
		t.Fatal("post-login setup status request failed")
	}
	t18AssertSetupStatus(t, body, true, "pending_sip")
	if err := browser.captureScreenshot(browserContext, filepath.Join(installDir, "t18-sip-onboarding.png")); err != nil {
		t.Log("SIP onboarding screenshot was unavailable")
	}

	sipIP := strings.TrimSpace(os.Getenv(t19BrowserSIPIPEnv))
	if sipIP == "" {
		return
	}
	if !t19ConcreteIPv4(sipIP) {
		t.Fatal("UVP_T19_BROWSER_SIP_IP must be a concrete IPv4 address")
	}

	sipContext, cancelSIP := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancelSIP()
	sipPassword := t18BrowserPassword(t)
	if err := browser.completeSIP(sipContext, sipIP, "34020000002000000001", sipPassword); err != nil {
		t19LogFlowFailure(t, browser.cdp, err)
		t19CaptureFailureScreenshot(t, browser, filepath.Join(installDir, "t19-sip-failure.png"))
		t.Fatal("standalone SIP browser flow could not be completed")
	}
	sipPassword = ""
	if err := t19WaitStandaloneComplete(t, sipContext, client, browser.cdp); err != nil {
		t18LogBrowserState(t, browser.cdp)
		t.Fatal("standalone SIP browser flow did not reach the completed home page")
	}
	if err := browser.captureScreenshot(sipContext, filepath.Join(installDir, "t19-completed-home.png")); err != nil {
		t.Fatal("completed home screenshot was unavailable")
	}
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
		"--window-size=1365,900",
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

func (browser *t18EdgeBrowser) loginAndSubmit(ctx context.Context, username, password string) error {
	usernameJSON, err := json.Marshal(username)
	if err != nil {
		return errors.New("encode browser username")
	}
	passwordJSON, err := json.Marshal(password)
	if err != nil {
		return errors.New("encode browser password")
	}
	expression := "(() => {" +
		"const form = Array.from(document.querySelectorAll('form')).find(candidate => " +
		"candidate.querySelector('input[type=password]') && " +
		"(candidate.querySelector('input[placeholder=\\\"请输入账号\\\"]') || candidate.querySelector('input[type=text]')));" +
		"if (!form) return false;" +
		"const setValue = (selector, value) => {" +
		"const input = form.querySelector(selector);" +
		"if (!input) return false;" +
		"const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;" +
		"setter.call(input, value);" +
		"input.dispatchEvent(new Event('input', {bubbles: true}));" +
		"input.dispatchEvent(new Event('change', {bubbles: true}));" +
		"return true;};" +
		"if (!setValue('input[placeholder=\\\"请输入账号\\\"]', " + string(usernameJSON) + ") &&" +
		"!setValue('input[type=text]', " + string(usernameJSON) + ")) return false;" +
		"if (!setValue('input[type=password]', " + string(passwordJSON) + ")) return false;" +
		"const submit = form.querySelector('button[type=submit]');" +
		"if (!submit || submit.disabled) return false;" +
		"submit.click(); return true;" +
		"})()"
	return browser.cdp.evalBool(ctx, expression)
}

func (browser *t18EdgeBrowser) completeSIP(ctx context.Context, mediaIP, serverID, password string) error {
	if browser == nil || browser.cdp == nil {
		return &t19BrowserFlowError{stage: "browser", kind: "unavailable"}
	}
	if err := t19RunStage(ctx, "deployment", func(stageCtx context.Context) error {
		return t19WaitClickText(stageCtx, browser.cdp, ".deployment-options .deployment-option", "局域网部署")
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "deployment-next", func(stageCtx context.Context) error {
		return t19WaitClickText(stageCtx, browser.cdp, ".sip-setup-dialog button", "下一步")
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "network-form", func(stageCtx context.Context) error {
		return t19WaitPendingSelector(stageCtx, browser.cdp, ".network-form")
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "network-select", func(stageCtx context.Context) error {
		return t19SelectNetworkIP(stageCtx, browser.cdp, mediaIP)
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "media-receive", func(stageCtx context.Context) error {
		return t19WaitSetInputAt(stageCtx, browser.cdp, ".media-addresses input", 0, mediaIP)
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "media-playback", func(stageCtx context.Context) error {
		return t19WaitSetInputAt(stageCtx, browser.cdp, ".media-addresses input", 1, mediaIP)
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "media-confirm", func(stageCtx context.Context) error {
		return t19WaitMediaValues(stageCtx, browser.cdp, mediaIP)
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "network-next", func(stageCtx context.Context) error {
		return t19WaitClickText(stageCtx, browser.cdp, ".sip-setup-dialog button", "下一步")
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "identity-form", func(stageCtx context.Context) error {
		return t19WaitPendingSelector(stageCtx, browser.cdp, ".identity-form")
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "identity-port", func(stageCtx context.Context) error {
		return t19WaitSetInputAt(stageCtx, browser.cdp, ".identity-form input[placeholder='1 - 65535']", 0, "15070")
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "identity-id", func(stageCtx context.Context) error {
		return t19WaitSetInputAt(stageCtx, browser.cdp, ".identity-form input[placeholder='20 位数字编码']", 0, serverID)
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "identity-password", func(stageCtx context.Context) error {
		return t19WaitSetInputAt(stageCtx, browser.cdp, ".identity-form input[type='text'][placeholder^='至少 12 位']", 0, password)
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "identity-confirm", func(stageCtx context.Context) error {
		return t19WaitIdentityValues(stageCtx, browser.cdp, serverID, password)
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "identity-next", func(stageCtx context.Context) error {
		return t19WaitClickText(stageCtx, browser.cdp, ".sip-setup-dialog button", "下一步")
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "confirmation-form", func(stageCtx context.Context) error {
		return t19WaitPendingSelector(stageCtx, browser.cdp, ".confirm-groups")
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "confirmation-values", func(stageCtx context.Context) error {
		return t19WaitConfirmValues(stageCtx, browser.cdp, mediaIP, serverID)
	}); err != nil {
		return err
	}
	if err := t19RunStage(ctx, "save", func(stageCtx context.Context) error {
		return t19WaitClickText(stageCtx, browser.cdp, ".sip-setup-dialog button", "保存并启动")
	}); err != nil {
		return err
	}
	return nil
}

const t19BrowserStageTimeout = 12 * time.Second

type t19BrowserFlowError struct {
	stage string
	kind  string
}

func (err *t19BrowserFlowError) Error() string {
	return "stage=" + err.stage + " error=" + err.kind
}

func t19RunStage(ctx context.Context, stage string, run func(context.Context) error) error {
	stageCtx, cancel := context.WithTimeout(ctx, t19BrowserStageTimeout)
	defer cancel()
	if err := run(stageCtx); err != nil {
		kind := "selector_or_protocol"
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			kind = "deadline"
		case errors.Is(err, context.Canceled):
			kind = "canceled"
		}
		return &t19BrowserFlowError{stage: stage, kind: kind}
	}
	return nil
}

func t19WaitClickText(ctx context.Context, cdp *t18CDP, selector, label string) error {
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return errors.New("encode browser selector")
	}
	labelJSON, err := json.Marshal(label)
	if err != nil {
		return errors.New("encode browser label")
	}
	expression := "(() => {" +
		"const visible = node => { if (!node) return false; const style = getComputedStyle(node); return style.display !== 'none' && style.visibility !== 'hidden' && (node.offsetWidth > 0 || node.offsetHeight > 0 || node.getClientRects().length > 0); };" +
		"const nodes = Array.from(document.querySelectorAll(" + string(selectorJSON) + "));" +
		"const label = " + string(labelJSON) + ";" +
		"const node = nodes.find(candidate => visible(candidate) && !candidate.disabled && String(candidate.textContent || '').includes(label));" +
		"if (!node) return false; node.click(); return true;" +
		"})()"
	return t19WaitEval(ctx, cdp, expression)
}

func t19SelectNetworkIP(ctx context.Context, cdp *t18CDP, mediaIP string) error {
	triggerExpression := "(() => {" +
		"const trigger = document.querySelector('.network-form .arco-select-view, .network-form .arco-select');" +
		"if (!trigger) return false; trigger.click(); return true;" +
		"})()"
	if err := t19WaitEval(ctx, cdp, triggerExpression); err != nil {
		return err
	}
	desiredJSON, err := json.Marshal(mediaIP)
	if err != nil {
		return errors.New("encode configured LAN address")
	}
	optionExpression := "(() => {" +
		"const visible = node => { if (!node) return false; const style = getComputedStyle(node); return style.display !== 'none' && style.visibility !== 'hidden' && (node.offsetWidth > 0 || node.offsetHeight > 0 || node.getClientRects().length > 0); };" +
		"const desired = " + string(desiredJSON) + ";" +
		"const option = Array.from(document.querySelectorAll('.arco-select-option, [role=option]')).find(candidate => { const text = String(candidate.textContent || '').trim(); return visible(candidate) && (text === desired || text.startsWith(desired + ' ')); });" +
		"if (!option) return false; option.click(); return true;" +
		"})()"
	if err := t19WaitEval(ctx, cdp, optionExpression); err != nil {
		return err
	}
	selectedExpression := "(() => {" +
		"const desired = " + string(desiredJSON) + ";" +
		"const value = document.querySelector('.network-form .arco-select-view-value, .network-form .arco-select-view');" +
		"if (!value) return false; const text = String(value.textContent || '').trim(); return text === desired || text.startsWith(desired + ' ');" +
		"})()"
	return t19WaitEval(ctx, cdp, selectedExpression)
}

func t19WaitSetInputAt(ctx context.Context, cdp *t18CDP, selector string, index int, value string) error {
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return errors.New("encode browser input selector")
	}
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return errors.New("encode browser input value")
	}
	expression := "(() => {" +
		"const visible = node => { if (!node) return false; const style = getComputedStyle(node); return style.display !== 'none' && style.visibility !== 'hidden' && (node.offsetWidth > 0 || node.offsetHeight > 0 || node.getClientRects().length > 0); };" +
		"const inputs = Array.from(document.querySelectorAll(" + string(selectorJSON) + ")).filter(visible);" +
		"const input = inputs[" + strconv.Itoa(index) + "]; const desired = " + string(valueJSON) + ";" +
		"if (!input || input.disabled) return false;" +
		"const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;" +
		"if (!setter) return false; setter.call(input, desired); input.dispatchEvent(new Event('input', {bubbles: true})); input.dispatchEvent(new Event('change', {bubbles: true})); input.blur(); return input.value === desired;" +
		"})()"
	return t19WaitEval(ctx, cdp, expression)
}

func t19WaitMediaValues(ctx context.Context, cdp *t18CDP, mediaIP string) error {
	desiredJSON, err := json.Marshal(mediaIP)
	if err != nil {
		return errors.New("encode configured media address")
	}
	expression := "(() => {" +
		"const desired = " + string(desiredJSON) + ";" +
		"const inputs = Array.from(document.querySelectorAll('.media-addresses input'));" +
		"return inputs.length >= 2 && inputs[0].value === desired && inputs[1].value === desired;" +
		"})()"
	return t19WaitEval(ctx, cdp, expression)
}

func t19WaitIdentityValues(ctx context.Context, cdp *t18CDP, serverID, password string) error {
	serverIDJSON, err := json.Marshal(serverID)
	if err != nil {
		return errors.New("encode SIP platform ID")
	}
	passwordJSON, err := json.Marshal(password)
	if err != nil {
		return errors.New("encode SIP password")
	}
	expression := "(() => {" +
		"const serverID = " + string(serverIDJSON) + "; const password = " + string(passwordJSON) + ";" +
		"const port = document.querySelector('.identity-form input[placeholder=\"1 - 65535\"]');" +
		"const id = document.querySelector('.identity-form input[placeholder=\"20 位数字编码\"]');" +
		"const domain = document.querySelector('.identity-form input[placeholder=\"填入平台 ID 后自动生成\"]');" +
		"const passwordInput = document.querySelector('.identity-form input[type=\"text\"][placeholder^=\"至少 12 位\"]');" +
		"return Boolean(port && id && domain && passwordInput) && port.value === '15070' && id.value === serverID && domain.value === serverID.slice(0, 10) && passwordInput.value === password;" +
		"})()"
	return t19WaitEval(ctx, cdp, expression)
}

func t19WaitConfirmValues(ctx context.Context, cdp *t18CDP, mediaIP, serverID string) error {
	mediaIPJSON, err := json.Marshal(mediaIP)
	if err != nil {
		return errors.New("encode confirmation media address")
	}
	serverIDJSON, err := json.Marshal(serverID)
	if err != nil {
		return errors.New("encode confirmation platform ID")
	}
	expression := "(() => {" +
		"const root = document.querySelector('.confirm-groups'); const text = root ? String(root.textContent || '') : '';" +
		"return Boolean(root) && text.includes(" + string(mediaIPJSON) + ") && text.includes(" + string(serverIDJSON) + ") && text.includes('15070') && text.includes('媒体接收地址') && text.includes('媒体播放地址');" +
		"})()"
	return t19WaitEval(ctx, cdp, expression)
}

func t19WaitPendingSelector(ctx context.Context, cdp *t18CDP, selector string) error {
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return errors.New("encode pending UI selector")
	}
	expression := "(() => {" +
		"const node = document.querySelector(" + string(selectorJSON) + ");" +
		"const visible = node => { if (!node) return false; const style = getComputedStyle(node); return style.display !== 'none' && style.visibility !== 'hidden' && (node.offsetWidth > 0 || node.offsetHeight > 0 || node.getClientRects().length > 0); };" +
		"const dashboard = Boolean(document.querySelector('.dashboard-shell, .dashboard-grid'));" +
		"const errorToast = Array.from(document.querySelectorAll('.arco-message, .arco-notification, [role=alert]')).some(candidate => { if (!visible(candidate)) return false; const text = String(candidate.textContent || ''); return text.includes('服务器异常') || text.includes('请联系管理员'); });" +
		"const businessRequest = performance.getEntriesByType('resource').some(entry => { try { const path = new URL(String(entry.name || ''), location.href).pathname; return path === '/api/gb28181/sip/platform' || path === '/api/gb28181/sip/dashboard/snapshot' || path.startsWith('/api/gb28181/sip/dashboard/') || path.startsWith('/api/gb28181/home/') || path === '/api/gb28181/zlm/overview'; } catch (_) { return false; } });" +
		"return visible(node) && !dashboard && !errorToast && !businessRequest;" +
		"})()"
	return t19WaitEval(ctx, cdp, expression)
}

func t19WaitEval(ctx context.Context, cdp *t18CDP, expression string) error {
	for {
		if err := cdp.evalBool(ctx, expression); err == nil {
			return nil
		}
		if err := t18WaitPoll(ctx); err != nil {
			return err
		}
	}
}

func (browser *t18EdgeBrowser) captureScreenshot(ctx context.Context, path string) error {
	if browser == nil || browser.cdp == nil {
		return errors.New("browser protocol is unavailable")
	}
	result, err := browser.cdp.call(ctx, "Page.captureScreenshot", map[string]string{"format": "png"})
	if err != nil {
		return err
	}
	var payload struct {
		Data string `json:"data"`
	}
	if json.Unmarshal(result, &payload) != nil || payload.Data == "" {
		return errors.New("invalid browser screenshot response")
	}
	png, err := base64.StdEncoding.DecodeString(payload.Data)
	if err != nil || len(png) == 0 {
		return errors.New("invalid browser screenshot data")
	}
	if err := os.WriteFile(path, png, 0600); err != nil {
		return errors.New("write browser screenshot")
	}
	return nil
}

func t19CaptureFailureScreenshot(t *testing.T, browser *t18EdgeBrowser, path string) {
	t.Helper()
	if browser == nil || browser.cdp == nil {
		return
	}
	redactCtx, cancelRedact := context.WithTimeout(context.Background(), time.Second)
	_ = browser.cdp.evalBool(redactCtx, "(() => {"+
		"document.querySelectorAll('input').forEach(input => { const placeholder = String(input.placeholder || ''); if (input.type === 'password' || placeholder.includes('密码') || placeholder.includes('至少 12 位')) { input.value = '[redacted]'; input.setAttribute('value', '[redacted]'); } });"+
		"document.querySelectorAll('.confirm-row').forEach(row => { const label = row.querySelector('.confirm-row__label'); if (label && String(label.textContent || '').includes('SIP 密码')) { const value = row.querySelector('.confirm-value'); if (value) value.textContent = '[redacted]'; } });"+
		"return true;"+
		"})()")
	cancelRedact()
	screenshotCtx, cancelScreenshot := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelScreenshot()
	if err := browser.captureScreenshot(screenshotCtx, path); err != nil {
		t.Log("T19 SIP failure screenshot was unavailable")
	}
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
	URLScrubbed             bool `json:"urlScrubbed"`
	SetupForm               bool `json:"setupForm"`
	LoginRoute              bool `json:"loginRoute"`
	HomeRoute               bool `json:"homeRoute"`
	LoginForm               bool `json:"loginForm"`
	DeploymentVisible       bool `json:"deploymentVisible"`
	NetworkFormVisible      bool `json:"networkFormVisible"`
	NetworkSelectVisible    bool `json:"networkSelectVisible"`
	MediaAddressesVisible   bool `json:"mediaAddressesVisible"`
	IdentityFormVisible     bool `json:"identityFormVisible"`
	ConfirmGroupsVisible    bool `json:"confirmGroupsVisible"`
	MediaInputCount         int  `json:"mediaInputCount"`
	VisibleOptionCount      int  `json:"visibleOptionCount"`
	NextEnabled             bool `json:"nextEnabled"`
	SaveEnabled             bool `json:"saveEnabled"`
	SIPModalVisible         bool `json:"sipModalVisible"`
	SIPRequired             bool `json:"sipRequired"`
	SIPSkipVisible          bool `json:"sipSkipVisible"`
	SIPCloseVisible         bool `json:"sipCloseVisible"`
	DashboardVisible        bool `json:"dashboardVisible"`
	DashboardAPIRequested   bool `json:"dashboardAPIRequested"`
	ServerErrorToastVisible bool `json:"serverErrorToastVisible"`
}

const t18BrowserDOMStateExpression = `(() => {
 const href = String(location.href || "");
 const route = (String(location.pathname || "") + " " + String(location.hash || "")).toLowerCase();
 const setupForm = Boolean(document.querySelector("form.standalone-setup-form input[name=username]")) &&
   Boolean(document.querySelector("form.standalone-setup-form input[name=password]")) &&
   Boolean(document.querySelector("form.standalone-setup-form input[name=passwordConfirmation]"));
 const loginForm = Array.from(document.querySelectorAll("form")).some(form =>
   Boolean(form.querySelector("input[type=password]")) &&
   Boolean(form.querySelector("input[placeholder='请输入账号'], input[type=text]"))
 );
 const visible = node => {
   if (!node) return false;
   const style = getComputedStyle(node);
   return style.display !== "none" && style.visibility !== "hidden" &&
     (node.offsetWidth > 0 || node.offsetHeight > 0 || node.getClientRects().length > 0);
 };
 const sipDialog = document.querySelector(".sip-setup-dialog");
 const sipModalVisible = visible(sipDialog);
 const requiredCopy = sipModalVisible && Array.from(sipDialog.querySelectorAll(".sip-modal-intro-text span"))
   .some(node => String(node.textContent || "").includes("请完成配置后再使用系统"));
 const sipSkip = sipDialog && sipDialog.querySelector(".sip-modal-skip");
 const sipClose = sipDialog && sipDialog.querySelector(".arco-modal-close-btn, .arco-modal-close, [aria-label='Close']");
 const deployment = document.querySelector(".deployment-options");
 const networkForm = document.querySelector(".network-form");
 const networkSelect = document.querySelector(".network-form .arco-select-view, .network-form .arco-select");
 const mediaAddresses = document.querySelector(".media-addresses");
 const identityForm = document.querySelector(".identity-form");
 const confirmGroups = document.querySelector(".confirm-groups");
 const visibleButton = label => Array.from(document.querySelectorAll(".sip-setup-dialog button")).some(node => visible(node) && !node.disabled && String(node.textContent || "").includes(label));
 const visibleOptions = Array.from(document.querySelectorAll(".arco-select-option, [role=option]")).filter(visible).length;
 const mediaInputCount = Array.from(document.querySelectorAll(".media-addresses input")).filter(visible).length;
 const dashboardVisible = Boolean(document.querySelector(".dashboard-shell, .dashboard-grid"));
 const dashboardAPIRequested = performance.getEntriesByType("resource").some(entry => {
   try {
     const path = new URL(String(entry.name || ""), href).pathname;
     return path === "/api/gb28181/sip/platform" ||
       path === "/api/gb28181/sip/dashboard/snapshot" ||
       path.startsWith("/api/gb28181/sip/dashboard/") ||
       path.startsWith("/api/gb28181/home/") ||
       path === "/api/gb28181/zlm/overview";
   } catch (_) {
     return false;
   }
 });
 const serverErrorToastVisible = Array.from(document.querySelectorAll(".arco-message, .arco-notification, [role=alert]")).some(node => {
   if (!visible(node)) return false;
   const text = String(node.textContent || "");
   return text.includes("服务器异常") || text.includes("请联系管理员");
 });
	return {
	  urlScrubbed: !href.includes("bootstrap_token="),
	  setupForm: setupForm,
		loginRoute: route.includes("/login"),
		homeRoute: route.includes("/home"),
		loginForm: loginForm,
		deploymentVisible: visible(deployment),
		networkFormVisible: visible(networkForm),
		networkSelectVisible: visible(networkSelect),
		mediaAddressesVisible: visible(mediaAddresses),
		identityFormVisible: visible(identityForm),
		confirmGroupsVisible: visible(confirmGroups),
		mediaInputCount: mediaInputCount,
		visibleOptionCount: visibleOptions,
		nextEnabled: visibleButton("下一步"),
		saveEnabled: visibleButton("保存并启动"),
   sipModalVisible: sipModalVisible,
   sipRequired: requiredCopy,
   sipSkipVisible: visible(sipSkip),
   sipCloseVisible: visible(sipClose),
   dashboardVisible: dashboardVisible,
   dashboardAPIRequested: dashboardAPIRequested,
   serverErrorToastVisible: serverErrorToastVisible
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
		if err == nil && state.URLScrubbed && state.LoginRoute && state.LoginForm {
			return nil
		}
		if err := t18WaitPoll(ctx); err != nil {
			return err
		}
	}
}

func t18WaitSIPRequired(ctx context.Context, cdp *t18CDP) error {
	for {
		state, err := cdp.evalState(ctx)
		if err == nil && !state.LoginRoute && state.URLScrubbed && state.SIPModalVisible && state.SIPRequired && !state.SIPSkipVisible && !state.SIPCloseVisible && !state.DashboardVisible && !state.DashboardAPIRequested && !state.ServerErrorToastVisible {
			return nil
		}
		if err := t18WaitPoll(ctx); err != nil {
			return err
		}
	}
}

func t19WaitStandaloneComplete(t *testing.T, ctx context.Context, client t18HTTPClient, cdp *t18CDP) error {
	t.Helper()
	for {
		status, headers, body := client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
		t18AssertResponseSafe(t, headers, body, "", "")
		phaseComplete := false
		if status == http.StatusOK {
			var setupStatus struct {
				Standalone bool   `json:"standalone"`
				Phase      string `json:"phase"`
			}
			if json.Unmarshal(body, &setupStatus) == nil && setupStatus.Standalone && setupStatus.Phase == "complete" {
				phaseComplete = true
			}
		}
		state, stateErr := cdp.evalState(ctx)
		if phaseComplete && stateErr == nil && state.URLScrubbed && state.HomeRoute && state.DashboardVisible && !state.ServerErrorToastVisible {
			return nil
		}
		if err := t18WaitPoll(ctx); err != nil {
			return err
		}
	}
}

func t19LogFlowFailure(t *testing.T, cdp *t18CDP, err error) {
	t.Helper()
	stage, kind := "unknown", "internal"
	var flowErr *t19BrowserFlowError
	if errors.As(err, &flowErr) {
		stage, kind = flowErr.stage, flowErr.kind
	}
	t.Logf("T19 SIP browser flow failed: stage=%s error=%s", stage, kind)
	t18LogBrowserState(t, cdp)
}

func t19ConcreteIPv4(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 3 || (len(part) > 1 && part[0] == '0') {
			return false
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
		}
		value, err := strconv.Atoi(part)
		if err != nil || value > 255 {
			return false
		}
	}
	first, _ := strconv.Atoi(parts[0])
	return first > 0 && first < 224
}

func t18LogBrowserState(t *testing.T, cdp *t18CDP) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	state, err := cdp.evalState(ctx)
	if err != nil {
		t.Log("browser DOM state unavailable")
		return
	}
	t.Logf("browser DOM state: scrubbed=%t setup=%t login_route=%t home_route=%t login_form=%t deployment=%t network=%t select=%t media=%t media_inputs=%d options=%d identity=%t confirm=%t next=%t save=%t sip_modal=%t sip_required=%t sip_skip=%t sip_close=%t dashboard=%t dashboard_api=%t server_error_toast=%t",
		state.URLScrubbed, state.SetupForm, state.LoginRoute, state.HomeRoute, state.LoginForm, state.DeploymentVisible, state.NetworkFormVisible, state.NetworkSelectVisible, state.MediaAddressesVisible, state.MediaInputCount, state.VisibleOptionCount, state.IdentityFormVisible, state.ConfirmGroupsVisible, state.NextEnabled, state.SaveEnabled, state.SIPModalVisible, state.SIPRequired, state.SIPSkipVisible, state.SIPCloseVisible, state.DashboardVisible, state.DashboardAPIRequested, state.ServerErrorToastVisible)
}
