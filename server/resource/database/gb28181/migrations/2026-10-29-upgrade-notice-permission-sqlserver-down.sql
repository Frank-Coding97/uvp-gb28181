DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path IN ('/api/system/upgrade/notice','/api/system/upgrade/notice/ignore'));
DELETE FROM sys_casbin_rule WHERE v1 IN ('/api/system/upgrade/notice','/api/system/upgrade/notice/ignore');
DELETE FROM sys_api WHERE path IN ('/api/system/upgrade/notice','/api/system/upgrade/notice/ignore');
