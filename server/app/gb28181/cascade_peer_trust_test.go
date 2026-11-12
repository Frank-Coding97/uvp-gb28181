package gb28181

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

func newPeerTrustTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "peer-trust.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.GbCascadePlatform{}))
	return db
}

func seedPeerTrustPlatform(t *testing.T, db *gorm.DB, name, host, transport string, enabled bool) {
	t.Helper()
	require.NoError(t, db.Create(&model.GbCascadePlatform{
		Name:             name,
		UpstreamServerID: "37010000002000000001",
		UpstreamDomain:   "3701000000",
		Host:             host,
		Port:             51064,
		LocalDeviceID:    "34020000002000000001",
		LocalDomain:      "3402000000",
		Transport:        transport,
		Enabled:          enabled,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}).Error)
}

// 上级平台按 IP 登记 UDP 接入时,它的源地址要被认成可信对端。
func TestCascadePlatformPeerTrustMatchesEnabledPlatformIP(t *testing.T) {
	db := newPeerTrustTestDB(t)
	seedPeerTrustPlatform(t, db, "62-上级", "62.234.152.39", "UDP", true)

	match := newCascadePlatformPeerTrust(db)
	require.True(t, match("62.234.152.39", "udp"))
	// 传输层不同不算:平台登记 UDP 时,从同一 IP 来的 TCP 请求不是它。
	require.False(t, match("62.234.152.39", "tcp"))
	require.False(t, match("62.234.152.40", "udp"))
	require.False(t, match("", "udp"))
}

// 停用的平台不参与放行 —— 否则「关掉级联」等于把它的地址留在准入白名单里。
func TestCascadePlatformPeerTrustIgnoresDisabledPlatform(t *testing.T) {
	db := newPeerTrustTestDB(t)
	seedPeerTrustPlatform(t, db, "62-上级", "62.234.152.39", "UDP", false)

	require.False(t, newCascadePlatformPeerTrust(db)("62.234.152.39", "udp"))
}

// Host 填域名是线上真实配置(62 就是 gb28181.uvplatform.com),要解析后再比。
func TestCascadePlatformPeerTrustResolvesHostname(t *testing.T) {
	db := newPeerTrustTestDB(t)
	seedPeerTrustPlatform(t, db, "localhost-上级", "localhost", "UDP", true)

	match := newCascadePlatformPeerTrust(db)
	require.True(t, match("127.0.0.1", "udp"))
	require.True(t, match("::1", "udp"))
}

// 表为空(新装、还没建级联)时不能放行任何来源。
func TestCascadePlatformPeerTrustEmptyTableRejectsAll(t *testing.T) {
	require.False(t, newCascadePlatformPeerTrust(newPeerTrustTestDB(t))("62.234.152.39", "udp"))
	require.False(t, newCascadePlatformPeerTrust(nil)("62.234.152.39", "udp"))
}

// 快照要过期:缓存在 TTL 内命中,过期后能读到新加的平台。
func TestCascadePlatformPeerTrustCacheRefreshesAfterTTL(t *testing.T) {
	db := newPeerTrustTestDB(t)
	cache := &cascadePeerTrustCache{db: db, ttl: 30 * time.Second}

	require.False(t, cache.match("62.234.152.39", "udp"))
	seedPeerTrustPlatform(t, db, "62-上级", "62.234.152.39", "UDP", true)
	// 仍在 TTL 内,读的是旧快照。
	require.False(t, cache.match("62.234.152.39", "udp"))

	// TTL 到期后重新加载。
	cache.mu.Lock()
	cache.expires = time.Now().Add(-time.Second)
	cache.mu.Unlock()
	require.True(t, cache.match("62.234.152.39", "udp"))
}
