package main

import (
	"context"
	"crypto/rand"
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
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	serverStartupTimeout = 30 * time.Second
	apiRequestTimeout    = 15 * time.Second
	mediaReadyTimeout    = 12 * time.Second
	recordWaitTimeout    = 15 * time.Second
	maxAPIResponseBytes  = 16 << 20
)

type probeOptions struct {
	root           string
	executable     string
	fixture        string
	expectedCommit string
	keepTemp       bool
}

type probeReport struct {
	Schema            int           `json:"schema"`
	Probe             string        `json:"probe"`
	StartedAt         string        `json:"started_at"`
	FinishedAt        string        `json:"finished_at"`
	Status            string        `json:"status"`
	Passed            bool          `json:"passed"`
	BinarySHA256      string        `json:"binary_sha256,omitempty"`
	FixtureSHA256     string        `json:"fixture_sha256,omitempty"`
	Workspace         string        `json:"workspace,omitempty"`
	WorkspaceKept     bool          `json:"workspace_kept"`
	ResourceFileCount int           `json:"resource_file_count,omitempty"`
	ResourceBytes     int64         `json:"resource_bytes,omitempty"`
	Checks            []checkResult `json:"checks"`
	Unexecuted        []string      `json:"unexecuted,omitempty"`
	Errors            []string      `json:"errors,omitempty"`
}

type checkResult struct {
	Name    string         `json:"name"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details,omitempty"`
	Error   string         `json:"error,omitempty"`
}

type resourceStats struct {
	Files int
	Bytes int64
}

type apiResponse struct {
	Code   int             `json:"code"`
	Msg    string          `json:"msg"`
	Data   json.RawMessage `json:"data"`
	Port   json.RawMessage `json:"port"`
	Hit    json.RawMessage `json:"hit"`
	Result json.RawMessage `json:"result"`
	Status json.RawMessage `json:"status"`
	Online json.RawMessage `json:"online"`
}

type apiClient struct {
	baseURL string
	http    *http.Client
}

type zlmProcess struct {
	cmd      *exec.Cmd
	done     chan error
	waited   bool
	waitErr  error
	stdout   *os.File
	stderr   *os.File
	closeMux sync.Mutex
}

type playerSession struct {
	cancel context.CancelFunc
	ready  <-chan error
	done   <-chan error
}

type rtpEntry struct {
	VHost     string `json:"vhost"`
	App       string `json:"app"`
	StreamID  string `json:"stream_id"`
	Port      int    `json:"port"`
	SSRC      uint32 `json:"ssrc"`
	TCPMode   int    `json:"tcp_mode"`
	OnlyTrack int    `json:"only_track"`
}

func runProbe(opts probeOptions) (report probeReport) {
	report = newReport(opts)
	report.Unexecuted = append(report.Unexecuted, "hook_callback_auth")
	report.Checks = append(report.Checks, notExecutedCheck("hook_callback_auth", "requires the real UVP hook receiver and its node credential contract"))
	defer func() {
		report.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		report.Passed = allChecksPassed(report.Checks)
		switch {
		case report.Passed:
			report.Status = "passed"
		case hasFailedCheck(report.Checks):
			report.Status = "failed"
		default:
			report.Status = "partial"
		}
		if !report.Passed && len(report.Errors) == 0 {
			report.Errors = []string{"one or more required ZLMediaKit checks did not pass"}
		}
	}()

	root, executable, relExecutable, err := resolveInput(opts)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("input", err.Error()))
		return report
	}

	stage, stageExecutable, stats, err := copyRuntimeTree(root, relExecutable)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("resource_copy", err.Error()))
		return report
	}
	report.Workspace = stage
	report.ResourceFileCount = stats.Files
	report.ResourceBytes = stats.Bytes
	report.Checks = append(report.Checks, passedCheck("resource_copy", map[string]any{
		"files":                           stats.Files,
		"bytes":                           stats.Bytes,
		"workspace_has_chinese_and_space": hasChineseAndSpace(stage),
	}))
	if hash, hashErr := sha256File(executable); hashErr == nil {
		report.BinarySHA256 = hash
	} else {
		report.Checks = append(report.Checks, failedCheck("binary_hash", "MediaServer.exe hash could not be computed"))
		return report
	}
	defer func() {
		if !opts.keepTemp {
			_ = os.RemoveAll(stage)
		}
	}()

	fixturePath := ""
	if opts.fixture != "" {
		fixturePath, err = copyFixture(opts.fixture, stage)
		if err != nil {
			report.Checks = append(report.Checks, failedCheck("fixture_copy", err.Error()))
			return report
		}
		if hash, hashErr := sha256File(opts.fixture); hashErr == nil {
			report.FixtureSHA256 = hash
		}
	} else {
		report.Unexecuted = append(report.Unexecuted, "media_and_recording")
		report.Checks = append(report.Checks, notExecutedCheck("media_and_recording", "an MP4 fixture is required for the media lifecycle checks"))
	}

	port, err := reserveTCPPort()
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("http_port", "could not reserve a local HTTP port"))
		return report
	}
	secret := randomToken("zlm-")
	if err := configureRuntime(filepath.Join(stage, "config.ini"), port, secret); err != nil {
		report.Checks = append(report.Checks, failedCheck("runtime_config", "isolated ZLMediaKit config could not be prepared"))
		return report
	}

	server, err := startZLM(stageExecutable, stage, filepath.Join(stage, "config.ini"))
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("server_start", "MediaServer.exe could not be started"))
		return report
	}
	client := &apiClient{baseURL: fmt.Sprintf("http://127.0.0.1:%d", port), http: &http.Client{Timeout: apiRequestTimeout}}
	defer func() {
		if stopErr := server.stop(10 * time.Second); stopErr != nil {
			report.Errors = append(report.Errors, "MediaServer.exe did not stop within the bounded cleanup window")
		}
	}()

	if err := waitForAPI(server, client, secret, serverStartupTimeout); err != nil {
		report.Checks = append(report.Checks, failedCheck("server_ready", "MediaServer.exe did not expose a ready HTTP API"))
		return report
	}
	report.Checks = append(report.Checks, passedCheck("server_ready", map[string]any{"http_port": port}))

	report.Checks = append(report.Checks, checkAPIs(client, secret, opts.expectedCommit))
	report.Checks = append(report.Checks, checkServerConfig(client, secret, port))
	report.Checks = append(report.Checks, checkWrongSecret(client))
	report.Checks = append(report.Checks, checkRTPLifecycle(client, secret))
	if fixturePath != "" {
		report.Checks = append(report.Checks, checkMediaLifecycle(client, secret, stage, fixturePath))
	}

	if err := server.stop(10 * time.Second); err != nil {
		report.Checks = append(report.Checks, failedCheck("server_stop", "the first MediaServer.exe instance did not stop"))
		return report
	}
	server = nil
	server, err = startZLM(stageExecutable, stage, filepath.Join(stage, "config.ini"))
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("server_restart", "MediaServer.exe could not be started a second time"))
		return report
	}
	if err := waitForAPI(server, client, secret, serverStartupTimeout); err != nil {
		report.Checks = append(report.Checks, failedCheck("server_restart", "the restarted MediaServer.exe did not become ready"))
		return report
	}
	if response, callErr := client.callSecret(context.Background(), "/index/api/getApiList", secret); callErr != nil || response.Code != 0 {
		report.Checks = append(report.Checks, failedCheck("server_restart", "the restarted API did not answer the authenticated readiness call"))
	} else {
		report.Checks = append(report.Checks, passedCheck("server_restart", map[string]any{"authenticated_api": true}))
	}

	return report
}

func resolveInput(opts probeOptions) (root, executable, relExecutable string, err error) {
	if opts.root == "" {
		return "", "", "", errors.New("-root is required")
	}
	root, err = filepath.Abs(opts.root)
	if err != nil {
		return "", "", "", errors.New("invalid runtime root")
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", "", "", errors.New("runtime root is not a directory")
	}
	if opts.executable == "" {
		executable = filepath.Join(root, "MediaServer.exe")
	} else if filepath.IsAbs(opts.executable) {
		executable = filepath.Clean(opts.executable)
	} else {
		executable = filepath.Join(root, opts.executable)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", "", "", errors.New("invalid MediaServer.exe path")
	}
	info, err = os.Stat(executable)
	if err != nil || info.IsDir() {
		return "", "", "", errors.New("MediaServer.exe does not exist")
	}
	relExecutable, err = filepath.Rel(root, executable)
	if err != nil || relExecutable == "." || isOutsideRoot(relExecutable) {
		return "", "", "", errors.New("MediaServer.exe must be inside -root")
	}
	if _, err := os.Stat(filepath.Join(root, "config.ini")); err != nil {
		return "", "", "", errors.New("runtime root is missing config.ini")
	}
	if info, err := os.Stat(filepath.Join(root, "www")); err != nil || !info.IsDir() {
		return "", "", "", errors.New("runtime root is missing the complete www resource directory")
	}
	return root, executable, relExecutable, nil
}

func copyRuntimeTree(root, relExecutable string) (stage, stageExecutable string, stats resourceStats, err error) {
	stage, err = os.MkdirTemp("", "uvp-zlm-probe-中文 空格-")
	if err != nil {
		return "", "", resourceStats{}, errors.New("could not create isolated workspace")
	}
	cleanup := true
	defer func() {
		if cleanup && err != nil {
			_ = os.RemoveAll(stage)
		}
	}()
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("runtime tree contains unsupported symlink")
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(stage, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("runtime tree contains unsupported special file")
		}
		if err := copyFile(path, target, entry); err != nil {
			return err
		}
		stats.Files++
		if info, infoErr := entry.Info(); infoErr == nil {
			stats.Bytes += info.Size()
		}
		return nil
	})
	if err != nil {
		return "", "", resourceStats{}, errors.New("complete runtime resource copy failed")
	}
	stageExecutable = filepath.Join(stage, relExecutable)
	if _, err := os.Stat(stageExecutable); err != nil {
		return "", "", resourceStats{}, errors.New("copied MediaServer.exe is missing")
	}
	cleanup = false
	return stage, stageExecutable, stats, nil
}

func copyFixture(source, stage string) (string, error) {
	info, err := os.Stat(source)
	if err != nil || info.IsDir() || info.Size() < 1024 {
		return "", errors.New("MP4 fixture is missing or too small")
	}
	destination := filepath.Join(stage, "测试媒体 中文 空格.mp4")
	if err := copyFile(source, destination, nil); err != nil {
		return "", errors.New("MP4 fixture could not be copied into the isolated workspace")
	}
	return destination, nil
}

func copyFile(source, destination string, entry os.DirEntry) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	mode := os.FileMode(0o600)
	if entry != nil {
		if info, infoErr := entry.Info(); infoErr == nil {
			mode = info.Mode().Perm()
		}
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func configureRuntime(path string, httpPort int, secret string) error {
	values := map[string]map[string]string{
		"api": {
			"apiDebug": "0",
			"secret":   secret,
		},
		"general": {
			"listen_ip":     "127.0.0.1",
			"mediaServerId": randomToken("probe-server-"),
		},
		"protocol": {
			"enable_hls":      "0",
			"enable_hls_fmp4": "0",
			"enable_mp4":      "0",
			"enable_rtsp":     "0",
			"enable_rtmp":     "0",
			"enable_ts":       "0",
			"enable_fmp4":     "1",
			"enable_audio":    "0",
			"mp4_as_player":   "0",
			"mp4_save_path":   "./录像 输出 中文 space",
		},
		"http": {
			"port":     strconv.Itoa(httpPort),
			"sslport":  "0",
			"rootPath": "./www",
		},
		"rtsp": {
			"port":    "0",
			"sslport": "0",
		},
		"rtmp": {
			"port":    "0",
			"sslport": "0",
		},
		"rtp_proxy": {"port": "0"},
		"shell":     {"port": "0"},
		"onvif":     {"port": "0"},
		"srt":       {"port": "0"},
		"rtc": {
			"signalingPort":    "0",
			"signalingSslPort": "0",
			"icePort":          "0",
			"iceTcpPort":       "0",
			"port":             "0",
			"tcpPort":          "0",
		},
	}
	return rewriteINI(path, values)
}

func rewriteINI(path string, values map[string]map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	seenSections := make(map[string]bool)
	seenKeys := make(map[string]map[string]bool)
	for section := range values {
		seenKeys[section] = make(map[string]bool)
	}
	output := make([]string, 0, len(lines)+len(values)*2)
	currentSection := ""
	flushMissing := func(section string) {
		wanted, ok := values[section]
		if !ok {
			return
		}
		keys := make([]string, 0, len(wanted))
		for key := range wanted {
			if !seenKeys[section][key] {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			output = append(output, key+"="+wanted[key])
			seenKeys[section][key] = true
		}
	}
	for _, line := range lines {
		lineHadCR := strings.HasSuffix(line, "\r")
		withoutCR := strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(withoutCR)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			flushMissing(currentSection)
			currentSection = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			seenSections[currentSection] = true
		}
		if equalPos := strings.IndexByte(withoutCR, '='); equalPos > 0 && currentSection != "" {
			key := strings.TrimSpace(withoutCR[:equalPos])
			if replacement, ok := values[currentSection][key]; ok {
				line = withoutCR[:equalPos+1] + replacement
				if lineHadCR {
					line += "\r"
				}
				seenKeys[currentSection][key] = true
			}
		}
		output = append(output, line)
	}
	flushMissing(currentSection)
	sections := make([]string, 0, len(values))
	for section := range values {
		if !seenSections[section] {
			sections = append(sections, section)
		}
	}
	sort.Strings(sections)
	for _, section := range sections {
		output = append(output, "", "["+section+"]")
		keys := make([]string, 0, len(values[section]))
		for key := range values[section] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			output = append(output, key+"="+values[section][key])
		}
	}
	return os.WriteFile(path, []byte(strings.Join(output, "\n")), 0o600)
}

func startZLM(executable, dir, config string) (*zlmProcess, error) {
	stdout, err := os.OpenFile(filepath.Join(dir, "probe-stdout.log"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	stderr, err := os.OpenFile(filepath.Join(dir, "probe-stderr.log"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}
	cmd := exec.Command(executable, "-c", filepath.Base(config), "-a", "0")
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, err
	}
	process := &zlmProcess{cmd: cmd, done: make(chan error, 1), stdout: stdout, stderr: stderr}
	go func() { process.done <- cmd.Wait() }()
	return process, nil
}

func (process *zlmProcess) pollExit() bool {
	if process == nil || process.waited {
		return process != nil && process.waited
	}
	select {
	case process.waitErr = <-process.done:
		process.waited = true
		return true
	default:
		return false
	}
}

func (process *zlmProcess) stop(timeout time.Duration) error {
	if process == nil {
		return nil
	}
	if !process.waited {
		_ = process.cmd.Process.Kill()
		select {
		case process.waitErr = <-process.done:
			process.waited = true
		case <-time.After(timeout):
			return errors.New("process stop timeout")
		}
	}
	process.closeMux.Lock()
	defer process.closeMux.Unlock()
	_ = process.stdout.Close()
	_ = process.stderr.Close()
	return nil
}

func waitForAPI(process *zlmProcess, client *apiClient, secret string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if process.pollExit() {
			return errors.New("MediaServer.exe exited before the HTTP API became ready")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		response, err := client.callSecret(ctx, "/index/api/getApiList", secret)
		cancel()
		if err == nil && response.Code == 0 {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("HTTP API readiness timeout")
}

func (client *apiClient) callSecret(ctx context.Context, path, secret string) (apiResponse, error) {
	query := url.Values{}
	query.Set("secret", secret)
	return client.call(ctx, path, query)
}

func (client *apiClient) call(ctx context.Context, path string, query url.Values) (apiResponse, error) {
	requestURL := client.baseURL + path
	if encoded := query.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return apiResponse{}, errors.New("could not create ZLMediaKit API request")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return apiResponse{}, errors.New("ZLMediaKit API request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return apiResponse{}, errors.New("ZLMediaKit API returned an unexpected HTTP status")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAPIResponseBytes+1))
	if err != nil || len(body) > maxAPIResponseBytes {
		return apiResponse{}, errors.New("ZLMediaKit API response could not be read")
	}
	var decoded apiResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return apiResponse{}, errors.New("ZLMediaKit API returned invalid JSON")
	}
	return decoded, nil
}

func checkAPIs(client *apiClient, secret, expectedCommit string) checkResult {
	response, err := client.callSecret(context.Background(), "/index/api/getApiList", secret)
	if err != nil || response.Code != 0 {
		return failedCheck("api_inventory", "authenticated getApiList failed")
	}
	var names []string
	if err := json.Unmarshal(response.Data, &names); err != nil {
		return failedCheck("api_inventory", "getApiList data is not a string array")
	}
	available := make(map[string]bool, len(names))
	for _, name := range names {
		available[name] = true
	}
	required := []string{
		"/index/api/getApiList",
		"/index/api/getServerConfig",
		"/index/api/openRtpServer",
		"/index/api/listRtpServer",
		"/index/api/closeRtpServer",
		"/index/api/getMediaTrafficStatistic",
		"/index/api/getMediaPlayerList",
		"/index/api/addProbe",
		"/index/api/loadMP4File",
		"/index/api/startRecord",
		"/index/api/stopRecord",
		"/index/api/isRecording",
	}
	missing := make([]string, 0)
	for _, name := range required {
		if !available[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		return checkResult{Name: "api_inventory", Status: "failed", Details: map[string]any{"api_count": len(names), "missing": missing}, Error: "required ZLMediaKit APIs are missing"}
	}
	details := map[string]any{"api_count": len(names), "required": len(required)}
	if expectedCommit != "" {
		version, versionErr := client.callSecret(context.Background(), "/index/api/version", secret)
		if versionErr != nil || version.Code != 0 {
			return failedCheck("api_inventory", "authenticated version API failed")
		}
		var info struct {
			CommitHash string `json:"commitHash"`
		}
		if json.Unmarshal(version.Data, &info) != nil || info.CommitHash == "" || !strings.HasPrefix(strings.ToLower(expectedCommit), strings.ToLower(info.CommitHash)) {
			return failedCheck("api_inventory", "ZLMediaKit version does not match the locked commit prefix")
		}
		details["commit_prefix"] = info.CommitHash
	}
	return passedCheck("api_inventory", details)
}

func checkServerConfig(client *apiClient, secret string, expectedHTTPPort int) checkResult {
	response, err := client.callSecret(context.Background(), "/index/api/getServerConfig", secret)
	if err != nil || response.Code != 0 {
		return failedCheck("server_config", "authenticated getServerConfig failed")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(response.Data, &entries); err != nil || len(entries) == 0 {
		return failedCheck("server_config", "getServerConfig did not return a configuration object")
	}
	port, portOK := rawString(entries[0]["http.port"])
	listenIP, ipOK := rawString(entries[0]["general.listen_ip"])
	if !portOK || port != strconv.Itoa(expectedHTTPPort) || !ipOK || listenIP != "127.0.0.1" {
		return failedCheck("server_config", "effective HTTP bind configuration is not isolated to the probe port and loopback")
	}
	return passedCheck("server_config", map[string]any{"config_entries": len(entries[0]), "http_port": expectedHTTPPort, "listen_ip": listenIP})
}

func checkWrongSecret(client *apiClient) checkResult {
	response, err := client.callSecret(context.Background(), "/index/api/getApiList", "definitely-invalid-zlm-secret")
	if err != nil {
		return failedCheck("wrong_secret_rejected", "wrong-secret request failed before an API response was returned")
	}
	if response.Code != -100 {
		return failedCheck("wrong_secret_rejected", "wrong-secret request was not rejected with the ZLMediaKit authentication error")
	}
	return passedCheck("wrong_secret_rejected", map[string]any{"api_code": response.Code, "cookie_replayed": false})
}

func checkRTPLifecycle(client *apiClient, secret string) checkResult {
	stream := randomToken("rtp-")
	query := url.Values{
		"port":        {"0"},
		"vhost":       {"__defaultVhost__"},
		"app":         {"rtp"},
		"stream_id":   {stream},
		"tcp_mode":    {"0"},
		"local_ip":    {"127.0.0.1"},
		"re_use_port": {"0"},
		"ssrc":        {strconv.FormatUint(uint64(time.Now().UnixNano())&0xffffffff, 10)},
		"only_track":  {"0"},
	}
	query.Set("secret", secret)
	open, err := client.call(context.Background(), "/index/api/openRtpServer", query)
	if err != nil || open.Code != 0 {
		return failedCheck("rtp_lifecycle", "openRtpServer failed")
	}
	var port int
	if json.Unmarshal(open.Port, &port) != nil || port <= 0 || port > 65535 {
		return failedCheck("rtp_lifecycle", "openRtpServer did not return an allocated port")
	}
	closeQuery := url.Values{"vhost": {"__defaultVhost__"}, "app": {"rtp"}, "stream_id": {stream}, "secret": {secret}}
	closed := false
	defer func() {
		if !closed {
			_, _ = client.call(context.Background(), "/index/api/closeRtpServer", closeQuery)
		}
	}()
	list, err := client.call(context.Background(), "/index/api/listRtpServer", url.Values{"secret": {secret}})
	if err != nil || list.Code != 0 {
		return failedCheck("rtp_lifecycle", "listRtpServer failed after opening the test server")
	}
	var entries []rtpEntry
	if json.Unmarshal(list.Data, &entries) != nil {
		return failedCheck("rtp_lifecycle", "listRtpServer data is not an array")
	}
	found := false
	for _, entry := range entries {
		if entry.VHost == "__defaultVhost__" && entry.App == "rtp" && entry.StreamID == stream && entry.Port == port {
			found = true
		}
	}
	if !found {
		return failedCheck("rtp_lifecycle", "opened RTP server was not visible in listRtpServer")
	}
	closeResponse, err := client.call(context.Background(), "/index/api/closeRtpServer", closeQuery)
	if err != nil || closeResponse.Code != 0 {
		return failedCheck("rtp_lifecycle", "closeRtpServer failed")
	}
	var hit int
	if json.Unmarshal(closeResponse.Hit, &hit) != nil || hit != 1 {
		return failedCheck("rtp_lifecycle", "closeRtpServer did not report a removed test server")
	}
	closed = true
	remaining, err := client.call(context.Background(), "/index/api/listRtpServer", url.Values{"secret": {secret}})
	if err != nil || remaining.Code != 0 {
		return failedCheck("rtp_lifecycle", "listRtpServer failed after closing the test server")
	}
	entries = nil
	if json.Unmarshal(remaining.Data, &entries) != nil {
		return failedCheck("rtp_lifecycle", "listRtpServer data is not an array after close")
	}
	for _, entry := range entries {
		if entry.VHost == "__defaultVhost__" && entry.App == "rtp" && entry.StreamID == stream {
			return failedCheck("rtp_lifecycle", "closed RTP server remained visible in listRtpServer")
		}
	}
	return passedCheck("rtp_lifecycle", map[string]any{"allocated_port": port, "listed_before_close": true, "close_hit": hit, "listed_after_close": false})
}

func checkMediaLifecycle(client *apiClient, secret, stage, fixture string) checkResult {
	vhost := "__defaultVhost__"
	app := "live"
	stream := randomToken("mp4-")
	loadQuery := url.Values{
		"vhost":       {vhost},
		"app":         {app},
		"stream":      {stream},
		"file_path":   {filepath.ToSlash(fixture)},
		"file_repeat": {"1"},
		"secret":      {secret},
	}
	loaded, err := client.call(context.Background(), "/index/api/loadMP4File", loadQuery)
	if err != nil || loaded.Code != 0 {
		return failedCheck("media_and_recording", "loadMP4File failed for the Chinese/space fixture path")
	}
	var loadedData struct {
		DurationMS uint64 `json:"duration_ms"`
	}
	if json.Unmarshal(loaded.Data, &loadedData) != nil || loadedData.DurationMS == 0 {
		return failedCheck("media_and_recording", "loadMP4File did not report a positive duration")
	}
	if err := waitMediaOnline(client, secret, vhost, app, stream); err != nil {
		return failedCheck("media_and_recording", "the loaded MP4 did not become an online fmp4 source")
	}
	player, err := openPlayer(client.baseURL, app, stream)
	if err != nil {
		return failedCheck("media_and_recording", "HTTP fmp4 player could not be opened")
	}
	defer player.stop(5 * time.Second)
	select {
	case readyErr := <-player.ready:
		if readyErr != nil {
			return failedCheck("media_and_recording", "HTTP fmp4 player did not receive media bytes")
		}
	case <-time.After(mediaReadyTimeout):
		return failedCheck("media_and_recording", "HTTP fmp4 player readiness timed out")
	}

	baseQuery := url.Values{"schema": {"fmp4"}, "vhost": {vhost}, "app": {app}, "stream": {stream}, "secret": {secret}}
	wrongMediaAuth, err := client.callSecret(context.Background(), "/index/api/getMediaPlayerList", "definitely-invalid-zlm-secret")
	if err != nil || wrongMediaAuth.Code != -100 {
		return failedCheck("media_and_recording", "wrong-secret media API request was not rejected")
	}
	players, err := client.call(context.Background(), "/index/api/getMediaPlayerList", baseQuery)
	if err != nil || players.Code != 0 {
		return failedCheck("media_and_recording", "getMediaPlayerList failed for the active fmp4 player")
	}
	var playerEntries []json.RawMessage
	if json.Unmarshal(players.Data, &playerEntries) != nil || len(playerEntries) == 0 {
		return failedCheck("media_and_recording", "getMediaPlayerList did not expose the active player")
	}
	traffic, err := client.call(context.Background(), "/index/api/getMediaTrafficStatistic", baseQuery)
	if err != nil || traffic.Code != 0 {
		return failedCheck("media_and_recording", "getMediaTrafficStatistic failed for the active fmp4 source")
	}
	var trafficData map[string]json.RawMessage
	if json.Unmarshal(traffic.Data, &trafficData) != nil {
		return failedCheck("media_and_recording", "getMediaTrafficStatistic did not return an object")
	}
	unit, unitOK := rawString(trafficData["unit"])
	if !unitOK || unit != "bytes/s" {
		return failedCheck("media_and_recording", "getMediaTrafficStatistic returned an unexpected unit")
	}

	probeQuery := url.Values{"vhost": {vhost}, "app": {app}, "stream": {stream}, "probe_ms": {"500"}, "secret": {secret}}
	probeResponse, err := client.call(context.Background(), "/index/api/addProbe", probeQuery)
	if err != nil || probeResponse.Code != 0 {
		return failedCheck("media_and_recording", "addProbe failed for the active fmp4 source")
	}
	var frames []json.RawMessage
	if json.Unmarshal(probeResponse.Data, &frames) != nil || len(frames) == 0 {
		return failedCheck("media_and_recording", "addProbe did not return observed media frames")
	}

	recordRoot := filepath.Join(stage, "录像 输出 中文 space")
	if err := os.MkdirAll(recordRoot, 0o700); err != nil {
		return failedCheck("media_and_recording", "could not create the Chinese/space recording root")
	}
	recordPath := "./录像 输出 中文 space/"
	startQuery := url.Values{"type": {"1"}, "vhost": {vhost}, "app": {app}, "stream": {stream}, "customized_path": {recordPath}, "max_second": {"30"}, "secret": {secret}}
	started, err := client.call(context.Background(), "/index/api/startRecord", startQuery)
	if err != nil || started.Code != 0 || !rawBool(started.Result) {
		return failedCheck("media_and_recording", "startRecord did not start MP4 recording")
	}
	if err := waitRecording(client, secret, vhost, app, stream, true); err != nil {
		return failedCheck("media_and_recording", "isRecording did not report the active MP4 recorder")
	}
	time.Sleep(2 * time.Second)
	stopQuery := url.Values{"type": {"1"}, "vhost": {vhost}, "app": {app}, "stream": {stream}, "secret": {secret}}
	stopped, err := client.call(context.Background(), "/index/api/stopRecord", stopQuery)
	if err != nil || stopped.Code != 0 || !rawBool(stopped.Result) {
		return failedCheck("media_and_recording", "stopRecord did not stop MP4 recording")
	}
	if err := waitRecording(client, secret, vhost, app, stream, false); err != nil {
		return failedCheck("media_and_recording", "isRecording did not report the stopped MP4 recorder")
	}
	files, bytes := waitForMP4(recordRoot, recordWaitTimeout)
	if files == 0 || bytes == 0 {
		return failedCheck("media_and_recording", "Chinese/space recording path did not produce a non-empty MP4 file")
	}
	return passedCheck("media_and_recording", map[string]any{
		"duration_ms":         loadedData.DurationMS,
		"players":             len(playerEntries),
		"probe_frames":        len(frames),
		"recorded_files":      files,
		"recorded_bytes":      bytes,
		"path_has_chinese":    hasChineseAndSpace(recordRoot),
		"traffic_unit":        unit,
		"media_auth_rejected": true,
	})
}

func waitMediaOnline(client *apiClient, secret, vhost, app, stream string) error {
	deadline := time.Now().Add(mediaReadyTimeout)
	for time.Now().Before(deadline) {
		query := url.Values{"schema": {"fmp4"}, "vhost": {vhost}, "app": {app}, "stream": {stream}, "secret": {secret}}
		response, err := client.call(context.Background(), "/index/api/isMediaOnline", query)
		if err == nil && response.Code == 0 {
			if rawBool(response.Online) {
				return nil
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("fmp4 source readiness timeout")
}

func openPlayer(baseURL, app, stream string) (*playerSession, error) {
	ctx, cancel := context.WithCancel(context.Background())
	requestURL := baseURL + "/" + url.PathEscape(app) + "/" + url.PathEscape(stream) + ".live.mp4"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	ready := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		client := &http.Client{}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			ready <- errors.New("player HTTP request failed")
			done <- requestErr
			return
		}
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			ready <- errors.New("player HTTP request returned an unexpected status")
			done <- errors.New("player HTTP status failed")
			return
		}
		buffer := make([]byte, 32*1024)
		read, readErr := response.Body.Read(buffer)
		if read == 0 && readErr != nil {
			_ = response.Body.Close()
			ready <- errors.New("player response contained no media bytes")
			done <- readErr
			return
		}
		ready <- nil
		for {
			_, readErr = response.Body.Read(buffer)
			if readErr != nil {
				break
			}
		}
		_ = response.Body.Close()
		done <- nil
	}()
	return &playerSession{cancel: cancel, ready: ready, done: done}, nil
}

func (player *playerSession) stop(timeout time.Duration) error {
	if player == nil {
		return nil
	}
	player.cancel()
	select {
	case <-player.done:
		return nil
	case <-time.After(timeout):
		return errors.New("player cleanup timeout")
	}
}

func waitRecording(client *apiClient, secret, vhost, app, stream string, expected bool) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		query := url.Values{"type": {"1"}, "vhost": {vhost}, "app": {app}, "stream": {stream}, "secret": {secret}}
		response, err := client.call(context.Background(), "/index/api/isRecording", query)
		if err == nil && response.Code == 0 && rawBool(response.Status) == expected {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("recording state timeout")
}

func waitForMP4(root string, timeout time.Duration) (files int, bytes int64) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		files, bytes = findMP4(root)
		if files > 0 && bytes > 0 {
			return files, bytes
		}
		time.Sleep(200 * time.Millisecond)
	}
	return findMP4(root)
}

func findMP4(root string) (files int, bytes int64) {
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".mp4") {
			return nil
		}
		if info, statErr := entry.Info(); statErr == nil && info.Size() > 0 {
			files++
			bytes += info.Size()
		}
		return nil
	})
	return files, bytes
}

func reserveTCPPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func randomToken(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return prefix + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return prefix + hex.EncodeToString(raw[:])
}

func rawString(raw json.RawMessage) (string, bool) {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func rawBool(raw json.RawMessage) bool {
	var value bool
	return len(raw) != 0 && json.Unmarshal(raw, &value) == nil && value
}

func isOutsideRoot(path string) bool {
	clean := filepath.Clean(path)
	return clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean)
}

func hasChineseAndSpace(value string) bool {
	return strings.Contains(value, "中文") && strings.Contains(value, " ")
}

func passedCheck(name string, details map[string]any) checkResult {
	return checkResult{Name: name, Status: "passed", Details: details}
}

func failedCheck(name, message string) checkResult {
	return checkResult{Name: name, Status: "failed", Error: message}
}

func notExecutedCheck(name, message string) checkResult {
	return checkResult{Name: name, Status: "not_executed", Error: message}
}

func allChecksPassed(checks []checkResult) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if check.Status != "passed" {
			return false
		}
	}
	return true
}

func hasFailedCheck(checks []checkResult) bool {
	for _, check := range checks {
		if check.Status == "failed" {
			return true
		}
	}
	return false
}
