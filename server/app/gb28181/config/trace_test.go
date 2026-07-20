package config

import (
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestLoadFromReadsTraceConfig(t *testing.T) {
	v := viper.New()
	v.Set("gb28181.trace.enabled", true)
	v.Set("gb28181.trace.address", "clickhouse.internal:9000")
	v.Set("gb28181.trace.database", "uvp_sip_trace")
	v.Set("gb28181.trace.username", "uvp_trace")
	v.Set("gb28181.trace.password_env", "UVP_TRACE_PASSWORD")
	v.Set("gb28181.trace.tls", true)
	v.Set("gb28181.trace.queue_capacity", 4096)
	v.Set("gb28181.trace.batch_size", 256)
	v.Set("gb28181.trace.flush_interval_ms", 750)
	v.Set("gb28181.trace.encryption_key_env", "UVP_TRACE_KEY")

	cfg := LoadFrom(v)
	require.Equal(t, TraceConfig{
		Enabled:          true,
		Address:          "clickhouse.internal:9000",
		Database:         "uvp_sip_trace",
		Username:         "uvp_trace",
		PasswordEnv:      "UVP_TRACE_PASSWORD",
		TLS:              true,
		QueueCapacity:    4096,
		BatchSize:        256,
		FlushIntervalMS:  750,
		EncryptionKeyEnv: "UVP_TRACE_KEY",
	}, cfg.Trace)
}

func TestExampleConfigKeepsTraceDisabled(t *testing.T) {
	v := viper.New()
	v.SetConfigFile(filepath.Join(serverConfigDir(), "config.example.yml"))
	require.NoError(t, v.ReadInConfig())

	cfg := LoadFrom(v)
	require.False(t, cfg.Trace.Enabled)
	require.Equal(t, "uvp_sip_trace", cfg.Trace.Database)
	require.Positive(t, cfg.Trace.QueueCapacity)
	require.Positive(t, cfg.Trace.BatchSize)
	require.Positive(t, cfg.Trace.FlushIntervalMS)
}
