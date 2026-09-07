//go:build windows

package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

const (
	t18AddressBrowserInstallDirEnv = "UVP_T18_ADDRESS_BROWSER_INSTALL_DIR"
	t18AddressSIPPort              = 15170
	t18AddressStaleSIP             = "192.0.2.123"
	t18AddressStaleReceive         = "198.51.100.123"
	t18AddressStalePlayback        = "203.0.113.123"
)

// TestWindowsStandaloneT18MovedInstallationAddressWarning is an opt-in native
// Edge regression test for a completed standalone package moved to a machine
// with different network addresses. The fixture must be fresh and disposable:
// this test creates its own administrator and SIP credentials, completes the
// first-install flow over HTTP, then stops the package before editing only the
// persisted advertised SIP and media addresses.
//
// The wildcard LAN listener is deliberately retained. It keeps the service
// able to start while the concrete advertised address is stale, so the test
// can prove both address warning behavior and the business-readiness fail-close
// contract without changing the host network.
func TestWindowsStandaloneT18MovedInstallationAddressWarning(t *testing.T) {
	installDir := strings.TrimSpace(os.Getenv(t18AddressBrowserInstallDirEnv))
	if installDir == "" {
		t.Skip("requires an explicitly prepared fresh t18-setup Windows fixture")
	}
	release, err := standalone.LoadRelease(installDir)
	if err != nil {
		t.Fatal("t18 address browser fixture release is unavailable")
	}
	if release.Version != t18BrowserReleaseVersion {
		t.Fatal("address browser test requires the isolated t18-setup release")
	}

	firstURLs := make(chan string, 1)
	first := t18Start(t, installDir, firstURLs)
	defer func() {
		if first != nil {
			first.cancel()
			if !t18WaitFinished(t, first, 90*time.Second) {
				t.Error("fresh address fixture cleanup exceeded its deadline")
			}
		}
	}()
	entry, ok := t18WaitBrowserEntry(first, firstURLs, 90*time.Second)
	if !ok {
		t.Fatal("fresh address fixture did not publish a browser entry")
	}
	baseURL, origin, bootstrapToken, ok := t18BrowserEndpoint(entry, true)
	if !ok {
		t.Fatal("fresh address fixture did not publish a valid loopback bootstrap URL")
	}
	client := t18HTTPClient{client: t18HTTPTransport(), baseURL: baseURL, origin: origin}
	status, headers, body := client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, "")
	if status != http.StatusOK {
		t.Fatalf("fresh setup status = %d, want %d", status, http.StatusOK)
	}
	t18AssertSetupStatus(t, body, true, "pending_admin")

	username := "t18-address-admin"
	adminPassword := t18RandomPassword(t)
	adminBody := t18AdminBody(t, username, adminPassword)
	status, headers, body = client.request(t, http.MethodPost, "/api/standalone/setup/admin", bootstrapToken, adminBody)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, adminPassword)
	if status != http.StatusCreated {
		t.Fatalf("fresh administrator setup = %d, want %d", status, http.StatusCreated)
	}
	t18AssertSetupStatus(t, body, true, "pending_sip")

	status, headers, body = client.request(t, http.MethodPost, "/api/login", "", adminBody)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, adminPassword)
	var login struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &login) != nil || login.Data.AccessToken == "" {
		t.Fatal("fresh address administrator could not log in")
	}
	client.accessToken = login.Data.AccessToken

	localIP := t18AddressSelectLocalIP(t, client, adminPassword)
	sipPassword := t18RandomPassword(t)
	configBody, err := json.Marshal(map[string]any{
		"deploymentMode":      "lan",
		"listenIp":            "0.0.0.0",
		"advertiseIp":         localIP,
		"advertiseIpInferred": false,
		"mediaReceiveHost":    localIP,
		"mediaPlaybackHost":   localIP,
		"port":                t18AddressSIPPort,
		"domain":              "3402000000",
		"serverId":            "34020000002000000001",
		"password":            sipPassword,
	})
	if err != nil {
		t.Fatal("could not encode fresh address SIP settings")
	}
	status, headers, body = client.request(t, http.MethodPut, "/api/gb28181/sip/setup/config", "", configBody)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, adminPassword)
	var saved struct {
		Data struct {
			ReloadedOK bool `json:"reloadedOk"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &saved) != nil || !saved.Data.ReloadedOK {
		t.Fatalf("fresh SIP activation failed, HTTP=%d", status)
	}
	sipPassword = ""
	adminBody = nil
	status, headers, body = client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, "", adminPassword)
	if status != http.StatusOK {
		t.Fatalf("fresh completed setup status = %d, want %d", status, http.StatusOK)
	}
	t18AssertSetupStatus(t, body, true, "complete")

	first.cancel()
	if !t18WaitFinished(t, first, 90*time.Second) {
		t.Fatal("fresh address fixture did not stop cleanly before the disk edit")
	}
	first = nil

	original := t18AddressMutateStoppedFixture(t, installDir)
	if original.SIPAdvertise == t18AddressStaleSIP || original.MediaReceive == t18AddressStaleReceive || original.MediaPlayback == t18AddressStalePlayback {
		t.Fatal("fresh fixture unexpectedly used one of the moved-address sentinels")
	}

	secondURLs := make(chan string, 1)
	second := t18Start(t, installDir, secondURLs)
	defer func() {
		if second != nil {
			second.cancel()
			if !t18WaitFinished(t, second, 90*time.Second) {
				t.Error("moved address fixture cleanup exceeded its deadline")
			}
		}
	}()
	secondEntry, ok := t18WaitBrowserEntry(second, secondURLs, 90*time.Second)
	if !ok {
		t.Fatal("moved address fixture did not publish a ready browser entry")
	}
	baseURL, origin, bootstrapToken, ok = t18BrowserEndpoint(secondEntry, false)
	if !ok || bootstrapToken != "" || strings.Contains(secondEntry, "bootstrap_token=") {
		t.Fatal("completed moved address fixture unexpectedly reopened bootstrap")
	}
	client.baseURL, client.origin = baseURL, origin

	browserContext, cancelBrowser := context.WithTimeout(context.Background(), 120*time.Second)
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
		t18LogBrowserState(t, browser.cdp)
		t.Fatal("could not navigate the owned browser to the completed package")
	}
	if err := t18WaitLoginRoute(browserContext, browser.cdp); err != nil {
		t18LogBrowserState(t, browser.cdp)
		t.Fatal("completed moved package did not show the login route")
	}
	if err := browser.loginAndSubmit(browserContext, username, adminPassword); err != nil {
		t18LogBrowserState(t, browser.cdp)
		t.Fatal("fresh address administrator could not log in after the move")
	}
	adminPassword = ""
	if err := t18WaitAddressWarning(browserContext, browser.cdp); err != nil {
		t18LogBrowserState(t, browser.cdp)
		t.Fatal("moved address warning did not become visible in the real Edge browser")
	}

	status, headers, body = client.request(t, http.MethodGet, "/api/gb28181/sip/setup/network-interfaces", "", nil)
	t18AssertResponseSafe(t, headers, body, "", "")
	if status != http.StatusOK {
		t.Fatalf("moved address interface scan = %d, want %d", status, http.StatusOK)
	}
	var interfaces struct {
		Data struct {
			Items []struct {
				IP string `json:"ip"`
			} `json:"items"`
			ScanStatus string `json:"scanStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &interfaces); err != nil || interfaces.Data.ScanStatus != "ok" {
		t.Fatalf("moved address interface scan was not successful: %s", body)
	}
	for _, item := range interfaces.Data.Items {
		if item.IP == t18AddressStaleSIP {
			t.Fatal("moved SIP sentinel unexpectedly appeared in the current interface list")
		}
	}

	sip := t18AddressReadSIPStatus(t, client)
	if sip.ListenIP != original.SIPListen || sip.AdvertiseIP != t18AddressStaleSIP {
		t.Fatalf("saved SIP addresses changed unexpectedly: listen=%q advertise=%q", sip.ListenIP, sip.AdvertiseIP)
	}
	if sip.RuntimeState != "running" {
		t.Fatalf("wildcard SIP listener did not remain running after the advertised address move: %q", sip.RuntimeState)
	}
	t18AddressAssertMediaAPI(t, client)
	t18WaitAddressBusinessFailClosed(t, second, 30*time.Second)

	second.cancel()
	if !t18WaitFinished(t, second, 90*time.Second) {
		t.Fatal("moved address fixture did not stop cleanly")
	}
	second = nil
	t18AddressAssertPersistedAfterMove(t, installDir, original)
}

func t18AddressSelectLocalIP(t *testing.T, client t18HTTPClient, adminPassword string) string {
	t.Helper()
	status, headers, body := client.request(t, http.MethodGet, "/api/gb28181/sip/setup/network-interfaces", "", nil)
	t18AssertResponseSafe(t, headers, body, "", adminPassword)
	if status != http.StatusOK {
		t.Fatalf("fresh interface scan = %d, want %d", status, http.StatusOK)
	}
	var response struct {
		Data struct {
			Items []struct {
				IP          string `json:"ip"`
				Loopback    bool   `json:"loopback"`
				ListenOnly  bool   `json:"listenOnly"`
				Recommended bool   `json:"recommended"`
			} `json:"items"`
			ScanStatus string `json:"scanStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Data.ScanStatus != "ok" {
		t.Fatal("fresh interface scan was not successful")
	}
	fallback := ""
	for _, item := range response.Data.Items {
		if item.Loopback || item.ListenOnly || item.IP == "0.0.0.0" || !t19ConcreteIPv4(item.IP) {
			continue
		}
		if fallback == "" {
			fallback = item.IP
		}
		if item.Recommended {
			return item.IP
		}
	}
	if fallback == "" {
		t.Fatal("fresh fixture has no concrete non-loopback IPv4 address")
	}
	return fallback
}

type t18AddressOriginal struct {
	SIPListen     string
	SIPAdvertise  string
	MediaReceive  string
	MediaPlayback string
	NodeID        int64
}

func t18AddressMutateStoppedFixture(t *testing.T, installDir string) t18AddressOriginal {
	t.Helper()
	release, err := standalone.LoadRelease(installDir)
	if err != nil {
		t.Fatal("reloading address fixture release failed")
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{
		InstallDir:  installDir,
		ConfigDir:   filepath.Join(installDir, "config"),
		DataDir:     filepath.Join(installDir, "data"),
		ResourceDir: release.ResourceDir,
		WebDir:      release.WebDir,
	})
	if err != nil {
		t.Fatal("resolving address fixture database path failed")
	}
	db, err := gormhelper.NewSQLiteClient(paths.DatabasePath)
	if err != nil {
		t.Fatal("opening stopped address fixture database failed")
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatal("opening stopped address fixture SQL handle failed")
	}
	defer raw.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var sipRows []struct {
		ID          uint8  `gorm:"column:id"`
		ListenIP    string `gorm:"column:listen_ip"`
		AdvertiseIP string `gorm:"column:advertise_ip"`
	}
	if result := db.WithContext(ctx).Table("gb_sip_config").Select("id, listen_ip, advertise_ip").Find(&sipRows); result.Error != nil {
		t.Fatalf("reading stopped SIP configuration failed: %v", result.Error)
	}
	if len(sipRows) != 1 {
		t.Fatalf("expected exactly one saved SIP configuration, got %d", len(sipRows))
	}
	if sipRows[0].ListenIP != "0.0.0.0" || sipRows[0].AdvertiseIP == "" {
		t.Fatalf("fresh SIP configuration did not preserve the wildcard LAN contract: listen=%q advertise=%q", sipRows[0].ListenIP, sipRows[0].AdvertiseIP)
	}

	var nodes []struct {
		ID           int64  `gorm:"column:id"`
		ReceiveHost  string `gorm:"column:receive_host"`
		PlaybackHost string `gorm:"column:playback_host"`
	}
	if result := db.WithContext(ctx).Table("meta_node").Select("id, receive_host, playback_host").Order("id").Find(&nodes); result.Error != nil {
		t.Fatalf("reading stopped media node failed: %v", result.Error)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected exactly one saved local media node, got %d", len(nodes))
	}

	original := t18AddressOriginal{
		SIPListen:     sipRows[0].ListenIP,
		SIPAdvertise:  sipRows[0].AdvertiseIP,
		MediaReceive:  nodes[0].ReceiveHost,
		MediaPlayback: nodes[0].PlaybackHost,
		NodeID:        nodes[0].ID,
	}
	if result := db.WithContext(ctx).Table("gb_sip_config").Where("id = ?", sipRows[0].ID).Updates(map[string]any{"advertise_ip": t18AddressStaleSIP}); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("rewriting the stopped SIP advertised address failed: %v rows=%d", result.Error, result.RowsAffected)
	}
	if result := db.WithContext(ctx).Table("meta_node").Where("id = ?", nodes[0].ID).Updates(map[string]any{
		"receive_host":  t18AddressStaleReceive,
		"playback_host": t18AddressStalePlayback,
	}); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("rewriting the stopped media addresses failed: %v rows=%d", result.Error, result.RowsAffected)
	}
	return original
}

type t18AddressSIPSnapshot struct {
	ListenIP     string
	AdvertiseIP  string
	RuntimeState string
}

func t18AddressReadSIPStatus(t *testing.T, client t18HTTPClient) t18AddressSIPSnapshot {
	t.Helper()
	status, headers, body := client.request(t, http.MethodGet, "/api/gb28181/sip/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, "", "")
	if status != http.StatusOK {
		t.Fatalf("moved SIP status = %d, want %d", status, http.StatusOK)
	}
	var response struct {
		Data struct {
			Config *struct {
				ListenIP    string `json:"listenIp"`
				AdvertiseIP string `json:"advertiseIp"`
			} `json:"config"`
			Runtime struct {
				State string `json:"state"`
			} `json:"runtime"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Data.Config == nil {
		t.Fatal("moved SIP status has no usable config")
	}
	return t18AddressSIPSnapshot{
		ListenIP:     response.Data.Config.ListenIP,
		AdvertiseIP:  response.Data.Config.AdvertiseIP,
		RuntimeState: response.Data.Runtime.State,
	}
}

func t18AddressAssertMediaAPI(t *testing.T, client t18HTTPClient) {
	t.Helper()
	status, headers, body := client.request(t, http.MethodGet, "/api/gb28181/zlm/nodes", "", nil)
	t18AssertResponseSafe(t, headers, body, "", "")
	if status != http.StatusOK {
		t.Fatalf("moved media node API = %d, want %d", status, http.StatusOK)
	}
	var response struct {
		Data struct {
			List []struct {
				ID           int64  `json:"id"`
				ReceiveHost  string `json:"receiveHost"`
				PlaybackHost string `json:"playbackHost"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil || len(response.Data.List) != 1 {
		t.Fatal("moved media node API returned an unexpected list")
	}
	node := response.Data.List[0]
	if node.ReceiveHost != t18AddressStaleReceive || node.PlaybackHost != t18AddressStalePlayback {
		t.Fatalf("saved media addresses were silently overwritten: receive=%q playback=%q", node.ReceiveHost, node.PlaybackHost)
	}
}

func t18AddressAssertPersistedAfterMove(t *testing.T, installDir string, original t18AddressOriginal) {
	t.Helper()
	release, err := standalone.LoadRelease(installDir)
	if err != nil {
		t.Fatal("reloading moved address fixture release failed")
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{
		InstallDir:  installDir,
		ConfigDir:   filepath.Join(installDir, "config"),
		DataDir:     filepath.Join(installDir, "data"),
		ResourceDir: release.ResourceDir,
		WebDir:      release.WebDir,
	})
	if err != nil {
		t.Fatal("resolving moved address fixture database path failed")
	}
	db, err := gormhelper.NewSQLiteClient(paths.DatabasePath)
	if err != nil {
		t.Fatal("reopening moved address fixture database failed")
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatal("opening moved address fixture SQL handle failed")
	}
	defer raw.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var sip struct {
		ListenIP    string `gorm:"column:listen_ip"`
		AdvertiseIP string `gorm:"column:advertise_ip"`
	}
	if result := db.WithContext(ctx).Table("gb_sip_config").Select("listen_ip, advertise_ip").Where("id = ?", 1).Take(&sip); result.Error != nil {
		t.Fatalf("reading moved SIP configuration failed: %v", result.Error)
	}
	if sip.ListenIP != original.SIPListen || sip.AdvertiseIP != t18AddressStaleSIP {
		t.Fatalf("moved SIP values were not retained: listen=%q advertise=%q", sip.ListenIP, sip.AdvertiseIP)
	}
	var node struct {
		ID           int64  `gorm:"column:id"`
		ReceiveHost  string `gorm:"column:receive_host"`
		PlaybackHost string `gorm:"column:playback_host"`
	}
	if result := db.WithContext(ctx).Table("meta_node").Where("id = ?", original.NodeID).Take(&node); result.Error != nil {
		t.Fatalf("reading moved media node failed: %v", result.Error)
	}
	if node.ReceiveHost != t18AddressStaleReceive || node.PlaybackHost != t18AddressStalePlayback {
		t.Fatalf("moved media values were not retained: receive=%q playback=%q", node.ReceiveHost, node.PlaybackHost)
	}
}

func t18WaitAddressWarning(ctx context.Context, cdp *t18CDP) error {
	if cdp == nil {
		return errors.New("browser protocol is unavailable")
	}
	const expression = `(() => {
	 const visible = node => {
	   if (!node) return false;
	   const style = getComputedStyle(node);
	   return style.display !== "none" && style.visibility !== "hidden" &&
	     (node.offsetWidth > 0 || node.offsetHeight > 0 || node.getClientRects().length > 0);
	 };
	 const warning = document.querySelector(".sip-modal-address-warning");
	 if (!visible(warning)) return false;
	 const text = String(warning.textContent || "");
	 if (!text.includes("SIP 接入地址已变化") ||
	     !text.includes("已保存的本机 SIP 地址不在当前网卡列表中") ||
	     !text.includes("当前已保存配置仍保留，未被自动修改")) return false;
	 return !Array.from(document.querySelectorAll(".arco-message, .arco-notification, [role=alert]")).some(node =>
	   visible(node) && (String(node.textContent || "").includes("服务器异常") || String(node.textContent || "").includes("请联系管理员")));
	})()`
	for {
		if err := cdp.evalBool(ctx, expression); err == nil {
			return nil
		}
		if err := t18WaitPoll(ctx); err != nil {
			return err
		}
	}
}

func t18WaitAddressBusinessFailClosed(t *testing.T, run *t18Launch, timeout time.Duration) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case state, ok := <-run.statuses:
			if !ok {
				t.Fatal("moved address launcher status stream closed before readiness was classified")
			}
			if state.State != Ready {
				continue
			}
			if state.BusinessReady {
				t.Fatalf("stale local media addresses were incorrectly reported business-ready: reason=%q", state.BusinessReason)
			}
			if state.BusinessReason == "media_address_unavailable" {
				return
			}
		case <-timer.C:
			t.Fatal("moved address readiness did not report media_address_unavailable within the deadline")
		}
	}
}
