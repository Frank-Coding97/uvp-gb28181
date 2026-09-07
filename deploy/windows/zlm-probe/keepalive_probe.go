package main

import (
	"bytes"
	"context"
	"crypto/hmac"
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
	"time"
)

const (
	keepaliveProbeInterval        = "0.5"
	keepaliveProbeChangedInterval = "1.0"
	keepaliveProbeQuietWindow     = 2 * time.Second
	keepaliveProbeTransitionLimit = 5 * time.Second
	keepaliveProbeDuplicateWindow = 650 * time.Millisecond
)

// runKeepaliveProbe is deliberately separate from the MP4 probe. It exercises
// only the native ZLMediaKit server-keepalive configuration lifecycle, so a
// failure here cannot be hidden by a successful media fixture.
func runKeepaliveProbe(opts probeOptions) (report probeReport) {
	report = newReport(opts)
	report.Probe = "windows-zlm-keepalive-probe"
	report.Checks = make([]checkResult, 0, 10)

	var stage string
	var server *zlmProcess
	var receiver *hookReceiver
	var audit *keepaliveAuditProxy
	var secret, node string
	defer func() {
		if server != nil {
			if err := server.stop(10 * time.Second); err != nil {
				report.Checks = append(report.Checks, failedCheck("server_stop", "MediaServer.exe did not stop within the bounded cleanup window"))
			}
		}
		if audit != nil {
			if err := audit.close(5 * time.Second); err != nil {
				report.Checks = append(report.Checks, failedCheck("keepalive_audit_stop", "keepalive audit receiver did not stop within the bounded cleanup window"))
			}
		}
		if receiver != nil {
			if err := receiver.close(5 * time.Second); err != nil {
				report.Checks = append(report.Checks, failedCheck("hook_receiver_stop", "controlled Hook receiver did not stop within the bounded cleanup window"))
			}
		}
		if stage != "" {
			sensitive := []string{secret}
			if secret != "" && node != "" {
				sensitive = append(sensitive, hookCapability(secret, node, hookOnServerKeepalive))
			}
			leaked, err := probeLogsContainCapabilityURL(stage, sensitive...)
			switch {
			case err != nil:
				report.Checks = append(report.Checks, failedCheck("log_redaction", "probe process logs could not be inspected for credential URLs"))
			case leaked:
				report.Checks = append(report.Checks, failedCheck("log_redaction", "probe process logs contained a capability URL or runtime credential"))
			default:
				report.Checks = append(report.Checks, passedCheck("log_redaction", map[string]any{"credential_values_absent": true}))
			}
			if !opts.keepTemp {
				_ = os.RemoveAll(stage)
			}
		}
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
			report.Errors = []string{"one or more ZLMediaKit keepalive checks did not pass"}
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

	port, err := reserveTCPPort()
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("http_port", "could not reserve a local HTTP port"))
		return report
	}
	secret = randomToken("zlm-")
	node = randomToken("probe-server-")
	receiver, err = newHookReceiver(node, secret)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("hook_receiver", "controlled Hook receiver could not be started"))
		return report
	}
	audit, err = newKeepaliveAuditProxy(receiver)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_audit", "keepalive audit receiver could not be started"))
		return report
	}
	configPath := filepath.Join(stage, "config.ini")
	if err := configureRuntime(configPath, port, 0, secret, node, receiver); err != nil {
		report.Checks = append(report.Checks, failedCheck("runtime_config", "isolated ZLMediaKit config could not be prepared"))
		return report
	}
	if err := rewriteINI(configPath, map[string]map[string]string{
		"hook": {
			hookOnServerKeepalive: "",
			"alive_interval":      keepaliveProbeInterval,
		},
	}); err != nil {
		report.Checks = append(report.Checks, failedCheck("runtime_config", "initial empty keepalive Hook config could not be prepared"))
		return report
	}

	server, err = startZLM(stageExecutable, stage, configPath)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("server_start", "MediaServer.exe could not be started"))
		return report
	}
	client := &apiClient{baseURL: "http://127.0.0.1:" + strconv.Itoa(port), http: &http.Client{Timeout: apiRequestTimeout}}
	if err := waitForAPI(server, client, secret, serverStartupTimeout); err != nil {
		report.Checks = append(report.Checks, failedCheck("api_readiness", "authenticated ZLMediaKit readiness check failed"))
		return report
	}

	initial, err := readServerKeepaliveConfig(client, secret)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_initial_empty", "authenticated getServerConfig could not be read"))
		return report
	}
	if !initial.matches(true, "", keepaliveProbeInterval) {
		report.Checks = append(report.Checks, failedCheck("keepalive_initial_empty", "server keepalive Hook was configured before the hot-enable step"))
		return report
	}
	initialBoundary := time.Now()
	if err := audit.waitForNoRequestsAfter(initialBoundary, keepaliveProbeQuietWindow); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_initial_empty", "server keepalive callback arrived while the Hook was empty"))
		return report
	}
	if receiver.count(hookOnServerKeepalive) != audit.total() {
		report.Checks = append(report.Checks, failedCheck("keepalive_initial_empty", "a keepalive request bypassed the audit URL"))
		return report
	}
	report.Checks = append(report.Checks, passedCheck("keepalive_initial_empty", map[string]any{
		"configured_empty": true,
		"hook_enable":      initial.enabled,
		"quiet_window_ms":  keepaliveProbeQuietWindow.Milliseconds(),
	}))

	urlA := audit.hookURL("a")
	baselineA := audit.count("a")
	if err := setServerKeepaliveConfig(client, secret, true, urlA, keepaliveProbeInterval, nil); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_hot_enable", "authenticated setServerConfig could not enable server keepalive"))
		return report
	}
	enabled, err := readServerKeepaliveConfig(client, secret)
	if err != nil || !enabled.matches(true, urlA, keepaliveProbeInterval) {
		report.Checks = append(report.Checks, failedCheck("keepalive_hot_enable", "server keepalive enable configuration did not take effect"))
		return report
	}
	if _, err := audit.waitForGeneration("a", baselineA, keepaliveProbeTransitionLimit); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_hot_enable", "hot-enabled server keepalive callback was not received with a valid capability"))
		return report
	}
	if receiver.count(hookOnServerKeepalive) != audit.total() {
		report.Checks = append(report.Checks, failedCheck("keepalive_hot_enable", "a keepalive request bypassed the enabled audit URL"))
		return report
	}
	report.Checks = append(report.Checks, passedCheck("keepalive_hot_enable", map[string]any{
		"hook_enable":       "1",
		"configured":        true,
		"callback_received": true,
		"url_generation":    "a",
	}))

	if err := setServerKeepaliveConfig(client, secret, false, "", keepaliveProbeInterval, nil); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_disabled", "authenticated setServerConfig could not disable server keepalive"))
		return report
	}
	disabledBoundary := time.Now()
	disabled, err := readServerKeepaliveConfig(client, secret)
	if err != nil || !disabled.matches(false, "", keepaliveProbeInterval) {
		report.Checks = append(report.Checks, failedCheck("keepalive_disabled", "server keepalive disable configuration did not take effect"))
		return report
	}
	if err := audit.waitForNoRequestsAfter(disabledBoundary, keepaliveProbeQuietWindow); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_disabled", "server keepalive callback continued after disable outside the in-flight boundary"))
		return report
	}
	if receiver.count(hookOnServerKeepalive) != audit.total() {
		report.Checks = append(report.Checks, failedCheck("keepalive_disabled", "a keepalive request bypassed the disabled audit URL"))
		return report
	}
	report.Checks = append(report.Checks, passedCheck("keepalive_disabled", map[string]any{
		"hook_enable":      "0",
		"configured_empty": true,
		"callbacks_after":  audit.countRequestsAfter(disabledBoundary),
		"quiet_window_ms":  keepaliveProbeQuietWindow.Milliseconds(),
	}))

	urlB := audit.hookURL("b")
	baselineB := audit.count("b")
	if err := setServerKeepaliveConfig(client, secret, true, urlB, keepaliveProbeInterval, nil); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_reenable", "authenticated setServerConfig could not re-enable server keepalive"))
		return report
	}
	switchBoundary := time.Now()
	reenabled, err := readServerKeepaliveConfig(client, secret)
	if err != nil || !reenabled.matches(true, urlB, keepaliveProbeInterval) {
		report.Checks = append(report.Checks, failedCheck("keepalive_reenable", "server keepalive URL or enable state did not return after re-enable"))
		return report
	}
	if _, err := audit.waitForGeneration("b", baselineB, keepaliveProbeTransitionLimit); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_reenable", "re-enabled server keepalive callback was not received with the new URL generation"))
		return report
	}
	if err := audit.waitForNoGenerationAfter("a", switchBoundary, keepaliveProbeQuietWindow); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_reenable", "old server keepalive URL continued after the URL switch"))
		return report
	}
	if err := audit.waitForNoInvalidAfter(switchBoundary, keepaliveProbeQuietWindow); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_reenable", "server keepalive callback capability was invalid after the URL switch"))
		return report
	}
	if receiver.count(hookOnServerKeepalive) != audit.total() {
		report.Checks = append(report.Checks, failedCheck("keepalive_reenable", "a keepalive request bypassed the new audit URL"))
		return report
	}
	report.Checks = append(report.Checks, passedCheck("keepalive_reenable", map[string]any{
		"hook_enable":          "1",
		"configured":           true,
		"callback_received":    true,
		"url_generation":       "b",
		"old_url_after_switch": false,
	}))

	if err := setServerKeepaliveConfig(client, secret, true, urlB, keepaliveProbeChangedInterval, nil); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_interval_change", "authenticated setServerConfig could not change keepalive interval"))
		return report
	}
	changed, err := readServerKeepaliveConfig(client, secret)
	if err != nil || !changed.matches(true, urlB, keepaliveProbeChangedInterval) {
		report.Checks = append(report.Checks, failedCheck("keepalive_interval_change", "server keepalive interval did not change without losing the active URL"))
		return report
	}
	intervalBaseline := audit.count("b")
	firstChanged, err := audit.waitForGeneration("b", intervalBaseline, keepaliveProbeTransitionLimit)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_interval_change", "first callback after interval change was not received"))
		return report
	}
	secondChanged, err := audit.waitForGeneration("b", intervalBaseline+1, keepaliveProbeTransitionLimit)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_interval_change", "second callback after interval change was not received"))
		return report
	}
	observedGap := secondChanged.arrivedAt.Sub(firstChanged.arrivedAt)
	if observedGap < keepaliveProbeDuplicateWindow {
		report.Checks = append(report.Checks, failedCheck("keepalive_interval_change", "keepalive callbacks remained at the pre-change interval"))
		return report
	}
	if receiver.count(hookOnServerKeepalive) != audit.total() {
		report.Checks = append(report.Checks, failedCheck("keepalive_interval_change", "a keepalive request bypassed the interval-change audit URL"))
		return report
	}
	report.Checks = append(report.Checks, passedCheck("keepalive_interval_change", map[string]any{
		"interval_seconds": 1,
		"observed_gap_ms":  observedGap.Milliseconds(),
		"url_generation":   "b",
		"capability_valid": true,
	}))

	time.Sleep(200 * time.Millisecond)
	reloadBaseline, lastBeforeReload, haveLast := audit.generationSnapshot("b")
	if !haveLast {
		report.Checks = append(report.Checks, failedCheck("keepalive_unrelated_reload", "keepalive callback history was empty before unrelated reload"))
		return report
	}
	if err := setServerConfigValues(client, secret, map[string]string{"general.flowThreshold": "0"}); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_unrelated_reload", "unrelated server configuration reload failed"))
		return report
	}
	reloaded, err := readServerKeepaliveConfig(client, secret)
	if err != nil || !reloaded.matches(true, urlB, keepaliveProbeChangedInterval) {
		report.Checks = append(report.Checks, failedCheck("keepalive_unrelated_reload", "unrelated reload changed the active keepalive configuration"))
		return report
	}
	nextAfterReload, err := audit.waitForGeneration("b", reloadBaseline, keepaliveProbeTransitionLimit)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_unrelated_reload", "keepalive callback did not continue after unrelated reload"))
		return report
	}
	reloadGap := nextAfterReload.arrivedAt.Sub(lastBeforeReload.arrivedAt)
	if reloadGap < keepaliveProbeDuplicateWindow {
		report.Checks = append(report.Checks, failedCheck("keepalive_unrelated_reload", "unrelated reload appeared to create a duplicate keepalive timer"))
		return report
	}
	if err := audit.waitForNoGenerationAfter("b", nextAfterReload.arrivedAt, keepaliveProbeDuplicateWindow); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_unrelated_reload", "unrelated reload produced an extra immediate keepalive callback"))
		return report
	}
	if receiver.count(hookOnServerKeepalive) != audit.total() {
		report.Checks = append(report.Checks, failedCheck("keepalive_unrelated_reload", "a keepalive request bypassed the reloaded audit URL"))
		return report
	}
	report.Checks = append(report.Checks, passedCheck("keepalive_unrelated_reload", map[string]any{
		"unrelated_reload":    true,
		"duplicate_window_ms": keepaliveProbeDuplicateWindow.Milliseconds(),
		"observed_gap_ms":     reloadGap.Milliseconds(),
		"url_generation":      "b",
		"timer_continued":     true,
		"capability_valid":    true,
	}))
	return report
}

type serverKeepaliveConfig struct {
	enabled  string
	interval string
	hookURL  string
}

func setServerKeepalive(client *apiClient, secret, hookURL string) error {
	return setServerKeepaliveConfig(client, secret, true, hookURL, keepaliveProbeInterval, nil)
}

func setServerKeepaliveConfig(client *apiClient, secret string, enabled bool, hookURL, interval string, extra map[string]string) error {
	values := map[string]string{
		"hook.enable":              boolConfigValue(enabled),
		"hook.alive_interval":      interval,
		"hook.on_server_keepalive": hookURL,
	}
	for key, value := range extra {
		values[key] = value
	}
	return setServerConfigValues(client, secret, values)
}

func setServerConfigValues(client *apiClient, secret string, values map[string]string) error {
	query := url.Values{}
	query.Set("secret", secret)
	for key, value := range values {
		query.Set(key, value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
	defer cancel()
	response, err := client.call(ctx, "/index/api/setServerConfig", query)
	if err != nil || response.Code != 0 {
		return errors.New("authenticated setServerConfig failed")
	}
	return nil
}

func readServerKeepaliveValue(client *apiClient, secret string) (string, error) {
	config, err := readServerKeepaliveConfig(client, secret)
	if err != nil {
		return "", err
	}
	return config.hookURL, nil
}

func readServerKeepaliveConfig(client *apiClient, secret string) (serverKeepaliveConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
	defer cancel()
	response, err := client.callSecret(ctx, "/index/api/getServerConfig", secret)
	if err != nil || response.Code != 0 {
		return serverKeepaliveConfig{}, errors.New("authenticated getServerConfig failed")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(response.Data, &entries); err != nil || len(entries) == 0 {
		return serverKeepaliveConfig{}, errors.New("getServerConfig did not return a configuration object")
	}
	hookURL, err := readConfigString(entries[0], "hook.on_server_keepalive")
	if err != nil {
		return serverKeepaliveConfig{}, errors.New("getServerConfig returned a non-string keepalive Hook")
	}
	enabled, err := readConfigString(entries[0], "hook.enable")
	if err != nil {
		return serverKeepaliveConfig{}, errors.New("getServerConfig returned a non-string Hook enable state")
	}
	interval, err := readConfigString(entries[0], "hook.alive_interval")
	if err != nil {
		return serverKeepaliveConfig{}, errors.New("getServerConfig returned a non-string keepalive interval")
	}
	return serverKeepaliveConfig{enabled: enabled, interval: interval, hookURL: hookURL}, nil
}

func readConfigString(entry map[string]json.RawMessage, key string) (string, error) {
	raw, ok := entry[key]
	if !ok || len(raw) == 0 {
		return "", nil
	}
	value, ok := rawString(raw)
	if !ok {
		return "", errors.New("configuration value is not a string")
	}
	return value, nil
}

func waitForKeepaliveQuiet(receiver *hookReceiver, baseline int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if receiver.count(hookOnServerKeepalive) != baseline {
			return errors.New("unexpected server keepalive callback")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil
		}
		interval := 50 * time.Millisecond
		if remaining < interval {
			interval = remaining
		}
		time.Sleep(interval)
	}
}

func (config serverKeepaliveConfig) matches(enabled bool, hookURL, interval string) bool {
	return config.enabled == boolConfigValue(enabled) && strings.TrimSpace(config.hookURL) == strings.TrimSpace(hookURL) && intervalsEqual(config.interval, interval)
}

func boolConfigValue(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}

func intervalsEqual(got, want string) bool {
	gotSeconds, gotErr := strconv.ParseFloat(strings.TrimSpace(got), 64)
	wantSeconds, wantErr := strconv.ParseFloat(strings.TrimSpace(want), 64)
	return gotErr == nil && wantErr == nil && gotSeconds >= 0 && wantSeconds >= 0 && gotSeconds-wantSeconds < 0.01 && wantSeconds-gotSeconds < 0.01
}

type keepaliveAuditRequest struct {
	generation      string
	capabilityValid bool
	arrivedAt       time.Time
}

type keepaliveAuditProxy struct {
	target   *hookReceiver
	server   *http.Server
	listener net.Listener
	baseURL  string
	client   *http.Client

	mu       sync.Mutex
	requests []keepaliveAuditRequest
}

func newKeepaliveAuditProxy(target *hookReceiver) (*keepaliveAuditProxy, error) {
	if target == nil {
		return nil, errors.New("keepalive audit target is nil")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("keepalive audit receiver could not bind a local port")
	}
	proxy := &keepaliveAuditProxy{
		target:   target,
		server:   &http.Server{ReadHeaderTimeout: 3 * time.Second},
		listener: listener,
		baseURL:  "http://" + listener.Addr().String(),
		client:   &http.Client{Timeout: 3 * time.Second},
	}
	proxy.server.Handler = http.HandlerFunc(proxy.handle)
	go func() { _ = proxy.server.Serve(listener) }()
	return proxy, nil
}

func (proxy *keepaliveAuditProxy) close(timeout time.Duration) error {
	if proxy == nil || proxy.server == nil {
		return nil
	}
	ctx, cancel := contextWithTimeout(timeout)
	defer cancel()
	err := proxy.server.Shutdown(ctx)
	if err != nil {
		_ = proxy.listener.Close()
	}
	return err
}

func (proxy *keepaliveAuditProxy) hookURL(generation string) string {
	query := url.Values{}
	query.Set("cap", hookCapability(proxy.target.secret, proxy.target.node, hookOnServerKeepalive))
	query.Set("node", proxy.target.node)
	query.Set("probe_generation", generation)
	return proxy.baseURL + "/index/hook/" + hookOnServerKeepalive + "?" + query.Encode()
}

func (proxy *keepaliveAuditProxy) handle(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.URL.Path != "/index/hook/"+hookOnServerKeepalive {
		http.NotFound(writer, request)
		return
	}
	query := request.URL.Query()
	caps := query["cap"]
	capabilityValid := query.Get("node") == proxy.target.node && len(caps) == 1 && hmac.Equal([]byte(caps[0]), []byte(hookCapability(proxy.target.secret, proxy.target.node, hookOnServerKeepalive)))
	auditRequest := keepaliveAuditRequest{generation: query.Get("probe_generation"), capabilityValid: capabilityValid, arrivedAt: time.Now()}
	defer proxy.record(auditRequest)

	body, err := io.ReadAll(io.LimitReader(request.Body, hookRequestBodyLimit+1))
	_ = request.Body.Close()
	if err != nil || len(body) == 0 || len(body) > hookRequestBodyLimit {
		http.Error(writer, "invalid Hook body", http.StatusBadRequest)
		return
	}
	forwardedURL := proxy.target.baseURL + "/index/hook/" + hookOnServerKeepalive
	if encoded := query.Encode(); encoded != "" {
		forwardedURL += "?" + encoded
	}
	forwarded, err := http.NewRequest(http.MethodPost, forwardedURL, bytes.NewReader(body))
	if err != nil {
		http.Error(writer, "Hook forwarding failed", http.StatusBadGateway)
		return
	}
	for _, header := range []string{"Content-Type", "X-VHOST"} {
		if value := request.Header.Get(header); value != "" {
			forwarded.Header.Set(header, value)
		}
	}
	response, err := proxy.client.Do(forwarded)
	if err != nil {
		http.Error(writer, "Hook forwarding failed", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		writer.Header().Set("Content-Type", contentType)
	}
	writer.WriteHeader(response.StatusCode)
	_, _ = io.CopyN(writer, response.Body, hookResponseWriteLimit)
}

func (proxy *keepaliveAuditProxy) record(request keepaliveAuditRequest) {
	proxy.mu.Lock()
	proxy.requests = append(proxy.requests, request)
	proxy.mu.Unlock()
}

func (proxy *keepaliveAuditProxy) count(generation string) int {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	count := 0
	for _, request := range proxy.requests {
		if request.generation == generation {
			count++
		}
	}
	return count
}

func (proxy *keepaliveAuditProxy) total() int {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	return len(proxy.requests)
}

func (proxy *keepaliveAuditProxy) generationSnapshot(generation string) (int, keepaliveAuditRequest, bool) {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	count := 0
	var last keepaliveAuditRequest
	haveLast := false
	for _, request := range proxy.requests {
		if request.generation != generation {
			continue
		}
		count++
		last = request
		haveLast = true
	}
	return count, last, haveLast
}

func (proxy *keepaliveAuditProxy) countRequestsAfter(boundary time.Time) int {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	count := 0
	for _, request := range proxy.requests {
		if request.arrivedAt.After(boundary) {
			count++
		}
	}
	return count
}

func (proxy *keepaliveAuditProxy) waitForGeneration(generation string, after int, timeout time.Duration) (keepaliveAuditRequest, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		proxy.mu.Lock()
		seen := 0
		for _, request := range proxy.requests {
			if request.generation != generation {
				continue
			}
			seen++
			if seen > after {
				proxy.mu.Unlock()
				if !request.capabilityValid {
					return keepaliveAuditRequest{}, errors.New("keepalive callback capability was invalid")
				}
				return request, nil
			}
		}
		proxy.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	return keepaliveAuditRequest{}, errors.New("keepalive callback was not received")
}

func (proxy *keepaliveAuditProxy) waitForNoRequestsAfter(boundary time.Time, timeout time.Duration) error {
	return proxy.waitForNoRequestAfter(boundary, func(keepaliveAuditRequest) bool { return true }, timeout)
}

func (proxy *keepaliveAuditProxy) waitForNoGenerationAfter(generation string, boundary time.Time, timeout time.Duration) error {
	return proxy.waitForNoRequestAfter(boundary, func(request keepaliveAuditRequest) bool {
		return request.generation == generation
	}, timeout)
}

func (proxy *keepaliveAuditProxy) waitForNoInvalidAfter(boundary time.Time, timeout time.Duration) error {
	return proxy.waitForNoRequestAfter(boundary, func(request keepaliveAuditRequest) bool {
		return !request.capabilityValid
	}, timeout)
}

func (proxy *keepaliveAuditProxy) waitForNoRequestAfter(boundary time.Time, matches func(keepaliveAuditRequest) bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		proxy.mu.Lock()
		for _, request := range proxy.requests {
			if request.arrivedAt.After(boundary) && matches(request) {
				proxy.mu.Unlock()
				return errors.New("unexpected keepalive callback after transition")
			}
		}
		proxy.mu.Unlock()
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		interval := 50 * time.Millisecond
		if remaining < interval {
			interval = remaining
		}
		time.Sleep(interval)
	}
	return nil
}

// probeLogsContainCapabilityURL returns only a boolean so a diagnostic report
// cannot echo a credential-bearing URL from the child process logs. The probe
// passes its actual random secret and capability value in sensitiveValues;
// the generic markers preserve detection for older runtimes that log a URL.
func probeLogsContainCapabilityURL(stage string, sensitiveValues ...string) (bool, error) {
	for _, name := range []string{"probe-stdout.log", "probe-stderr.log"} {
		data, err := os.ReadFile(filepath.Join(stage, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		text := string(data)
		lower := strings.ToLower(text)
		for _, value := range sensitiveValues {
			if value != "" && (strings.Contains(text, value) || strings.Contains(lower, strings.ToLower(value))) {
				return true, nil
			}
		}
		if strings.Contains(lower, "cap=") || strings.Contains(lower, "cap%3d") {
			return true, nil
		}
	}
	return false, nil
}
