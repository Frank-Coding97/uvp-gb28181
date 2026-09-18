// Package logging owns the application's logging runtime without importing business packages.
package logging

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"go.uber.org/zap/zapcore"
)

type Source interface{ Get(string) interface{} }

type Config struct {
	Outputs                            []string
	Level                              zapcore.Level
	Modules                            map[string]zapcore.Level
	FilePath, FileFormat, StdoutFormat string
	MaxSizeMB, MaxBackups, MaxAgeDays  int
	Compress, Routes                   bool
	Notices                            []string
}

func ParseConfig(src Source, basePath string) (Config, error) {
	c := Config{Level: zapcore.InfoLevel, Modules: map[string]zapcore.Level{"access": zapcore.InfoLevel}, FileFormat: "console", StdoutFormat: "console", MaxSizeMB: 5, MaxBackups: 7, MaxAgeDays: 15}
	get := func(k string) interface{} {
		if src == nil {
			return nil
		}
		return src.Get(k)
	}
	str := func(k, def string) (string, error) {
		v := get(k)
		if v == nil {
			return def, nil
		}
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("%s must be a string", k)
		}
		return s, nil
	}
	boolean := func(k string, def bool) (bool, error) {
		v := get(k)
		if v == nil {
			return def, nil
		}
		switch x := v.(type) {
		case bool:
			return x, nil
		case string:
			b, e := strconv.ParseBool(x)
			if e == nil {
				return b, nil
			}
		}
		return false, fmt.Errorf("%s must be a boolean", k)
	}
	console, err := boolean("logs.console", true)
	if err != nil {
		return c, err
	}
	if raw := get("logs.outputs"); raw != nil {
		switch v := raw.(type) {
		case []string:
			c.Outputs = append([]string(nil), v...)
		case []interface{}:
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					return c, errors.New("logs.outputs must contain strings")
				}
				c.Outputs = append(c.Outputs, s)
			}
		default:
			return c, errors.New("logs.outputs must be a list")
		}
		if get("logs.console") != nil {
			c.Notices = append(c.Notices, "logging.legacy_console_ignored")
		}
	} else {
		c.Outputs = []string{"file"}
		if console {
			c.Outputs = append(c.Outputs, "stdout")
		}
		c.Notices = append(c.Notices, "logging.legacy_outputs")
	}
	level, err := str("logs.level", "info")
	if err != nil {
		return c, err
	}
	if c.Level, err = parseLevel(level); err != nil {
		return c, errors.New("logs.level is invalid")
	}
	if legacy := get("scheduler.log.level"); legacy != nil {
		s, ok := legacy.(string)
		if !ok {
			return c, errors.New("scheduler.log.level is invalid")
		}
		l, e := parseLevel(s)
		if e != nil {
			return c, errors.New("scheduler.log.level is invalid")
		}
		c.Modules["scheduler"] = l
	}
	if raw := get("logs.modules"); raw != nil {
		modules := map[string]string{}
		switch v := raw.(type) {
		case map[string]string:
			modules = v
		case map[string]interface{}:
			for k, val := range v {
				s, ok := val.(string)
				if !ok {
					return c, errors.New("logs.modules values must be levels")
				}
				modules[k] = s
			}
		default:
			return c, errors.New("logs.modules must be a map")
		}
		for module, value := range modules {
			if module == "" || strings.TrimSpace(module) != module {
				return c, errors.New("logs.modules contains an invalid name")
			}
			l, e := parseLevel(value)
			if e != nil {
				return c, errors.New("logs.modules contains an invalid level")
			}
			c.Modules[module] = l
		}
	}
	if c.FileFormat, err = str("logs.textformat", "console"); err != nil {
		return c, err
	}
	if c.StdoutFormat, err = str("logs.stdoutformat", "console"); err != nil {
		return c, err
	}
	explicit, err := str("logs.filepath", "")
	if err != nil {
		return c, err
	}
	legacy, err := str("logs.zaplogname", "/resource/logs/uvp-gb28181.log")
	if err != nil {
		return c, err
	}
	c.FilePath = ResolveFilePath(basePath, explicit, legacy)
	for k, dst := range map[string]*int{"logs.maxsize": &c.MaxSizeMB, "logs.maxbackups": &c.MaxBackups, "logs.maxage": &c.MaxAgeDays} {
		if v := get(k); v != nil {
			n, ok := configInteger(v)
			if !ok || n <= 0 {
				return c, fmt.Errorf("%s must be a positive integer", k)
			}
			*dst = n
		}
	}
	if c.Compress, err = boolean("logs.compress", false); err != nil {
		return c, err
	}
	if c.Routes, err = boolean("logs.routes", false); err != nil {
		return c, err
	}
	if get("logs.timeprecision") == "second" {
		c.Notices = append(c.Notices, "logging.millisecond_time")
	}
	if get("logs.ginlogname") != nil {
		c.Notices = append(c.Notices, "logging.gin_file_retired")
	}
	if get("scheduler.log.dir") != nil {
		c.Notices = append(c.Notices, "logging.scheduler_file_retired")
	}
	return c, c.validate()
}

// ResolveFilePath preserves the old project-relative /resource spelling while
// giving the new filepath setting normal absolute/relative path semantics.
func ResolveFilePath(base, explicit, legacy string) string {
	if explicit != "" {
		if filepath.IsAbs(explicit) {
			return filepath.Clean(explicit)
		}
		return filepath.Join(base, explicit)
	}
	return filepath.Join(base, strings.TrimLeft(legacy, "/\\"))
}
func parseLevel(s string) (zapcore.Level, error) {
	var l zapcore.Level
	err := l.UnmarshalText([]byte(strings.ToLower(strings.TrimSpace(s))))
	if s == "" {
		err = errors.New("empty level")
	}
	return l, err
}
func configInteger(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), int64(int(n)) == n
	case float64:
		if n > 0 && n < float64(math.MaxInt) && n == math.Trunc(n) {
			return int(n), true
		}
	}
	return 0, false
}
func (c Config) validate() error {
	if len(c.Outputs) == 0 {
		return errors.New("logs.outputs must not be empty")
	}
	seen := map[string]bool{}
	for _, s := range c.Outputs {
		if s != "file" && s != "stdout" {
			return errors.New("unknown logs.outputs target")
		}
		if seen[s] {
			return errors.New("duplicate logs.outputs target")
		}
		seen[s] = true
	}
	if c.Level < zapcore.DebugLevel || c.Level > zapcore.FatalLevel {
		return errors.New("invalid logs.level")
	}
	for _, v := range c.Modules {
		if v < zapcore.DebugLevel || v > zapcore.FatalLevel {
			return errors.New("invalid module level")
		}
	}
	for _, s := range []string{c.FileFormat, c.StdoutFormat} {
		if s != "console" && s != "json" {
			return errors.New("log format must be console or json")
		}
	}
	if c.MaxSizeMB <= 0 || int64(c.MaxSizeMB) > math.MaxInt64/(1<<20) || c.MaxBackups <= 0 || c.MaxAgeDays <= 0 || int64(c.MaxAgeDays) > math.MaxInt64/(int64(24*60*60)*1000000000) {
		return errors.New("log retention must be positive and bounded")
	}
	return nil
}
func (c Config) clone() Config {
	out := c
	out.Outputs = append([]string(nil), c.Outputs...)
	out.Notices = append([]string(nil), c.Notices...)
	out.Modules = make(map[string]zapcore.Level, len(c.Modules))
	for k, v := range c.Modules {
		out.Modules[k] = v
	}
	return out
}
