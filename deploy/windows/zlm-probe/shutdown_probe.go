package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	shutdownProbeName             = "windows-zlm-shutdown-probe"
	shutdownBarrierMarker         = "uvp-finalization-barrier-waiting"
	shutdownBarrierFailureMarker  = "uvp-finalization-barrier-failed"
	shutdownHookWait              = 12 * time.Second
	shutdownBarrierWait           = 30 * time.Second
	shutdownBarrierAliveProof     = 500 * time.Millisecond
	shutdownProcessExitWait       = 30 * time.Second
	shutdownRecordingDataMinBytes = 1024
)

type shutdownHookMode uint8

const (
	shutdownHookBlockThenSuccess shutdownHookMode = iota + 1
	shutdownHookRetryThenSuccess
	shutdownHookAlwaysFailure
)

type shutdownProbeScenario struct {
	name                string
	mode                shutdownHookMode
	expectCleanExit     bool
	requireBarrier      bool
	requireBarrierAlive bool
	requireBarrierFail  bool
	expectedHookAttempt int
}

func runShutdownProbe(opts probeOptions) (report probeReport) {
	report = newReport(opts)
	report.Probe = shutdownProbeName
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
			report.Errors = []string{"one or more shutdown scenarios did not pass or execute"}
		}
	}()

	if opts.fixture == "" {
		report.Unexecuted = append(report.Unexecuted, "shutdown_hook_blocked", "shutdown_hook_retry", "shutdown_hook_failure")
		report.Checks = append(report.Checks, failedCheck("shutdown_probe_input", "an MP4 fixture is required for the shutdown probe"))
		return report
	}
	root, executable, relExecutable, err := resolveInput(opts)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("shutdown_probe_input", "runtime input is invalid"))
		return report
	}
	if report.BinarySHA256, err = sha256File(executable); err != nil {
		report.Checks = append(report.Checks, failedCheck("shutdown_probe_input", "MediaServer.exe hash could not be computed"))
		return report
	}
	if report.FixtureSHA256, err = sha256File(opts.fixture); err != nil {
		report.Checks = append(report.Checks, failedCheck("shutdown_probe_input", "MP4 fixture hash could not be computed"))
		return report
	}

	scenarios := []shutdownProbeScenario{
		{name: "shutdown_hook_blocked", mode: shutdownHookBlockThenSuccess, expectCleanExit: true, requireBarrier: true, requireBarrierAlive: true, expectedHookAttempt: 1},
		{name: "shutdown_hook_retry", mode: shutdownHookRetryThenSuccess, expectCleanExit: true, requireBarrier: true, requireBarrierAlive: true, expectedHookAttempt: 2},
		{name: "shutdown_hook_failure", mode: shutdownHookAlwaysFailure, expectCleanExit: false, requireBarrier: true, requireBarrierFail: true, expectedHookAttempt: 2},
	}
	for _, scenario := range scenarios {
		check, stage, stats := runShutdownScenario(opts, scenario, root, relExecutable)
		report.Checks = append(report.Checks, check)
		if report.Workspace == "" && stage != "" {
			report.Workspace = stage
		}
		if report.ResourceFileCount == 0 && stats.Files != 0 {
			report.ResourceFileCount = stats.Files
			report.ResourceBytes = stats.Bytes
		}
	}
	return report
}

func runShutdownScenario(opts probeOptions, scenario shutdownProbeScenario, root, relExecutable string) (result checkResult, stage string, stats resourceStats) {
	result = failedCheck(scenario.name, "shutdown scenario did not complete")
	var stageExecutable string
	var err error
	stage, stageExecutable, stats, err = copyRuntimeTree(root, relExecutable)
	if err != nil {
		return failedCheck(scenario.name, "isolated runtime copy failed"), stage, stats
	}

	var receiver *hookReceiver
	var gate *shutdownHookGate
	var player *playerSession
	playerStopped := false
	var inputReader, inputWriter *os.File
	var server *zlmProcess
	defer func() {
		if gate != nil {
			gate.release()
		}
		if player != nil && !playerStopped {
			if stopErr := player.stop(5 * time.Second); stopErr != nil && result.Status == "passed" {
				result = failedCheck(scenario.name, "HTTP fmp4 player cleanup timed out")
			}
		}
		if server != nil {
			if stopErr := server.stop(10 * time.Second); stopErr != nil && result.Status == "passed" {
				result = failedCheck(scenario.name, "MediaServer.exe cleanup timed out")
			}
		}
		if inputWriter != nil {
			_ = inputWriter.Close()
		}
		if inputReader != nil {
			_ = inputReader.Close()
		}
		if gate != nil {
			gate.close()
		}
		if receiver != nil {
			if closeErr := receiver.close(5 * time.Second); closeErr != nil && result.Status == "passed" {
				result = failedCheck(scenario.name, "Hook receiver cleanup timed out")
			}
		}
		if !opts.keepTemp {
			_ = os.RemoveAll(stage)
		}
	}()

	fixturePath, err := copyFixture(opts.fixture, stage)
	if err != nil {
		return failedCheck(scenario.name, "MP4 fixture copy failed"), stage, stats
	}
	httpPort, err := reserveTCPPort()
	if err != nil {
		return failedCheck(scenario.name, "shutdown probe HTTP port could not be reserved"), stage, stats
	}
	rtspPort, err := reserveTCPPort()
	if err != nil {
		return failedCheck(scenario.name, "shutdown probe RTSP port could not be reserved"), stage, stats
	}
	secret := randomToken("zlm-")
	node := randomToken("shutdown-node-")
	receiver, err = newHookReceiver(node, secret)
	if err != nil {
		return failedCheck(scenario.name, "controlled Hook receiver could not be started"), stage, stats
	}
	playToken := randomToken("fixture-play-")
	publishToken := randomToken("fixture-publish-")
	receiver.setTokens(playToken, publishToken)
	configPath := filepath.Join(stage, "config.ini")
	if err := configureRuntime(configPath, httpPort, rtspPort, secret, node, receiver); err != nil {
		return failedCheck(scenario.name, "isolated ZLMediaKit config could not be prepared"), stage, stats
	}
	markerPath := filepath.Join(stage, "shutdown-index.marker")
	gate, err = newShutdownHookGate(receiver, scenario.mode, markerPath)
	if err != nil {
		return failedCheck(scenario.name, "recording Hook gate could not be started"), stage, stats
	}
	if err := rewriteINI(configPath, map[string]map[string]string{"hook": {hookOnRecordMP4: gate.url()}}); err != nil {
		return failedCheck(scenario.name, "recording Hook gate could not be configured"), stage, stats
	}

	inputReader, inputWriter, err = os.Pipe()
	if err != nil {
		return failedCheck(scenario.name, "exclusive stdin pipe could not be created"), stage, stats
	}
	server, err = startZLMWithInput(stageExecutable, stage, configPath, inputReader)
	if err != nil {
		return failedCheck(scenario.name, "MediaServer.exe could not be started with stdin control"), stage, stats
	}
	client := &apiClient{baseURL: "http://127.0.0.1:" + strconv.Itoa(httpPort), http: &http.Client{Timeout: apiRequestTimeout}}
	if err := waitForAPI(server, client, secret, serverStartupTimeout); err != nil {
		return failedCheck(scenario.name, "MediaServer.exe did not expose a ready authenticated HTTP API"), stage, stats
	}
	if check := checkAPIs(client, secret, opts.expectedCommit); check.Status != "passed" {
		return failedCheck(scenario.name, "authenticated API inventory or locked commit verification failed"), stage, stats
	}

	vhost, app, stream := "__defaultVhost__", "live", randomToken("shutdown-")
	loaded, err := client.call(context.Background(), "/index/api/loadMP4File", url.Values{
		"vhost": {vhost}, "app": {app}, "stream": {stream}, "file_path": {filepath.ToSlash(fixturePath)}, "file_repeat": {"1"}, "secret": {secret},
	})
	if err != nil || loaded.Code != 0 {
		return failedCheck(scenario.name, "loadMP4File failed for the shutdown fixture"), stage, stats
	}
	var loadedData struct {
		DurationMS uint64 `json:"duration_ms"`
	}
	if json.Unmarshal(loaded.Data, &loadedData) != nil || loadedData.DurationMS == 0 {
		return failedCheck(scenario.name, "loadMP4File did not report a positive duration"), stage, stats
	}
	if err := waitMediaOnline(client, secret, vhost, app, stream); err != nil {
		return failedCheck(scenario.name, "the loaded fixture did not become an online fmp4 source"), stage, stats
	}
	player, err = openPlayerWithQuery(client.baseURL, app, stream, url.Values{"play_token": {playToken}})
	if err != nil {
		return failedCheck(scenario.name, "HTTP fmp4 player could not be opened for the shutdown fixture"), stage, stats
	}
	select {
	case readyErr := <-player.ready:
		if readyErr != nil {
			return failedCheck(scenario.name, "HTTP fmp4 player did not receive fixture media bytes"), stage, stats
		}
	case <-time.After(mediaReadyTimeout):
		return failedCheck(scenario.name, "HTTP fmp4 player readiness timed out"), stage, stats
	}

	recordRoot := filepath.Join(stage, "录像 输出 中文 space")
	if err := os.MkdirAll(recordRoot, 0o700); err != nil {
		return failedCheck(scenario.name, "recording output directory could not be created"), stage, stats
	}
	recordPath := "./录像 输出 中文 space/"
	started, err := client.call(context.Background(), "/index/api/startRecord", url.Values{
		"type": {"1"}, "vhost": {vhost}, "app": {app}, "stream": {stream}, "customized_path": {recordPath}, "max_second": {"30"}, "secret": {secret},
	})
	if err != nil || started.Code != 0 || !rawBool(started.Result) {
		return failedCheck(scenario.name, "startRecord did not start MP4 recording"), stage, stats
	}
	if err := waitRecording(client, secret, vhost, app, stream, true); err != nil {
		return failedCheck(scenario.name, "isRecording did not report the active MP4 recorder"), stage, stats
	}
	files, bytes := waitForMP4Size(recordRoot, shutdownRecordingDataMinBytes, recordWaitTimeout)
	if files == 0 || bytes < shutdownRecordingDataMinBytes {
		return failedCheck(scenario.name, "recording produced no confirmed temporary MP4 data"), stage, stats
	}

	stopped, err := client.call(context.Background(), "/index/api/stopRecord", url.Values{
		"type": {"1"}, "vhost": {vhost}, "app": {app}, "stream": {stream}, "secret": {secret},
	})
	if err != nil || stopped.Code != 0 || !rawBool(stopped.Result) {
		return failedCheck(scenario.name, "stopRecord did not stop MP4 recording"), stage, stats
	}
	if err := waitRecording(client, secret, vhost, app, stream, false); err != nil {
		return failedCheck(scenario.name, "isRecording did not report the stopped MP4 recorder"), stage, stats
	}
	if err := player.stop(5 * time.Second); err != nil {
		return failedCheck(scenario.name, "HTTP fmp4 player cleanup timed out before shutdown"), stage, stats
	}
	playerStopped = true
	if err := gate.waitForAttempt(scenario.expectedHookAttempt, shutdownHookWait); err != nil {
		return failedCheck(scenario.name, "on_record_mp4 did not reach the controlled Hook gate"), stage, stats
	}
	formalPath, ok := gate.recordPath()
	if !ok {
		return failedCheck(scenario.name, "on_record_mp4 did not provide a formal MP4 path before shutdown"), stage, stats
	}
	formalPath, ok = shutdownPathInStage(stage, formalPath)
	if !ok {
		return failedCheck(scenario.name, "recording Hook returned a path outside the isolated workspace before shutdown"), stage, stats
	}
	if err := waitForShutdownFileSize(formalPath, shutdownRecordingDataMinBytes, recordWaitTimeout); err != nil {
		return failedCheck(scenario.name, "formal MP4 was not complete before shutdown"), stage, stats
	}
	temporaryPath := filepath.Join(filepath.Dir(formalPath), "."+filepath.Base(formalPath))
	if err := waitForShutdownFileAbsent(temporaryPath, recordWaitTimeout); err != nil {
		return failedCheck(scenario.name, "temporary MP4 remained when the recording Hook was entered"), stage, stats
	}
	if server.pollExit() {
		return failedCheck(scenario.name, "MediaServer.exe exited before stdin shutdown"), stage, stats
	}
	if _, err := inputWriter.WriteString("shutdown\n"); err != nil {
		return failedCheck(scenario.name, "shutdown command could not be written to exclusive stdin"), stage, stats
	}

	if scenario.requireBarrier {
		if err := waitForShutdownBarrier(server, stage, shutdownBarrierMarker, shutdownBarrierWait); err != nil {
			return failedCheck(scenario.name, "finalization barrier event was not observed"), stage, stats
		}
		if scenario.requireBarrierAlive {
			if err := waitForShutdownProcessAlive(server, shutdownBarrierAliveProof); err != nil {
				return failedCheck(scenario.name, "MediaServer.exe exited while the finalization barrier was waiting"), stage, stats
			}
		}
	}
	if scenario.requireBarrierFail {
		if err := waitForShutdownBarrier(server, stage, shutdownBarrierFailureMarker, shutdownBarrierWait); err != nil {
			return failedCheck(scenario.name, "finalization barrier failure event was not observed"), stage, stats
		}
	}

	if !scenario.expectCleanExit {
		exitErr := waitForShutdownProcessExit(server, shutdownProcessExitWait)
		if exitErr == nil || !server.pollExit() || server.waitErr == nil {
			return failedCheck(scenario.name, "MediaServer.exe did not exit with a nonzero status after Hook failure"), stage, stats
		}
		var processExit *exec.ExitError
		if !errors.As(server.waitErr, &processExit) || processExit.ExitCode() != 2 {
			return failedCheck(scenario.name, "MediaServer.exe did not exit with status 2 after Hook failure"), stage, stats
		}
		if gate.successes() != 0 {
			return failedCheck(scenario.name, "failed recording Hook scenario reported a successful index"), stage, stats
		}
		return passedCheck(scenario.name, map[string]any{
			"hook_attempts":      gate.attempts(),
			"hook_successes":     gate.successes(),
			"expected_exit_code": 2,
			"barrier_observed":   scenario.requireBarrier,
			"barrier_failed":     scenario.requireBarrierFail,
			"index_written":      false,
		}), stage, stats
	}

	gate.release()
	if exitErr := waitForShutdownProcessExit(server, shutdownProcessExitWait); exitErr != nil {
		return failedCheck(scenario.name, "MediaServer.exe did not exit cleanly after Hook release"), stage, stats
	}
	if gate.successes() != 1 {
		return failedCheck(scenario.name, "recording Hook did not write exactly one successful index"), stage, stats
	}
	if err := waitForShutdownFileSize(formalPath, shutdownRecordingDataMinBytes, recordWaitTimeout); err != nil {
		return failedCheck(scenario.name, "formal MP4 was not present after clean exit"), stage, stats
	}
	temporaryPath = filepath.Join(filepath.Dir(formalPath), "."+filepath.Base(formalPath))
	if err := waitForShutdownFileAbsent(temporaryPath, recordWaitTimeout); err != nil {
		return failedCheck(scenario.name, "temporary MP4 remained after clean exit"), stage, stats
	}
	formalFiles, formalBytes := findMP4(recordRoot)
	if formalFiles != 1 || formalBytes < shutdownRecordingDataMinBytes {
		return failedCheck(scenario.name, "formal MP4 inventory did not contain exactly one non-empty file"), stage, stats
	}
	if markerLines, err := shutdownProbeMarkerLines(markerPath); err != nil || markerLines != 1 {
		return failedCheck(scenario.name, "simulated recording index was not written exactly once"), stage, stats
	}
	return passedCheck(scenario.name, map[string]any{
		"hook_attempts":            gate.attempts(),
		"hook_successes":           gate.successes(),
		"barrier_observed":         scenario.requireBarrier,
		"process_alive_at_barrier": true,
		"formal_mp4_files":         formalFiles,
		"formal_mp4_bytes":         formalBytes,
		"temporary_mp4_absent":     true,
		"index_lines":              1,
	}), stage, stats
}

type shutdownHookGate struct {
	receiver    *hookReceiver
	mode        shutdownHookMode
	marker      string
	server      *httptest.Server
	releaseCh   chan struct{}
	releaseOnce sync.Once
	entered     chan int

	mu             sync.Mutex
	attemptCount   int
	successCount   int
	recordFilePath string
}

func newShutdownHookGate(receiver *hookReceiver, mode shutdownHookMode, marker string) (*shutdownHookGate, error) {
	if receiver == nil || marker == "" {
		return nil, errors.New("shutdown Hook gate requires a receiver and marker")
	}
	gate := &shutdownHookGate{
		receiver:  receiver,
		mode:      mode,
		marker:    marker,
		releaseCh: make(chan struct{}),
		entered:   make(chan int, 16),
	}
	gate.server = httptest.NewServer(http.HandlerFunc(gate.handle))
	return gate, nil
}

func (gate *shutdownHookGate) url() string {
	return gate.server.URL + "/index/hook/" + hookOnRecordMP4 + "?node=" + url.QueryEscape(gate.receiver.node) + "&cap=" + url.QueryEscape(hookCapability(gate.receiver.secret, gate.receiver.node, hookOnRecordMP4))
}

func (gate *shutdownHookGate) handle(writer http.ResponseWriter, request *http.Request) {
	event, eventOK := hookEventFromPath(request.URL.Path)
	body, bodyOK := readHookBody(request)
	valid, fields := gate.receiver.validateRequest(request, event, body)
	if !eventOK || request.Method != http.MethodPost || !bodyOK || !valid {
		gate.receiver.recordValidationFailure(event, fields)
		writeShutdownHookResponse(writer, http.StatusOK, -1, "hook authorization denied")
		return
	}

	gate.mu.Lock()
	gate.attemptCount++
	attempt := gate.attemptCount
	if rawPath, ok := rawString(body["file_path"]); ok {
		gate.recordFilePath = rawPath
	}
	gate.mu.Unlock()
	gate.entered <- attempt

	switch gate.mode {
	case shutdownHookAlwaysFailure:
		writeShutdownHookResponse(writer, http.StatusInternalServerError, -1, "controlled recording Hook failure")
		return
	case shutdownHookRetryThenSuccess:
		if attempt == 1 || attempt > 2 {
			writeShutdownHookResponse(writer, http.StatusInternalServerError, -1, "controlled recording Hook retry failure")
			return
		}
	case shutdownHookBlockThenSuccess:
		if attempt != 1 {
			writeShutdownHookResponse(writer, http.StatusInternalServerError, -1, "unexpected duplicate recording Hook")
			return
		}
	default:
		writeShutdownHookResponse(writer, http.StatusInternalServerError, -1, "unknown recording Hook mode")
		return
	}

	<-gate.releaseCh
	if err := gate.writeIndexMarker(); err != nil {
		writeShutdownHookResponse(writer, http.StatusInternalServerError, -1, "controlled recording index failed")
		return
	}
	writeShutdownHookResponse(writer, http.StatusOK, 0, "success")
}

func (gate *shutdownHookGate) writeIndexMarker() error {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	file, err := os.OpenFile(gate.marker, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString("indexed\n")
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	gate.successCount++
	return nil
}

func (gate *shutdownHookGate) release() {
	if gate == nil {
		return
	}
	gate.releaseOnce.Do(func() { close(gate.releaseCh) })
}

func (gate *shutdownHookGate) close() error {
	if gate == nil {
		return nil
	}
	gate.release()
	if gate.server != nil {
		gate.server.Close()
	}
	return nil
}

func (gate *shutdownHookGate) waitForAttempt(want int, timeout time.Duration) error {
	if gate == nil {
		return errors.New("shutdown Hook gate is unavailable")
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		if gate.attempts() >= want {
			return nil
		}
		select {
		case <-gate.entered:
		case <-timer.C:
			return errors.New("shutdown Hook attempt timeout")
		}
	}
}

func (gate *shutdownHookGate) attempts() int {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.attemptCount
}

func (gate *shutdownHookGate) successes() int {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.successCount
}

func (gate *shutdownHookGate) recordPath() (string, bool) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.recordFilePath, gate.recordFilePath != ""
}

func waitForMP4Size(root string, minimum int64, timeout time.Duration) (files int, bytes int64) {
	deadline := time.Now().Add(timeout)
	for {
		files, bytes = findMP4(root)
		if bytes >= minimum || !time.Now().Before(deadline) {
			return files, bytes
		}
		timer := time.NewTimer(200 * time.Millisecond)
		<-timer.C
	}
}

func shutdownLogContains(stage, marker string) bool {
	for _, name := range []string{"probe-stdout.log", "probe-stderr.log"} {
		data, err := os.ReadFile(filepath.Join(stage, name))
		if err == nil && bytes.Contains(data, []byte(marker)) {
			return true
		}
	}
	return false
}

func waitForShutdownBarrier(process *zlmProcess, stage, marker string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if shutdownLogContains(stage, marker) {
			return nil
		}
		if process != nil && process.pollExit() {
			// The final log write can race with cmd.Wait; inspect both streams once
			// after observing process exit before reporting a missing barrier.
			if shutdownLogContains(stage, marker) {
				return nil
			}
			return errors.New("shutdown barrier event missing after process exit")
		}
		if !time.Now().Before(deadline) {
			break
		}
		timer := time.NewTimer(50 * time.Millisecond)
		<-timer.C
	}
	return errors.New("shutdown barrier event timeout")
}

func waitForShutdownProcessAlive(process *zlmProcess, duration time.Duration) error {
	if process == nil {
		return errors.New("shutdown process is unavailable")
	}
	deadline := time.Now().Add(duration)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if process.pollExit() {
			return errors.New("shutdown process exited")
		}
		if !time.Now().Before(deadline) {
			return nil
		}
		<-ticker.C
	}
}

func waitForShutdownProcessExit(process *zlmProcess, timeout time.Duration) error {
	if process == nil {
		return errors.New("shutdown process is unavailable")
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if process.pollExit() {
			return process.waitErr
		}
		if !time.Now().Before(deadline) {
			return errors.New("shutdown process exit timeout")
		}
		<-ticker.C
	}
}

func shutdownPathInStage(stage, rawPath string) (string, bool) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", false
	}
	path := filepath.FromSlash(rawPath)
	if !filepath.IsAbs(path) {
		path = filepath.Join(stage, path)
	}
	stageAbs, stageErr := filepath.Abs(stage)
	pathAbs, pathErr := filepath.Abs(path)
	if stageErr != nil || pathErr != nil {
		return "", false
	}
	rel, err := filepath.Rel(stageAbs, pathAbs)
	if err != nil || isOutsideRoot(rel) {
		return "", false
	}
	return pathAbs, true
}

func waitForShutdownFileSize(path string, minimum int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() && info.Size() >= minimum {
			return nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		<-timer.C
	}
	return errors.New("shutdown formal file timeout")
}

func waitForShutdownFileAbsent(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		<-timer.C
	}
	return errors.New("shutdown temporary file cleanup timeout")
}

func shutdownProbeMarkerLines(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strings.Count(string(data), "indexed\n"), nil
}

func writeShutdownHookResponse(writer http.ResponseWriter, status, code int, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(hookResponse{Code: code, Msg: message})
}
