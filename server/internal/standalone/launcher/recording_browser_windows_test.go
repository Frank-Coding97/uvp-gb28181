//go:build windows

package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestWindowsStandaloneCloudRecordingBrowserDecode is an opt-in acceptance
// test for decoding an existing cloud-recording MP4 through a real Edge
// browser. It does not exercise the product recording-list UI.
func TestWindowsStandaloneCloudRecordingBrowserDecode(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("UVP_RECORD_BROWSER_ROOT"))
	if root == "" {
		t.Skip("requires an isolated installed cloud-recording fixture")
	}
	privatePath := strings.TrimSpace(os.Getenv("UVP_CORE_PRIVATE_FILE"))
	if privatePath == "" {
		t.Fatal("requires UVP_CORE_PRIVATE_FILE for private login credentials")
	}
	var credentials corePrivateCredentials
	raw, err := os.ReadFile(privatePath)
	if err != nil || json.Unmarshal(raw, &credentials) != nil || strings.TrimSpace(credentials.Username) == "" || credentials.Password == "" {
		t.Fatal("private login unavailable")
	}

	browserURLs := make(chan string, 1)
	run := t18StartWithRecordings(t, root, strings.TrimSpace(os.Getenv("UVP_RECORD_BROWSER_DIR")), browserURLs)
	defer func() {
		run.cancel()
		t18WaitFinished(t, run, 90*time.Second)
	}()
	entry, ok := t18WaitBrowserEntry(run, browserURLs, 90*time.Second)
	if !ok {
		t.Fatal("browser entry unavailable")
	}
	base, origin, _, ok := t18BrowserEndpoint(entry, false)
	if !ok {
		t.Fatal("browser entry was not a valid installed loopback URL")
	}
	t19WaitBusinessReady(t, run)

	client := t18HTTPClient{client: t18HTTPTransport(), baseURL: base, origin: origin}
	loginBody := t18AdminBody(t, credentials.Username, credentials.Password)
	status, headers, body := client.request(t, http.MethodPost, "/api/login", "", loginBody)
	t18AssertResponseSafe(t, headers, body, "", credentials.Password)
	var login struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &login) != nil || login.Data.AccessToken == "" {
		t.Fatal("cloud-recording browser login failed")
	}
	client.accessToken = login.Data.AccessToken

	status, headers, body = client.request(t, http.MethodGet, "/api/gb28181/cloud-recordings/files?page=1&pageSize=50", "", nil)
	t18AssertResponseSafe(t, headers, body, client.accessToken, credentials.Password)
	var page struct {
		Data struct {
			List []struct {
				ID        string `json:"id"`
				ChannelID string `json:"channelId"`
			} `json:"list"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &page) != nil {
		t.Fatal("cloud-recording file list unavailable")
	}
	fileID := ""
	channelID := uint(0)
	for _, file := range page.Data.List {
		parsedChannelID, parseErr := strconv.ParseUint(strings.TrimSpace(file.ChannelID), 10, 0)
		if strings.TrimSpace(file.ID) != "" && parseErr == nil && parsedChannelID > 0 {
			fileID = strings.TrimSpace(file.ID)
			channelID = uint(parsedChannelID)
			break
		}
	}
	if fileID == "" {
		t.Fatal("cloud-recording file list had no usable file and channel ID")
	}
	coreVerifyRecording(t, &client, channelID, &coreEvidence{Version: "uvp-record-browser-v1", Stage: "recording_browser"})

	accessBody, err := json.Marshal(map[string]string{"mode": "play"})
	if err != nil {
		t.Fatal("could not encode cloud-recording access request")
	}
	accessPath := "/api/gb28181/cloud-recordings/files/" + url.PathEscape(fileID) + "/access"
	status, headers, body = client.request(t, http.MethodPost, accessPath, "", accessBody)
	t18AssertResponseSafe(t, headers, body, client.accessToken, credentials.Password)
	var access struct {
		Data struct {
			Mode       string `json:"mode"`
			Capability string `json:"capability"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &access) != nil || access.Data.Mode != "play" || access.Data.Capability == "" {
		t.Fatal("cloud-recording play access was not issued")
	}

	contentURL := strings.TrimRight(base, "/") + "/api/gb28181/cloud-recordings/content/" + url.PathEscape(fileID) + "?cap=" + url.QueryEscape(access.Data.Capability)
	browserContext, cancelBrowser := context.WithTimeout(context.Background(), 150*time.Second)
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
	if err := browser.navigate(browserContext, base); err != nil {
		t.Fatal("could not open the installed loopback base")
	}
	if err := t19WaitEval(browserContext, browser.cdp, `document.readyState === 'complete'`); err != nil {
		t.Fatal("installed loopback base did not become ready")
	}
	if err := t19WaitEval(browserContext, browser.cdp, `!!document.querySelector('input[type=password]')`); err != nil {
		t.Fatal("installed loopback login page did not become ready")
	}

	contentURLJSON, err := json.Marshal(contentURL)
	if err != nil {
		t.Fatal("could not encode cloud-recording content URL")
	}
	placeVideo := "(() => {" +
		"const source = " + string(contentURLJSON) + ";" +
		"const video = document.createElement('video');" +
		"video.muted = true; video.autoplay = true; video.playsInline = true;" +
		"video.setAttribute('muted', ''); video.setAttribute('autoplay', ''); video.setAttribute('playsinline', '');" +
		"video.setAttribute('src', source);" +
		"document.body.replaceChildren(video);" +
		"const playback = video.play(); if (playback && typeof playback.catch === 'function') playback.catch(() => {});" +
		"return video.muted && video.autoplay && video.src === new URL(source, location.href).href;" +
		"})()"
	if err := browser.cdp.evalBool(browserContext, placeVideo); err != nil {
		t.Fatal("could not place the authorized cloud-recording video")
	}
	if err := t19WaitEval(browserContext, browser.cdp, `(() => { const video = document.querySelector('video'); return Boolean(video && video.readyState >= 2 && video.videoWidth > 0 && video.currentTime > 2); })()`); err != nil {
		t.Fatal("authorized cloud-recording MP4 did not decode in Edge")
	}
	t.Log("BROWSER_CLOUD_RECORDING_MP4_DECODED")

	if screenshotPath := strings.TrimSpace(os.Getenv("UVP_CORE_SCREENSHOT")); screenshotPath != "" {
		screenshotContext, cancelScreenshot := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelScreenshot()
		if err := browser.captureScreenshot(screenshotContext, screenshotPath); err != nil {
			t.Fatal("cloud-recording browser screenshot unavailable")
		}
	}
}
