package standalone

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// InstanceConfig describes committed configuration without exposing secrets in
// JSON or diagnostic formatting. Component credentials require explicit access.
type InstanceConfig struct {
	ConfigPath      string `json:"config_path"`
	RedisConfigPath string `json:"redis_config_path"`
	ZLMConfigPath   string `json:"zlm_config_path"`
	ConfigSHA256    string `json:"config_sha256"`
	Created         bool   `json:"created"`
	values          map[string]any
}

func (c InstanceConfig) String() string        { return "standalone configuration sha256=" + c.ConfigSHA256 }
func (c InstanceConfig) GoString() string      { return c.String() }
func (c InstanceConfig) JWTSecret() string     { return configString(c.values, "token", "jwttokensignkey") }
func (c InstanceConfig) RedisPassword() string { return configString(c.values, "redis", "password") }
func (c InstanceConfig) ZLMSecret() string     { return configString(c.values, "gb28181", "zlm", "secret") }
func (c InstanceConfig) RedisAddress() string {
	return net.JoinHostPort(configString(c.values, "redis", "host"), strconv.Itoa(configInt(c.values, "redis", "port")))
}

// InitializeConfig publishes a single authoritative YAML file and rebuilds
// component input files. It never starts a process or opens a database.
func InitializeConfig(paths Paths) (InstanceConfig, error) { return initializeConfig(paths, nil) }

// LoadConfig validates existing production configuration without creating or
// repairing files. The backend uses this before starting business components.
func LoadConfig(paths Paths) (InstanceConfig, error) {
	if !paths.Explicit {
		return InstanceConfig{}, errors.New("standalone configuration requires explicit paths")
	}
	if err := paths.Validate(); err != nil {
		return InstanceConfig{}, err
	}
	if err := protectConfigDir(paths.DataDir, false); err != nil {
		return InstanceConfig{}, err
	}
	raw, err := readSecureConfigFile(paths.ConfigFile)
	if err != nil {
		return InstanceConfig{}, err
	}
	values, err := decodeInstanceConfig(raw)
	if err != nil {
		return InstanceConfig{}, err
	}
	sum := sha256.Sum256(raw)
	return InstanceConfig{ConfigPath: paths.ConfigFile, RedisConfigPath: filepath.Join(paths.ConfigDir, "redis.conf"), ZLMConfigPath: filepath.Join(paths.ConfigDir, "zlm.ini"), ConfigSHA256: hex.EncodeToString(sum[:]), values: values}, nil
}

func initializeConfig(paths Paths, hook func(string) error) (InstanceConfig, error) {
	var result InstanceConfig
	if !paths.Explicit {
		return result, errors.New("standalone configuration requires explicit paths")
	}
	err := withConfigLock(paths.InstallDir, func() error {
		if err := paths.Validate(); err != nil {
			return err
		}
		// Serialize first-use directory protection with validation's write probes.
		for _, dir := range []string{paths.ConfigDir, paths.DataDir} {
			entries, err := os.ReadDir(dir)
			if err != nil {
				return err
			}
			if err := protectConfigDir(dir, len(entries) == 0); err != nil {
				return err
			}
		}
		raw, err := readSecureConfigFile(paths.ConfigFile)
		created := false
		if errors.Is(err, os.ErrNotExist) {
			values, genErr := newInstanceConfigValues()
			if genErr != nil {
				return genErr
			}
			raw, err = yaml.Marshal(values)
			if err != nil {
				return errors.New("cannot encode standalone configuration")
			}
			if err := callSecureHook(prefixConfigHook("authority", hook), "parse", paths.ConfigFile); err != nil {
				return err
			}
			if _, err = decodeInstanceConfig(raw); err != nil {
				return err
			}
			err = writeSecureConfigFile(paths.ConfigFile, raw, false, prefixConfigHook("authority", hook))
			if err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			created = err == nil
			raw, err = readSecureConfigFile(paths.ConfigFile)
		}
		if err != nil {
			return err
		}
		values, err := decodeInstanceConfig(raw)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		result = InstanceConfig{ConfigPath: paths.ConfigFile, RedisConfigPath: filepath.Join(paths.ConfigDir, "redis.conf"), ZLMConfigPath: filepath.Join(paths.ConfigDir, "zlm.ini"), ConfigSHA256: hex.EncodeToString(sum[:]), Created: created, values: values}
		redisDir := filepath.Join(paths.DataDir, "redis")
		if err := os.MkdirAll(redisDir, 0700); err != nil {
			return err
		}
		entries, err := os.ReadDir(redisDir)
		if err != nil {
			return err
		}
		if err := protectConfigDir(redisDir, len(entries) == 0); err != nil {
			return err
		}
		relative, err := filepath.Rel(paths.ConfigDir, redisDir)
		if err != nil {
			return err
		}
		// Redis is started with ConfigDir as its working directory. Forward slashes
		// are accepted by the bundled Cygwin runtime, including Chinese paths.
		relative = filepath.ToSlash(relative)
		quotedDir := strconv.Quote(relative)
		redis := fmt.Sprintf("bind 127.0.0.1\nport %d\nprotected-mode yes\ndaemonize no\nsupervised no\ndir %s\nappendonly yes\nappendfilename appendonly.aof\nappendfsync always\nsave \"\"\nmaxmemory-policy noeviction\nrequirepass %s\nlogfile \"\"\n", configInt(values, "redis", "port"), quotedDir, result.RedisPassword())
		if err := writeSecureConfigFile(result.RedisConfigPath, []byte(redis), true, prefixConfigHook("redis", hook)); err != nil {
			return err
		}
		// Full media listeners/hooks are configured and accepted by T19. These
		// deterministic inputs carry the same instance secret, not a second source.
		zlm := fmt.Sprintf("[api]\napiDebug=0\nsecret=%s\n[general]\nlisten_ip=127.0.0.1\n[http]\nport=%d\nsslport=0\n[rtsp]\nport=0\nsslport=0\n[rtmp]\nport=0\nsslport=0\n[shell]\nport=0\n[srt]\nport=0\n[rtp_proxy]\nport=0\n[rtc]\nport=0\ntcpPort=0\nsignalingPort=0\nsignalingSslPort=0\nicePort=0\niceTcpPort=0\n", result.ZLMSecret(), configInt(values, "gb28181", "zlm", "httpport"))
		if err := writeSecureConfigFile(result.ZLMConfigPath, []byte(zlm), true, prefixConfigHook("zlm", hook)); err != nil {
			return err
		}
		if err := paths.Validate(); err != nil {
			return err
		}
		final, err := readSecureConfigFile(paths.ConfigFile)
		if err != nil {
			return err
		}
		if !bytes.Equal(raw, final) {
			return errors.New("standalone configuration changed during initialization")
		}
		return nil
	})
	if err != nil {
		return InstanceConfig{}, err
	}
	return result, nil
}

func prefixConfigHook(prefix string, hook func(string) error) func(string) error {
	if hook == nil {
		return nil
	}
	return func(stage string) error { return hook(prefix + "." + stage) }
}

func decodeInstanceConfig(raw []byte) (map[string]any, error) {
	var values map[string]any
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&values); err != nil {
		return nil, errors.New("invalid standalone YAML configuration")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("standalone configuration must contain one YAML document")
	}
	debug, ok := configValue(values, "server", "appdebug").(bool)
	if !ok || debug {
		return nil, errors.New("server.appdebug must be false")
	}
	if configString(values, "server", "cachetype") != "redis" {
		return nil, errors.New("standalone server.cachetype must be redis")
	}
	if configString(values, "gormv2", "usedbtype") != "sqlite" {
		return nil, errors.New("standalone gormv2.usedbtype must be sqlite")
	}
	for _, kind := range []string{"mysql", "postgresql", "sqlserver"} {
		if configInt(values, "gormv2", kind, "isinitglobalgorm"+kind) != 0 {
			return nil, errors.New("standalone configuration cannot initialize another database")
		}
	}
	keys := [][]string{{"token", "jwttokensignkey"}, {"redis", "password"}, {"gb28181", "zlm", "secret"}}
	seen := map[string]bool{}
	for _, key := range keys {
		secret := configString(values, key...)
		decoded, err := base64.RawURLEncoding.DecodeString(secret)
		if err != nil || len(decoded) < 32 || base64.RawURLEncoding.EncodeToString(decoded) != secret || seen[secret] {
			return nil, fmt.Errorf("%s must contain an independent random secret of at least 256 bits", strings.Join(key, "."))
		}
		seen[secret] = true
	}
	if configString(values, "redis", "host") != "127.0.0.1" {
		return nil, errors.New("standalone Redis must bind to 127.0.0.1")
	}
	for _, key := range [][]string{{"redis", "port"}, {"gb28181", "zlm", "httpport"}} {
		port := configInt(values, key...)
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("%s must be a valid TCP port", strings.Join(key, "."))
		}
	}
	return values, nil
}

func configValue(values map[string]any, keys ...string) any {
	var value any = values
	for _, key := range keys {
		m, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = m[key]
	}
	return value
}
func configString(values map[string]any, keys ...string) string {
	value, _ := configValue(values, keys...).(string)
	return value
}
func configInt(values map[string]any, keys ...string) int {
	value, _ := configValue(values, keys...).(int)
	return value
}

func newInstanceConfigValues() (map[string]any, error) {
	secrets := make([]string, 3)
	for i := range secrets {
		var raw [32]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, errors.New("cannot generate instance secrets")
		}
		secrets[i] = base64.RawURLEncoding.EncodeToString(raw[:])
	}
	return map[string]any{
		"server":     map[string]any{"appdebug": false, "cachetype": "redis", "syslog": true, "notcheckuser": []uint{}, "demoaccount": map[string]any{"enabled": false}},
		"system":     map[string]any{"systemname": "UVP", "systemlogo": "", "systemicon": ""},
		"httpserver": map[string]any{"port": "127.0.0.1:8280", "serverrootpath": "/public", "allowcrossdomain": false, "read_timeout": 30, "write_timeout": 30, "idle_timeout": 60, "handler_timeout": 30, "trustedproxies": []string{}},
		"safe":       map[string]any{"loginlockthreshold": 3, "loginlockexpire": 60, "loginlockduration": 600, "minpasswordlength": 6, "requirespecialchar": true},
		"captcha":    map[string]any{"open": false, "length": 4},
		"token":      map[string]any{"jwttokensignkey": secrets[0], "jwttokenexpire": 43200, "jwttokenrefreshexpire": 2592000, "cachekeyprefix": "uvp-gb28181:", "iscache": false},
		"redis":      map[string]any{"host": "127.0.0.1", "port": 16379, "password": secrets[1], "indexdb": 0},
		"gormv2":     map[string]any{"usedbtype": "sqlite"},
		"logs":       map[string]any{"level": "info", "console": true, "textformat": "console", "timeprecision": "millisecond", "maxsize": 5, "maxbackups": 7, "maxage": 15, "compress": false},
		"scheduler":  map[string]any{"log": map[string]any{"level": "info"}, "job_results_buffer_size": 1000},
		"upload":     map[string]any{"upload_type": "local", "max_size": 10, "chunk_max_size": 5120, "max_chunk_size": 5, "allowed_types": []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".txt", ".zip", ".rar", ".svg", ".ico", ".mp4", ".avi", ".webp"}, "chunk_allowed_types": []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".txt", ".zip", ".rar", ".svg", ".ico", ".mp4", ".avi", ".webp"}},
		"casbin": map[string]any{"autoloadpolicyseconds": 120, "tableprefix": "sys_", "tablename": "casbin_rule", "modelconfig": `[request_definition]
r = sub, obj, act, dom
[policy_definition]
p = sub, obj, act, dom
[role_definition]
g = _, _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = g(r.sub, p.sub, r.dom) && keyMatch2(r.obj, p.obj) && regexMatch(r.act, p.act) && (r.dom == p.dom || p.dom == "*")
`},
		"gb28181": map[string]any{
			"enabled":          true,
			"device":           map[string]any{"default_owner_dept_id": 1, "keepalive_interval": 60, "keepalive_timeout_count": 3, "keepalive_grace_seconds": 15, "offline_scan_interval": 30, "sync_channels_on_online": true, "online_on_heartbeat": true},
			"alarm":            map[string]any{"save_messages": true},
			"position_history": map[string]any{"enabled": true, "retention_days": 7},
			"zlm":              map[string]any{"host": "127.0.0.1", "receivehost": "", "playbackhost": "", "httpport": 18080, "rtpport": 40000, "secret": secrets[2]},
			"media":            map[string]any{"hookhost": "127.0.0.1", "hookport": 8280, "streamnonereadertimeout": 20, "rtpservertimeout": 15},
			"record_query":     map[string]any{"timezone": "Asia/Shanghai", "timeout_sec": 15},
			"play":             map[string]any{"auth": map[string]any{"enabled": true, "bind_client_ip": false, "ttl_seconds": 120}},
		},
	}, nil
}

// BackendAddress and MediaAddress are the configured management listeners.
func (c InstanceConfig) BackendAddress() string {
	return configString(c.values, "httpserver", "port")
}
func (c InstanceConfig) MediaAddress() string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(configInt(c.values, "gb28181", "zlm", "httpport")))
}
