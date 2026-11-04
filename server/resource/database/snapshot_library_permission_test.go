package database_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 图像库按钮权限（2026-11-04）。
//
// 背景：图像库上线时只有一条 type=2 页面菜单，permission 为空，页面可见性借用
// 「多屏播放 → 设备图像抓拍会话」的 gb28181:device:snapshot；「删除抓拍图像」
// (sys_api 639) 更是**没有任何 sys_menu_api 挂载** ⇒ 角色配置界面配不出删除权，
// 只有 casbin 里写死的 role_1 能用。
//
// 本测试钉住拆分后的三档权限与接口归属。⛔ 判据是「接口挂在哪个菜单」而不是
// 「接口存在」：接口全部早就在 sys_api 里了，问题恰恰是挂错了地方/没挂。
const (
	snapshotLibraryMenuID  = 140509
	snapshotDownloadMenuID = 140620
	snapshotDeleteMenuID   = 140621
	snapshotCaptureMenuID  = 140450
)

type snapshotLibraryMenu struct {
	ID         int    `json:"id"`
	ParentID   int    `json:"parent_id"`
	Title      string `json:"title"`
	Type       int    `json:"type"`
	Permission string `json:"permission"`
}

func TestSnapshotLibraryMenuCarriesItsOwnPermission(t *testing.T) {
	menus := readSeedJSONL[snapshotLibraryMenu](t, "sys_menu.jsonl")
	byID := make(map[int]snapshotLibraryMenu, len(menus))
	permissionOwner := make(map[string]int, len(menus))
	for _, menu := range menus {
		byID[menu.ID] = menu
		if menu.Type == 3 && menu.Permission != "" {
			// 同一个权限码挂两个按钮 = 勾其中一个就顺带得到另一个的入口，
			// 拆分权限的全部意义都会被抵消。
			require.NotContains(t, permissionOwner, menu.Permission,
				"权限码 %s 被 %d 与 %d 两个按钮共用", menu.Permission, permissionOwner[menu.Permission], menu.ID)
			permissionOwner[menu.Permission] = menu.ID
		}
	}

	page, ok := byID[snapshotLibraryMenuID]
	require.True(t, ok, "图像库页面菜单 %d 缺失", snapshotLibraryMenuID)
	require.Equal(t, 2, page.Type, "图像库 %d 必须是页面菜单（type=2）", snapshotLibraryMenuID)
	// 页面菜单行必须自带 view 码：userservice 收集权限码时取 type IN (2,3)，
	// 存量角色只勾了菜单、没勾按钮，靠这一行才不至于升级后掉页面。
	require.Equal(t, "gb28181:snapshot:view", page.Permission,
		"图像库菜单 %d 必须自带 gb28181:snapshot:view（勾菜单即得查看权）", snapshotLibraryMenuID)

	for _, want := range []struct {
		id         int
		title      string
		permission string
	}{
		{snapshotDownloadMenuID, "批量下载图像", "gb28181:snapshot:download"},
		{snapshotDeleteMenuID, "删除图像", "gb28181:snapshot:delete"},
	} {
		menu, exists := byID[want.id]
		require.True(t, exists, "图像库按钮菜单 %d 缺失", want.id)
		require.Equal(t, 3, menu.Type, "按钮 %d 必须是 type=3", want.id)
		require.Equal(t, snapshotLibraryMenuID, menu.ParentID, "按钮 %d 必须挂在图像库菜单下", want.id)
		require.Equal(t, want.title, menu.Title, "按钮 %d 标题", want.id)
		require.Equal(t, want.permission, menu.Permission, "按钮 %d 权限码", want.id)
	}
}

func TestSnapshotLibraryAPIsAreMountedOnTheRightMenu(t *testing.T) {
	apis := readSeedJSONL[seedAPI](t, "sys_api.jsonl")
	menuIDsByAPI := make(map[int]map[int]bool)
	for _, link := range readSeedJSONL[seedMenuAPI](t, "sys_menu_api.jsonl") {
		if menuIDsByAPI[link.APIID] == nil {
			menuIDsByAPI[link.APIID] = make(map[int]bool, 2)
		}
		menuIDsByAPI[link.APIID][link.MenuID] = true
	}

	// 按 (path, method) 找接口，不写死 id：id 在不同环境/后续新增里会漂。
	findAPI := func(path, method string) seedAPI {
		for _, api := range apis {
			if api.Path == path && api.Method == method {
				return api
			}
		}
		require.FailNow(t, "sys_api 里找不到接口", "%s %s", method, path)
		return seedAPI{}
	}

	cases := []struct {
		path   string
		method string
		menuID int
		why    string
	}{
		{"/api/gb28181/device-mgmt/snapshots", "GET", snapshotLibraryMenuID,
			"列表接口是页面基础数据，随查看权走"},
		{"/api/gb28181/device-mgmt/snapshots/:id/content", "GET", snapshotLibraryMenuID,
			"取图接口同时喂缩略图，不能归到下载按钮（否则没下载权整页白图）"},
		{"/api/gb28181/device-mgmt/snapshots/archive", "GET", snapshotDownloadMenuID,
			"打包下载必须由下载按钮单独授权"},
		{"/api/gb28181/device-mgmt/snapshots", "DELETE", snapshotDeleteMenuID,
			"删除是永久删磁盘文件，必须能单独授予/收回"},
	}

	for _, tc := range cases {
		api := findAPI(tc.path, tc.method)
		menus := menuIDsByAPI[api.ID]
		require.NotNil(t, menus, "%s %s（api %d）没有任何菜单挂载 —— 角色配置界面配不出它", tc.method, tc.path, api.ID)
		require.True(t, menus[tc.menuID], "%s %s（api %d）应挂在菜单 %d：%s", tc.method, tc.path, api.ID, tc.menuID, tc.why)
		// 原来这些接口都挂在「设备图像抓拍会话」上，会让「能抓拍」顺带等于「能删图」。
		require.False(t, menus[snapshotCaptureMenuID],
			"%s %s（api %d）仍挂在抓拍会话按钮 %d 上：抓拍权会顺带拿到图像库写权限", tc.method, tc.path, api.ID, snapshotCaptureMenuID)
	}
}

func TestSnapshotLibraryPermissionMigrationIsIdempotent(t *testing.T) {
	path := filepath.Join("gb28181", "migrations", "2026-11-04-snapshot-library-permissions.sql")
	content, err := os.ReadFile(path)
	require.NoError(t, err, "补开发库的迁移脚本缺失：%s", path)
	sql := strings.ToLower(string(content))

	for _, token := range []string{
		"gb28181:snapshot:view",
		"gb28181:snapshot:download",
		"gb28181:snapshot:delete",
		"140509",
		"140620",
		"140621",
		"/api/gb28181/device-mgmt/snapshots/archive",
		"sys_menu_api",
		"not exists",
	} {
		require.Contains(t, sql, strings.ToLower(token), "迁移脚本缺少 %s", token)
	}
}
