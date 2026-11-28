package controllers_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 菜单**不得**再按配置文件的开关过滤（老板 2026-10-08 定）。
//
// 原实现里 `GetRouters` 会按 `gb28181.trace.enabled` 把
// `/gb28181/sip-traces`（SIP 日志）及其子菜单剔掉，三条问题都真实发生过：
//  1. 用户只认界面上的开关（「国标服务配置 → 是否开启 SIP 日志」），
//     没人改 config.yml ⇒ 界面上开了、菜单仍不出现；
//  2. 该配置只有重启后端才进 viper，而界面保存走的是 SaveConfig + 热重载 SIP 服务；
//  3. 功能关闭时点进去一片空白，分不清是「没数据」还是「功能没开」。
//
// ⇒ 契约改为：**数据库里有就展示**，关闭态由页面自己提示并引导去开启。
//
// ⛔ 断言扫的是**代码**（先去掉注释与字符串字面量），
//
//	否则这段说明自己的注释里出现同一个键名就会自我判红 —— 第一次就这么红过。
func TestMenuTreeIsNotFilteredByConfigFlags(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("sysmenu.go"))
	require.NoError(t, err)
	code := stripCommentsAndStrings(string(body))

	require.NotContains(t, code, "filterDisabledFeatureMenus",
		"菜单不得再按配置文件的开关过滤（2026-10-08 起）")
	require.NotContains(t, code, "disabledFeatures",
		"SIP 日志菜单不得因 trace 开关关闭而被隐藏（2026-10-08 起）")
	require.NotContains(t, code, "sip-traces",
		"GetRouters 里不应再出现对 sip-traces 菜单路径的特殊处理")
}

// ⛔ 反向提醒：一旦有人再加回「按配置隐藏菜单」的机制，上面那条会红。
// 判据用**函数名/变量名**而不是配置键 —— 换名字重新实现同样要拦住，
// 而注释里为了说明历史必然会提到那个配置键，不能拿它当判据。
func TestMenuControllerHasNoFeatureVisibilitySwitch(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("sysmenu.go"))
	require.NoError(t, err)
	code := stripCommentsAndStrings(string(body))

	// ⛔⛔ 判据必须**逐个词**分开查，不能只查 "trace" ——
	//   第一版就只写了一个 `NotContains(code, "trace")`，
	//   结果内联版过滤逻辑（变量叫 `disabledFeatures`）把这条绕过去了，
	//   实测那条变异体里它是 **PASS 的假绿灯**。
	for _, token := range []string{
		"trace",           // 配置键 gb28181.trace.enabled 的后缀
		"disabledFeature", // 原实现那个 map 的名字（去尾匹配也能命中 disabledFeatures）
		"sip-traces",      // 被隐藏的那个菜单路径
		"GetBool",         // 从配置读布尔来决定显隐的典型动作
	} {
		require.NotContains(t, code, token,
			"GetRouters 里不应再按配置隐藏菜单（出现了 "+token+
				"）；功能可用性请由页面的 health.state 提示，不要在菜单层藏入口")
	}
}

// stripCommentsAndStrings 粗粒度去掉注释与双引号字符串，避免文档性文字触发源码断言。
// ⛔ 只处理 `//` 与 `/* */`、以及一行里**第一个**双引号之后的内容 ——
//
//	它服务于「扫关键词」这个用途，不追求可编译；过度加工反而会漏判。
func stripCommentsAndStrings(src string) string {
	var out strings.Builder
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if inBlock {
			if idx := strings.Index(trimmed, "*/"); idx >= 0 {
				line, inBlock = trimmed[idx+2:], false
			} else {
				continue
			}
		}
		if strings.HasPrefix(trimmed, "/*") {
			if idx := strings.Index(trimmed, "*/"); idx >= 0 {
				line = trimmed[idx+2:]
			} else {
				inBlock = true
				continue
			}
		}
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		if q := strings.Index(line, `"`); q >= 0 {
			line = line[:q]
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}
