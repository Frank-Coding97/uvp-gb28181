package setup

import (
	"context"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/utils/cachehelper"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

// T1 — QRService.
// 扫码回填 SIP:平台生成一次性 token(内含六元组快照),模拟器扫码后原子兑换。
// spec: wiki/projects/uvp-gb28181/specs/qr-sip-provisioning.md

func newQRTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SIPConfig{}))
	return db
}

// seedSIPConfig 写入一条可用的 SIP 配置(单例 id=1)
func seedSIPConfig(t *testing.T, db *gorm.DB, password string) {
	t.Helper()
	row := SIPConfig{
		ID:             SingletonID,
		DeploymentMode: DeploymentLAN,
		ListenIP:       "0.0.0.0",
		AdvertiseIP:    "192.168.1.10",
		Port:           5061,
		Domain:         "3402000000",
		ServerID:       "34020000002000000001",
		Password:       password,
	}
	require.NoError(t, db.Save(&row).Error)
}

// newQRService 组装一个用内存 cache 的 QRService
func newQRService(t *testing.T, db *gorm.DB, transport []string) *QRService {
	t.Helper()
	cache := cachehelper.NewMemoryHelper()
	t.Cleanup(func() { _ = cache.Close() })
	service := NewQRService(cache, NewSIPConfigService(db), func() []string { return transport })
	service.networkProvider = fakeInterfaceProvider{
		interfaces: []net.Interface{{Index: 1, Name: "en0", Flags: net.FlagUp}},
		addresses:  map[int][]net.Addr{1: {mustCIDR(t, "192.168.1.10/24")}},
	}
	return service
}

// 1.1 token 长度与字符集
func TestQRGenerateToken_Format(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")
	svc := newQRService(t, db, []string{"udp"})

	token, expiresIn, err := svc.GenerateToken(context.Background())
	require.NoError(t, err)
	require.Len(t, token, 22, "128 bit 经 base64url 无填充编码为 22 字符")
	require.NotContains(t, token, "=", "RawURLEncoding 不应有填充")
	require.NotContains(t, token, "+")
	require.NotContains(t, token, "/")
	_, decErr := base64.RawURLEncoding.DecodeString(token)
	require.NoError(t, decErr, "token 必须是合法 base64url")
	require.Equal(t, 300, expiresIn, "TTL 5 分钟")
}

// 1.2 两次生成不重复
func TestQRGenerateToken_Unique(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")
	svc := newQRService(t, db, []string{"udp"})
	ctx := context.Background()

	a, _, err := svc.GenerateToken(ctx)
	require.NoError(t, err)
	b, _, err := svc.GenerateToken(ctx)
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

// 1.3 正常兑换返回六元组
func TestQRExchange_ReturnsPayload(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")
	svc := newQRService(t, db, []string{"udp"})
	ctx := context.Background()

	token, _, err := svc.GenerateToken(ctx)
	require.NoError(t, err)

	payload, err := svc.Exchange(ctx, token)
	require.NoError(t, err)
	require.Equal(t, "34020000002000000001", payload.ServerID)
	require.Equal(t, "3402000000", payload.Domain)
	require.Equal(t, "192.168.1.10", payload.IP)
	require.Equal(t, 5061, payload.Port)
	require.Equal(t, "udp", payload.Transport)
	require.Equal(t, "Str0ng!Passw0rd#2026", payload.Password)
}

// 1.4 一次性 —— 核心安全性质
func TestQRExchange_SingleUse(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")
	svc := newQRService(t, db, []string{"udp"})
	ctx := context.Background()

	token, _, err := svc.GenerateToken(ctx)
	require.NoError(t, err)

	_, err = svc.Exchange(ctx, token)
	require.NoError(t, err, "第一次兑换应成功")

	_, err = svc.Exchange(ctx, token)
	require.ErrorIs(t, err, ErrTokenInvalid, "第二次兑换必须失败")
}

// 1.5 格式合法但未生成的 token
func TestQRExchange_UnknownToken(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")
	svc := newQRService(t, db, []string{"udp"})

	_, err := svc.Exchange(context.Background(), strings.Repeat("A", 22))
	require.ErrorIs(t, err, ErrTokenInvalid)
}

// 1.6 格式非法不查 cache
func TestQRExchange_MalformedTooShort(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")
	svc := newQRService(t, db, []string{"udp"})

	_, err := svc.Exchange(context.Background(), "abc")
	require.ErrorIs(t, err, ErrTokenMalformed)
}

// 1.7 含非 base64url 字符
func TestQRExchange_MalformedBadChars(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")
	svc := newQRService(t, db, []string{"udp"})

	// 22 字符但含 base64url 不允许的 + / =
	_, err := svc.Exchange(context.Background(), "AAAA+AAAA/AAAA=AAAAAAA")
	require.ErrorIs(t, err, ErrTokenMalformed)
}

// 1.8 SIP 未配置时不发码
func TestQRGenerateToken_SIPNotConfigured(t *testing.T) {
	db := newQRTestDB(t) // 不 seed
	svc := newQRService(t, db, []string{"udp"})

	_, _, err := svc.GenerateToken(context.Background())
	require.ErrorIs(t, err, ErrSIPNotConfigured)
}

// 1.9 transport 取首项
func TestQRPayload_TransportFirst(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")
	svc := newQRService(t, db, []string{"tcp", "udp"})
	ctx := context.Background()

	token, _, err := svc.GenerateToken(ctx)
	require.NoError(t, err)
	payload, err := svc.Exchange(ctx, token)
	require.NoError(t, err)
	require.Equal(t, "tcp", payload.Transport)
}

// 1.10 transport 为空时回落 udp
func TestQRPayload_TransportFallback(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")
	svc := newQRService(t, db, nil)
	ctx := context.Background()

	token, _, err := svc.GenerateToken(ctx)
	require.NoError(t, err)
	payload, err := svc.Exchange(ctx, token)
	require.NoError(t, err)
	require.Equal(t, "udp", payload.Transport)
}

// 1.11 快照不受后续改配置影响 —— spec D8 的回归防线
// v1 设计"兑换时现读配置"会让二维码生成后改密码,旧 token 换到新密码。
func TestQRExchange_SnapshotImmuneToConfigChange(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Old!Passw0rd#2026")
	svc := newQRService(t, db, []string{"udp"})
	ctx := context.Background()

	token, _, err := svc.GenerateToken(ctx)
	require.NoError(t, err)

	// 生成二维码后管理员轮换了统一密码
	seedSIPConfig(t, db, "New!Passw0rd#2026")

	payload, err := svc.Exchange(ctx, token)
	require.NoError(t, err)
	require.Equal(t, "Old!Passw0rd#2026", payload.Password,
		"token 必须返回生成时的快照,不能换到轮换后的新密码")
}

// 1.12 兑换不查 DB —— 快照自足
func TestQRExchange_DoesNotTouchDB(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")
	svc := newQRService(t, db, []string{"udp"})
	ctx := context.Background()

	token, _, err := svc.GenerateToken(ctx)
	require.NoError(t, err)

	// 关掉底层连接:若 Exchange 还查 DB 就会报错
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	payload, err := svc.Exchange(ctx, token)
	require.NoError(t, err, "兑换路径不应依赖 DB")
	require.Equal(t, "Str0ng!Passw0rd#2026", payload.Password)
}

// 1.13 TTL 生效
func TestQRExchange_Expired(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")
	cache := cachehelper.NewMemoryHelper()
	t.Cleanup(func() { _ = cache.Close() })
	// 用极短 TTL 的服务实例验证过期路径
	svc := NewQRService(cache, NewSIPConfigService(db), func() []string { return []string{"udp"} })
	svc.ttl = 10 * time.Millisecond

	ctx := context.Background()
	token, _, err := svc.GenerateToken(ctx)
	require.NoError(t, err)

	time.Sleep(30 * time.Millisecond)

	_, err = svc.Exchange(ctx, token)
	require.ErrorIs(t, err, ErrTokenInvalid)
}

// 1.14 兑换限流:burst 耗尽后返回 ErrTooManyAttempts,不查缓存
func TestQRExchange_RateLimited(t *testing.T) {
	db := newQRTestDB(t)
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")
	svc := newQRService(t, db, []string{"udp"})
	svc.limiter = rate.NewLimiter(rate.Limit(1), 2) // burst=2

	ctx := context.Background()
	token, _, err := svc.GenerateToken(ctx)
	require.NoError(t, err)

	// burst 内:第 1 次真实兑换成功
	payload, err := svc.Exchange(ctx, token)
	require.NoError(t, err)
	require.Equal(t, "Str0ng!Passw0rd#2026", payload.Password)

	// 第 2 次:token 已消费 → ErrTokenInvalid(仍消耗配额)
	_, err = svc.Exchange(ctx, token)
	require.ErrorIs(t, err, ErrTokenInvalid)

	// 第 3 次:burst 耗尽 → ErrTooManyAttempts
	_, err = svc.Exchange(ctx, token)
	require.ErrorIs(t, err, ErrTooManyAttempts)
}
