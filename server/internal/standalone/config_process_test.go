package standalone

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	configProcessModeEnv       = "UVP_STANDALONE_CONFIG_CHILD"
	configProcessModeRace      = "race"
	configProcessModeExit      = "publish-exit"
	configProcessInstallEnv    = "UVP_STANDALONE_TEST_INSTALL_DIR"
	configProcessConfigEnv     = "UVP_STANDALONE_TEST_CONFIG_DIR"
	configProcessResourceEnv   = "UVP_STANDALONE_TEST_RESOURCE_DIR"
	configProcessWebEnv        = "UVP_STANDALONE_TEST_WEB_DIR"
	configProcessDataEnv       = "UVP_STANDALONE_TEST_DATA_DIR"
	configProcessRecordingsEnv = "UVP_STANDALONE_TEST_RECORDINGS_DIR"
	configProcessGateEnv       = "UVP_STANDALONE_TEST_START_GATE"
	configProcessReadyEnv      = "UVP_STANDALONE_TEST_READY_FILE"
	configProcessResultEnv     = "UVP_STANDALONE_TEST_RESULT_FILE"
)

type configProcessResult struct {
	ConfigSHA256 string `json:"config_sha256"`
	Created      bool   `json:"created"`
}

type runningConfigProcess struct {
	cmd        *exec.Cmd
	output     bytes.Buffer
	resultPath string
}

func TestInitializeConfigAcrossProcesses(t *testing.T) {
	paths := newConfigProcessPaths(t)
	root := filepath.Dir(paths.InstallDir)
	gatePath := filepath.Join(root, "start gate")
	children := make([]*runningConfigProcess, 0, 2)
	t.Cleanup(func() {
		for _, child := range children {
			if child.cmd.Process != nil {
				_ = child.cmd.Process.Kill()
				_ = child.cmd.Wait()
			}
		}
	})

	for i := 0; i < 2; i++ {
		readyPath := filepath.Join(root, fmt.Sprintf("ready %d", i))
		resultPath := filepath.Join(root, fmt.Sprintf("result %d.json", i))
		children = append(children, startConfigProcess(t, paths, configProcessModeRace, gatePath, readyPath, resultPath))
	}
	for _, child := range children {
		readyPath := childReadyPath(child)
		require.NoError(t, waitForConfigProcessFile(readyPath))
	}
	require.NoError(t, os.WriteFile(gatePath, []byte("start"), 0o600))

	waitConfigProcesses(t, children)
	results := make([]configProcessResult, len(children))
	created := 0
	for i, child := range children {
		result, err := readConfigProcessResult(child.resultPath)
		require.NoError(t, err, "child %d output: %s", i, child.output.String())
		results[i] = result
		if result.Created {
			created++
		}
	}
	require.Equal(t, 1, created)
	require.NotEmpty(t, results[0].ConfigSHA256)
	require.Equal(t, results[0].ConfigSHA256, results[1].ConfigSHA256)

	loaded, err := LoadConfig(paths)
	require.NoError(t, err)
	require.Equal(t, results[0].ConfigSHA256, loaded.ConfigSHA256)
	assertConfigProcessOutputHasNoSecrets(t, strings.Join(processOutputs(children), "\n"), loaded)
}

func TestInitializeConfigProcessExitAtAuthorityPublish(t *testing.T) {
	paths := newConfigProcessPaths(t)
	var output bytes.Buffer
	cmd := newConfigProcessCommand(paths, configProcessModeExit, "", "", "")
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Equal(t, 73, exitErr.ExitCode())

	for _, path := range []string{paths.ConfigFile, filepath.Join(paths.ConfigDir, "redis.conf"), filepath.Join(paths.ConfigDir, "zlm.ini")} {
		_, statErr := os.Stat(path)
		require.ErrorIs(t, statErr, os.ErrNotExist, "unexpected published path: %s", path)
	}
	assertConfigProcessOutputHasNoSecretValues(t, output.String(), configProcessTempSecrets(t, paths.ConfigDir)...)

	retry, err := InitializeConfig(paths)
	require.NoError(t, err)
	require.True(t, retry.Created)
	loaded, err := LoadConfig(paths)
	require.NoError(t, err)
	require.Equal(t, retry.ConfigSHA256, loaded.ConfigSHA256)
	for _, path := range []string{paths.ConfigFile, retry.RedisConfigPath, retry.ZLMConfigPath} {
		_, statErr := os.Stat(path)
		require.NoError(t, statErr, "missing complete configuration path: %s", path)
	}
	redis, err := os.ReadFile(retry.RedisConfigPath)
	require.NoError(t, err)
	require.Contains(t, string(redis), "requirepass "+retry.RedisPassword())
	zlm, err := os.ReadFile(retry.ZLMConfigPath)
	require.NoError(t, err)
	require.Contains(t, string(zlm), "secret="+retry.ZLMSecret())
	assertConfigProcessOutputHasNoSecrets(t, output.String(), retry)
}

// TestStandaloneConfigProcessChild is invoked by the parent tests through the
// current test binary. It intentionally writes only metadata to the result
// file; generated credentials never enter process output or result JSON.
func TestStandaloneConfigProcessChild(t *testing.T) {
	mode := os.Getenv(configProcessModeEnv)
	if mode == "" {
		return
	}
	paths, err := configProcessPathsFromEnv()
	if err != nil {
		configProcessFatal("resolve paths", err)
	}
	if readyPath := os.Getenv(configProcessReadyEnv); readyPath != "" {
		if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
			configProcessFatal("write ready marker", err)
		}
		gatePath := os.Getenv(configProcessGateEnv)
		if gatePath == "" {
			configProcessFatal("wait for start gate", errors.New("start gate is required"))
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			_, gateErr := os.Stat(gatePath)
			if gateErr == nil {
				break
			}
			if !errors.Is(gateErr, os.ErrNotExist) || time.Now().After(deadline) {
				configProcessFatal("wait for start gate", gateErr)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	var config InstanceConfig
	switch mode {
	case configProcessModeRace:
		config, err = InitializeConfig(paths)
	case configProcessModeExit:
		config, err = initializeConfig(paths, func(stage string) error {
			if stage == "authority.publish" {
				os.Exit(73)
			}
			return nil
		})
	default:
		configProcessFatal("run configuration", fmt.Errorf("unknown child mode %q", mode))
	}
	if err != nil {
		configProcessFatal("initialize configuration", err)
	}
	resultPath := os.Getenv(configProcessResultEnv)
	if resultPath == "" {
		configProcessFatal("write result", errors.New("result file is required"))
	}
	result, err := json.Marshal(configProcessResult{ConfigSHA256: config.ConfigSHA256, Created: config.Created})
	if err != nil {
		configProcessFatal("encode result", err)
	}
	if err := os.WriteFile(resultPath, result, 0o600); err != nil {
		configProcessFatal("write result", err)
	}
}

func newConfigProcessPaths(t *testing.T) Paths {
	t.Helper()
	root := filepath.Join(t.TempDir(), "UVP 实例 # spaces")
	options := PathOptions{
		InstallDir:    root,
		ConfigDir:     filepath.Join(root, "配置 目录"),
		ResourceDir:   filepath.Join(root, "资源 assets"),
		WebDir:        filepath.Join(root, "web 页面"),
		DataDir:       filepath.Join(root, "数据 空间"),
		RecordingsDir: filepath.Join(root, "录像 目录"),
	}
	for _, dir := range []string{options.InstallDir, options.ConfigDir, options.ResourceDir, options.WebDir, options.DataDir, options.RecordingsDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	paths, err := ResolvePaths(options)
	require.NoError(t, err)
	return paths
}

func startConfigProcess(t *testing.T, paths Paths, mode, gatePath, readyPath, resultPath string) *runningConfigProcess {
	t.Helper()
	child := &runningConfigProcess{
		cmd:        newConfigProcessCommand(paths, mode, gatePath, readyPath, resultPath),
		resultPath: resultPath,
	}
	child.cmd.Stdout = &child.output
	child.cmd.Stderr = &child.output
	require.NoError(t, child.cmd.Start())
	return child
}

func newConfigProcessCommand(paths Paths, mode, gatePath, readyPath, resultPath string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestStandaloneConfigProcessChild$", "-test.count=1")
	values := map[string]string{
		configProcessModeEnv:       mode,
		configProcessInstallEnv:    paths.InstallDir,
		configProcessConfigEnv:     paths.ConfigDir,
		configProcessResourceEnv:   paths.ResourceDir,
		configProcessWebEnv:        paths.WebDir,
		configProcessDataEnv:       paths.DataDir,
		configProcessRecordingsEnv: paths.RecordingsDir,
		configProcessGateEnv:       gatePath,
		configProcessReadyEnv:      readyPath,
		configProcessResultEnv:     resultPath,
	}
	cmd.Env = replaceConfigProcessEnv(os.Environ(), values)
	return cmd
}

func replaceConfigProcessEnv(base []string, values map[string]string) []string {
	env := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, replace := values[key]; replace {
				continue
			}
		}
		env = append(env, entry)
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

func configProcessPathsFromEnv() (Paths, error) {
	return ResolvePaths(PathOptions{
		InstallDir:    os.Getenv(configProcessInstallEnv),
		ConfigDir:     os.Getenv(configProcessConfigEnv),
		ResourceDir:   os.Getenv(configProcessResourceEnv),
		WebDir:        os.Getenv(configProcessWebEnv),
		DataDir:       os.Getenv(configProcessDataEnv),
		RecordingsDir: os.Getenv(configProcessRecordingsEnv),
	})
}

func waitForConfigProcessFile(path string) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, err := os.Stat(path)
		if err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			return fmt.Errorf("wait for %q: %w", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitConfigProcesses(t *testing.T, children []*runningConfigProcess) {
	t.Helper()
	errs := make([]error, len(children))
	var wg sync.WaitGroup
	for i, child := range children {
		wg.Add(1)
		go func(i int, child *runningConfigProcess) {
			defer wg.Done()
			errs[i] = child.cmd.Wait()
		}(i, child)
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "child %d output: %s", i, children[i].output.String())
	}
}

func readConfigProcessResult(path string) (configProcessResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return configProcessResult{}, err
	}
	var result configProcessResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return configProcessResult{}, err
	}
	return result, nil
}

func childReadyPath(child *runningConfigProcess) string {
	for _, entry := range child.cmd.Env {
		if strings.HasPrefix(entry, configProcessReadyEnv+"=") {
			return strings.TrimPrefix(entry, configProcessReadyEnv+"=")
		}
	}
	return ""
}

func processOutputs(children []*runningConfigProcess) []string {
	outputs := make([]string, len(children))
	for i, child := range children {
		outputs[i] = child.output.String()
	}
	return outputs
}

func assertConfigProcessOutputHasNoSecrets(t *testing.T, output string, config InstanceConfig) {
	t.Helper()
	assertConfigProcessOutputHasNoSecretValues(t, output, config.JWTSecret(), config.RedisPassword(), config.ZLMSecret())
}

func assertConfigProcessOutputHasNoSecretValues(t *testing.T, output string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret == "" {
			t.Fatal("generated configuration secret is empty")
		}
		if strings.Contains(output, secret) {
			t.Fatal("child process output contains a generated configuration secret")
		}
	}
}

func configProcessTempSecrets(t *testing.T, dir string) []string {
	t.Helper()
	tempPaths, err := filepath.Glob(filepath.Join(dir, secureTempPrefix+"*"))
	require.NoError(t, err)
	secrets := make([]string, 0, len(tempPaths)*3)
	for _, path := range tempPaths {
		raw, readErr := os.ReadFile(path)
		require.NoError(t, readErr, "read interrupted secure temporary file")
		var values map[string]any
		require.NoError(t, yaml.Unmarshal(raw, &values), "decode interrupted secure temporary file")
		for _, key := range [][]string{{"token", "jwttokensignkey"}, {"redis", "password"}, {"gb28181", "zlm", "secret"}} {
			if secret := configString(values, key...); secret != "" {
				secrets = append(secrets, secret)
			}
		}
	}
	return secrets
}

func configProcessFatal(operation string, err error) {
	_, _ = fmt.Fprintf(os.Stderr, "standalone child %s failed: %v\n", operation, err)
	os.Exit(71)
}
