package setup

import (
	"context"
	"net"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type fakeLegacyConfig map[string]any

func (f fakeLegacyConfig) GetString(key string) string {
	value, _ := f[key].(string)
	return value
}

func (f fakeLegacyConfig) GetInt(key string) int {
	value, _ := f[key].(int)
	return value
}

func (f fakeLegacyConfig) GetStringSlice(key string) []string {
	value, _ := f[key].([]string)
	return value
}

func completeLegacyConfig() fakeLegacyConfig {
	return fakeLegacyConfig{
		"gb28181.sip.ip":        "192.168.1.10",
		"gb28181.sip.port":      5061,
		"gb28181.sip.transport": []string{"udp", "tcp"},
		"gb28181.sip.domain":    "3402000000",
		"gb28181.sip.serverid":  "34020000002000000001",
		"gb28181.sip.password":  "Secret123",
	}
}

func newEffectiveConfigDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SIPConfig{}))
	return db
}

func TestLoadEffectiveSIPConfig_DatabaseWins(t *testing.T) {
	db := newEffectiveConfigDB(t)
	require.NoError(t, db.Create(&SIPConfig{
		ID: SingletonID, DeploymentMode: DeploymentLAN, ListenIP: "10.0.0.1", AdvertiseIP: "10.0.0.1",
		Port: 5062, Domain: "4401000000", ServerID: "44010000002000000001", Password: "db-secret",
	}).Error)
	legacy := completeLegacyConfig()

	got, err := NewEffectiveConfigLoader(db, legacy, nil).Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", got.ListenIP)
	require.Equal(t, 5062, got.Port)
	require.Equal(t, ConfigSourceDatabase, got.Source)

	var count int64
	require.NoError(t, db.Model(&SIPConfig{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestLoadEffectiveSIPConfig_SeedsConcreteLegacyIPOnce(t *testing.T) {
	db := newEffectiveConfigDB(t)
	legacy := completeLegacyConfig()
	loader := NewEffectiveConfigLoader(db, legacy, nil)

	got, err := loader.Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, "192.168.1.10", got.ListenIP)
	require.Equal(t, "192.168.1.10", got.AdvertiseIP)
	require.Equal(t, ConfigSourceYAMLSeed, got.Source)

	legacy["gb28181.sip.ip"] = "192.168.1.99"
	second, err := loader.Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, "192.168.1.10", second.ListenIP)
	require.Equal(t, ConfigSourceDatabase, second.Source)
}

func TestLoadEffectiveSIPConfig_UsesExplicitAdvertiseIP(t *testing.T) {
	db := newEffectiveConfigDB(t)
	legacy := completeLegacyConfig()
	legacy["gb28181.sip.ip"] = wildcardIPv4
	legacy["gb28181.sip.advertiseip"] = "203.0.113.10"

	got, err := NewEffectiveConfigLoader(db, legacy, nil).Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, wildcardIPv4, got.ListenIP)
	require.Equal(t, "203.0.113.10", got.AdvertiseIP)
	require.False(t, got.AdvertiseIPInferred)
}

func TestLoadEffectiveSIPConfig_InfersAdvertiseIP(t *testing.T) {
	db := newEffectiveConfigDB(t)
	legacy := completeLegacyConfig()
	legacy["gb28181.sip.ip"] = wildcardIPv4
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{{Index: 1, Name: "en0", Flags: net.FlagUp}},
		addresses:  map[int][]net.Addr{1: {mustCIDR(t, "192.168.10.20/24")}},
	}

	got, err := NewEffectiveConfigLoader(db, legacy, provider).Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, "192.168.10.20", got.AdvertiseIP)
	require.True(t, got.AdvertiseIPInferred)
	require.Equal(t, ConfigSourceInferred, got.Source)
}

func TestLoadEffectiveSIPConfig_MissingDoesNotPersistPartialRow(t *testing.T) {
	db := newEffectiveConfigDB(t)
	legacy := completeLegacyConfig()
	legacy["gb28181.sip.ip"] = wildcardIPv4
	provider := fakeInterfaceProvider{}

	got, err := NewEffectiveConfigLoader(db, legacy, provider).Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, ConfigSourceMissing, got.Source)

	var count int64
	require.NoError(t, db.Model(&SIPConfig{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestLoadEffectiveSIPConfig_DefaultsTransport(t *testing.T) {
	db := newEffectiveConfigDB(t)
	legacy := completeLegacyConfig()
	legacy["gb28181.sip.transport"] = []string(nil)

	got, err := NewEffectiveConfigLoader(db, legacy, nil).Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"udp", "tcp"}, got.Transport)
}
