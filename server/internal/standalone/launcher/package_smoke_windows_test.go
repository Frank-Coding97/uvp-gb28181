//go:build windows

package launcher

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestWindowsFinalPackageFreshBrowser(t *testing.T) {
	root := os.Getenv("UVP_FINAL_PACKAGE_ROOT")
	if root == "" {
		t.Skip("requires fresh extracted candidate ZIP")
	}
	urls := make(chan string, 1)
	run := t18Start(t, root, urls)
	defer func() { run.cancel(); t18WaitFinished(t, run, 90*time.Second) }()
	entry, ok := t18WaitBrowserEntry(run, urls, 90*time.Second)
	if !ok {
		t.Fatal("package did not publish setup entry")
	}
	base, origin, _, ok := t18BrowserEndpoint(entry, true)
	if !ok {
		t.Fatal("package did not start fresh")
	}
	client := t18HTTPClient{client: t18HTTPTransport(), baseURL: base, origin: origin}
	status, _, body := client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	if status != 200 {
		t.Fatal("setup unavailable")
	}
	t18AssertSetupStatus(t, body, true, "pending_admin")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	browser, err := t18StartHeadlessEdge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.close()
	if err = browser.navigate(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err = t19WaitEval(ctx, browser.cdp, `!!document.querySelector('input[type=password]') && document.body.innerText.includes('管理员')`); err != nil {
		t.Fatal("fresh setup page unavailable")
	}
	if err = browser.cdp.evalBool(ctx, `!document.querySelector('.arco-message-error')`); err != nil {
		t.Fatal("fresh setup page displayed an error")
	}
	if path := os.Getenv("UVP_FINAL_SCREENSHOT"); path != "" {
		if err = browser.captureScreenshot(ctx, path); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("FINAL_ZIP_FRESH_SETUP_BROWSER_READY")
}
