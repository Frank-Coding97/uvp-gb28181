package standalone

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func configTestPaths(t *testing.T) Paths {
	t.Helper()
	root := filepath.Join(t.TempDir(), "实例 中文 # spaces")
	for _, name := range []string{"config", "resource", "web", "data", "recordings"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, name), 0755))
	}
	paths, err := ResolvePaths(PathOptions{InstallDir: root, ConfigDir: filepath.Join(root, "config"), ResourceDir: filepath.Join(root, "resource"), WebDir: filepath.Join(root, "web"), DataDir: filepath.Join(root, "data"), RecordingsDir: filepath.Join(root, "recordings")})
	require.NoError(t, err)
	return paths
}

func TestInstanceConfigIndependentSecretsAndRepeat(t *testing.T) {
	aPaths, bPaths := configTestPaths(t), configTestPaths(t)
	a, err := InitializeConfig(aPaths)
	require.NoError(t, err)
	b, err := InitializeConfig(bPaths)
	require.NoError(t, err)
	secrets := []string{a.JWTSecret(), a.RedisPassword(), a.ZLMSecret(), b.JWTSecret(), b.RedisPassword(), b.ZLMSecret()}
	seen := map[string]bool{}
	for _, secret := range secrets {
		raw, err := base64.RawURLEncoding.DecodeString(secret)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(raw), 32)
		require.False(t, seen[secret])
		seen[secret] = true
		require.NotContains(t, fmt.Sprintf("%+v", a), secret)
	}
	again, err := InitializeConfig(aPaths)
	require.NoError(t, err)
	require.Equal(t, a.ConfigSHA256, again.ConfigSHA256)
	require.Equal(t, a.RedisPassword(), again.RedisPassword())
	require.True(t, a.Created)
	require.False(t, again.Created)
	redis, err := os.ReadFile(a.RedisConfigPath)
	require.NoError(t, err)
	require.Contains(t, string(redis), "requirepass "+a.RedisPassword())
	require.Contains(t, string(redis), "appendfsync always")
	require.Contains(t, string(redis), "maxmemory-policy noeviction")
	zlm, err := os.ReadFile(a.ZLMConfigPath)
	require.NoError(t, err)
	require.Contains(t, string(zlm), "secret="+a.ZLMSecret())
	_, err = os.Stat(aPaths.DatabasePath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestInstanceConfigConcurrentInitialization(t *testing.T) {
	paths := configTestPaths(t)
	const n = 6
	configs := make([]InstanceConfig, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) { defer wg.Done(); configs[i], errs[i] = InitializeConfig(paths) }(i)
	}
	wg.Wait()
	created := 0
	for i := range n {
		require.NoError(t, errs[i])
		require.Equal(t, configs[0].ConfigSHA256, configs[i].ConfigSHA256)
		if configs[i].Created {
			created++
		}
	}
	require.Equal(t, 1, created)
}

func TestInstanceConfigFaultsNeverPublishPartialAuthority(t *testing.T) {
	for _, stage := range []string{"authority.create", "authority.write", "authority.flush", "authority.acl", "authority.parse", "authority.publish"} {
		t.Run(stage, func(t *testing.T) {
			paths := configTestPaths(t)
			injected := errors.New("injected disk or permission failure")
			hit := false
			_, err := initializeConfig(paths, func(got string) error {
				if got == stage {
					hit = true
					return injected
				}
				return nil
			})
			require.Error(t, err)
			require.True(t, hit)
			_, err = os.Stat(paths.ConfigFile)
			require.ErrorIs(t, err, os.ErrNotExist)
			retry, err := InitializeConfig(paths)
			require.NoError(t, err)
			require.True(t, retry.Created)
		})
	}
}

func TestInstanceConfigDerivedFailurePreservesCommittedSecrets(t *testing.T) {
	paths := configTestPaths(t)
	injected := errors.New("derived failure")
	hit := false
	_, err := initializeConfig(paths, func(stage string) error {
		if stage == "redis.publish" {
			hit = true
			return injected
		}
		return nil
	})
	require.Error(t, err)
	require.True(t, hit)
	authority, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)
	again, err := InitializeConfig(paths)
	require.NoError(t, err)
	require.False(t, again.Created)
	unchanged, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)
	require.Equal(t, authority, unchanged)
}

func TestInstanceConfigRejectsCorruptionAndPreservesUserSettings(t *testing.T) {
	for _, kind := range []string{"yaml", "debug", "placeholder", "reused", "missing", "multiple-documents"} {
		t.Run(kind, func(t *testing.T) {
			paths := configTestPaths(t)
			cfg, err := InitializeConfig(paths)
			require.NoError(t, err)
			raw, err := os.ReadFile(paths.ConfigFile)
			require.NoError(t, err)
			var values map[string]any
			require.NoError(t, yaml.Unmarshal(raw, &values))
			switch kind {
			case "debug":
				values["server"].(map[string]any)["appdebug"] = true
			case "placeholder":
				values["token"].(map[string]any)["jwttokensignkey"] = "CHANGE_ME"
			case "reused":
				values["redis"].(map[string]any)["password"] = cfg.JWTSecret()
			case "missing":
				delete(values, "redis")
			}
			invalid, err := yaml.Marshal(values)
			require.NoError(t, err)
			if kind == "yaml" {
				invalid = []byte("token: [broken")
			}
			if kind == "multiple-documents" {
				invalid = append(invalid, []byte("---\nserver: {}\n")...)
			}
			require.NoError(t, os.WriteFile(paths.ConfigFile, invalid, 0600))
			_, err = InitializeConfig(paths)
			require.Error(t, err)
			require.NotContains(t, err.Error(), cfg.JWTSecret())
			preserved, err := os.ReadFile(paths.ConfigFile)
			require.NoError(t, err)
			require.Equal(t, invalid, preserved)
		})
	}
	paths := configTestPaths(t)
	_, err := InitializeConfig(paths)
	require.NoError(t, err)
	raw, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)
	custom := strings.Replace(string(raw), "16379", "16380", 1) + "customer_setting: retained\n"
	require.NoError(t, os.WriteFile(paths.ConfigFile, []byte(custom), 0600))
	cfg, err := InitializeConfig(paths)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:16380", cfg.RedisAddress())
	preserved, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)
	require.Equal(t, custom, string(preserved))
}

func TestLoadConfigDoesNotGenerateOrRepair(t *testing.T) {
	paths := configTestPaths(t)
	_, err := LoadConfig(paths)
	require.Error(t, err)
	_, err = os.Stat(paths.ConfigFile)
	require.ErrorIs(t, err, os.ErrNotExist)
	cfg, err := InitializeConfig(paths)
	require.NoError(t, err)
	require.NoError(t, os.Remove(cfg.RedisConfigPath))
	loaded, err := LoadConfig(paths)
	require.NoError(t, err)
	require.Equal(t, cfg.ConfigSHA256, loaded.ConfigSHA256)
	_, err = os.Stat(cfg.RedisConfigPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, os.WriteFile(paths.ConfigFile, []byte("invalid: ["), 0600))
	_, err = LoadConfig(paths)
	require.Error(t, err)
	raw, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)
	require.Equal(t, "invalid: [", string(raw))
}

func TestInstanceConfigManagementAddresses(t *testing.T) {
	values, err := newInstanceConfigValues()
	if err != nil {
		t.Fatal(err)
	}
	config := InstanceConfig{values: values}
	if config.BackendAddress() != "127.0.0.1:8280" || config.MediaAddress() != "127.0.0.1:18080" {
		t.Fatal("unexpected management addresses")
	}
}

func TestInstanceMediaListenersDefaultsAndOverrides(t *testing.T) {
	values, err := newInstanceConfigValues()
	require.NoError(t, err)
	config := InstanceConfig{values: values}
	require.Equal(t, []MediaListener{
		{Network: "tcp", Address: "0.0.0.0:18080"},
		{Network: "tcp", Address: "0.0.0.0:40000"},
		{Network: "udp", Address: "0.0.0.0:40000"},
		{Network: "udp", Address: "0.0.0.0:18000"},
		{Network: "tcp", Address: "0.0.0.0:18000"},
	}, config.MediaListeners())

	zlm := values["gb28181"].(map[string]any)["zlm"].(map[string]any)
	zlm["listenip"] = "127.0.0.1"
	zlm["rtpport"] = 41000
	zlm["rtcport"] = 18001
	zlm["rtctcpport"] = 18002
	require.Equal(t, []MediaListener{
		{Network: "tcp", Address: "127.0.0.1:18080"},
		{Network: "tcp", Address: "127.0.0.1:41000"},
		{Network: "udp", Address: "127.0.0.1:41000"},
		{Network: "udp", Address: "127.0.0.1:18001"},
		{Network: "tcp", Address: "127.0.0.1:18002"},
	}, (InstanceConfig{values: values}).MediaListeners())
}

func TestInstanceMediaListenIPPolicy(t *testing.T) {
	for _, listenIP := range []string{"0.0.0.0", "127.0.0.1"} {
		t.Run("allow "+listenIP, func(t *testing.T) {
			values, err := newInstanceConfigValues()
			require.NoError(t, err)
			zlm := values["gb28181"].(map[string]any)["zlm"].(map[string]any)
			zlm["listenip"] = listenIP
			raw, err := yaml.Marshal(values)
			require.NoError(t, err)
			_, err = decodeInstanceConfig(raw)
			require.NoError(t, err)
		})
	}
}

func TestInstanceMediaConfigOverrideRebuildsDerivedListeners(t *testing.T) {
	paths := configTestPaths(t)
	initial, err := InitializeConfig(paths)
	require.NoError(t, err)
	raw, err := os.ReadFile(initial.ConfigPath)
	require.NoError(t, err)
	var values map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &values))
	zlm := values["gb28181"].(map[string]any)["zlm"].(map[string]any)
	zlm["listenip"] = "127.0.0.1"
	zlm["httpport"] = 18180
	zlm["rtpport"] = 41000
	zlm["rtcport"] = 18001
	zlm["rtctcpport"] = 18002
	media := values["gb28181"].(map[string]any)["media"].(map[string]any)
	media["managertcexternip"] = false
	override, err := yaml.Marshal(values)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(initial.ConfigPath, override, 0600))

	loaded, err := InitializeConfig(paths)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:18180", loaded.MediaAddress())
	require.Equal(t, (InstanceConfig{values: values}).MediaListeners(), loaded.MediaListeners())
	zlmRaw, err := os.ReadFile(loaded.ZLMConfigPath)
	require.NoError(t, err)
	for _, expected := range []string{"listen_ip=127.0.0.1", "port=18180", "port=41000", "port_range=30000-35000", "port=18001", "tcpPort=18002"} {
		require.Contains(t, string(zlmRaw), expected)
	}
}

func TestInstanceMediaConfigAllowsDisabledRTC(t *testing.T) {
	values, err := newInstanceConfigValues()
	require.NoError(t, err)
	zlm := values["gb28181"].(map[string]any)["zlm"].(map[string]any)
	zlm["rtcport"] = 0
	zlm["rtctcpport"] = 0
	raw, err := yaml.Marshal(values)
	require.NoError(t, err)
	_, err = decodeInstanceConfig(raw)
	require.NoError(t, err)
	listeners := (InstanceConfig{values: values}).MediaListeners()
	for _, listener := range listeners {
		require.NotContains(t, listener.Address, ":0")
	}
	require.Len(t, listeners, 3)
}

func TestInstanceMediaConfigRejectsInvalidListenIPAndPorts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "hostname", mutate: func(zlm map[string]any) { zlm["listenip"] = "localhost" }},
		{name: "remote IPv4", mutate: func(zlm map[string]any) { zlm["listenip"] = "192.0.2.1" }},
		{name: "other loopback IPv4", mutate: func(zlm map[string]any) { zlm["listenip"] = "127.0.0.2" }},
		{name: "IPv6", mutate: func(zlm map[string]any) { zlm["listenip"] = "::1" }},
		{name: "listen IP wrong type", mutate: func(zlm map[string]any) { zlm["listenip"] = 127001 }},
		{name: "remote ZLM host", mutate: func(zlm map[string]any) { zlm["host"] = "192.0.2.1" }},
		{name: "RTP zero", mutate: func(zlm map[string]any) { zlm["rtpport"] = 0 }},
		{name: "RTP above range", mutate: func(zlm map[string]any) { zlm["rtpport"] = 65536 }},
		{name: "RTP wrong type", mutate: func(zlm map[string]any) { zlm["rtpport"] = "40000" }},
		{name: "RTC negative", mutate: func(zlm map[string]any) { zlm["rtcport"] = -1 }},
		{name: "RTC TCP above range", mutate: func(zlm map[string]any) { zlm["rtctcpport"] = 65536 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values, err := newInstanceConfigValues()
			require.NoError(t, err)
			zlm := values["gb28181"].(map[string]any)["zlm"].(map[string]any)
			tc.mutate(zlm)
			raw, err := yaml.Marshal(values)
			require.NoError(t, err)
			_, err = decodeInstanceConfig(raw)
			require.Error(t, err)
		})
	}
}

func TestInstanceMediaConfigUsesDefaultsForLegacyFields(t *testing.T) {
	paths := configTestPaths(t)
	initial, err := InitializeConfig(paths)
	require.NoError(t, err)
	raw, err := os.ReadFile(initial.ConfigPath)
	require.NoError(t, err)
	var values map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &values))
	zlm := values["gb28181"].(map[string]any)["zlm"].(map[string]any)
	delete(zlm, "listenip")
	delete(zlm, "rtcport")
	delete(zlm, "rtctcpport")
	delete(values["gb28181"].(map[string]any)["media"].(map[string]any), "managertcexternip")
	legacy, err := yaml.Marshal(values)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(initial.ConfigPath, legacy, 0600))

	loaded, err := InitializeConfig(paths)
	require.NoError(t, err)
	require.Equal(t, initial.MediaAddress(), loaded.MediaAddress())
	require.Equal(t, (InstanceConfig{values: values}).MediaListeners(), loaded.MediaListeners())
	zlmRaw, err := os.ReadFile(loaded.ZLMConfigPath)
	require.NoError(t, err)
	require.Contains(t, string(zlmRaw), "listen_ip=0.0.0.0")
	require.Contains(t, string(zlmRaw), "port=40000")
	require.Contains(t, string(zlmRaw), "port=18000")
}

func TestInstanceMediaConfigDisablesAPIDebug(t *testing.T) {
	cfg, err := InitializeConfig(configTestPaths(t))
	require.NoError(t, err)
	raw, err := os.ReadFile(cfg.ZLMConfigPath)
	require.NoError(t, err)
	if !strings.Contains(string(raw), "\napiDebug=0\n") {
		t.Fatal("media API debug must be explicitly disabled to keep management secrets out of logs")
	}
}

func TestInitialMediaConfigDeclaresApprovedListeners(t *testing.T) {
	cfg, err := InitializeConfig(configTestPaths(t))
	require.NoError(t, err)
	authority, err := os.ReadFile(cfg.ConfigPath)
	require.NoError(t, err)
	var values map[string]any
	require.NoError(t, yaml.Unmarshal(authority, &values))
	zlmValues := values["gb28181"].(map[string]any)["zlm"].(map[string]any)
	mediaValues := values["gb28181"].(map[string]any)["media"].(map[string]any)
	require.Equal(t, "0.0.0.0", zlmValues["listenip"])
	require.Equal(t, 40000, zlmValues["rtpport"])
	require.Equal(t, 18000, zlmValues["rtcport"])
	require.Equal(t, 18000, zlmValues["rtctcpport"])
	require.Equal(t, true, mediaValues["managertcexternip"])
	raw, err := os.ReadFile(cfg.ZLMConfigPath)
	require.NoError(t, err)
	sections := map[string]map[string]string{}
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[]")
			sections[section] = map[string]string{}
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			sections[section][key] = value
		}
	}
	for section, keys := range map[string][]string{"http": {"sslport"}, "rtsp": {"port", "sslport"}, "rtmp": {"port", "sslport"}, "shell": {"port"}, "srt": {"port"}, "rtc": {"signalingPort", "signalingSslPort", "icePort", "iceTcpPort"}} {
		for _, key := range keys {
			if sections[section][key] != "0" {
				t.Errorf("unconfigured listener %s.%s must be disabled", section, key)
			}
		}
	}
	if sections["general"]["listen_ip"] != "0.0.0.0" || sections["http"]["port"] != "18080" || sections["rtp_proxy"]["port"] != "40000" || sections["rtp_proxy"]["port_range"] != "30000-35000" || sections["rtc"]["port"] != "18000" || sections["rtc"]["tcpPort"] != "18000" {
		t.Error("initial media configuration does not expose the approved LAN listeners")
	}
}
