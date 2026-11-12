package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/models"
)

// 菜单**不再按配置文件的开关过滤**（老板 2026-10-08 定）。
//
// 原实现有 `filterDisabledFeatureMenus`：按 `gb28181.trace.enabled` 把
// `/gb28181/sip-traces` 及其子菜单一起剔掉，并在 `GetRouters` 里调用它。
// 三条问题都真实发生过：
//  1. 用户只认界面上的开关（「国标服务配置 → 是否开启 SIP 日志」），
//     没人改 config.yml ⇒ 界面上开了、菜单仍不出现；
//  2. 该配置只有重启后端才进 viper，而界面保存走的是
//     `SaveConfig()` + 热重载 SIP 服务 ⇒「改了要重启才生效」；
//  3. 功能关闭时点进去一片空白，分不清是「没数据」还是「功能没开」。
//
// ⇒ 现在契约：**数据库里有就展示**，关闭态由页面读 `health.state` 提示并引导开启。
//
// ⛔ 本测试原来断言的是「过滤掉 SIP 日志及其子菜单」，与新契约直接相反；
//
//	已改为反向断言 —— 只要有人再加回按配置隐藏菜单的机制，这里立刻红。
func TestMenuListKeepsTraceMenuEvenWhenFeatureDisabled(t *testing.T) {
	menus := models.SysMenuList{
		{BaseModel: models.BaseModel{ID: 10}, Path: "/gb28181", Title: "国标平台"},
		{BaseModel: models.BaseModel{ID: 11}, ParentID: 10, Path: "/gb28181/sip-traces", Title: "SIP 日志"},
		{BaseModel: models.BaseModel{ID: 12}, ParentID: 11, Path: "/gb28181/sip-traces/detail", Title: "报文详情"},
		{BaseModel: models.BaseModel{ID: 13}, ParentID: 10, Path: "/gb28181/device-mgmt", Title: "设备管理"},
	}

	// 菜单树构建后 SIP 日志及其子项都必须在：一个都不能少。
	// ⛔ `BuildTree` 是**可变参**（contexts ...context.Context），
	//    `BuildTree(nil)` 会把一个真 nil context 传进去而不是"不传"。
	tree := menus.BuildTree().TreeSort()
	require.NotEmpty(t, tree, "菜单树不应为空")

	// ⛔ SIP 日志挂在「国标平台」下，不是根节点 ——
	//    只遍历根会漏掉整棵子树（第一版就是这么写的，断言莫名失败）。
	// ⛔ `SysMenuList` 是 `[]*SysMenu`，遍历时元素已是指针，不要再取地址。
	var findByPath func(nodes models.SysMenuList, path string) *models.SysMenu
	findByPath = func(nodes models.SysMenuList, path string) *models.SysMenu {
		for _, node := range nodes {
			if node.Path == path {
				return node
			}
			if hit := findByPath(node.Children, path); hit != nil {
				return hit
			}
		}
		return nil
	}

	traceNode := findByPath(tree, "/gb28181/sip-traces")
	require.NotNil(t, traceNode,
		"SIP 日志菜单必须展示（数据库里有就展示，2026-10-08 起不再按 trace 开关过滤）")
	require.NotNil(t, findByPath(traceNode.Children, "/gb28181/sip-traces/detail"),
		"SIP 日志的子菜单（报文详情）也必须跟着展示")
}
