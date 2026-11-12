-- SQL Server 权限迁移；与主迁移保持相同的收敛目标。
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path LIKE '/api/gb28181/sip/service-config/%');
DELETE FROM sys_casbin_rule WHERE v1 LIKE '/api/gb28181/sip/service-config/%';
DELETE FROM sys_api WHERE path LIKE '/api/gb28181/sip/service-config/%';
IF NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config' AND method='GET') INSERT INTO sys_api (id,title,path,method,api_group,created_at,updated_at,deleted_at,created_by) VALUES (639,N'读取国标服务聚合配置','/api/gb28181/sip/service-config','GET',N'国标服务配置',GETDATE(),GETDATE(),NULL,1);
IF NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config' AND method='PUT') INSERT INTO sys_api (id,title,path,method,api_group,created_at,updated_at,deleted_at,created_by) VALUES (640,N'修改国标服务聚合配置','/api/gb28181/sip/service-config','PUT',N'国标服务配置',GETDATE(),GETDATE(),NULL,1);
INSERT INTO sys_menu_api (menu_id,api_id) SELECT 140369,id FROM sys_api WHERE path='/api/gb28181/sip/service-config' AND method IN ('GET','PUT') AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=140369 AND ma.api_id=sys_api.id);
IF NOT EXISTS (SELECT 1 FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' AND v1='/api/gb28181/sip/service-config' AND v2='GET') INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5) VALUES ('p','role_1','/api/gb28181/sip/service-config','GET','*','','');
IF NOT EXISTS (SELECT 1 FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' AND v1='/api/gb28181/sip/service-config' AND v2='PUT') INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5) VALUES ('p','role_1','/api/gb28181/sip/service-config','PUT','*','','');
