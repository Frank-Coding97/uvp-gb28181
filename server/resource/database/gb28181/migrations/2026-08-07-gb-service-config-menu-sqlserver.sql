-- 2026-08-07 国标服务配置菜单 seed (SQL Server)
-- 一级菜单：后续国标视频目录下的菜单将逐步迁移到同一层级。
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT v.title,v.path,v.method,N'GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (VALUES
 (N'读取移动位置历史轨迹配置','/api/gb28181/sip/service-config/position-history','GET'),
 (N'修改移动位置历史轨迹配置','/api/gb28181/sip/service-config/position-history','PUT')
) v(title,path,method)
WHERE NOT EXISTS (
    SELECT 1 FROM [sys_api] a
    WHERE a.[path]=v.path AND a.[method]=v.method AND a.[deleted_at] IS NULL
);

DECLARE @GB_MENU_ID BIGINT;

UPDATE sys_menu
SET parent_id = 0,
    name = 'gb28181-sip-service-config',
    component = 'gb28181/sip/ServiceConfig',
    title = N'国标服务配置',
    icon = 'lucide:ServerCog',
    sort = 3,
    hide = 0,
    disable = 0,
    type = 2,
    updated_at = CURRENT_TIMESTAMP
WHERE path = '/gb28181/sip/config' AND deleted_at IS NULL;

IF NOT EXISTS (
    SELECT 1 FROM sys_menu WHERE path = '/gb28181/sip/config' AND deleted_at IS NULL
)
BEGIN
    INSERT INTO sys_menu (parent_id,path,name,component,title,hide,disable,sort,type,icon,created_at,updated_at,created_by)
    VALUES (0,'/gb28181/sip/config','gb28181-sip-service-config','gb28181/sip/ServiceConfig',N'国标服务配置',0,0,3,2,'lucide:ServerCog',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
END;

SELECT TOP 1 @GB_MENU_ID = id
FROM sys_menu
WHERE path = '/gb28181/sip/config' AND deleted_at IS NULL
ORDER BY id;

IF @GB_MENU_ID IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM sys_role_menu WHERE role_id = 1 AND menu_id = @GB_MENU_ID
)
BEGIN
    INSERT INTO sys_role_menu (role_id,menu_id) VALUES (1,@GB_MENU_ID);
END;

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT @GB_MENU_ID,a.id
FROM sys_api a
WHERE @GB_MENU_ID IS NOT NULL
  AND ((a.path = '/api/gb28181/sip/setup/status' AND a.method = 'GET')
    OR (a.path = '/api/gb28181/sip/setup/network-interfaces' AND a.method = 'GET')
    OR (a.path = '/api/gb28181/sip/setup/config' AND a.method = 'PUT')
    OR (a.path = '/api/gb28181/sip/service-config/position-history' AND a.method IN ('GET','PUT')))
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id = @GB_MENU_ID AND x.api_id = a.id);

-- 复用既有 SIP 配置权限：gb28181:sip:config:view / gb28181:sip:config:update。

INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT 'p','role_1',a.[path],a.[method],'*','',''
FROM [sys_api] a
WHERE a.[path] = '/api/gb28181/sip/service-config/position-history'
  AND a.[method] IN ('GET','PUT')
  AND NOT EXISTS (
      SELECT 1 FROM [sys_casbin_rule] c
      WHERE c.[ptype]='p' AND c.[v0]='role_1' AND c.[v1]=a.[path]
        AND c.[v2]=a.[method] AND c.[v3]='*'
  );
