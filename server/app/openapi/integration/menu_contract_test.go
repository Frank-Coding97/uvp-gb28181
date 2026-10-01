package integration

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var openAPIClientMenuButtons = []struct {
	permission string
	name       string
	title      string
}{
	{permission: "gb28181:openapi:client:read", name: "Permission_gb28181_openapi_client_read", title: "查看 OpenAPI 客户端"},
	{permission: "gb28181:openapi:client:create", name: "Permission_gb28181_openapi_client_create", title: "创建 OpenAPI 客户端"},
	{permission: "gb28181:openapi:client:grant", name: "Permission_gb28181_openapi_client_grant", title: "分配 OpenAPI 客户端能力"},
	{permission: "gb28181:openapi:client:rotate", name: "Permission_gb28181_openapi_client_rotate", title: "轮换 OpenAPI 客户端密钥"},
	{permission: "gb28181:openapi:client:status", name: "Permission_gb28181_openapi_client_status", title: "启停或撤销 OpenAPI 客户端"},
	{permission: "gb28181:openapi:client:audit", name: "Permission_gb28181_openapi_client_audit", title: "查看 OpenAPI 客户端审计"},
}

const (
	openAPIClientMenuPath      = "/gb28181/openapi-client"
	openAPIClientMenuName      = "gb28181-openapi-client"
	openAPIClientMenuComponent = "gb28181/openapi-client/index"
)

type baselineMenuRow struct {
	ID         int64  `json:"id"`
	ParentID   int64  `json:"parent_id"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	Component  string `json:"component"`
	Title      string `json:"title"`
	KeepAlive  int    `json:"keep_alive"`
	Type       int    `json:"type"`
	Permission string `json:"permission"`
}

type baselineRoleMenuRow struct {
	RoleID int64 `json:"role_id"`
	MenuID int64 `json:"menu_id"`
}

type baselineMenuAPIRow struct {
	MenuID int64 `json:"menu_id"`
	APIID  int64 `json:"api_id"`
}

type baselineAPIRow struct {
	ID     int64  `json:"id"`
	Path   string `json:"path"`
	Method string `json:"method"`
}

func TestOpenAPIClientMenuBaselineContract(t *testing.T) {
	menus := readBaselineJSONL[baselineMenuRow](t, "sys_menu.jsonl")
	var page baselineMenuRow
	buttons := make(map[string]baselineMenuRow, len(openAPIClientMenuButtons))
	for _, row := range menus {
		if row.Path == openAPIClientMenuPath {
			require.Zero(t, page.ID, "OpenAPI 客户端页面只能有一个活动基线入口")
			page = row
		}
		if row.Permission != "" {
			buttons[row.Permission] = row
		}
	}
	require.NotZero(t, page.ID)
	require.Equal(t, int64(0), page.ParentID)
	require.Equal(t, openAPIClientMenuName, page.Name)
	require.Equal(t, openAPIClientMenuComponent, page.Component)
	require.Equal(t, "OpenAPI 客户端", page.Title)
	require.Zero(t, page.KeepAlive)
	require.Equal(t, 2, page.Type)

	buttonIDs := make(map[int64]struct{}, len(openAPIClientMenuButtons))
	for _, expected := range openAPIClientMenuButtons {
		row, ok := buttons[expected.permission]
		require.True(t, ok, expected.permission)
		require.Equal(t, page.ID, row.ParentID, expected.permission)
		require.Equal(t, expected.name, row.Name, expected.permission)
		require.Equal(t, expected.title, row.Title, expected.permission)
		require.Equal(t, 3, row.Type, expected.permission)
		buttonIDs[row.ID] = struct{}{}
	}

	roleMenus := readBaselineJSONL[baselineRoleMenuRow](t, "sys_role_menu.jsonl")
	granted := make(map[int64]map[int64]struct{})
	for _, row := range roleMenus {
		if granted[row.RoleID] == nil {
			granted[row.RoleID] = make(map[int64]struct{})
		}
		granted[row.RoleID][row.MenuID] = struct{}{}
	}
	for menuID := range buttonIDs {
		_, ok := granted[1][menuID]
		require.True(t, ok, "系统管理员缺少 OpenAPI 按钮 %d", menuID)
	}
	_, ok := granted[1][page.ID]
	require.True(t, ok, "系统管理员缺少 OpenAPI 页面")
	for _, roleID := range []int64{2, 3} {
		_, ok := granted[roleID][page.ID]
		require.False(t, ok, "角色 %d 不应默认获得 OpenAPI 页面", roleID)
	}
}

func TestOpenAPIClientMenuBaselineBindsEveryManagementAPI(t *testing.T) {
	apis := readBaselineJSONL[baselineAPIRow](t, "sys_api.jsonl")
	openAPIIDs := make(map[int64]struct{})
	for _, row := range apis {
		if strings.HasPrefix(row.Path, "/api/gb28181/openapi-clients") {
			require.NotEmpty(t, row.Method)
			openAPIIDs[row.ID] = struct{}{}
		}
	}
	require.Len(t, openAPIIDs, 12)

	menus := readBaselineJSONL[baselineMenuRow](t, "sys_menu.jsonl")
	buttonIDs := make(map[int64]struct{}, len(openAPIClientMenuButtons))
	for _, row := range menus {
		for _, expected := range openAPIClientMenuButtons {
			if row.Permission == expected.permission {
				buttonIDs[row.ID] = struct{}{}
			}
		}
	}
	links := readBaselineJSONL[baselineMenuAPIRow](t, "sys_menu_api.jsonl")
	linked := make(map[int64]struct{}, len(openAPIIDs))
	for _, row := range links {
		if _, isButton := buttonIDs[row.MenuID]; isButton {
			linked[row.APIID] = struct{}{}
		}
	}
	for apiID := range openAPIIDs {
		_, ok := linked[apiID]
		require.True(t, ok, "OpenAPI 管理 API %d 没有按钮权限绑定", apiID)
	}
}

func readBaselineJSONL[T any](t *testing.T, name string) []T {
	t.Helper()
	path := filepath.Join("../../../resource/database/baseline/seeds", name)
	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	rows := make([]T, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row T
		require.NoError(t, json.Unmarshal([]byte(line), &row), line)
		rows = append(rows, row)
	}
	require.NoError(t, scanner.Err())
	return rows
}
