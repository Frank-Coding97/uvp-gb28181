-- 回滚 joblog-clear-permission.sql
-- 顺序：先删 casbin 策略，再删挂载，最后删权限点与接口登记（避免留悬空引用）。
-- 只删本脚本造的行（id 687 / 140619 / 10359），不碰其它数据。

DELETE FROM sys_casbin_rule WHERE ptype = 'p' AND v0 = 'role_1' AND v1 = '/api/sysJobResults/clear' AND v2 = 'POST';
DELETE FROM sys_role_menu WHERE role_id = 1 AND menu_id = 140619;
DELETE FROM sys_menu_api WHERE menu_id = 140619 AND api_id = 687;
DELETE FROM sys_menu WHERE id = 140619;
DELETE FROM sys_api WHERE id = 687;
