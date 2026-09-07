package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	keepaliveProbeInterval        = "0.5"
	keepaliveProbeQuietWindow     = 2 * time.Second
	keepaliveProbeTransitionLimit = 5 * time.Second
)

// runKeepaliveProbe is deliberately separate from the MP4 probe. It exercises
// only the native ZLMediaKit server-keepalive configuration lifecycle, so a
// failure here cannot be hidden by a successful media fixture.
func runKeepaliveProbe(opts probeOptions) (report probeReport) {
	report = newReport(opts)
	report.Probe = "windows-zlm-keepalive-probe"
	report.Checks = make([]checkResult, 0, 8)

	var stage string
	var server *zlmProcess
	var receiver *hookReceiver
	defer func() {
		if server != nil {
			if err := server.stop(10 * time.Second); err != nil {
				report.Checks = append(report.Checks, failedCheck("server_stop", "MediaServer.exe did not stop within the bounded cleanup window"))
			}
		}
		if receiver != nil {
			if err := receiver.close(5 * time.Second); err != nil {
				report.Checks = append(report.Checks, failedCheck("hook_receiver_stop", "controlled Hook receiver did not stop within the bounded cleanup window"))
			}
		}
		if stage != "" {
			leaked, err := probeLogsContainCapabilityURL(stage)
			switch {
			case err != nil:
				report.Checks = append(report.Checks, failedCheck("log_redaction", "probe process logs could not be inspected for credential URLs"))
			case leaked:
				report.Checks = append(report.Checks, failedCheck("log_redaction", "probe process logs contained a capability URL"))
			default:
				report.Checks = append(report.Checks, passedCheck("log_redaction", map[string]any{"capability_url_absent": true}))
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
	secret := randomToken("zlm-")
	node := randomToken("probe-server-")
	receiver, err = newHookReceiver(node, secret)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("hook_receiver", "controlled Hook receiver could not be started"))
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

	initialValue, err := readServerKeepaliveValue(client, secret)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_initial_empty", "authenticated getServerConfig could not be read"))
		return report
	}
	if strings.TrimSpace(initialValue) != "" {
		report.Checks = append(report.Checks, failedCheck("keepalive_initial_empty", "server keepalive Hook was configured before the hot-enable step"))
		return report
	}
	initialCount := receiver.count(hookOnServerKeepalive)
	if err := waitForKeepaliveQuiet(receiver, initialCount, keepaliveProbeQuietWindow); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_initial_empty", "server keepalive callback arrived while the Hook was empty"))
		return report
	}
	report.Checks = append(report.Checks, passedCheck("keepalive_initial_empty", map[string]any{
		"configured_empty": true,
		"quiet_window_ms":  keepaliveProbeQuietWindow.Milliseconds(),
	}))

	if err := setServerKeepalive(client, secret, receiver.hookURL(hookOnServerKeepalive)); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_hot_enable", "authenticated setServerConfig could not enable server keepalive"))
		return report
	}
	configuredValue, err := readServerKeepaliveValue(client, secret)
	if err != nil || configuredValue != receiver.hookURL(hookOnServerKeepalive) {
		report.Checks = append(report.Checks, failedCheck("keepalive_hot_enable", "server keepalive configuration did not take effect"))
		return report
	}
	hotEnableCount := receiver.count(hookOnServerKeepalive)
	if _, err := receiver.waitFor(hookOnServerKeepalive, hotEnableCount, keepaliveProbeTransitionLimit); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_hot_enable", "hot-enabled server keepalive callback was not received"))
		return report
	}
	report.Checks = append(report.Checks, passedCheck("keepalive_hot_enable", map[string]any{"configured": true, "callback_received": true}))

	if err := setServerKeepalive(client, secret, ""); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_disabled", "authenticated setServerConfig could not disable server keepalive"))
		return report
	}
	disabledValue, err := readServerKeepaliveValue(client, secret)
	if err != nil || strings.TrimSpace(disabledValue) != "" {
		report.Checks = append(report.Checks, failedCheck("keepalive_disabled", "server keepalive configuration did not become empty"))
		return report
	}
	disabledCount := receiver.count(hookOnServerKeepalive)
	if err := waitForKeepaliveQuiet(receiver, disabledCount, keepaliveProbeQuietWindow); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_disabled", "server keepalive callback continued after the Hook was disabled"))
		return report
	}
	report.Checks = append(report.Checks, passedCheck("keepalive_disabled", map[string]any{
		"configured_empty": true,
		"quiet_window_ms":  keepaliveProbeQuietWindow.Milliseconds(),
	}))

	if err := setServerKeepalive(client, secret, receiver.hookURL(hookOnServerKeepalive)); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_reenable", "authenticated setServerConfig could not re-enable server keepalive"))
		return report
	}
	reenabledValue, err := readServerKeepaliveValue(client, secret)
	if err != nil || reenabledValue != receiver.hookURL(hookOnServerKeepalive) {
		report.Checks = append(report.Checks, failedCheck("keepalive_reenable", "server keepalive configuration did not return after re-enable"))
		return report
	}
	reenableCount := receiver.count(hookOnServerKeepalive)
	if _, err := receiver.waitFor(hookOnServerKeepalive, reenableCount, keepaliveProbeTransitionLimit); err != nil {
		report.Checks = append(report.Checks, failedCheck("keepalive_reenable", "re-enabled server keepalive callback was not received"))
		return report
	}
	report.Checks = append(report.Checks, passedCheck("keepalive_reenable", map[string]any{"configured": true, "callback_received": true}))
	return report
}

func setServerKeepalive(client *apiClient, secret, hookURL string) error {
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("hook.alive_interval", keepaliveProbeInterval)
	query.Set("hook.on_server_keepalive", hookURL)
	ctx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
	defer cancel()
	response, err := client.call(ctx, "/index/api/setServerConfig", query)
	if err != nil || response.Code != 0 {
		return errors.New("authenticated setServerConfig failed")
	}
	return nil
}

func readServerKeepaliveValue(client *apiClient, secret string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
	defer cancel()
	response, err := client.callSecret(ctx, "/index/api/getServerConfig", secret)
	if err != nil || response.Code != 0 {
		return "", errors.New("authenticated getServerConfig failed")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(response.Data, &entries); err != nil || len(entries) == 0 {
		return "", errors.New("getServerConfig did not return a configuration object")
	}
	raw, ok := entries[0]["hook.on_server_keepalive"]
	if !ok || len(raw) == 0 {
		return "", nil
	}
	value, ok := rawString(raw)
	if !ok {
		return "", errors.New("getServerConfig returned a non-string keepalive Hook")
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

// probeLogsContainCapabilityURL returns only a boolean so a diagnostic report
// cannot echo a credential-bearing URL from the child process logs.
func probeLogsContainCapabilityURL(stage string) (bool, error) {
	for _, name := range []string{"probe-stdout.log", "probe-stderr.log"} {
		data, err := os.ReadFile(filepath.Join(stage, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if strings.Contains(strings.ToLower(string(data)), "cap=") {
			return true, nil
		}
	}
	return false, nil
}
