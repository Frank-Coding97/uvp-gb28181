//go:build windows

package launcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
	coreEnableEnv             = "UVP_CORE_ENABLE"
	coreInstallDirEnv         = "UVP_CORE_INSTALL_DIR"
	coreProvisionFileEnv      = "UVP_CORE_PROVISION_FILE"
	corePrivateFileEnv        = "UVP_CORE_PRIVATE_FILE"
	coreEvidenceFileEnv       = "UVP_CORE_EVIDENCE_FILE"
	coreDeviceIDEnv           = "UVP_CORE_DEVICE_ID"
	coreSIPIPEnv              = "UVP_CORE_SIP_IP"
	coreDirectoryOnlyEnv      = "UVP_CORE_DIRECTORY_ONLY"
	coreContinueFileEnv       = "UVP_CORE_CONTINUE_FILE"
	coreSimulatorRestartedEnv = "UVP_CORE_SIM_RESTARTED_FILE"
	coreBusinessTimeoutEnv    = "UVP_CORE_BUSINESS_READY_TIMEOUT"
	coreDirectoryTimeoutEnv   = "UVP_CORE_DIRECTORY_TIMEOUT"
	coreContinueTimeoutEnv    = "UVP_CORE_CONTINUE_TIMEOUT"
	coreRestartTimeoutEnv     = "UVP_CORE_RESTART_TIMEOUT"
	coreActiveStopEnv         = "UVP_CORE_ACTIVE_STOP"

	coreSIPPort   = 15070
	coreServerID  = "34020000002000000001"
	coreSIPDomain = "3402000000"
	coreFLVBytes  = 1024
)

type coreProvision struct {
	ServerHost   string `json:"serverHost"`
	ServerPort   int    `json:"serverPort"`
	ServerDomain string `json:"serverDomain"`
	ServerID     string `json:"serverId"`
	DeviceID     string `json:"deviceId"`
	Password     string `json:"password"`
}

type corePrivateCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type coreEvidence struct {
	Version                 string `json:"version"`
	Stage                   string `json:"stage"`
	Partial                 bool   `json:"partial,omitempty"`
	Failure                 string `json:"failure,omitempty"`
	DeviceID                string `json:"deviceId,omitempty"`
	ChannelID               string `json:"channelId,omitempty"`
	DeviceOnline            bool   `json:"deviceOnline"`
	ChannelCount            int    `json:"channelCount"`
	RegisterTime            string `json:"registerTime,omitempty"`
	PlayHTTPStatus          int    `json:"playHttpStatus,omitempty"`
	PlayBackendCode         int    `json:"playBackendCode,omitempty"`
	PlayBackendMessage      string `json:"playBackendMessage,omitempty"`
	FLVBytes                int    `json:"flvBytes,omitempty"`
	FLVHTTPStatus           int    `json:"flvHttpStatus,omitempty"`
	FLVHeader               string `json:"flvHeader,omitempty"`
	StopHTTPStatus          int    `json:"stopHttpStatus,omitempty"`
	ActiveStop              bool   `json:"activeStop,omitempty"`
	FLVBytesUntilEOF        int64  `json:"flvBytesUntilEOF,omitempty"`
	FLVEOFObserved          bool   `json:"flvEOFObserved,omitempty"`
	ActiveStopPortsReleased bool   `json:"activeStopPortsReleased,omitempty"`
	ActiveStopMarkerCleared bool   `json:"activeStopMarkerCleared,omitempty"`
	RestartSIPConfigPort    int    `json:"restartSIPConfigPort,omitempty"`
	RestartDeviceOnline     bool   `json:"restartDeviceOnline,omitempty"`
	RestartChannelCount     int    `json:"restartChannelCount,omitempty"`
	RestartRegisterTime     string `json:"restartRegisterTime,omitempty"`
	RestartMarkerObserved   bool   `json:"restartMarkerObserved,omitempty"`
	ProvisionWritten        bool   `json:"provisionWritten"`
	BusinessReadyObserved   bool   `json:"businessReadyObserved"`
	RestartBusinessObserved bool   `json:"restartBusinessObserved,omitempty"`
}

type coreDevice struct {
	DeviceID     string `json:"deviceId"`
	Online       bool   `json:"online"`
	RegisterTime string `json:"registerTime"`
}

type coreChannel struct {
	ID        uint   `json:"id"`
	DeviceID  string `json:"deviceId"`
	ChannelID string `json:"channelId"`
}

type coreDirectoryObservation struct {
	ChannelRowID uint
	DeviceOnline bool
	ChannelID    string
	ChannelCount int
	RegisterTime string
}

type corePlayObservation struct {
	StreamID   string `json:"streamId"`
	HTTPFlvURL string `json:"httpFlvUrl"`
	URLs       struct {
		HTTPFlvURL string `json:"httpFlv"`
	} `json:"urls"`
}

type coreFLVObservation struct {
	HTTPStatus int
	Bytes      int
	Header     string
}

type coreFLVStream struct {
	Body io.ReadCloser
}

type coreFLVReadResult struct {
	Bytes int64
	Err   error
}

// TestWindowsStandaloneDeviceCorePath is an opt-in Windows core-device chain.
// It owns only the launcher process started by t18Start; the simulator reads
// the explicitly supplied provisioning file and remains an external fixture.
// The test proves authenticated setup, registration/catalog, actual HTTP-FLV
// bytes, stop, and registration after a launcher restart. It does not claim
// browser decoding or simulator lifecycle ownership.
func TestWindowsStandaloneDeviceCorePath(t *testing.T) {
	runWindowsStandaloneDeviceCorePath(t, false)
}

func runWindowsStandaloneDeviceCorePath(t *testing.T, activeStop bool) {
	if os.Getenv(coreEnableEnv) != "1" {
		t.Skip("set UVP_CORE_ENABLE=1 to run the isolated device core harness")
	}
	installDir := strings.TrimSpace(os.Getenv(coreInstallDirEnv))
	if installDir == "" {
		installDir = strings.TrimSpace(os.Getenv("UVP_T18_INSTALL_DIR"))
	}
	if installDir == "" {
		t.Skip("requires an isolated t18-setup fixture directory")
	}
	provisionPath := strings.TrimSpace(os.Getenv(coreProvisionFileEnv))
	if provisionPath == "" {
		t.Skip("requires UVP_CORE_PROVISION_FILE for the external simulator")
	}
	deviceID := strings.TrimSpace(os.Getenv(coreDeviceIDEnv))
	if !coreValidDeviceID(deviceID) {
		t.Skip("requires a 20-digit UVP_CORE_DEVICE_ID for the external simulator")
	}
	sipIP := strings.TrimSpace(os.Getenv(coreSIPIPEnv))
	if sipIP == "" {
		sipIP = "192.168.10.52"
	}
	if !coreValidSIPIP(sipIP) {
		t.Fatal("UVP_CORE_SIP_IP must be a concrete IPv4 address")
	}

	evidencePath := strings.TrimSpace(os.Getenv(coreEvidenceFileEnv))
	privatePath := strings.TrimSpace(os.Getenv(corePrivateFileEnv))
	if privatePath != "" && privatePath == provisionPath {
		t.Fatal("UVP_CORE_PRIVATE_FILE and UVP_CORE_PROVISION_FILE must be different files")
	}
	evidence := &coreEvidence{Version: "uvp-core-device-v1", Stage: "starting", DeviceID: deviceID, ActiveStop: activeStop}
	defer coreWriteEvidence(t, evidencePath, evidence)

	release, err := standalone.LoadRelease(installDir)
	if err != nil || release.Version != "t18-setup" {
		coreFail(t, evidence, "fixture", "t18_setup_release_unavailable")
	}
	businessTimeout := coreDurationEnv(t, coreBusinessTimeoutEnv, 5*time.Minute)
	directoryTimeout := coreDurationEnv(t, coreDirectoryTimeoutEnv, 5*time.Minute)
	continueTimeout := coreDurationEnv(t, coreContinueTimeoutEnv, 10*time.Minute)
	restartTimeout := coreDurationEnv(t, coreRestartTimeoutEnv, 5*time.Minute)

	adminUsername := "core-" + strings.TrimPrefix(t18RandomPassword(t), "T18!")
	adminPassword := t18RandomPassword(t)
	sipPassword := t18RandomPassword(t)
	adminBody := t18AdminBody(t, adminUsername, adminPassword)
	configBody, err := json.Marshal(map[string]any{
		"deploymentMode":    "lan",
		"listenIp":          sipIP,
		"advertiseIp":       sipIP,
		"port":              coreSIPPort,
		"domain":            coreSIPDomain,
		"serverId":          coreServerID,
		"password":          sipPassword,
		"mediaReceiveHost":  sipIP,
		"mediaPlaybackHost": sipIP,
	})
	if err != nil {
		coreFail(t, evidence, "setup", "request_encoding_failed")
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
	evidence.Stage = "launcher_started"
	firstEntry, ok := t18WaitBrowserEntry(first, firstURLs, 90*time.Second)
	if !ok {
		coreFail(t, evidence, "launcher", "browser_endpoint_not_published")
	}
	baseURL, origin, bootstrapToken, ok := t18BrowserEndpoint(firstEntry, true)
	if !ok {
		coreFail(t, evidence, "launcher", "invalid_loopback_bootstrap_endpoint")
	}
	client := t18HTTPClient{client: t18HTTPTransport(), baseURL: baseURL, origin: origin}

	status, headers, body := client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	coreAssertResponseSafe(t, headers, body, bootstrapToken, adminPassword, sipPassword)
	if status != http.StatusOK {
		coreFail(t, evidence, "setup", "initial_setup_status_unavailable")
	}
	t18AssertSetupStatus(t, body, true, "pending_admin")

	status, headers, body = client.request(t, http.MethodPost, "/api/standalone/setup/admin", bootstrapToken, adminBody)
	coreAssertResponseSafe(t, headers, body, bootstrapToken, adminPassword, sipPassword)
	if status != http.StatusCreated {
		coreFail(t, evidence, "setup", "administrator_creation_rejected")
	}

	status, headers, body = client.request(t, http.MethodPost, "/api/login", "", adminBody)
	coreAssertResponseSafe(t, headers, body, bootstrapToken, adminPassword, sipPassword)
	accessToken, ok := coreAccessToken(body)
	if status != http.StatusOK || !ok {
		coreFail(t, evidence, "login", "administrator_login_rejected")
	}
	client.accessToken = accessToken

	status, headers, body = client.request(t, http.MethodPut, "/api/gb28181/sip/setup/config", "", configBody)
	coreAssertResponseSafe(t, headers, body, bootstrapToken, adminPassword)
	var saved struct {
		ReloadedOK bool `json:"reloadedOk"`
	}
	if status != http.StatusOK || !coreResponseData(body, &saved) || !saved.ReloadedOK {
		coreFail(t, evidence, "sip_setup", "sip_activation_not_confirmed")
	}
	status, headers, body = client.request(t, http.MethodGet, "/api/standalone/setup/status", "", nil)
	coreAssertResponseSafe(t, headers, body, bootstrapToken, adminPassword, sipPassword)
	if status != http.StatusOK {
		coreFail(t, evidence, "sip_setup", "completed_setup_status_unavailable")
	}
	t18AssertSetupStatus(t, body, true, "complete")
	coreWaitBusinessReady(t, first, businessTimeout, evidence, "business_ready")

	provision := coreProvision{
		ServerHost:   sipIP,
		ServerPort:   coreSIPPort,
		ServerDomain: coreSIPDomain,
		ServerID:     coreServerID,
		DeviceID:     deviceID,
		Password:     sipPassword,
	}
	if err := coreWriteJSONFile(provisionPath, provision); err != nil {
		coreFail(t, evidence, "provision", "provisioning_file_write_failed")
	}
	if privatePath != "" {
		if err := coreWriteJSONFile(privatePath, corePrivateCredentials{Username: adminUsername, Password: adminPassword}); err != nil {
			coreFail(t, evidence, "provision", "private_credentials_file_write_failed")
		}
	}
	evidence.ProvisionWritten = true
	evidence.Stage = "provision_ready"
	t.Log("ROOT_READY_FOR_SIM")

	firstDirectory := coreWaitDirectory(t, &client, deviceID, directoryTimeout, "", evidence)
	if firstDirectory.RegisterTime == "" {
		coreFail(t, evidence, "directory", "register_time_not_exposed")
	}
	evidence.Stage = "directory_ready"
	evidence.DeviceOnline = firstDirectory.DeviceOnline
	evidence.ChannelID = firstDirectory.ChannelID
	evidence.ChannelCount = firstDirectory.ChannelCount
	evidence.RegisterTime = firstDirectory.RegisterTime
	t.Log("DEVICE_DIRECTORY_READY")

	if os.Getenv(coreDirectoryOnlyEnv) == "1" {
		evidence.Partial = true
		evidence.Stage = "partial_directory_only"
		t.Log("PARTIAL_DIRECTORY_ONLY")
		return
	}

	playPath := "/api/gb28181/play/" + url.PathEscape(deviceID) + "/" + url.PathEscape(firstDirectory.ChannelID)
	if os.Getenv("UVP_CORE_RECORD_STOP") == "1" {
		coreEnableRecording(t, &client, firstDirectory.ChannelRowID, evidence)
	}
	status, headers, body = client.request(t, http.MethodPost, playPath, "", nil)
	coreAssertResponseSafe(t, headers, body, bootstrapToken, adminPassword, sipPassword)
	var playResult corePlayObservation
	if status != http.StatusOK || !coreResponseData(body, &playResult) {
		coreFail(t, evidence, "play", "play_request_rejected")
	}
	if playResult.HTTPFlvURL == "" {
		playResult.HTTPFlvURL = playResult.URLs.HTTPFlvURL
	}
	if playResult.StreamID == "" || playResult.HTTPFlvURL == "" {
		coreFail(t, evidence, "play", "play_response_missing_stream_or_httpflv")
	}
	flvTimeout := 15 * time.Second
	if activeStop {
		flvTimeout = 2 * time.Minute
	}
	flvContext, flvCancel := context.WithTimeout(context.Background(), flvTimeout)
	var activeFLV *coreFLVStream
	var activeFLVDone chan coreFLVReadResult
	var flv coreFLVObservation
	var flvReason string
	if activeStop {
		activeClient := *client.client
		activeClient.Timeout = 0
		activeFLV, flv, flvReason = coreOpenFLV(flvContext, &activeClient, playResult.HTTPFlvURL)
		if activeFLV != nil && flvReason == "" {
			activeFLVDone = make(chan coreFLVReadResult, 1)
			go func() {
				bytesRead, err := coreReadFLVUntilEOF(activeFLV.Body)
				activeFLVDone <- coreFLVReadResult{Bytes: bytesRead, Err: err}
			}()
		}
	} else {
		flv, flvReason = coreFetchFLV(flvContext, client.client, playResult.HTTPFlvURL)
	}
	evidence.PlayHTTPStatus = flv.HTTPStatus
	evidence.FLVBytes = flv.Bytes
	evidence.FLVHeader = flv.Header
	if activeFLV != nil {
		defer func() {
			_ = activeFLV.Body.Close()
			flvCancel()
		}()
	} else {
		flvCancel()
	}
	if flvReason != "" {
		coreFail(t, evidence, "play_stream", flvReason)
	}
	evidence.Stage = "play_ready"
	t.Log("PLAY_READY")
	if os.Getenv("UVP_CORE_RECORD_STOP") == "1" {
		time.Sleep(15 * time.Second)
	}

	if continuePath := strings.TrimSpace(os.Getenv(coreContinueFileEnv)); continuePath != "" {
		if !coreWaitForFile(continuePath, continueTimeout) {
			coreFail(t, evidence, "play_wait", "continue_marker_timeout")
		}
	}

	if activeStop {
		select {
		case result := <-activeFLVDone:
			if result.Err == nil {
				coreFail(t, evidence, "active_stop", "httpflv_ended_before_launcher_stop")
			}
			coreFail(t, evidence, "active_stop", "httpflv_read_failed_before_launcher_stop")
		default:
		}
		evidence.Stage = "active_play_ready"
		t.Log("ACTIVE_PLAY_READY")
	} else {
		stopPath := "/api/gb28181/play/" + url.PathEscape(playResult.StreamID)
		status, headers, body = client.request(t, http.MethodDelete, stopPath, "", nil)
		coreAssertResponseSafe(t, headers, body, bootstrapToken, adminPassword, sipPassword)
		evidence.StopHTTPStatus = status
		if status != http.StatusOK {
			coreFail(t, evidence, "stop", "stop_request_rejected")
		}
		evidence.Stage = "play_stopped"
		t.Log("PLAY_STOPPED")
	}

	first.cancel()
	if !t18WaitFinished(t, first, 90*time.Second) {
		coreFail(t, evidence, "restart", "first_launcher_did_not_stop")
	}
	if activeStop {
		select {
		case result := <-activeFLVDone:
			if result.Err != nil {
				coreFail(t, evidence, "active_stop", "httpflv_did_not_reach_eof")
			}
			evidence.FLVBytesUntilEOF = int64(evidence.FLVBytes) + result.Bytes
			evidence.FLVEOFObserved = true
		case <-time.After(30 * time.Second):
			coreFail(t, evidence, "active_stop", "httpflv_eof_timeout")
		}
		activePaths, err := standalone.ResolvePaths(standalone.PathOptions{
			InstallDir:  installDir,
			ConfigDir:   filepath.Join(installDir, "config"),
			DataDir:     filepath.Join(installDir, "data"),
			ResourceDir: release.ResourceDir,
			WebDir:      release.WebDir,
		})
		if err != nil {
			coreFail(t, evidence, "active_stop", "instance_paths_unavailable")
		}
		activeConfig, err := standalone.LoadConfig(activePaths)
		if err != nil {
			coreFail(t, evidence, "active_stop", "instance_config_unavailable")
		}
		if err := checkPorts([]string{activeConfig.RedisAddress(), activeConfig.BackendAddress()}, activeConfig.MediaListeners()); err != nil {
			coreFail(t, evidence, "active_stop", "instance_ports_not_released")
		}
		evidence.ActiveStopPortsReleased = true
		if _, err := os.Stat(filepath.Join(activePaths.DataDir, ".uvp-running.json")); !errors.Is(err, os.ErrNotExist) {
			coreFail(t, evidence, "active_stop", "running_marker_not_cleared")
		}
		evidence.ActiveStopMarkerCleared = true
		evidence.Stage = "active_play_stopped"
		t.Log("ACTIVE_PLAY_STOPPED")
	}

	secondURLs := make(chan string, 1)
	second := t18Start(t, installDir, secondURLs)
	runs = append(runs, second)
	secondEntry, ok := t18WaitBrowserEntry(second, secondURLs, 90*time.Second)
	if !ok {
		coreFail(t, evidence, "restart", "second_launcher_endpoint_not_published")
	}
	baseURL, origin, _, ok = t18BrowserEndpoint(secondEntry, false)
	if !ok {
		coreFail(t, evidence, "restart", "invalid_loopback_restart_endpoint")
	}
	client.baseURL = baseURL
	client.origin = origin
	coreWaitBusinessReady(t, second, businessTimeout, evidence, "restart_business_ready")
	evidence.RestartBusinessObserved = true

	status, headers, body = client.request(t, http.MethodPost, "/api/login", "", adminBody)
	coreAssertResponseSafe(t, headers, body, bootstrapToken, adminPassword, sipPassword)
	accessToken, ok = coreAccessToken(body)
	if status != http.StatusOK || !ok {
		coreFail(t, evidence, "restart_login", "administrator_relogin_rejected")
	}
	client.accessToken = accessToken
	if os.Getenv("UVP_CORE_RECORD_STOP") == "1" {
		coreVerifyRecording(t, &client, firstDirectory.ChannelRowID, evidence)
	}
	t.Log("RESTART_READY_FOR_SIM")
	if activeStop {
		status, headers, body = client.request(t, http.MethodGet, "/api/gb28181/sip/setup/status", "", nil)
		// This permission-protected configuration endpoint intentionally returns
		// the SIP password for device enrollment, never the administrator secret.
		coreAssertResponseSafe(t, headers, body, bootstrapToken, adminPassword)
		var persisted struct {
			Data struct {
				Config struct {
					Port int `json:"port"`
				} `json:"config"`
			} `json:"data"`
		}
		if status != http.StatusOK || json.Unmarshal(body, &persisted) != nil || persisted.Data.Config.Port != coreSIPPort {
			coreFail(t, evidence, "restart", "sip_config_not_preserved")
		}
		evidence.RestartSIPConfigPort = persisted.Data.Config.Port
	}
	if restartPath := strings.TrimSpace(os.Getenv(coreSimulatorRestartedEnv)); restartPath != "" {
		if !coreWaitForFile(restartPath, restartTimeout) {
			coreFail(t, evidence, "restart", "simulator_restart_marker_timeout")
		}
		evidence.RestartMarkerObserved = true
	}

	secondDirectory := coreWaitDirectory(t, &client, deviceID, restartTimeout, firstDirectory.RegisterTime, evidence)
	evidence.RestartDeviceOnline = secondDirectory.DeviceOnline
	evidence.RestartChannelCount = secondDirectory.ChannelCount
	evidence.RestartRegisterTime = secondDirectory.RegisterTime
	evidence.Stage = "complete"
	t.Log("DEVICE_REREGISTERED")
}

func coreFail(t *testing.T, evidence *coreEvidence, stage, reason string) {
	evidence.Stage = stage
	evidence.Failure = reason
	t.Fatalf("core stage %s failed: %s", stage, reason)
}

func coreDurationEnv(t *testing.T, name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 || value > 20*time.Minute {
		t.Fatalf("invalid core timeout configuration")
	}
	return value
}

func coreValidDeviceID(value string) bool {
	if len(value) != 20 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func coreValidSIPIP(value string) bool {
	ip := net.ParseIP(value)
	return ip != nil && ip.To4() != nil && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.Equal(net.IPv4bcast)
}

func coreWriteJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func coreWriteEvidence(t *testing.T, path string, evidence *coreEvidence) {
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil || os.WriteFile(path, append(data, '\n'), 0o600) != nil {
		t.Log("core evidence file could not be written")
	}
}

func coreAccessToken(raw []byte) (string, bool) {
	var response struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return "", false
	}
	return response.Data.AccessToken, response.Data.AccessToken != ""
}

func coreResponseData(raw []byte, target any) bool {
	var response struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Data) == 0 || string(response.Data) == "null" {
		return false
	}
	return json.Unmarshal(response.Data, target) == nil
}

func coreWaitBusinessReady(t *testing.T, run *t18Launch, timeout time.Duration, evidence *coreEvidence, stage string) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case state := <-run.statuses:
			if state.State == Ready && state.BusinessReady && state.BusinessReason == "ready" && state.SIPState == "running" {
				evidence.BusinessReadyObserved = true
				if stage != "business_ready" {
					evidence.RestartBusinessObserved = true
				}
				return
			}
		case <-run.finished:
			coreFail(t, evidence, stage, "launcher_exited_before_business_ready")
		case <-timer.C:
			coreFail(t, evidence, stage, "business_ready_timeout")
		}
	}
}

func coreWaitDirectory(t *testing.T, client *t18HTTPClient, deviceID string, timeout time.Duration, previousRegisterTime string, evidence *coreEvidence) coreDirectoryObservation {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		observation, ok, reason := coreReadDirectory(ctx, client, deviceID)
		if reason == "authorization_rejected" || reason == "route_missing" || reason == "invalid_response" {
			coreFail(t, evidence, "directory", reason)
		}
		if ok && (previousRegisterTime == "" || observation.RegisterTime != "" && observation.RegisterTime != previousRegisterTime) {
			return observation
		}
		select {
		case <-ctx.Done():
			if previousRegisterTime != "" {
				coreFail(t, evidence, "directory", "device_reregistration_timeout")
			}
			coreFail(t, evidence, "directory", "device_directory_timeout")
		case <-ticker.C:
		}
	}
}

func coreReadDirectory(ctx context.Context, client *t18HTTPClient, deviceID string) (coreDirectoryObservation, bool, string) {
	status, _, body, err := client.coreRequest(ctx, http.MethodGet, "/api/gb28181/device/list?page=1&pageSize=200", nil)
	if err != nil {
		return coreDirectoryObservation{}, false, "backend_unavailable"
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return coreDirectoryObservation{}, false, "authorization_rejected"
	}
	if status == http.StatusNotFound {
		return coreDirectoryObservation{}, false, "route_missing"
	}
	if status != http.StatusOK {
		return coreDirectoryObservation{}, false, "backend_unavailable"
	}
	var devices struct {
		List []coreDevice `json:"list"`
	}
	if !coreResponseData(body, &devices) {
		return coreDirectoryObservation{}, false, "invalid_response"
	}
	var device *coreDevice
	for index := range devices.List {
		if devices.List[index].DeviceID == deviceID {
			device = &devices.List[index]
			break
		}
	}
	if device == nil || !device.Online {
		return coreDirectoryObservation{}, false, "device_offline"
	}

	channelPath := "/api/gb28181/device/" + url.PathEscape(deviceID) + "/channels"
	status, _, body, err = client.coreRequest(ctx, http.MethodGet, channelPath, nil)
	if err != nil {
		return coreDirectoryObservation{}, false, "backend_unavailable"
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return coreDirectoryObservation{}, false, "authorization_rejected"
	}
	if status == http.StatusNotFound {
		return coreDirectoryObservation{}, false, "route_missing"
	}
	if status != http.StatusOK {
		return coreDirectoryObservation{}, false, "backend_unavailable"
	}
	var channels struct {
		List []coreChannel `json:"list"`
	}
	if !coreResponseData(body, &channels) {
		return coreDirectoryObservation{}, false, "invalid_response"
	}
	for _, channel := range channels.List {
		if channel.ChannelID == "" || channel.DeviceID != "" && channel.DeviceID != deviceID {
			continue
		}
		return coreDirectoryObservation{
			ChannelRowID: channel.ID,
			DeviceOnline: true,
			ChannelID:    channel.ChannelID,
			ChannelCount: len(channels.List),
			RegisterTime: device.RegisterTime,
		}, true, ""
	}
	return coreDirectoryObservation{}, false, "catalog_pending"
}

func (client t18HTTPClient) coreRequest(ctx context.Context, method, path string, body []byte) (int, http.Header, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(client.baseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	if client.origin != "" {
		request.Header.Set("Origin", client.origin)
	}
	if client.accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+client.accessToken)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.client.Do(request)
	if err != nil {
		return 0, nil, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		if err == nil {
			err = io.ErrShortBuffer
		}
		return 0, nil, nil, err
	}
	return response.StatusCode, response.Header, raw, nil
}

func coreAssertResponseSafe(t *testing.T, headers http.Header, body []byte, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		t18AssertResponseSafe(t, headers, body, secret, "")
	}
}

func coreFetchFLV(ctx context.Context, client *http.Client, rawURL string) (coreFLVObservation, string) {
	stream, observation, reason := coreOpenFLV(ctx, client, rawURL)
	if stream == nil {
		return observation, reason
	}
	defer stream.Body.Close()
	return observation, ""
}

func coreOpenFLV(ctx context.Context, client *http.Client, rawURL string) (*coreFLVStream, coreFLVObservation, string) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, coreFLVObservation{}, "invalid_httpflv_url"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, coreFLVObservation{}, "invalid_httpflv_request"
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, coreFLVObservation{}, "httpflv_unavailable"
	}
	observation := coreFLVObservation{HTTPStatus: response.StatusCode}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, observation, "httpflv_bad_status"
	}
	data := make([]byte, coreFLVBytes)
	read, err := io.ReadFull(response.Body, data)
	observation.Bytes = read
	if err != nil || read < coreFLVBytes {
		response.Body.Close()
		return nil, observation, "httpflv_short_body"
	}
	if !bytes.Equal(data[:3], []byte("FLV")) {
		response.Body.Close()
		return nil, observation, "httpflv_header_invalid"
	}
	observation.Header = "FLV"
	return &coreFLVStream{Body: response.Body}, observation, ""
}

func coreReadFLVUntilEOF(body io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		read, err := body.Read(buffer)
		total += int64(read)
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

func coreWaitForFile(path string, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return true
		}
		select {
		case <-deadline.C:
			return false
		case <-ticker.C:
		}
	}
}
