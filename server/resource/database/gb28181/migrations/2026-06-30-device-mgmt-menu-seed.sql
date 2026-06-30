-- 2026-06-30 设备管理页菜单 seed
-- 假设父菜单 "国标平台" id=? — 跑前先查 sys_menu WHERE title LIKE '%国标%'
-- 替换 @GB_PARENT_ID = 实际 id(通常跟 zlm-menu-seed 同一个父)
--
-- 菜单层次:
--   国标平台 (一级)
--     国标设备 (现有 — 注册心跳页)
--     流媒体节点 (已有 — ZLM 集群)
--     设备管理 (NEW — 多级目录 + 三视图 + 详情抽屉)
--     目录异常 (NEW — 异常治理子页,隐藏菜单)

SET @GB_PARENT_ID = NULL; -- 老板手填

-- 设备管理主入口(列表/卡片/地图三视图)
INSERT INTO `sys_menu` (`parent_id`, `name`, `path`, `component`, `title`, `icon`, `sort`, `status`, `created_at`, `updated_at`)
SELECT @GB_PARENT_ID, 'device-mgmt', '/gb28181/device-mgmt', '/gb28181/device-mgmt/index', '设备管理', 'icon-camera', 5, 1, NOW(), NOW()
WHERE @GB_PARENT_ID IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path` = '/gb28181/device-mgmt');

-- 目录异常子页(隐藏菜单,通过左侧树底部 AnomalyEntry 跳转)
INSERT INTO `sys_menu` (`parent_id`, `name`, `path`, `component`, `title`, `sort`, `status`, `hide`, `created_at`, `updated_at`)
SELECT @GB_PARENT_ID, 'device-mgmt-anomaly', '/gb28181/device-mgmt/anomaly', '/gb28181/device-mgmt/anomaly/index', '目录异常', 6, 1, 1, NOW(), NOW()
WHERE @GB_PARENT_ID IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path` = '/gb28181/device-mgmt/anomaly');
