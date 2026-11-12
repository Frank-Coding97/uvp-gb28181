DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE BINARY path IN (BINARY '/api/system/upgrade/notice',BINARY '/api/system/upgrade/notice/ignore'));
DELETE FROM sys_casbin_rule WHERE BINARY v1 IN (BINARY '/api/system/upgrade/notice',BINARY '/api/system/upgrade/notice/ignore');
DELETE FROM sys_api WHERE BINARY path IN (BINARY '/api/system/upgrade/notice',BINARY '/api/system/upgrade/notice/ignore');
