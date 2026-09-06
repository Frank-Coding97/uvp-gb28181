-- openapi-client-menu:begin
-- T14 dynamic menu entry. This migration owns one page row and only reparents
-- the six pre-existing OpenAPI management buttons; it never creates APIs.
-- The temporary duplicate-key guard makes a foreign page/button collision fail
-- before any persistent row is changed.

CREATE TEMP TABLE IF NOT EXISTS __openapi_client_menu_guard (
  id SMALLINT PRIMARY KEY
);
INSERT INTO __openapi_client_menu_guard (id)
SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM __openapi_client_menu_guard WHERE id=1);
INSERT INTO __openapi_client_menu_guard (id)
SELECT 1
WHERE
  EXISTS (
    SELECT 1 FROM sys_menu
    WHERE deleted_at IS NULL AND path='/gb28181/openapi-client'
      AND (parent_id<>0 OR COALESCE(name,'')<>'gb28181-openapi-client'
        OR COALESCE(component,'')<>'gb28181/openapi-client/index'
        OR COALESCE(title,'')<>'OpenAPI 客户端' OR COALESCE(redirect,'')<>''
        OR COALESCE(is_full,0)<>0 OR COALESCE(hide,0)<>0 OR COALESCE(disable,0)<>0
        OR COALESCE(keep_alive,0)<>0 OR COALESCE(affix,0)<>0 OR COALESCE(link,'')<>''
        OR COALESCE(iframe,0)<>0 OR COALESCE(svg_icon,'')<>'' OR COALESCE(icon,'')<>'lucide:KeyRound'
        OR COALESCE(sort,0)<>15 OR COALESCE(type,0)<>2 OR COALESCE(is_link,0)<>0 OR COALESCE(permission,'')<>'')
  )
  OR EXISTS (
    SELECT 1 FROM sys_menu
    WHERE deleted_at IS NULL AND name='gb28181-openapi-client'
      AND path<>'/gb28181/openapi-client'
  )
  OR (SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL AND path='/gb28181/openapi-client')>1
  OR (SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:openapi:client:read')<>1
  OR (SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:openapi:client:create')<>1
  OR (SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:openapi:client:grant')<>1
  OR (SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:openapi:client:rotate')<>1
  OR (SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:openapi:client:status')<>1
  OR (SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:openapi:client:audit')<>1
  OR EXISTS (SELECT 1 FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:openapi:client:read' AND (COALESCE(name,'')<>'Permission_gb28181_openapi_client_read' OR COALESCE(path,'')<>'' OR COALESCE(redirect,'')<>'' OR COALESCE(component,'')<>'' OR COALESCE(title,'')<>'查看 OpenAPI 客户端' OR COALESCE(is_full,0)<>0 OR COALESCE(hide,0)<>1 OR COALESCE(disable,0)<>0 OR COALESCE(keep_alive,0)<>0 OR COALESCE(affix,0)<>0 OR COALESCE(link,'')<>'' OR COALESCE(iframe,0)<>0 OR COALESCE(svg_icon,'')<>'' OR COALESCE(icon,'')<>'' OR COALESCE(sort,0)<>100 OR COALESCE(type,0)<>3 OR COALESCE(is_link,0)<>0))
  OR EXISTS (SELECT 1 FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:openapi:client:create' AND (COALESCE(name,'')<>'Permission_gb28181_openapi_client_create' OR COALESCE(path,'')<>'' OR COALESCE(redirect,'')<>'' OR COALESCE(component,'')<>'' OR COALESCE(title,'')<>'创建 OpenAPI 客户端' OR COALESCE(is_full,0)<>0 OR COALESCE(hide,0)<>1 OR COALESCE(disable,0)<>0 OR COALESCE(keep_alive,0)<>0 OR COALESCE(affix,0)<>0 OR COALESCE(link,'')<>'' OR COALESCE(iframe,0)<>0 OR COALESCE(svg_icon,'')<>'' OR COALESCE(icon,'')<>'' OR COALESCE(sort,0)<>100 OR COALESCE(type,0)<>3 OR COALESCE(is_link,0)<>0))
  OR EXISTS (SELECT 1 FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:openapi:client:grant' AND (COALESCE(name,'')<>'Permission_gb28181_openapi_client_grant' OR COALESCE(path,'')<>'' OR COALESCE(redirect,'')<>'' OR COALESCE(component,'')<>'' OR COALESCE(title,'')<>'分配 OpenAPI 客户端能力' OR COALESCE(is_full,0)<>0 OR COALESCE(hide,0)<>1 OR COALESCE(disable,0)<>0 OR COALESCE(keep_alive,0)<>0 OR COALESCE(affix,0)<>0 OR COALESCE(link,'')<>'' OR COALESCE(iframe,0)<>0 OR COALESCE(svg_icon,'')<>'' OR COALESCE(icon,'')<>'' OR COALESCE(sort,0)<>100 OR COALESCE(type,0)<>3 OR COALESCE(is_link,0)<>0))
  OR EXISTS (SELECT 1 FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:openapi:client:rotate' AND (COALESCE(name,'')<>'Permission_gb28181_openapi_client_rotate' OR COALESCE(path,'')<>'' OR COALESCE(redirect,'')<>'' OR COALESCE(component,'')<>'' OR COALESCE(title,'')<>'轮换 OpenAPI 客户端密钥' OR COALESCE(is_full,0)<>0 OR COALESCE(hide,0)<>1 OR COALESCE(disable,0)<>0 OR COALESCE(keep_alive,0)<>0 OR COALESCE(affix,0)<>0 OR COALESCE(link,'')<>'' OR COALESCE(iframe,0)<>0 OR COALESCE(svg_icon,'')<>'' OR COALESCE(icon,'')<>'' OR COALESCE(sort,0)<>100 OR COALESCE(type,0)<>3 OR COALESCE(is_link,0)<>0))
  OR EXISTS (SELECT 1 FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:openapi:client:status' AND (COALESCE(name,'')<>'Permission_gb28181_openapi_client_status' OR COALESCE(path,'')<>'' OR COALESCE(redirect,'')<>'' OR COALESCE(component,'')<>'' OR COALESCE(title,'')<>'启停或撤销 OpenAPI 客户端' OR COALESCE(is_full,0)<>0 OR COALESCE(hide,0)<>1 OR COALESCE(disable,0)<>0 OR COALESCE(keep_alive,0)<>0 OR COALESCE(affix,0)<>0 OR COALESCE(link,'')<>'' OR COALESCE(iframe,0)<>0 OR COALESCE(svg_icon,'')<>'' OR COALESCE(icon,'')<>'' OR COALESCE(sort,0)<>100 OR COALESCE(type,0)<>3 OR COALESCE(is_link,0)<>0))
  OR EXISTS (SELECT 1 FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:openapi:client:audit' AND (COALESCE(name,'')<>'Permission_gb28181_openapi_client_audit' OR COALESCE(path,'')<>'' OR COALESCE(redirect,'')<>'' OR COALESCE(component,'')<>'' OR COALESCE(title,'')<>'查看 OpenAPI 客户端审计' OR COALESCE(is_full,0)<>0 OR COALESCE(hide,0)<>1 OR COALESCE(disable,0)<>0 OR COALESCE(keep_alive,0)<>0 OR COALESCE(affix,0)<>0 OR COALESCE(link,'')<>'' OR COALESCE(iframe,0)<>0 OR COALESCE(svg_icon,'')<>'' OR COALESCE(icon,'')<>'' OR COALESCE(sort,0)<>100 OR COALESCE(type,0)<>3 OR COALESCE(is_link,0)<>0))
  OR EXISTS (
    SELECT 1 FROM sys_menu b
    WHERE b.deleted_at IS NULL
      AND b.permission IN ('gb28181:openapi:client:read','gb28181:openapi:client:create','gb28181:openapi:client:grant','gb28181:openapi:client:rotate','gb28181:openapi:client:status','gb28181:openapi:client:audit')
      AND b.parent_id<>0
      AND (NOT EXISTS (SELECT 1 FROM sys_menu p WHERE p.deleted_at IS NULL AND p.path='/gb28181/openapi-client' AND p.name='gb28181-openapi-client' AND p.component='gb28181/openapi-client/index' AND p.parent_id=0 AND p.type=2)
        OR b.parent_id<>(SELECT MIN(p.id) FROM sys_menu p WHERE p.deleted_at IS NULL AND p.path='/gb28181/openapi-client' AND p.name='gb28181-openapi-client' AND p.component='gb28181/openapi-client/index' AND p.parent_id=0 AND p.type=2))
  );
DROP TABLE IF EXISTS __openapi_client_menu_guard;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,is_full,hide,disable,keep_alive,affix,link,iframe,svg_icon,icon,sort,type,is_link,permission,created_at,updated_at,created_by)
SELECT 0,'/gb28181/openapi-client','gb28181-openapi-client','','gb28181/openapi-client/index','OpenAPI 客户端',0,0,0,0,0,'',0,'','lucide:KeyRound',15,2,0,'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/openapi-client' AND deleted_at IS NULL);

UPDATE sys_menu b
SET parent_id=(SELECT id FROM (SELECT MIN(p.id) AS id FROM sys_menu p WHERE p.path='/gb28181/openapi-client' AND p.name='gb28181-openapi-client' AND p.component='gb28181/openapi-client/index' AND p.deleted_at IS NULL) AS page)
WHERE b.permission IN ('gb28181:openapi:client:read','gb28181:openapi:client:create','gb28181:openapi:client:grant','gb28181:openapi:client:rotate','gb28181:openapi:client:status','gb28181:openapi:client:audit')
  AND b.deleted_at IS NULL AND b.parent_id=0;

INSERT INTO sys_role_menu (role_id,menu_id)
SELECT r.id,m.id
FROM sys_role r CROSS JOIN sys_menu m
WHERE r.id=1 AND r.status=1 AND r.deleted_at IS NULL
  AND m.path='/gb28181/openapi-client' AND m.name='gb28181-openapi-client' AND m.component='gb28181/openapi-client/index' AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=r.id AND x.menu_id=m.id);

-- openapi-client-menu:end
