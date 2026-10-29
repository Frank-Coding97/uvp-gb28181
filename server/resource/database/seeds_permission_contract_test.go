package database_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// seeds 是「新装库」的唯一真源：迁移目录已清空，三方言全量脚本全部由它生成。
// 这里钉住**接口授权关联数据**的结构不变量 —— 2026-09-23 的三方对账
// （Go 路由表 ↔ sys_api 登记 ↔ sys_menu_api 挂载）在真源里修掉的正是这几类缺陷：
//   - 幽灵登记：path 指向已删除/改名的路由（`sys_api` 有、路由无）
//   - 悬空挂载：`sys_menu_api.api_id` / `menu_id` 指向不存在的行
//   - 重复登记：同一 (path, method) 登记两次
//
// ⛔ 本测试**不**断言「api_group ∈ 受控清单」：受控清单在 sysapigroup.go，
// 与 seeds 的分组漂移是一件独立的事（当前 593~596 有 4 行不在清单内），
// 混进这里会让「关联数据」和「分组口径」两个问题互相掩盖。

type seedAPI struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Path     string `json:"path"`
	Method   string `json:"method"`
	APIGroup string `json:"api_group"`
}

type seedMenu struct {
	ID int `json:"id"`
}

type seedMenuAPI struct {
	MenuID int `json:"menu_id"`
	APIID  int `json:"api_id"`
}

func readSeedJSONL[T any](t *testing.T, name string) []T {
	t.Helper()
	path := filepath.Join("baseline", "seeds", name)
	f, err := os.Open(path)
	require.NoError(t, err, "打开 %s 失败", path)
	defer func() { require.NoError(t, f.Close()) }()

	var out []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var v T
		require.NoError(t, json.Unmarshal([]byte(line), &v), "%s 第 %d 行不是合法 JSON", name, len(out)+1)
		out = append(out, v)
	}
	require.NoError(t, sc.Err(), "读取 %s 失败", name)
	require.NotEmpty(t, out, "%s 为空", name)
	return out
}

var apiPathPattern = regexp.MustCompile(`^/api/`)

// sys_api 自身的结构不变量：id 唯一、(path, method) 唯一、路径形如 /api/...
func TestSeedsSysApiStructuralInvariants(t *testing.T) {
	apis := readSeedJSONL[seedAPI](t, "sys_api.jsonl")
	require.NotEmpty(t, apis)

	byID := map[int]int{}
	byRoute := map[string]int{}
	for i, a := range apis {
		require.Positive(t, a.ID, "sys_api.jsonl 第 %d 行 id 必须为正整数", i+1)
		require.Empty(t, byID[a.ID], "sys_api.id=%d 重复（第 %d 行与第 %d 行）", a.ID, byID[a.ID], i+1)
		byID[a.ID] = i + 1

		require.NotEmpty(t, strings.TrimSpace(a.Method), "sys_api.id=%d 的 method 为空", a.ID)
		require.Regexp(t, apiPathPattern, a.Path, "sys_api.id=%d 的 path=%q 不以 /api/ 开头", a.ID, a.Path)

		key := a.Method + " " + a.Path
		require.Empty(t, byRoute[key], "%s 被重复登记（sys_api.id=%d 与 %d）", key, byRoute[key], a.ID)
		byRoute[key] = a.ID
	}
	for _, a := range apis {
		if a.Path == "/api/gb28181/play/:deviceId/:channelId" && a.Method == "POST" {
			require.Equal(t, "设备管理", a.APIGroup, "实时播放接口不应归入多屏播放业务分类")
		}
		require.NotEqual(t, "/api/gb28181/play/:deviceId/:channelId/authorization", a.Path, "固定播放地址授权接口已移除")
	}
}

// sys_menu_api 的引用完整性：不能指向不存在的 api / menu，也不能重复挂同一对。
// 桥表是纯联合主键（menu_id, api_id），任何悬空引用都会让该权限点在授权时静默失效。
func TestSeedsMenuApiReferencesAreResolvable(t *testing.T) {
	apis := readSeedJSONL[seedAPI](t, "sys_api.jsonl")
	menus := readSeedJSONL[seedMenu](t, "sys_menu.jsonl")
	links := readSeedJSONL[seedMenuAPI](t, "sys_menu_api.jsonl")

	apiIDs := make(map[int]struct{}, len(apis))
	for _, a := range apis {
		apiIDs[a.ID] = struct{}{}
	}
	menuIDs := make(map[int]struct{}, len(menus))
	for _, m := range menus {
		menuIDs[m.ID] = struct{}{}
	}

	seen := map[[2]int]int{}
	var danglingAPI, danglingMenu, duplicated []string
	for i, l := range links {
		if _, ok := apiIDs[l.APIID]; !ok {
			danglingAPI = append(danglingAPI, strconv.Itoa(l.MenuID)+"→"+strconv.Itoa(l.APIID))
		}
		if _, ok := menuIDs[l.MenuID]; !ok {
			danglingMenu = append(danglingMenu, strconv.Itoa(l.MenuID)+"→"+strconv.Itoa(l.APIID))
		}
		key := [2]int{l.MenuID, l.APIID}
		if prev, ok := seen[key]; ok {
			duplicated = append(duplicated, strconv.Itoa(l.MenuID)+"→"+strconv.Itoa(l.APIID)+"(第 "+strconv.Itoa(prev)+"/"+strconv.Itoa(i+1)+" 行)")
		}
		seen[key] = i + 1
	}

	require.Empty(t, danglingAPI, "sys_menu_api 引用了不存在的 api_id（悬空挂载）: %v", danglingAPI)
	require.Empty(t, danglingMenu, "sys_menu_api 引用了不存在的 menu_id（悬空挂载）: %v", danglingMenu)
	require.Empty(t, duplicated, "sys_menu_api 出现重复挂载: %v", duplicated)
}
