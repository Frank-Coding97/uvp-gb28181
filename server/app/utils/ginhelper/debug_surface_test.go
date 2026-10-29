package ginhelper

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

// 占位/过短的签名密钥必须被识别出来 ——
// 否则「出厂包带着 CHANGE_ME 上线」这条会从调试面重新漏回来。
func TestSignKeyLooksPlaceholder(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want bool
	}{
		{"空串", "", true},
		{"纯空白", "   ", true},
		{"模板占位符", "CHANGE_ME_TO_A_RANDOM_SECRET", true},
		{"小写占位符", "change_me_to_a_random_secret", true},
		{"your_secret 变体", "your_secret_key_for_uvp_platform", true},
		{"31 位（密钥长度不足）", strings.Repeat("a", 31), true},
		{"32 位随机串（刚好达标）", strings.Repeat("a", 32), false},
		{"64 位十六进制", strings.Repeat("5ab1ecd7", 8), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, SignKeyLooksPlaceholder(tc.key))
		})
	}
}

// 缓存转储的脱敏必须覆盖「拿到就能冒用身份」的键，
// 同时不能把普通业务键也一并抹掉（否则这个调试端点就没用了）。
func TestMaskSensitiveCacheItems(t *testing.T) {
	origin := []app.CacheItem{
		{Key: "uvp-gb28181:refresh_token:11", Value: "eyJhbGciOi.REFRESH.TOKEN", ExpiresAt: time.Now()},
		{Key: "uvp-gb28181:token:11:ab12cd34", Value: "eyJhbGciOi.ACCESS.TOKEN", ExpiresAt: time.Now()},
		{Key: "uvp-gb28181:qr:token:abc", Value: "one-time-qr-token", ExpiresAt: time.Now()},
		{Key: "uvp-gb28181:device:status:34020000001320000001", Value: "ONLINE", ExpiresAt: time.Now()},
		{Key: "uvp-gb28181:zlm:secret", Value: "s3cret", ExpiresAt: time.Now()},
	}

	masked := MaskSensitiveCacheItems(origin)

	require.Len(t, masked, len(origin))
	for i, item := range masked {
		if sensitiveCacheKeyPattern.MatchString(item.Key) {
			require.Equal(t, redactedCacheValue, item.Value, "键 %s 的值必须脱敏", item.Key)
		} else {
			require.Equal(t, origin[i].Value, item.Value, "非敏感键 %s 的值应保持原样", item.Key)
		}
	}

	// 脱敏只能作用在副本上：入参被就地改写会让「谁调用了谁」变得不可预测。
	require.Equal(t, "eyJhbGciOi.REFRESH.TOKEN", origin[0].Value)
	require.Equal(t, "ONLINE", origin[3].Value)
}

// 出厂默认配置（appdebug=false）下调试面必须整体不生效。
func TestDebugSurfaceAllowedRequiresStrongKey(t *testing.T) {
	require.False(t, debugSurfaceAllowedFor(true, "CHANGE_ME_TO_A_RANDOM_SECRET"),
		"appdebug 开着但密钥是占位符时也绝不能放行")
	require.False(t, debugSurfaceAllowedFor(true, ""), "空密钥必须视为占位符")
	require.False(t, debugSurfaceAllowedFor(true, strings.Repeat("a", 31)), "过短密钥必须视为占位符")

	require.True(t, debugSurfaceAllowedFor(true, strings.Repeat("9", 64)),
		"强密钥 + appdebug 属于本地调试的合法组合")

	require.False(t, debugSurfaceAllowedFor(false, strings.Repeat("9", 64)),
		"appdebug 关闭时调试面必须关闭")
}
