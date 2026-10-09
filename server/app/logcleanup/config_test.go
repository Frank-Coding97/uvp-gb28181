package logcleanup

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type configTestYML struct{ values map[string]interface{} }

func (c *configTestYML) ConfigFileChangeListen(...func()) {}
func (c *configTestYML) Get(key string) interface{}       { return c.values[key] }
func (c *configTestYML) GetString(key string) string {
	value, _ := c.values[key].(string)
	return value
}
func (c *configTestYML) GetBool(key string) bool           { value, _ := c.values[key].(bool); return value }
func (c *configTestYML) GetInt(key string) int             { value, _ := c.values[key].(int); return value }
func (c *configTestYML) GetInt32(key string) int32         { return int32(c.GetInt(key)) }
func (c *configTestYML) GetInt64(key string) int64         { return int64(c.GetInt(key)) }
func (c *configTestYML) GetFloat64(string) float64         { return 0 }
func (c *configTestYML) GetDuration(string) time.Duration  { return 0 }
func (c *configTestYML) GetStringSlice(string) []string    { return nil }
func (c *configTestYML) GetUintSlice(string) []uint        { return nil }
func (c *configTestYML) Set(key string, value interface{}) { c.values[key] = value }
func (c *configTestYML) SaveConfig() error                 { return nil }

func TestCurrentConfigUsesExistingSIPValueAndRetentionDefaults(t *testing.T) {
	previous := app.ConfigYml
	app.ConfigYml = &configTestYML{values: map[string]interface{}{
		"gb28181.trace.retention_days": 21,
	}}
	t.Cleanup(func() { app.ConfigYml = previous })

	got := CurrentConfig()
	require.Equal(t, 21, got.SIPRetentionDays)
	require.Equal(t, 180, got.OperationRetentionDays)
	require.Equal(t, 180, got.LoginRetentionDays)
	require.Equal(t, 30, got.JobRetentionDays)
	require.Equal(t, 7, got.PlaybackRetentionDays)
	require.Equal(t, 7, got.SchedulerRetentionDays)
	require.False(t, got.Configured)
}

func TestCurrentConfigReadsPersistedValuesAndConfiguredFlag(t *testing.T) {
	previous := app.ConfigYml
	app.ConfigYml = &configTestYML{values: map[string]interface{}{
		"gb28181.trace.retention_days":  15,
		OperationRetentionDaysConfigKey: 90,
		LoginRetentionDaysConfigKey:     120,
		JobRetentionDaysConfigKey:       14,
		PlaybackRetentionDaysConfigKey:  3,
		SchedulerRetentionDaysConfigKey: 5,
		ConfiguredConfigKey:             true,
	}}
	t.Cleanup(func() { app.ConfigYml = previous })

	got := CurrentConfig()
	require.Equal(t, Config{
		SIPRetentionDays: 15, OperationRetentionDays: 90, LoginRetentionDays: 120,
		JobRetentionDays: 14, PlaybackRetentionDays: 3, SchedulerRetentionDays: 5, Configured: true,
	}, got)
}

func TestValidateConfigRequiresEveryRetentionWithinOneTo365Days(t *testing.T) {
	valid := Config{SIPRetentionDays: 1, OperationRetentionDays: 365, LoginRetentionDays: 180, JobRetentionDays: 30, PlaybackRetentionDays: 7, SchedulerRetentionDays: 7}
	require.NoError(t, ValidateConfig(valid))
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"sip below minimum", func(c *Config) { c.SIPRetentionDays = 0 }},
		{"operation above maximum", func(c *Config) { c.OperationRetentionDays = 366 }},
		{"login negative", func(c *Config) { c.LoginRetentionDays = -1 }},
		{"job zero", func(c *Config) { c.JobRetentionDays = 0 }},
		{"playback above maximum", func(c *Config) { c.PlaybackRetentionDays = 999 }},
		{"scheduler negative", func(c *Config) { c.SchedulerRetentionDays = -5 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.edit(&candidate)
			require.Error(t, ValidateConfig(candidate))
		})
	}
}
