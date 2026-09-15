-- drop-catalog-anomaly-menu:down:start
-- Restore the "catalog anomaly" menu row and its role binding (PostgreSQL).
-- The frontend page was deleted together with the menu, so restoring the menu also
-- requires restoring web/src/views/gb28181/device-mgmt/anomaly/index.vue from git.
INSERT INTO sys_menu (id,parent_id,path,name,component,title,sort,type,hide,disable,keep_alive,icon,created_by,created_at,updated_at)
SELECT 140357,0,'/gb28181/device-mgmt/anomaly','device-mgmt-anomaly','gb28181/device-mgmt/anomaly/index','目录异常',6,2,1,0,0,'lucide:Cctv',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE id=140357);
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m WHERE m.path='/gb28181/device-mgmt/anomaly'
AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);
-- drop-catalog-anomaly-menu:down:end
