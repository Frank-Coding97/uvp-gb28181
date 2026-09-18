-- 2026-07-22 合并 SIP 日志菜单：用"SIP 日志新"替换"SIP 日志",删除重复菜单
-- 用法: 直接执行即可,安全幂等

-- 1. 更新旧的"SIP 日志"菜单,改用新版 component
UPDATE `sys_menu`
SET
  `component` = 'gb28181/sip-log-v2/index',
  `updated_at` = NOW()
WHERE `path` = '/gb28181/sip-traces'
  AND `deleted_at` IS NULL;

-- 2. 软删除"SIP 日志新"菜单(path: /gb28181/sip-traces-v2)
UPDATE `sys_menu`
SET
  `deleted_at` = NOW(),
  `updated_at` = NOW()
WHERE `path` = '/gb28181/sip-traces-v2'
  AND `deleted_at` IS NULL;

-- 3. 清理"SIP 日志新"菜单的角色关联(sys_role_menu)
DELETE FROM `sys_role_menu`
WHERE `menu_id` IN (
  SELECT `id` FROM `sys_menu`
  WHERE `path` = '/gb28181/sip-traces-v2'
);
