//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	recordingCrashMediaRootEnv = "UVP_ZLM_RECORDING_MEDIA_ROOT"
	recordingCrashMediaExeEnv  = "UVP_ZLM_RECORDING_MEDIA_EXE"
	recordingCrashFixtureEnv   = "UVP_ZLM_RECORDING_FIXTURE"
	recordingCrashRounds       = 20
)

// TestWindowsRecordingFinalizationProcessKill kills the real MediaServer.exe
// only after the recording Hook finalization barrier reports that the process
// is alive and waiting. It deliberately covers the Hook/index wait window; it
// does not claim to cover a kill during mux writes or physical-camera input.
func TestWindowsRecordingFinalizationProcessKill(t *testing.T) {
	root, executable, fixture := recordingCrashInputs(t)
	for round := 1; round <= recordingCrashRounds; round++ {
		round := round
		t.Run(fmt.Sprintf("round-%02d", round), func(t *testing.T) {
			if err := runRecordingCrashRound(t, root, executable, fixture); err != nil {
				t.Fatalf("recording finalization process-kill round %02d: %v", round, err)
			}
		})
	}
}

func recordingCrashInputs(t *testing.T) (root, executable, fixture string) {
	t.Helper()
	root = strings.TrimSpace(os.Getenv(recordingCrashMediaRootEnv))
	executable = strings.TrimSpace(os.Getenv(recordingCrashMediaExeEnv))
	fixture = strings.TrimSpace(os.Getenv(recordingCrashFixtureEnv))
	if root == "" || executable == "" || fixture == "" {
		t.Skip("requires UVP_ZLM_RECORDING_MEDIA_ROOT, UVP_ZLM_RECORDING_MEDIA_EXE, and UVP_ZLM_RECORDING_FIXTURE")
	}
	resolvedRoot, resolvedExecutable, _, err := resolveInput(probeOptions{root: root, executable: executable, fixture: fixture})
	if err != nil {
		t.Fatalf("invalid MediaServer.exe input: %v", err)
	}
	fixture, err = filepath.Abs(fixture)
	if err != nil {
		t.Fatalf("invalid H264 fixture path: %v", err)
	}
	if info, statErr := os.Stat(fixture); statErr != nil || info.IsDir() {
		t.Fatalf("H264 fixture is unavailable: %v", statErr)
	}
	return resolvedRoot, resolvedExecutable, filepath.Clean(fixture)
}

func runRecordingCrashRound(t *testing.T, root, executable, fixtureSource string) (runErr error) {
	t.Helper()
	_, _, relExecutable, err := resolveInput(probeOptions{root: root, executable: executable, fixture: fixtureSource})
	if err != nil {
		return fmt.Errorf("resolve runtime input: %w", err)
	}
	stage, stageExecutable, _, err := copyRuntimeTree(root, relExecutable)
	if err != nil {
		return fmt.Errorf("copy isolated runtime tree: %w", err)
	}
	defer func() {
		if runErr == nil {
			_ = os.RemoveAll(stage)
			return
		}
		t.Logf("retained recording finalization crash evidence at %s", stage)
	}()

	var receiver *hookReceiver
	var gate *shutdownHookGate
	var player *playerSession
	var inputReader, inputWriter *os.File
	var server *zlmProcess
	playerStopped := false
	gateClosed := false
	abortGate := func() {
		if gate == nil || gateClosed {
			return
		}
		// A blocked Hook request must fail closed during test cleanup. If it were
		// released with the normal marker path, cleanup itself could look like a
		// successful backend index after MediaServer.exe was killed.
		noIndexMarker := filepath.Join(stage, ".uvp-recording-crash-no-index", "indexed")
		_ = os.RemoveAll(filepath.Dir(noIndexMarker))
		gate.mu.Lock()
		gate.marker = noIndexMarker
		gate.mu.Unlock()
		_ = gate.close()
		gateClosed = true
	}
	defer func() {
		abortGate()
		if player != nil && !playerStopped {
			_ = player.stop(5 * time.Second)
		}
		if server != nil {
			_ = server.stop(10 * time.Second)
		}
		if inputWriter != nil {
			_ = inputWriter.Close()
		}
		if inputReader != nil {
			_ = inputReader.Close()
		}
		if receiver != nil {
			_ = receiver.close(5 * time.Second)
		}
	}()

	fixturePath, err := copyFixture(fixtureSource, stage)
	if err != nil {
		return fmt.Errorf("copy H264 fixture: %w", err)
	}
	httpPort, err := reserveTCPPort()
	if err != nil {
		return fmt.Errorf("reserve HTTP port: %w", err)
	}
	rtspPort, err := reserveTCPPort()
	if err != nil {
		return fmt.Errorf("reserve RTSP port: %w", err)
	}
	secret := randomToken("zlm-")
	node := randomToken("recording-crash-node-")
	receiver, err = newHookReceiver(node, secret)
	if err != nil {
		return fmt.Errorf("start Hook receiver: %w", err)
	}
	playToken := randomToken("fixture-play-")
	receiver.setTokens(playToken, randomToken("fixture-publish-"))
	configPath := filepath.Join(stage, "config.ini")
	if err := configureRuntime(configPath, httpPort, rtspPort, secret, node, receiver); err != nil {
		return fmt.Errorf("configure isolated MediaServer: %w", err)
	}
	indexMarker := filepath.Join(stage, "shutdown-index.marker")
	gate, err = newShutdownHookGate(receiver, shutdownHookBlockThenSuccess, indexMarker)
	if err != nil {
		return fmt.Errorf("start recording Hook gate: %w", err)
	}
	if err := rewriteINI(configPath, map[string]map[string]string{"hook": {hookOnRecordMP4: gate.url()}}); err != nil {
		return fmt.Errorf("configure recording Hook gate: %w", err)
	}
	inputReader, inputWriter, err = os.Pipe()
	if err != nil {
		return fmt.Errorf("create stdin control pipe: %w", err)
	}
	server, err = startZLMWithInput(stageExecutable, stage, configPath, inputReader)
	if err != nil {
		return fmt.Errorf("start MediaServer.exe: %w", err)
	}
	client := &apiClient{baseURL: fmt.Sprintf("http://127.0.0.1:%d", httpPort), http: &http.Client{Timeout: apiRequestTimeout}}
	if err := waitForAPI(server, client, secret, serverStartupTimeout); err != nil {
		return fmt.Errorf("wait for MediaServer API: %w", err)
	}

	vhost, app, stream := "__defaultVhost__", "live", randomToken("recording-crash-")
	loaded, err := client.call(context.Background(), "/index/api/loadMP4File", url.Values{
		"vhost": {vhost}, "app": {app}, "stream": {stream}, "file_path": {filepath.ToSlash(fixturePath)}, "file_repeat": {"1"}, "secret": {secret},
	})
	if err != nil || loaded.Code != 0 {
		return fmt.Errorf("load H264 fixture into MediaServer: %v", err)
	}
	var loadedData struct {
		DurationMS uint64 `json:"duration_ms"`
	}
	if json.Unmarshal(loaded.Data, &loadedData) != nil || loadedData.DurationMS == 0 {
		return errors.New("loaded H264 fixture has no positive duration")
	}
	if err := waitMediaOnline(client, secret, vhost, app, stream); err != nil {
		return fmt.Errorf("wait for fixture media: %w", err)
	}
	player, err = openPlayerWithQuery(client.baseURL, app, stream, url.Values{"play_token": {playToken}})
	if err != nil {
		return fmt.Errorf("open fixture player: %w", err)
	}
	select {
	case readyErr := <-player.ready:
		if readyErr != nil {
			return fmt.Errorf("fixture player did not receive media: %w", readyErr)
		}
	case <-time.After(mediaReadyTimeout):
		return errors.New("fixture player readiness timeout")
	}

	recordRoot := filepath.Join(stage, "录像 输出 中文 space")
	if err := os.RemoveAll(recordRoot); err != nil {
		return fmt.Errorf("clear isolated recording directory: %w", err)
	}
	if err := os.MkdirAll(recordRoot, 0o700); err != nil {
		return fmt.Errorf("create recording directory: %w", err)
	}
	started, err := client.call(context.Background(), "/index/api/startRecord", url.Values{
		"type": {"1"}, "vhost": {vhost}, "app": {app}, "stream": {stream}, "customized_path": {"./录像 输出 中文 space/"}, "max_second": {"30"}, "secret": {secret},
	})
	if err != nil || started.Code != 0 || !rawBool(started.Result) {
		return errors.New("startRecord did not start MP4 recording")
	}
	if err := waitRecording(client, secret, vhost, app, stream, true); err != nil {
		return fmt.Errorf("wait for active MP4 recorder: %w", err)
	}
	// ZLMediaKit may buffer the MP4 until stopRecord closes the muxer; the
	// formal file is validated after the controlled Hook has entered its wait.
	stopped, err := client.call(context.Background(), "/index/api/stopRecord", url.Values{
		"type": {"1"}, "vhost": {vhost}, "app": {app}, "stream": {stream}, "secret": {secret},
	})
	if err != nil || stopped.Code != 0 || !rawBool(stopped.Result) {
		return errors.New("stopRecord did not stop MP4 recording")
	}
	if err := waitRecording(client, secret, vhost, app, stream, false); err != nil {
		return fmt.Errorf("wait for stopped MP4 recorder: %w", err)
	}
	if err := gate.waitForAttempt(1, shutdownHookWait); err != nil {
		return fmt.Errorf("recording Hook did not reach finalization wait: %w", err)
	}
	if gate.attempts() != 1 || gate.logicalHooks() != 1 {
		return fmt.Errorf("recording Hook attempts/logical paths = %d/%d; want exactly 1/1", gate.attempts(), gate.logicalHooks())
	}
	formalPath, ok := gate.recordPath()
	if !ok {
		return errors.New("recording Hook did not provide a formal MP4 path")
	}
	formalPath, ok = shutdownPathInStage(stage, formalPath)
	if !ok {
		return errors.New("recording Hook returned a path outside the isolated workspace")
	}
	if err := waitForShutdownFileSize(formalPath, shutdownRecordingDataMinBytes, recordWaitTimeout); err != nil {
		return fmt.Errorf("formal MP4 was not complete before the barrier: %w", err)
	}
	temporaryPath := filepath.Join(filepath.Dir(formalPath), "."+filepath.Base(formalPath))
	if err := waitForShutdownFileAbsent(temporaryPath, recordWaitTimeout); err != nil {
		return fmt.Errorf("temporary MP4 remained at finalization barrier: %w", err)
	}
	formalSize, formalSHA, err := recordingCrashFileFingerprint(formalPath)
	if err != nil {
		return fmt.Errorf("fingerprint formal MP4 before kill: %w", err)
	}
	if markerExists, markerErr := recordingCrashIndexMarkerExists(indexMarker); markerErr != nil || markerExists {
		return errors.New("recording Hook wrote a successful index before the process kill")
	}
	if _, err := inputWriter.WriteString("shutdown\n"); err != nil {
		return fmt.Errorf("write stdin shutdown command: %w", err)
	}
	if err := waitForShutdownBarrier(server, stage, shutdownBarrierMarker, shutdownBarrierWait); err != nil {
		return fmt.Errorf("finalization barrier was not observed: %w", err)
	}
	if err := waitForShutdownProcessAlive(server, shutdownBarrierAliveProof); err != nil {
		return fmt.Errorf("MediaServer.exe was not alive at finalization barrier: %w", err)
	}
	killErr := server.cmd.Process.Kill()
	if killErr != nil {
		return fmt.Errorf("kill MediaServer.exe at finalization barrier: %w", killErr)
	}
	exitErr := waitForShutdownProcessExit(server, shutdownProcessExitWait)
	if exitErr == nil || !server.pollExit() || server.waitErr == nil {
		return errors.New("MediaServer.exe did not report a forced non-clean exit after Kill")
	}
	if markerExists, markerErr := recordingCrashIndexMarkerExists(indexMarker); markerErr != nil || markerExists {
		return errors.New("forced process kill produced a successful recording index")
	}
	abortGate()
	if gate.successes() != 0 {
		return errors.New("post-kill Hook cleanup reported a successful index")
	}
	if markerExists, markerErr := recordingCrashIndexMarkerExists(indexMarker); markerErr != nil || markerExists {
		return errors.New("recording index marker appeared during failed Hook cleanup")
	}
	finalSize, finalSHA, err := recordingCrashFileFingerprint(formalPath)
	if err != nil {
		return fmt.Errorf("fingerprint formal MP4 after kill: %w", err)
	}
	if finalSize != formalSize || finalSHA != formalSHA {
		return errors.New("formal MP4 changed after finalization process kill")
	}
	files, bytes := findMP4(recordRoot)
	if files != 1 || bytes < shutdownRecordingDataMinBytes {
		return fmt.Errorf("post-kill recording inventory = files %d, bytes %d; want one non-empty formal MP4", files, bytes)
	}
	playerStopped = true
	if err := player.stop(5 * time.Second); err != nil {
		return fmt.Errorf("stop fixture player after process kill: %w", err)
	}
	t.Logf("finalization barrier process kill preserved formal MP4 bytes=%d sha256=%s without index", finalSize, finalSHA)
	return nil
}

func recordingCrashFileFingerprint(path string) (int64, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, "", err
	}
	if info.IsDir() || info.Size() < shutdownRecordingDataMinBytes {
		return 0, "", errors.New("formal MP4 is missing or too small")
	}
	sha, err := sha256File(path)
	if err != nil {
		return 0, "", err
	}
	return info.Size(), sha, nil
}

func recordingCrashIndexMarkerExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
