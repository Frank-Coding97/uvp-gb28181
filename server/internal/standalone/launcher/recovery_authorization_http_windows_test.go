//go:build windows

package launcher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

const (
	recoveryAuthorizationCaseEnv     = "UVP_RECOVERY_AUTHORIZATION_CASE"
	recoveryAuthorizationCandidate   = "2.0.0-win10"
	recoveryAuthorizationDevice      = "34020000002000000071"
	recoveryAuthorizationChannel     = "34020000002000000072"
	recoveryAuthorizationServerID    = "34020000002000000001"
	recoveryAuthorizationDomain      = "3402000000"
	recoveryAuthorizationHTTPHost    = "127.0.0.1"
	recoveryAuthorizationTestTimeout = 8 * time.Minute
)

type recoveryAuthorizationTokenPair struct {
	AccessToken  string
	RefreshToken string
}

type recoveryAuthorizationQR struct {
	Token            string
	ExpiresInSeconds int
}

type recoveryAuthorizationPlayResult struct {
	HTTPFlvURL string `json:"httpFlvUrl"`
	URLs       struct {
		HTTPFlvURL string `json:"httpFlv"`
	} `json:"urls"`
}

// TestWindowsRecoveryAuthorizationHTTP exercises the real login, session,
// QR and fixed playback authorization endpoints around a stopped recovery.
// The Hook assertion intentionally covers only authorization admission; it
// does not claim that a physical device or media bytes were present.
func TestWindowsRecoveryAuthorizationHTTP(t *testing.T) {
	root := strings.TrimSpace(os.Getenv(maintenanceChildTestRootEnv))
	if root == "" {
		t.Skip("requires isolated recovery authorization driver")
	}
	testCase := strings.TrimSpace(os.Getenv(recoveryAuthorizationCaseEnv))
	if testCase == "" {
		testCase = "complete"
	}
	switch testCase {
	case "complete", "unclean", "write-load":
	default:
		t.Fatalf("unsupported recovery authorization case %q", testCase)
	}

	current, err := standalone.LoadRelease(root)
	require.NoError(t, err)
	candidate, err := standalone.LoadReleaseVersion(root, recoveryAuthorizationCandidate)
	require.NoError(t, err)
	paths, err := standalone.ResolvePaths(standalone.PathOptions{
		InstallDir:    root,
		ConfigDir:     filepath.Join(root, "config"),
		DataDir:       filepath.Join(root, "data"),
		ResourceDir:   current.ResourceDir,
		WebDir:        current.WebDir,
		RecordingsDir: filepath.Join(root, "recordings"),
	})
	require.NoError(t, err)
	require.NotEmpty(t, candidate.Version)

	adminUsername := "t26-admin"
	adminPassword := t18RandomPassword(t)
	sipPassword := t18RandomPassword(t)
	adminBody := t18AdminBody(t, adminUsername, adminPassword)

	runs := make([]*t18Launch, 0, 5)
	defer func() {
		for _, run := range runs {
			run.cancel()
			t18WaitFinished(t, run, 90*time.Second)
		}
	}()

	firstURLs := make(chan string, 1)
	first := t18StartWithRecordings(t, root, paths.RecordingsDir, firstURLs)
	runs = append(runs, first)
	firstEntry, ok := t18WaitBrowserEntry(first, firstURLs, 90*time.Second)
	require.True(t, ok)
	baseURL, origin, bootstrapToken, ok := t18BrowserEndpoint(firstEntry, true)
	require.True(t, ok)
	client := t18HTTPClient{client: t18HTTPTransport(), baseURL: baseURL, origin: origin}

	status, headers, body := client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, adminPassword)
	t26RequireHTTPStatus(t, "/api/standalone/setup/status", status, http.StatusOK)
	t18AssertSetupStatus(t, body, true, "pending_admin")
	status, headers, body = client.request(t, http.MethodPost, "/api/standalone/setup/admin", bootstrapToken, adminBody)
	t18AssertResponseSafe(t, headers, body, bootstrapToken, adminPassword)
	t26RequireHTTPStatus(t, "/api/standalone/setup/admin", status, http.StatusCreated)
	t18AssertSetupStatus(t, body, true, "pending_sip")

	firstPair := t26LoginPair(t, client, adminUsername, adminPassword)
	client.accessToken = firstPair.AccessToken
	if testCase == "write-load" {
		t27SQLiteWriteLoadKill(t, paths, first, client, adminBody)
		return
	}
	first.cancel()
	require.True(t, t18WaitFinished(t, first, 90*time.Second))

	secondURLs := make(chan string, 1)
	second := t18StartWithRecordings(t, root, paths.RecordingsDir, secondURLs)
	runs = append(runs, second)
	secondEntry, ok := t18WaitBrowserEntry(second, secondURLs, 90*time.Second)
	require.True(t, ok)
	baseURL, origin, _, ok = t18BrowserEndpoint(secondEntry, false)
	require.True(t, ok)
	client.baseURL, client.origin = baseURL, origin
	status, headers, body = client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, "", adminPassword)
	t26RequireHTTPStatus(t, "/api/standalone/setup/status", status, http.StatusOK)
	t18AssertSetupStatus(t, body, true, "pending_sip")

	secondPair := t26LoginPair(t, client, adminUsername, adminPassword)
	client.accessToken = secondPair.AccessToken
	localIP := t18AddressSelectLocalIP(t, client, adminPassword)
	sipPort := t26FindDualPort(t)
	configBody, err := json.Marshal(map[string]any{
		"deploymentMode":    "lan",
		"listenIp":          "0.0.0.0",
		"advertiseIp":       localIP,
		"port":              sipPort,
		"domain":            recoveryAuthorizationDomain,
		"serverId":          recoveryAuthorizationServerID,
		"password":          sipPassword,
		"mediaReceiveHost":  localIP,
		"mediaPlaybackHost": localIP,
	})
	require.NoError(t, err)
	status, headers, body = client.request(t, http.MethodPut, "/api/gb28181/sip/setup/config", "", configBody)
	// The authorized setup response may include the device registration
	// password by design; the administrator and bootstrap credentials remain
	// protected by this assertion.
	t18AssertResponseSafe(t, headers, body, "", adminPassword)
	t26RequireHTTPStatus(t, "/api/gb28181/sip/setup/config", status, http.StatusOK)
	var saved struct {
		Data struct {
			ReloadedOK bool `json:"reloadedOk"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &saved))
	require.True(t, saved.Data.ReloadedOK)

	status, headers, body = client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, "", adminPassword)
	t26RequireHTTPStatus(t, "/api/standalone/setup/status", status, http.StatusOK)
	t18AssertSetupStatus(t, body, true, "complete")
	t19WaitBusinessReady(t, second)
	mediaUUID := t19AssertLocalMediaNode(t, client, localIP)

	fixedBody := []byte(`{"fixedAddressEnabled":true,"autoOnDemandEnabled":false}`)
	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPut, "/api/gb28181/sip/service-config/fixed-address-playback", secondPair.AccessToken, "", fixedBody)
	t26AssertNoSecret(t, headers, body, adminPassword, sipPassword)
	t26RequireHTTPStatus(t, "/api/gb28181/sip/service-config/fixed-address-playback", status, http.StatusOK)

	second.cancel()
	require.True(t, t18WaitFinished(t, second, 90*time.Second))
	t26SeedDeviceAndChannel(t, paths.DatabasePath, adminUsername, recoveryAuthorizationDevice, recoveryAuthorizationChannel)

	thirdURLs := make(chan string, 1)
	third := t18StartWithRecordings(t, root, paths.RecordingsDir, thirdURLs)
	runs = append(runs, third)
	thirdEntry, ok := t18WaitBrowserEntry(third, thirdURLs, 90*time.Second)
	require.True(t, ok)
	baseURL, origin, _, ok = t18BrowserEndpoint(thirdEntry, false)
	require.True(t, ok)
	client.baseURL, client.origin = baseURL, origin
	status, headers, body = client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, "", adminPassword)
	t26RequireHTTPStatus(t, "/api/standalone/setup/status", status, http.StatusOK)
	t18AssertSetupStatus(t, body, true, "complete")
	oldPair := t26LoginPair(t, client, adminUsername, adminPassword)
	client.accessToken = oldPair.AccessToken
	t19WaitBusinessReady(t, third)

	oldCapabilityStarted := time.Now()
	oldQR := t26GenerateQR(t, client, oldPair.AccessToken, adminPassword, sipPassword)
	require.NotEmpty(t, oldQR.Token)

	if testCase == "unclean" {
		third.cancel()
		require.True(t, t18WaitFinished(t, third, 90*time.Second))
		t26RunUncleanAOFRecovery(t, root, paths, adminUsername, adminPassword, oldPair, oldQR, oldCapabilityStarted, &client, &runs)
		return
	}
	oldPlayToken := t26AuthorizeFixedPlayback(t, client, oldPair.AccessToken, adminPassword, sipPassword)
	require.NotEmpty(t, oldPlayToken)
	third.cancel()
	require.True(t, t18WaitFinished(t, third, 90*time.Second))

	config, err := standalone.LoadConfig(paths)
	require.NoError(t, err)
	blocker, err := net.Listen("tcp4", config.MediaAddress())
	require.NoError(t, err)
	blockerClosed := false
	defer func() {
		if !blockerClosed {
			_ = blocker.Close()
		}
	}()

	upgradeCtx, upgradeCancel := context.WithTimeout(context.Background(), recoveryAuthorizationTestTimeout)
	destination := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-t26-recovery-backup")
	upgradeErr := UpgradeStopped(upgradeCtx, root, paths.RecordingsDir, candidate.Version, destination)
	upgradeCancel()
	require.Error(t, upgradeErr)
	failureJournal, err := standalone.ReadMaintenanceJournal(root)
	require.NoError(t, err)
	require.Equal(t, standalone.MaintenanceRestoreRequired, failureJournal.Phase)
	require.Equal(t, current.Version, failureJournal.OldVersion)
	require.NoError(t, blocker.Close())
	blockerClosed = true

	restoreCtx, restoreCancel := context.WithTimeout(context.Background(), recoveryAuthorizationTestTimeout)
	restoredJournal, err := RestoreStopped(restoreCtx, root, paths.RecordingsDir)
	restoreCancel()
	require.NoError(t, err)
	require.Equal(t, standalone.MaintenanceAwaitingConfirmation, restoredJournal.Phase)
	require.Equal(t, failureJournal.OperationID, restoredJournal.OperationID)

	confirmCtx, confirmCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	err = standalone.ConfirmRecovery(confirmCtx, root, restoredJournal.OperationID,
		func(info standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error) {
			require.Equal(t, restoredJournal.OperationID, info.OperationID)
			require.False(t, info.BackupTime.IsZero())
			return standalone.RecoveryConfirmationInput{
				Username:        adminUsername,
				Password:        adminPassword,
				Acknowledgement: "CONFIRM " + restoredJournal.OperationID,
			}, nil
		})
	confirmCancel()
	require.NoError(t, err)
	require.NoError(t, standalone.CheckMaintenanceGate(root))
	selected, err := standalone.LoadRelease(root)
	require.NoError(t, err)
	require.Equal(t, current.Version, selected.Version)

	fourthURLs := make(chan string, 1)
	fourth := t18StartWithRecordings(t, root, paths.RecordingsDir, fourthURLs)
	runs = append(runs, fourth)
	fourthEntry, ok := t18WaitBrowserEntry(fourth, fourthURLs, 90*time.Second)
	require.True(t, ok)
	baseURL, origin, _, ok = t18BrowserEndpoint(fourthEntry, false)
	require.True(t, ok)
	client.baseURL, client.origin = baseURL, origin
	status, headers, body = client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, "", adminPassword)
	t26RequireHTTPStatus(t, "/api/standalone/setup/status", status, http.StatusOK)
	t18AssertSetupStatus(t, body, true, "complete")

	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/users/session/heartbeat", oldPair.AccessToken, "", nil)
	t26AssertNoSecret(t, headers, body, oldPair.AccessToken, oldPair.RefreshToken, adminPassword, sipPassword)
	t26RequireHTTPStatus(t, "/api/users/session/heartbeat", status, http.StatusUnauthorized)
	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/refreshToken", "", oldPair.RefreshToken, nil)
	t26AssertNoSecret(t, headers, body, oldPair.AccessToken, oldPair.RefreshToken, adminPassword, sipPassword)
	t26RequireHTTPStatus(t, "/api/refreshToken", status, http.StatusUnauthorized)

	newPair := t26LoginPair(t, client, adminUsername, adminPassword)
	if oldPair.AccessToken == newPair.AccessToken || oldPair.RefreshToken == newPair.RefreshToken {
		t.Fatal("recovery login reused a previous token")
	}
	refreshedPair := t26RefreshPair(t, client, newPair, adminPassword, sipPassword)
	if newPair.AccessToken == refreshedPair.AccessToken || newPair.RefreshToken == refreshedPair.RefreshToken {
		t.Fatal("refresh endpoint reused a previous token")
	}
	newPair = refreshedPair
	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/users/session/heartbeat", newPair.AccessToken, "", nil)
	t26AssertNoSecret(t, headers, body, newPair.AccessToken, newPair.RefreshToken, adminPassword, sipPassword)
	t26RequireHTTPStatus(t, "/api/users/session/heartbeat", status, http.StatusOK)

	oldQRBody, err := json.Marshal(map[string]string{"token": oldQR.Token})
	require.NoError(t, err)
	if time.Since(oldCapabilityStarted) >= time.Duration(oldQR.ExpiresInSeconds)*time.Second {
		t.Fatal("old QR expired before rejection check; recovery revocation is unproven")
	}
	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/gb28181/sip/qr/exchange", "", "", oldQRBody)
	if time.Since(oldCapabilityStarted) >= time.Duration(oldQR.ExpiresInSeconds)*time.Second {
		t.Fatal("old QR expired while the rejection request was in flight; recovery revocation is unproven")
	}
	t26AssertNoSecret(t, headers, body, oldQR.Token, oldPair.AccessToken, oldPair.RefreshToken, adminPassword, sipPassword)
	t26RequireHTTPStatus(t, "/api/gb28181/sip/qr/exchange", status, http.StatusGone)

	newQR := t26GenerateQR(t, client, newPair.AccessToken, adminPassword, sipPassword)
	newQRBody, err := json.Marshal(map[string]string{"token": newQR.Token})
	require.NoError(t, err)
	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/gb28181/sip/qr/exchange", "", "", newQRBody)
	t26AssertNoSecret(t, headers, body, newQR.Token, newPair.AccessToken, newPair.RefreshToken, adminPassword)
	t26RequireHTTPStatus(t, "/api/gb28181/sip/qr/exchange", status, http.StatusOK)
	var exchanged struct {
		Data struct {
			ServerID  string `json:"serverId"`
			Domain    string `json:"domain"`
			IP        string `json:"ip"`
			Port      int    `json:"port"`
			Password  string `json:"password"`
			Transport string `json:"transport"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &exchanged))
	require.Equal(t, recoveryAuthorizationServerID, exchanged.Data.ServerID)
	require.Equal(t, recoveryAuthorizationDomain, exchanged.Data.Domain)
	require.NotEmpty(t, exchanged.Data.IP)
	require.Positive(t, exchanged.Data.Port)
	require.NotEmpty(t, exchanged.Data.Transport)
	if exchanged.Data.Password != sipPassword {
		t.Fatal("recovery QR exchange returned the wrong SIP password")
	}
	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/gb28181/sip/qr/exchange", "", "", newQRBody)
	t26AssertNoSecret(t, headers, body, newQR.Token, newPair.AccessToken, newPair.RefreshToken, adminPassword, sipPassword)
	t26RequireHTTPStatus(t, "/api/gb28181/sip/qr/exchange", status, http.StatusGone)

	newPlayToken := t26AuthorizeFixedPlayback(t, client, newPair.AccessToken, adminPassword, sipPassword)
	require.NotEmpty(t, newPlayToken)
	if oldPlayToken == newPlayToken {
		t.Fatal("recovery playback authorization reused a previous token")
	}

	config, err = standalone.LoadConfig(paths)
	require.NoError(t, err)
	hookCapability, err := playauth.HookCapability(config.ZLMSecret(), mediaUUID, playauth.HookOnPlay)
	require.NoError(t, err)
	hookQuery := url.Values{"node": {mediaUUID}, "cap": {hookCapability}}
	hookBody, err := json.Marshal(map[string]string{
		"app":           "rtp",
		"stream":        recoveryAuthorizationDevice + "_" + recoveryAuthorizationChannel,
		"schema":        "fmp4",
		"vhost":         "__defaultVhost__",
		"params":        "?" + url.Values{playauth.QueryParameter: {oldPlayToken}}.Encode(),
		"mediaServerId": mediaUUID,
		"ip":            recoveryAuthorizationHTTPHost,
	})
	require.NoError(t, err)
	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/index/hook/on_play?"+hookQuery.Encode(), "", "", hookBody)
	if time.Since(oldCapabilityStarted) >= playauth.DefaultTTL {
		t.Fatal("old playback capability expired before rejection check; recovery revocation is unproven")
	}
	t26AssertNoSecret(t, headers, body, oldPlayToken, newPlayToken, oldPair.AccessToken, newPair.AccessToken, sipPassword)
	t26RequireHTTPStatus(t, "/index/hook/on_play", status, http.StatusOK)
	var hookResult struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(body, &hookResult))
	require.Equal(t, -1, hookResult.Code)

	fourth.cancel()
	require.True(t, t18WaitFinished(t, fourth, 90*time.Second))
}

func t26LoginPair(t *testing.T, client t18HTTPClient, username, password string) recoveryAuthorizationTokenPair {
	t.Helper()
	body := t18AdminBody(t, username, password)
	status, headers, raw := t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/login", "", "", body)
	t26AssertNoSecret(t, headers, raw, password)
	t26RequireHTTPStatus(t, "/api/login", status, http.StatusOK)
	var response struct {
		Code int `json:"code"`
		Data struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	require.Zero(t, response.Code)
	require.NotEmpty(t, response.Data.AccessToken)
	require.NotEmpty(t, response.Data.RefreshToken)
	return recoveryAuthorizationTokenPair{AccessToken: response.Data.AccessToken, RefreshToken: response.Data.RefreshToken}
}

func t26RefreshPair(t *testing.T, client t18HTTPClient, previous recoveryAuthorizationTokenPair, adminPassword, sipPassword string) recoveryAuthorizationTokenPair {
	t.Helper()
	status, headers, raw := t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/refreshToken", "", previous.RefreshToken, nil)
	t26AssertNoSecret(t, headers, raw, previous.AccessToken, previous.RefreshToken, adminPassword, sipPassword)
	t26RequireHTTPStatus(t, "/api/refreshToken", status, http.StatusOK)
	var response struct {
		Code int `json:"code"`
		Data struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	require.Zero(t, response.Code)
	require.NotEmpty(t, response.Data.AccessToken)
	require.NotEmpty(t, response.Data.RefreshToken)
	return recoveryAuthorizationTokenPair{AccessToken: response.Data.AccessToken, RefreshToken: response.Data.RefreshToken}
}

func t26GenerateQR(t *testing.T, client t18HTTPClient, accessToken, adminPassword, sipPassword string) recoveryAuthorizationQR {
	t.Helper()
	status, headers, body := t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/gb28181/sip/qr/token", accessToken, "", nil)
	t26AssertNoSecret(t, headers, body, accessToken, adminPassword, sipPassword)
	t26RequireHTTPStatus(t, "/api/gb28181/sip/qr/token", status, http.StatusOK)
	var response struct {
		Code int                     `json:"code"`
		Data recoveryAuthorizationQR `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	require.Zero(t, response.Code)
	if len(response.Data.Token) != 22 {
		t.Fatal("QR token has an invalid length")
	}
	require.Positive(t, response.Data.ExpiresInSeconds)
	return response.Data
}

func t26AuthorizeFixedPlayback(t *testing.T, client t18HTTPClient, accessToken, adminPassword, sipPassword string) string {
	t.Helper()
	path := "/api/gb28181/play/" + recoveryAuthorizationDevice + "/" + recoveryAuthorizationChannel + "/authorization"
	status, headers, body := t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, path, accessToken, "", nil)
	t26AssertNoSecret(t, headers, body, accessToken, adminPassword, sipPassword)
	t26RequireHTTPStatus(t, "/api/gb28181/play/:deviceId/:channelId/authorization", status, http.StatusOK)
	var response struct {
		Code int                             `json:"code"`
		Data recoveryAuthorizationPlayResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	require.Zero(t, response.Code)
	return t26ExtractPlayToken(t, response.Data)
}

func t26ExtractPlayToken(t *testing.T, result recoveryAuthorizationPlayResult) string {
	t.Helper()
	rawURL := result.HTTPFlvURL
	if rawURL == "" {
		rawURL = result.URLs.HTTPFlvURL
	}
	require.NotEmpty(t, rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal("fixed playback response contained an invalid URL")
	}
	token := parsed.Query().Get(playauth.QueryParameter)
	require.NotEmpty(t, token)
	return token
}

func t26RequireHTTPStatus(t *testing.T, endpoint string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("HTTP_STATUS endpoint=%s status=%d", endpoint, got)
	}
}

func t26HTTPCall(t *testing.T, httpClient *http.Client, baseURL, origin, method, path, bearer, refresh string, body []byte) (int, http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, strings.TrimRight(baseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal("could not create recovery HTTP request")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	if refresh != "" {
		request.Header.Set("RefreshToken", refresh)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := httpClient.Do(request)
	if err != nil {
		t.Fatal("recovery HTTP request failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		t.Fatal("recovery HTTP response could not be read")
	}
	return response.StatusCode, response.Header.Clone(), raw
}

func t26AssertNoSecret(t *testing.T, headers http.Header, body []byte, secrets ...string) {
	t.Helper()
	values := []string{string(body)}
	for _, entries := range headers {
		values = append(values, entries...)
	}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, value := range values {
			if strings.Contains(value, secret) {
				t.Fatal("HTTP response exposed a credential")
			}
		}
	}
}

func t26FindDualPort(t *testing.T) int {
	t.Helper()
	for port := 20000; port < 25000; port++ {
		tcp, err := net.Listen("tcp4", recoveryAuthorizationHTTPHost+":"+strconv.Itoa(port))
		if err != nil {
			continue
		}
		udp, err := net.ListenPacket("udp4", recoveryAuthorizationHTTPHost+":"+strconv.Itoa(port))
		_ = tcp.Close()
		if err != nil {
			continue
		}
		_ = udp.Close()
		return port
	}
	t.Fatal("could not allocate an isolated SIP TCP/UDP port")
	return 0
}

func t26SeedDeviceAndChannel(t *testing.T, databasePath, adminUsername, deviceID, channelID string) {
	t.Helper()
	db, err := gormhelper.NewSQLiteClient(databasePath)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()
	var admin basemodels.User
	result := db.Select("id, dept_id").Where("username = ?", adminUsername).Limit(1).Find(&admin)
	require.NoError(t, result.Error)
	require.Equal(t, int64(1), result.RowsAffected)
	if admin.DeptID == 0 {
		t.Fatal("standalone admin has no valid department for fixture ownership")
	}
	device := gbmodels.GbDevice{
		DeviceID:            deviceID,
		Name:                "t26-device",
		Transport:           "UDP",
		Status:              gbmodels.DeviceStatusOffline,
		KeepaliveInterval:   60,
		CreatedBy:           admin.ID,
		OwnerDeptID:         admin.DeptID,
		SubscribeCapability: gbmodels.SubscribeUnknown,
	}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{
		DeviceID:        deviceID,
		ChannelID:       channelID,
		Name:            "t26-channel",
		Status:          gbmodels.ChannelStatusOffline,
		OwnerDeptID:     device.OwnerDeptID,
		StreamTransport: "TCP-Passive",
	}
	require.NoError(t, db.Create(&channel).Error)
}

// t26RunUncleanAOFRecovery consumes a real QR key, then restores the exact
// pre-consumption Redis tree before creating the previous-run marker. The
// recovery path must therefore remove the old QR during its Redis staging;
// a direct Redis Set would not prove that contract.
func t26RunUncleanAOFRecovery(t *testing.T, root string, paths standalone.Paths, adminUsername, adminPassword string, oldPair recoveryAuthorizationTokenPair, oldQR recoveryAuthorizationQR, oldCapabilityStarted time.Time, client *t18HTTPClient, runs *[]*t18Launch) {
	t.Helper()
	activeRedis := filepath.Join(paths.DataDir, "redis")
	// Keep evidence in an independent protected directory below the disposable
	// fixture root; it must not become part of the active data tree or recovery
	// snapshot. t.TempDir only supplies mode bits and is not an ACL boundary on
	// Windows.
	evidenceRoot := filepath.Join(root, ".t27-redis-evidence")
	require.NoError(t, os.MkdirAll(evidenceRoot, 0700))
	require.NoError(t, t26ProtectEvidenceRoot(evidenceRoot))
	t.Cleanup(func() { _ = os.RemoveAll(evidenceRoot) })
	beforeRedis := filepath.Join(evidenceRoot, "redis-before-consumption")
	afterRedis := filepath.Join(evidenceRoot, "redis-after-consumption")
	require.NoError(t, t26CopyRedisTree(activeRedis, beforeRedis))
	beforeDigest, err := t26RedisTreeDigest(beforeRedis)
	require.NoError(t, err)
	beforeHasAOF, err := t26RedisTreeHasAOF(beforeRedis)
	require.NoError(t, err)
	require.True(t, beforeHasAOF, "unclean HTTP case requires a durable Redis AOF tree")
	// Run the first exchange from the copied tree itself. A successful first
	// exchange then proves that the pre-consumption copy retained the QR state,
	// rather than merely proving that the original active tree had it.
	require.NoError(t, os.RemoveAll(activeRedis))
	require.NoError(t, t26CopyRedisTree(beforeRedis, activeRedis))
	preflightDigest, err := t26RedisTreeDigest(activeRedis)
	require.NoError(t, err)
	require.Equal(t, beforeDigest, preflightDigest, "pre-consumption Redis copy was not installed intact")

	consumeURLs := make(chan string, 1)
	consumeRun := t18StartWithRecordings(t, root, paths.RecordingsDir, consumeURLs)
	*runs = append(*runs, consumeRun)
	consumeEntry, ok := t18WaitBrowserEntry(consumeRun, consumeURLs, 90*time.Second)
	require.True(t, ok)
	baseURL, origin, _, ok := t18BrowserEndpoint(consumeEntry, false)
	require.True(t, ok)
	client.baseURL, client.origin = baseURL, origin
	status, headers, body := client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, "", adminPassword)
	t26RequireHTTPStatus(t, "/api/standalone/setup/status", status, http.StatusOK)
	t18AssertSetupStatus(t, body, true, "complete")

	qrBody, err := json.Marshal(map[string]string{"token": oldQR.Token})
	require.NoError(t, err)
	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/gb28181/sip/qr/exchange", "", "", qrBody)
	if time.Since(oldCapabilityStarted) >= time.Duration(oldQR.ExpiresInSeconds)*time.Second {
		t.Fatal("old QR expired while the first exchange request was in flight; QR durability is unproven")
	}
	t26AssertNoSecret(t, headers, body, oldQR.Token, oldPair.AccessToken, oldPair.RefreshToken, adminPassword)
	t26RequireHTTPStatus(t, "/api/gb28181/sip/qr/exchange", status, http.StatusOK)
	var exchanged struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(body, &exchanged))
	require.Zero(t, exchanged.Code)

	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/gb28181/sip/qr/exchange", "", "", qrBody)
	if time.Since(oldCapabilityStarted) >= time.Duration(oldQR.ExpiresInSeconds)*time.Second {
		t.Fatal("old QR expired while the second exchange request was in flight; single-use evidence is unproven")
	}
	t26AssertNoSecret(t, headers, body, oldQR.Token, oldPair.AccessToken, oldPair.RefreshToken, adminPassword)
	t26RequireHTTPStatus(t, "/api/gb28181/sip/qr/exchange", status, http.StatusGone)
	consumeRun.cancel()
	require.True(t, t18WaitFinished(t, consumeRun, 90*time.Second))

	require.NoError(t, t26CopyRedisTree(activeRedis, afterRedis))
	afterDigest, err := t26RedisTreeDigest(afterRedis)
	require.NoError(t, err)
	require.NotEqual(t, beforeDigest, afterDigest, "QR exchange did not change the durable Redis tree")
	afterHasAOF, err := t26RedisTreeHasAOF(afterRedis)
	require.NoError(t, err)
	require.True(t, afterHasAOF, "post-consumption Redis tree lost its durable AOF")

	require.NoError(t, os.RemoveAll(activeRedis))
	require.NoError(t, t26CopyRedisTree(beforeRedis, activeRedis))
	restoredDigest, err := t26RedisTreeDigest(activeRedis)
	require.NoError(t, err)
	require.Equal(t, beforeDigest, restoredDigest, "active Redis tree was not restored from the pre-consumption copy")

	lock, err := standalone.AcquireInstanceLock(root)
	require.NoError(t, err)
	_, err = standalone.BeginRun(paths)
	require.NoError(t, err)
	require.NoError(t, lock.Close())
	require.FileExists(t, filepath.Join(paths.DataDir, ".uvp-running.json"))

	snapshot := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-t27-unclean-recovery")
	recoverCtx, recoverCancel := context.WithTimeout(context.Background(), recoveryAuthorizationTestTimeout)
	result, err := RecoverUncleanStopped(recoverCtx, root, paths.RecordingsDir, snapshot)
	recoverCancel()
	require.NoError(t, err)
	require.True(t, result.AwaitingLocalConfirmation)
	j, err := standalone.ReadMaintenanceJournal(root)
	require.NoError(t, err)
	require.Equal(t, 2, j.Schema)
	require.Equal(t, "unclean_recovery", j.Kind)
	require.Equal(t, standalone.MaintenanceAwaitingConfirmation, j.Phase)
	require.ErrorIs(t, standalone.CheckMaintenanceGate(root), standalone.ErrMaintenanceRequired)

	confirmCtx, confirmCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	err = standalone.ConfirmRecovery(confirmCtx, root, j.OperationID,
		func(info standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error) {
			require.Equal(t, j.OperationID, info.OperationID)
			require.Equal(t, "unclean_recovery", info.Kind)
			require.False(t, info.BackupTime.IsZero())
			return standalone.RecoveryConfirmationInput{
				Username:        adminUsername,
				Password:        adminPassword,
				Acknowledgement: "CONFIRM " + j.OperationID,
			}, nil
		})
	confirmCancel()
	require.NoError(t, err)
	require.NoError(t, standalone.CheckMaintenanceGate(root))
	selected, err := standalone.LoadRelease(root)
	require.NoError(t, err)
	require.Equal(t, selected.Version, j.OldVersion)

	finalURLs := make(chan string, 1)
	finalRun := t18StartWithRecordings(t, root, paths.RecordingsDir, finalURLs)
	*runs = append(*runs, finalRun)
	finalEntry, ok := t18WaitBrowserEntry(finalRun, finalURLs, 90*time.Second)
	require.True(t, ok)
	baseURL, origin, _, ok = t18BrowserEndpoint(finalEntry, false)
	require.True(t, ok)
	client.baseURL, client.origin = baseURL, origin
	client.accessToken = ""
	status, headers, body = client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	t18AssertResponseSafe(t, headers, body, "", adminPassword)
	t26RequireHTTPStatus(t, "/api/standalone/setup/status", status, http.StatusOK)
	t18AssertSetupStatus(t, body, true, "complete")

	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/users/session/heartbeat", oldPair.AccessToken, "", nil)
	t26AssertNoSecret(t, headers, body, oldPair.AccessToken, oldPair.RefreshToken, adminPassword)
	t26RequireHTTPStatus(t, "/api/users/session/heartbeat", status, http.StatusUnauthorized)
	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/refreshToken", "", oldPair.RefreshToken, nil)
	t26AssertNoSecret(t, headers, body, oldPair.AccessToken, oldPair.RefreshToken, adminPassword)
	t26RequireHTTPStatus(t, "/api/refreshToken", status, http.StatusUnauthorized)

	if time.Since(oldCapabilityStarted) >= time.Duration(oldQR.ExpiresInSeconds)*time.Second {
		t.Fatal("old QR expired before unclean rejection check; recovery revocation is unproven")
	}
	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/gb28181/sip/qr/exchange", "", "", qrBody)
	if time.Since(oldCapabilityStarted) >= time.Duration(oldQR.ExpiresInSeconds)*time.Second {
		t.Fatal("old QR expired while the unclean rejection request was in flight; recovery revocation is unproven")
	}
	t26AssertNoSecret(t, headers, body, oldQR.Token, oldPair.AccessToken, oldPair.RefreshToken, adminPassword)
	t26RequireHTTPStatus(t, "/api/gb28181/sip/qr/exchange", status, http.StatusGone)

	newPair := t26LoginPair(t, *client, adminUsername, adminPassword)
	if oldPair.AccessToken == newPair.AccessToken || oldPair.RefreshToken == newPair.RefreshToken {
		t.Fatal("unclean recovery login reused a previous token")
	}
	refreshedPair := t26RefreshPair(t, *client, newPair, adminPassword, "")
	if newPair.AccessToken == refreshedPair.AccessToken || newPair.RefreshToken == refreshedPair.RefreshToken {
		t.Fatal("unclean recovery refresh endpoint reused a previous token")
	}
	newPair = refreshedPair
	status, headers, body = t26HTTPCall(t, client.client, client.baseURL, client.origin,
		http.MethodPost, "/api/users/session/heartbeat", newPair.AccessToken, "", nil)
	t26AssertNoSecret(t, headers, body, newPair.AccessToken, newPair.RefreshToken, adminPassword)
	t26RequireHTTPStatus(t, "/api/users/session/heartbeat", status, http.StatusOK)

	finalRun.cancel()
	require.True(t, t18WaitFinished(t, finalRun, 90*time.Second))
}

func t26CopyRedisTree(source, target string) error {
	if source == "" || target == "" || filepath.Clean(source) == filepath.Clean(target) {
		return fmt.Errorf("invalid Redis tree copy paths")
	}
	if err := os.MkdirAll(target, 0700); err != nil {
		return err
	}
	// Replacing the Redis root also replaces its explicit protected DACL.
	// Restore it before copying persistence files: an inherited ACL on a
	// non-empty root is deliberately rejected by normal startup.
	if err := t26ProtectEvidenceRoot(target); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Redis tree contains a symlink")
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Redis tree contains a non-regular file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return err
		}
		return os.WriteFile(destination, raw, 0600)
	})
}

func t26RedisTreeDigest(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("Redis tree root is empty")
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Redis tree root is not a directory")
	}
	hash := sha256.New()
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Redis tree contains a symlink")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." || entry.IsDir() {
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !fileInfo.Mode().IsRegular() {
			return fmt.Errorf("Redis tree contains a non-regular file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = hash.Write([]byte(filepath.ToSlash(rel)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(raw)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func t26RedisTreeHasAOF(root string) (bool, error) {
	if root == "" {
		return false, fmt.Errorf("Redis tree root is empty")
	}
	found := false
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Redis tree contains a symlink")
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if name == "manifest" || strings.HasSuffix(name, ".aof") {
			found = true
		}
		return nil
	})
	return found, err
}

func t26ProtectEvidenceRoot(path string) error {
	if path == "" {
		return errors.New("Redis evidence root is empty")
	}
	userSID, err := t26CurrentWindowsUserSID()
	if err != nil {
		return err
	}
	systemSID, err := windows.StringToSid("S-1-5-18")
	if err != nil {
		return errors.New("build SYSTEM SID")
	}
	const fileAllAccessMask windows.ACCESS_MASK = 0x001F01FF
	inheritance := uint32(windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT)
	entries := []windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: fileAllAccessMask,
			AccessMode:        windows.SET_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(userSID),
			},
		},
		{
			AccessPermissions: fileAllAccessMask,
			AccessMode:        windows.SET_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(systemSID),
			},
		},
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return errors.New("build Redis evidence ACL")
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return errors.New("set Redis evidence ACL")
	}
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err != nil {
		return errors.New("read Redis evidence ACL")
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return errors.New("Redis evidence ACL is inheritable")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil || dacl.AceCount != uint16(len(entries)) {
		return errors.New("Redis evidence ACL has unexpected entries")
	}
	want := map[string]bool{userSID.String(): false, systemSID.String(): false}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil || ace == nil {
			return errors.New("read Redis evidence ACL entry")
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE ||
			ace.Header.AceFlags != windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE ||
			ace.Mask != fileAllAccessMask {
			return errors.New("Redis evidence ACL entry is overbroad")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() {
			return errors.New("Redis evidence ACL entry has invalid SID")
		}
		if _, ok := want[sid.String()]; !ok {
			return errors.New("Redis evidence ACL contains an unexpected principal")
		}
		if want[sid.String()] {
			return errors.New("Redis evidence ACL contains a duplicate principal")
		}
		want[sid.String()] = true
	}
	for _, present := range want {
		if !present {
			return errors.New("Redis evidence ACL omitted a required principal")
		}
	}
	return nil
}

func t26CurrentWindowsUserSID() (*windows.SID, error) {
	token := windows.GetCurrentThreadEffectiveToken()
	user, err := token.GetTokenUser()
	if err == nil {
		return user.User.Sid.Copy()
	}
	if !errors.Is(err, windows.ERROR_NO_TOKEN) {
		return nil, errors.New("query effective Windows user token")
	}
	var processToken windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &processToken); err != nil {
		return nil, errors.New("query process Windows user token")
	}
	defer processToken.Close()
	user, err = processToken.GetTokenUser()
	if err != nil {
		return nil, errors.New("query process Windows user token")
	}
	return user.User.Sid.Copy()
}
