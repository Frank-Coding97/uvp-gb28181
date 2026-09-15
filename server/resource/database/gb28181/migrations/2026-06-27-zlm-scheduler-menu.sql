-- 2026-06-27 ZLM 调度算法 + 调度日志 菜单 seed(M3 T3.3)
-- 跟 2026-06-26-zlm-menu-seed.sql 同模式,真机回归时手工跑。
--
-- 用法:
--   1) SELECT id, title FROM sys_menu WHERE title LIKE '%国标%' OR title LIKE '%流媒体%';
--   2) @GB_PARENT_ID 填实际 id(通常跟 ZLM 节点同父)
--   3) 跑 INSERT

SET @GB_PARENT_ID = NULL; -- 老板手填

-- 调度算法
INSERT INTO `sys_menu` (`parent_id`, `name`, `path`, `component`, `title`, `icon`, `sort`, `status`, `created_at`, `updated_at`)
SELECT @GB_PARENT_ID, 'zlm-scheduler-strategy', '/gb28181/zlm/scheduler', '/gb28181/zlm/SchedulerStrategy', '调度算法', 'icon-sync', 20, 1, NOW(), NOW()
WHERE @GB_PARENT_ID IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path` = '/gb28181/zlm/scheduler');

-- 调度日志
INSERT INTO `sys_menu` (`parent_id`, `name`, `path`, `component`, `title`, `icon`, `sort`, `status`, `created_at`, `updated_at`)
SELECT @GB_PARENT_ID, 'zlm-scheduler-log', '/gb28181/zlm/scheduler/logs', '/gb28181/zlm/SchedulerLog', '调度日志', 'icon-file', 21, 1, NOW(), NOW()
WHERE @GB_PARENT_ID IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path` = '/gb28181/zlm/scheduler/logs');
