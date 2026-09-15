-- 2026-07-20 独立 SIP 日志菜单 seed (SQL Server)
-- 用法:
-- 1) 先查父菜单 id:
--    SELECT id, title, path FROM sys_menu WHERE title LIKE N'%国标%' OR path = '/gb28181';
-- 2) 用 sqlcmd :setvar GB_PARENT_ID 0 或者手工替换后执行。

-- :setvar GB_PARENT_ID 0

UPDATE sys_menu
SET
    parent_id  = ISNULL(NULLIF($(GB_PARENT_ID), 0), parent_id),
    component  = 'gb28181/sip/trace/TraceWorkbench',
    title      = N'SIP 日志',
    icon       = 'icon-list',
    sort       = 5,
    status     = 1,
    updated_at = GETDATE()
WHERE path = '/gb28181/sip-traces';

IF $(GB_PARENT_ID) <> 0 AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path = '/gb28181/sip-traces')
BEGIN
    INSERT INTO sys_menu (
        parent_id, name, path, component, title, icon, sort, status, created_at, updated_at
    ) VALUES (
        $(GB_PARENT_ID),
        'gb28181-sip-traces',
        '/gb28181/sip-traces',
        'gb28181/sip/trace/TraceWorkbench',
        N'SIP 日志',
        'icon-list',
        5,
        1,
        GETDATE(),
        GETDATE()
    );
END;
