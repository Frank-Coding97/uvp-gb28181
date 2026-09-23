package models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 分组清单是「接口管理」唯一可读的组织维度，且不参与鉴权 —— 一旦脏了就没人拦得住。
// 这组断言把 2026-09 那次整治的三条教训钉住：乱码组名、重复项、自由文本。
func TestSysApiGroupNamesIsCleanControlledList(t *testing.T) {
	names := SysApiGroupNames()
	require.NotEmpty(t, names)

	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		require.NotEmpty(t, name, "分组名不能为空")
		require.Equal(t, strings.TrimSpace(name), name, "分组名不能有首尾空白: %q", name)

		_, dup := seen[name]
		require.Falsef(t, dup, "分组名重复: %q", name)
		seen[name] = struct{}{}

		// 只允许 ASCII 可见字符与 CJK 汉字。
		// ⛔ 这条是乱码回归锚点：历史数据里出现过 `æŒ‰é’®æƒé™ç›®å½•`
		//    （「按钮权限目录」的 UTF-8 被按 cp1252 二次解读），
		//    `æ`/`’`/`™` 都落在允许集之外，会被这里挡下。
		for _, r := range name {
			ok := (r >= 0x20 && r <= 0x7E) || (r >= 0x4E00 && r <= 0x9FFF)
			require.Truef(t, ok, "分组名含非法字符 %q (U+%04X): %q", r, r, name)
		}
	}
}

func TestIsValidSysApiGroupAcceptsOnlyControlledNames(t *testing.T) {
	for _, name := range SysApiGroupNames() {
		require.Truef(t, IsValidSysApiGroup(name), "清单内的 %q 应被接受", name)
	}

	rejected := []string{
		"",
		" 设备管理",
		"设备管理 ",
		"设备管理2",
		"按钮权限目录",         // 整治前的兜底分组，已解散
		"GB28181 媒体管理",  // 整治前的旧名，已并入「流媒体管理」
		"GB28181设备管理",   // 整治前缺空格
		"æŒ‰é’®æƒé™ç®å½•",  // 双重编码的乱码组名
		"随便写一个分组",
	}
	for _, name := range rejected {
		require.Falsef(t, IsValidSysApiGroup(name), "清单外的 %q 应被拒绝", name)
	}
}

// 返回的必须是副本 —— 否则调用方（比如给前端拼响应）一改就把全局清单改了。
func TestSysApiGroupNamesReturnsCopy(t *testing.T) {
	got := SysApiGroupNames()
	require.NotEmpty(t, got)
	original := got[0]

	got[0] = "被改坏了"
	require.Equal(t, original, SysApiGroupNames()[0], "修改返回值不应影响清单本身")
	require.True(t, IsValidSysApiGroup(original))
	require.False(t, IsValidSysApiGroup("被改坏了"))
}
