//go:build integration

package bootstrap

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

type cacheInitTestConfig struct {
	cacheType string
	dbType    string
	redisHost string
	redisPort string
}

func (c cacheInitTestConfig) ConfigFileChangeListen(...func()) {}
func (c cacheInitTestConfig) Get(string) interface{}           { return nil }
func (c cacheInitTestConfig) GetString(key string) string {
	switch key {
	case "server.cachetype":
		return c.cacheType
	case "redis.host":
		return c.redisHost
	case "redis.port":
		return c.redisPort
	case "gormv2.usedbtype":
		return c.dbType
	default:
		return ""
	}
}
func (cacheInitTestConfig) GetBool(string) bool              { return false }
func (cacheInitTestConfig) GetInt(string) int                { return 0 }
func (cacheInitTestConfig) GetInt32(string) int32            { return 0 }
func (cacheInitTestConfig) GetInt64(string) int64            { return 0 }
func (cacheInitTestConfig) GetFloat64(string) float64        { return 0 }
func (cacheInitTestConfig) GetDuration(string) time.Duration { return 0 }
func (cacheInitTestConfig) GetStringSlice(string) []string   { return nil }
func (cacheInitTestConfig) GetUintSlice(string) []uint       { return nil }
func (cacheInitTestConfig) Set(string, interface{})          {}
func (cacheInitTestConfig) SaveConfig() error                { return nil }

func TestNewCacheLegacyMemoryModeRemainsAvailable(t *testing.T) {
	previousConfig, previousPaths := app.ConfigYml, standalonePaths
	t.Cleanup(func() {
		app.ConfigYml = previousConfig
		standalonePaths = previousPaths
	})
	app.ConfigYml = cacheInitTestConfig{cacheType: "memory"}
	standalonePaths = standalone.Paths{}

	cache := newCache()
	require.NotNil(t, cache)
	require.NoError(t, cache.Close())
}

func TestNewCacheStandaloneRejectsMemoryFallback(t *testing.T) {
	previousConfig, previousPaths := app.ConfigYml, standalonePaths
	t.Cleanup(func() {
		app.ConfigYml = previousConfig
		standalonePaths = previousPaths
	})
	app.ConfigYml = cacheInitTestConfig{cacheType: "memory"}
	standalonePaths = standalone.Paths{Explicit: true}

	require.Panics(t, func() { _ = newCache() })
}

func TestNewCacheSQLiteRejectsMemoryFallback(t *testing.T) {
	previousConfig, previousPaths := app.ConfigYml, standalonePaths
	t.Cleanup(func() {
		app.ConfigYml = previousConfig
		standalonePaths = previousPaths
	})
	app.ConfigYml = cacheInitTestConfig{cacheType: "memory", dbType: "sqlite"}
	standalonePaths = standalone.Paths{}

	require.Panics(t, func() { _ = newCache() })
}

func TestNewCacheRedisFailureStopsStartup(t *testing.T) {
	previousConfig, previousPaths := app.ConfigYml, standalonePaths
	t.Cleanup(func() {
		app.ConfigYml = previousConfig
		standalonePaths = previousPaths
	})
	app.ConfigYml = cacheInitTestConfig{cacheType: "redis", redisHost: "127.0.0.1", redisPort: "1"}
	standalonePaths = standalone.Paths{Explicit: true}

	require.Panics(t, func() { _ = newCache() })
}
