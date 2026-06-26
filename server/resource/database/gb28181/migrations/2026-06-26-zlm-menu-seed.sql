-- 2026-06-26 ZLM 集群管理菜单 seed
-- 假设父菜单 "国标平台" id=? — 实际部署时手工查 sys_menu 表替换 parent_id
-- 这是给老板真机回归时手动跑的参考 SQL,不强制执行。
--
-- 用法:
--   1) 先查 SELECT id, title FROM sys_menu WHERE title LIKE '%国标%';
--   2) 替换下面的 @GB_PARENT_ID = 实际 id
--   3) 跑 INSERT
--
-- 菜单层次:
--   国标平台 (一级)
--     国标设备 (现有,已在表)
--     流媒体节点 (NEW)

SET @GB_PARENT_ID = NULL; -- 老板手填,如 100

-- 一级:流媒体节点入口(指向 NodeList)
INSERT INTO `sys_menu` (`parent_id`, `name`, `path`, `component`, `title`, `icon`, `sort`, `status`, `created_at`, `updated_at`)
SELECT @GB_PARENT_ID, 'zlm-nodes', '/gb28181/zlm/nodes', '/gb28181/zlm/NodeList', '流媒体节点', 'icon-cloud', 10, 1, NOW(), NOW()
WHERE @GB_PARENT_ID IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path` = '/gb28181/zlm/nodes');

-- 一级:节点详情(隐藏菜单,通过列表跳转)
INSERT INTO `sys_menu` (`parent_id`, `name`, `path`, `component`, `title`, `sort`, `status`, `hide`, `created_at`, `updated_at`)
SELECT @GB_PARENT_ID, 'zlm-node-detail', '/gb28181/zlm/nodes/:id', '/gb28181/zlm/NodeDetail', '节点详情', 11, 1, 1, NOW(), NOW()
WHERE @GB_PARENT_ID IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path` = '/gb28181/zlm/nodes/:id');
