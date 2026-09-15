-- device-maintenance:start
-- Separate viewing maintenance records from executing a whole-device reboot.

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看设备维护','/api/gb28181/device-mgmt/device/:id/maintenance-operations','GET','GB28181 设备维护',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceMaintenanceView','','查看设备维护',1,0,1,3,'gb28181:device:maintenance:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:maintenance:view' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND a.method='GET' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=CONCAT('role_',rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重启设备','/api/gb28181/device-mgmt/device/:id/reboot','POST','GB28181 设备维护',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/reboot' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceReboot','','重启设备',1,0,1,3,'gb28181:device:reboot','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:reboot' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:reboot' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:reboot' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/reboot' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:reboot' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/reboot' AND a.method='POST' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=CONCAT('role_',rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- device-maintenance:end
