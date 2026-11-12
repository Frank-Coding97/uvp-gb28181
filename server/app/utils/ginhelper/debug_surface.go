package ginhelper

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

// minSignKeyLen JWT 签名密钥的最小长度。
//
// ⛔ 32 是按「HS256 至少 256 bit」定的：低于这个长度的密钥，
// 配合 `/viewCache` 这类调试面暴露出去后，签名强度形同没有签名。
const minSignKeyLen = 32

// placeholderSignKeyFragments 模板里留下的占位密钥特征。
//
// ⛔ 只判「空」是不够的：2026-10-20 的出厂包实测就是
// `jwttokensignkey: "CHANGE_ME_TO_A_RANDOM_SECRET"` —— 非空、但不是密钥。
// 所以判据必须是「长度 + 占位特征」，两者任一命中都算不合格。
var placeholderSignKeyFragments = []string{
	"change_me", "change-me", "changeme",
	"your_secret", "your-secret", "placeholder",
	"random_secret", "todo",
}

// SignKeyLooksPlaceholder 判断 JWT 签名密钥是否是「空 / 占位 / 过短」。
//
// 导出是为了让测试与出包校验复用同一套判据 ——
// 判据写两份，早晚会漂移成「包校验通过、运行时仍然放行」。
func SignKeyLooksPlaceholder(key string) bool {
	trimmed := strings.TrimSpace(key)
	if len(trimmed) < minSignKeyLen {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, fragment := range placeholderSignKeyFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

// DebugSurfaceAllowed 判断**调试面**（swagger / pprof / viewCache）是否允许注册。
//
// ⛔⛔ 为什么不能只看 `server.appdebug`：
//
//	已有安装的 config.yml 是**用户资产**，升级时不会被覆盖 —— 出包模板改了
//	`appdebug: false` 只对新装生效，老现场仍然带着 `appdebug: true` 跑到线上。
//	所以这里再加一道运行时判据：签名密钥还是占位符 ⇒ 调试面一律不注册。
//	表现是「老现场升级后 /viewCache、/debug/pprof 直接 404」，不需要客户改配置；
//	本地开发用的 config.yml 本来就是 64 位随机密钥，不受影响。
func DebugSurfaceAllowed() bool {
	if app.ConfigYml == nil {
		return false
	}
	return debugSurfaceAllowedFor(
		app.ConfigYml.GetBool("server.appdebug"),
		app.ConfigYml.GetString("token.jwttokensignkey"),
	)
}

// debugSurfaceAllowedFor 是判定本身（与配置来源解耦，便于单测）。
func debugSurfaceAllowedFor(appDebug bool, signKey string) bool {
	return appDebug && !SignKeyLooksPlaceholder(signKey)
}

// sensitiveCacheKeyPattern 命中即脱敏的缓存键特征。
//
// 覆盖的是「拿到就能冒用身份 / 解密密文」的那几类：
// token（accessToken、refresh_token、qr 配网令牌）、secret、password、
// capability/signature（一次性下载票据）、private_key / api_key。
var sensitiveCacheKeyPattern = regexp.MustCompile(
	`(?i)(token|secret|password|passwd|credential|capability|signature|private[_-]?key|api[_-]?key)`,
)

const redactedCacheValue = "<redacted>"

// MaskSensitiveCacheItems 返回脱敏后的缓存项副本（不修改入参）。
//
// ⛔⛔ `/viewCache` 的价值是「看有哪些键、什么时候过期」，
//
//	不是「把值抄出来」—— 而它扫的是整库，refresh token 是**原文**存进去的
//	（`tokenhelper.storeRefreshToken` 直接 Set 令牌字符串，且不受 `token.isCache` 控制）。
//	⇒ 只要值仍然原样返回，任何登录用户都能把别人的 30 天续期令牌抄走。
//	因此键名命中敏感特征时，值一律替换成 `<redacted>`，只保留键与过期时间。
func MaskSensitiveCacheItems(items []app.CacheItem) []app.CacheItem {
	masked := make([]app.CacheItem, 0, len(items))
	for _, item := range items {
		next := item
		if sensitiveCacheKeyPattern.MatchString(item.Key) {
			next.Value = redactedCacheValue
		}
		masked = append(masked, next)
	}
	return masked
}

// ViewCacheHandler 缓存查看端点（仅调试面注册，且必须挂在登录鉴权之后）。
//
// ⛔ 该端点扫描整库（`Scan("*")`），**绝不能**在前置中间件缺失的情况下注册；
//
//	历史形态是裸挂在 engine 上，未登录即可 `curl /viewCache` 拿到全部缓存值。
func ViewCacheHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := app.Cache.GetAll(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取缓存项失败", "details": err.Error()})
			return
		}
		c.JSON(http.StatusOK, MaskSensitiveCacheItems(items))
	}
}
