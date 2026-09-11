//go:build windows

package launcher

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestWindowsStandaloneDeviceBrowser(t *testing.T) {
	root := os.Getenv("UVP_CORE_BROWSER_ROOT")
	if root == "" {
		t.Skip("requires completed isolated core fixture")
	}
	var credentials corePrivateCredentials
	raw, err := os.ReadFile(os.Getenv("UVP_CORE_PRIVATE_FILE"))
	if err != nil || json.Unmarshal(raw, &credentials) != nil {
		t.Fatal("private login unavailable")
	}
	urls := make(chan string, 1)
	run := t18Start(t, root, urls)
	defer func() { run.cancel(); t18WaitFinished(t, run, 90*time.Second) }()
	entry, ok := t18WaitBrowserEntry(run, urls, 90*time.Second)
	if !ok {
		t.Fatal("browser entry unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	browser, err := t18StartHeadlessEdge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.close()
	if err = browser.navigate(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err = t19WaitEval(ctx, browser.cdp, `!!document.querySelector('input[type=password]')`); err != nil {
		t.Fatal(err)
	}
	if err = browser.loginAndSubmit(ctx, credentials.Username, credentials.Password); err != nil {
		t.Fatal(err)
	}
	if err = t19WaitEval(ctx, browser.cdp, `!document.querySelector('input[type=password]') && !location.hash.includes('login')`); err != nil {
		t.Fatal(err)
	}
	base, _, _, ok := t18BrowserEndpoint(entry, false)
	if !ok {
		t.Fatal("base unavailable")
	}
	if err = browser.navigate(ctx, base+"/#/gb28181/device-mgmt/index"); err != nil {
		t.Fatal(err)
	}
	t.Log("BROWSER_DEVICE_PAGE_READY")
	if marker := os.Getenv("UVP_CORE_BROWSER_SIM_READY"); marker != "" {
		if !coreWaitForFile(marker, time.Minute) {
			t.Fatal("external simulator readiness marker missing")
		}
	}
	if err = t19WaitClickText(ctx, browser.cdp, "button", "通道"); err != nil {
		t.Fatal("channel view unavailable")
	}
	defer func() {
		capture, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = browser.captureScreenshot(capture, os.Getenv("UVP_CORE_SCREENSHOT"))
	}()
	if err = t19WaitClickText(ctx, browser.cdp, ".uvp-table-action--preview", "播放"); err != nil {
		t.Fatal("device play control unavailable")
	}
	if err = t19WaitEval(ctx, browser.cdp, `Array.from(document.querySelectorAll('video')).some(v => v.readyState >= 2 && v.videoWidth > 0 && v.currentTime > 2)`); err != nil {
		t.Fatal("video did not decode")
	}
	t.Log("BROWSER_VIDEO_DECODED")
}
