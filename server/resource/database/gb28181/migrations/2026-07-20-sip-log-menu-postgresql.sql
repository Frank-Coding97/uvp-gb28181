-- 2026-07-20 独立 SIP 日志菜单 seed (PostgreSQL)
-- 用法:
-- 1) 先查父菜单 id:
--    SELECT id, title, path FROM sys_menu WHERE title LIKE '%国标%' OR path = '/gb28181';
-- 2) 用 psql :variable 或替换 :gb_parent_id 后执行。

-- \set gb_parent_id 0

UPDATE sys_menu
SET
    parent_id  = COALESCE(NULLIF(:gb_parent_id, 0), parent_id),
    component  = 'gb28181/sip/trace/TraceWorkbench',
    title      = 'SIP 日志',
    icon       = 'icon-list',
    sort       = 5,
    status     = 1,
    updated_at = NOW()
WHERE path = '/gb28181/sip-traces';

INSERT INTO sys_menu (
    parent_id, name, path, component, title, icon, sort, status, created_at, updated_at
)
SELECT
    :gb_parent_id,
    'gb28181-sip-traces',
    '/gb28181/sip-traces',
    'gb28181/sip/trace/TraceWorkbench',
    'SIP 日志',
    'icon-list',
    5,
    1,
    NOW(),
    NOW()
WHERE :gb_parent_id <> 0
  AND NOT EXISTS (
    SELECT 1 FROM sys_menu WHERE path = '/gb28181/sip-traces'
  );
