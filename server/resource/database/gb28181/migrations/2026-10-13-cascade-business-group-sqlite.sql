CREATE TABLE IF NOT EXISTS "gb_cascade_business_node" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "platform_id" INTEGER NOT NULL,
  "code" TEXT NOT NULL CHECK (length("code") <= 20),
  "name" TEXT NOT NULL CHECK (length("name") <= 128),
  "kind" TEXT NOT NULL CHECK (length("kind") <= 16),
  "parent_code" TEXT NOT NULL DEFAULT '' CHECK (length("parent_code") <= 20),
  "sort" INTEGER NOT NULL DEFAULT 0,
  CONSTRAINT "uk_cascade_business_node_code" UNIQUE ("platform_id", "code")
);
CREATE TABLE IF NOT EXISTS "gb_cascade_business_binding" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "platform_id" INTEGER NOT NULL,
  "source_channel_id" INTEGER NOT NULL,
  "node_code" TEXT NOT NULL CHECK (length("node_code") <= 20),
  CONSTRAINT "uk_cascade_business_binding_channel" UNIQUE ("platform_id", "source_channel_id")
);
-- business-group endpoint registrations use the existing cascade view/share menu permissions.
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,deleted_at,created_by)
SELECT '查看级联业务分组','/api/gb28181/cascade/platforms/:id/business-group','GET','国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/business-group' AND method='GET');
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,deleted_at,created_by)
SELECT '保存级联业务分组','/api/gb28181/cascade/platforms/:id/business-group','PUT','国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/business-group' AND method='PUT');
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.path='/api/gb28181/cascade/platforms/:id/business-group' AND a.method='GET'
WHERE m.permission='gb28181:cascade:view'
AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.path='/api/gb28181/cascade/platforms/:id/business-group' AND a.method='PUT'
WHERE m.permission='gb28181:cascade:share'
AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1','/api/gb28181/cascade/platforms/:id/business-group','GET','*','',''
WHERE NOT EXISTS (SELECT 1 FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' AND v1='/api/gb28181/cascade/platforms/:id/business-group' AND v2='GET');
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1','/api/gb28181/cascade/platforms/:id/business-group','PUT','*','',''
WHERE NOT EXISTS (SELECT 1 FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' AND v1='/api/gb28181/cascade/platforms/:id/business-group' AND v2='PUT');
