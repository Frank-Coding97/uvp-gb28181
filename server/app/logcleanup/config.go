package logcleanup

import (
	"errors"
	"fmt"
	"sync"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

const (
	ConfiguredConfigKey             = "log_cleanup.configured"
	OperationRetentionDaysConfigKey = "log_cleanup.operation_retention_days"
	LoginRetentionDaysConfigKey     = "log_cleanup.login_retention_days"
	JobRetentionDaysConfigKey       = "log_cleanup.job_retention_days"
	PlaybackRetentionDaysConfigKey  = "log_cleanup.playback_retention_days"
	SchedulerRetentionDaysConfigKey = "log_cleanup.scheduler_retention_days"
	minRetentionDays                = 1
	maxRetentionDays                = 365
)

// Config is the single retention snapshot used by log cleanup tasks.
type Config struct {
	SIPRetentionDays       int  `json:"sipRetentionDays"`
	OperationRetentionDays int  `json:"operationRetentionDays"`
	LoginRetentionDays     int  `json:"loginRetentionDays"`
	JobRetentionDays       int  `json:"jobRetentionDays"`
	PlaybackRetentionDays  int  `json:"playbackRetentionDays"`
	SchedulerRetentionDays int  `json:"schedulerRetentionDays"`
	Configured             bool `json:"configured"`
}

var configMu sync.Mutex

// LockConfig serializes a complete settings snapshot or update. The caller
// must call the returned unlock function exactly once.
func LockConfig() func() {
	configMu.Lock()
	return configMu.Unlock
}

// CurrentConfig returns one consistent snapshot of the configured retention
// periods and their defaults.
func CurrentConfig() Config {
	unlock := LockConfig()
	defer unlock()
	return CurrentConfigUnlocked()
}

// CurrentConfigUnlocked reads the current configuration while the caller holds
// LockConfig. It is exported for callers that need to read and modify a single
// snapshot without recursively acquiring the lock.
func CurrentConfigUnlocked() Config {
	c := app.ConfigYml
	result := Config{
		SIPRetentionDays:       gbconfig.DefaultSIPTraceRetentionDays,
		OperationRetentionDays: 180,
		LoginRetentionDays:     180,
		JobRetentionDays:       30,
		PlaybackRetentionDays:  7,
		SchedulerRetentionDays: 7,
	}
	if c == nil {
		return result
	}
	if c.Get(gbconfig.SIPTraceRetentionDaysConfigKey) != nil {
		result.SIPRetentionDays = validOrDefault(c.GetInt(gbconfig.SIPTraceRetentionDaysConfigKey), result.SIPRetentionDays)
	}
	result.OperationRetentionDays = configuredDays(c, OperationRetentionDaysConfigKey, result.OperationRetentionDays)
	result.LoginRetentionDays = configuredDays(c, LoginRetentionDaysConfigKey, result.LoginRetentionDays)
	result.JobRetentionDays = configuredDays(c, JobRetentionDaysConfigKey, result.JobRetentionDays)
	result.PlaybackRetentionDays = configuredDays(c, PlaybackRetentionDaysConfigKey, result.PlaybackRetentionDays)
	result.SchedulerRetentionDays = configuredDays(c, SchedulerRetentionDaysConfigKey, result.SchedulerRetentionDays)
	result.Configured = c.GetBool(ConfiguredConfigKey)
	return result
}

func configuredDays(c app.YmlConfigInterf, key string, defaultDays int) int {
	if c.Get(key) == nil {
		return defaultDays
	}
	return validOrDefault(c.GetInt(key), defaultDays)
}

func validOrDefault(days, defaultDays int) int {
	if days < minRetentionDays || days > maxRetentionDays {
		return defaultDays
	}
	return days
}

// ValidateConfig rejects values outside the supported whole-day range.
func ValidateConfig(c Config) error {
	values := []struct {
		name string
		days int
	}{
		{"sipRetentionDays", c.SIPRetentionDays},
		{"operationRetentionDays", c.OperationRetentionDays},
		{"loginRetentionDays", c.LoginRetentionDays},
		{"jobRetentionDays", c.JobRetentionDays},
		{"playbackRetentionDays", c.PlaybackRetentionDays},
		{"schedulerRetentionDays", c.SchedulerRetentionDays},
	}
	for _, value := range values {
		if value.days < minRetentionDays || value.days > maxRetentionDays {
			return fmt.Errorf("%s 必须是 %d–%d 之间的整数", value.name, minRetentionDays, maxRetentionDays)
		}
	}
	return nil
}

var sipReloader struct {
	sync.RWMutex
	reload func() error
}

// SetSIPReloader installs the bootstrap-owned hook used to apply changes to
// the existing SIP runtime without introducing a package dependency cycle.
func SetSIPReloader(reload func() error) {
	sipReloader.Lock()
	sipReloader.reload = reload
	sipReloader.Unlock()
}

// ApplySIPRetention reloads the SIP runtime after its retention setting has
// been saved. A missing hook is an explicit error so callers cannot report the
// runtime value as applied when no runtime reload was performed.
func ApplySIPRetention() error {
	sipReloader.RLock()
	reload := sipReloader.reload
	sipReloader.RUnlock()
	if reload == nil {
		return errors.New("SIP 保留期运行时重载器尚未初始化")
	}
	return reload()
}
