-- drop-dup-security-menu:down:start
-- Restore the duplicate "GB28181 access security" menu, its role binding and menu-API bindings (PostgreSQL).
-- The frontend page was deleted together with the menu, so restoring the menu also
-- requires restoring web/src/views/gb28181/security/index.vue from git.
INSERT INTO sys_menu (id,parent_id,path,name,component,title,sort,type,hide,disable,keep_alive,icon,created_by,created_at,updated_at)
SELECT 140372,0,'/gb28181/security','gb28181-security','gb28181/security/index','国标接入安全',5,2,0,1,0,'lucide:ShieldCheck',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE id=140372);
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m WHERE m.path='/gb28181/security'
AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.path LIKE '/api/gb28181/security/%' AND a.deleted_at IS NULL
WHERE m.path='/gb28181/security'
AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
-- drop-dup-security-menu:down:end
