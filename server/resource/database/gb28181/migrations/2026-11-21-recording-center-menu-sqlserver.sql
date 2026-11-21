-- 录像中心：把「设备录像 / 云端录像 / 录像缓存 / 录像计划」收进一个目录。
--
-- 三条既有子菜单**只改 parent_id 与 sort**，路径和组件一律不动：
--   改路径会连带影响客户收藏的书签、已经发出去的分享链接，以及一批按路径断言的测试；
--   而侧边栏组装只看 parent_id，不看路径前缀（/media 目录下挂的就是 /gb28181/zlm/*）。
--
-- ⛔⛔ 目录本身也必须授权。sys_role_menu 里挂的是**子菜单 id**，父目录不在里面时
--      侧边栏找不到父节点，子菜单会集体"消失"。第 5 段专门给「已拥有任一录像子菜单」
--      的角色补这一行 —— 漏了就是客户现场升级后录像菜单整片不见，而管理员账号
--      往往因为全选看不出来。
--
-- 设备录像沿用旧的隐藏路由（/gb28181/device-record-playback/:channelId）的组件：
-- 设备列表点「录像」仍然按 name 跳转，落到同一个新页面，旧链接也不会 404。

-- 1) 录像中心目录（顶层，占据原云端录像的位置）
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,type,permission,hide,disable,icon,sort,created_at,updated_at,created_by)
SELECT 0,'/recording-center','RecordingCenter','/recording-center/device','',N'录像中心',1,'',0,0,'lucide:Video',40,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (SELECT 1 AS seed) v
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/recording-center' AND deleted_at IS NULL);

-- 2) 设备录像菜单（挂在目录下；路径带可选通道号，深链和独立入口共用同一个页面）
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,type,permission,hide,disable,icon,sort,created_at,updated_at,created_by)
SELECT p.id,'/recording-center/device/:channelId?','recording-center-device','','recording-center/device/index',N'设备录像',2,'',0,0,'lucide:History',10,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu p
WHERE p.path='/recording-center' AND p.type=1 AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/recording-center/device/:channelId?' AND deleted_at IS NULL);

-- 3) 既有三个页面挂到目录下，并按「看录像 → 归档 → 配置」重排
UPDATE sys_menu
SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/recording-center' AND type=1 AND deleted_at IS NULL) recording_center),
    sort=CASE path
           WHEN '/gb28181/cloud-recordings' THEN 20
           WHEN '/gb28181/record-cache' THEN 30
           ELSE 40
         END
WHERE path IN ('/gb28181/cloud-recordings','/gb28181/record-cache','/gb28181/recording-schedules')
  AND deleted_at IS NULL
  AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/recording-center' AND type=1 AND deleted_at IS NULL) recording_center) IS NOT NULL;

-- 4) 旧隐藏路由复用新页面：设备列表的「录像」入口与老书签都落到同一份实现
UPDATE sys_menu SET component='recording-center/device/index'
WHERE path='/gb28181/device-record-playback/:channelId' AND deleted_at IS NULL;

-- 5a) 给「已经拥有任一录像子菜单」的角色补父目录授权（老库升级不丢菜单）
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,p.id
FROM sys_role_menu rm
JOIN sys_menu c ON c.id=rm.menu_id AND c.deleted_at IS NULL
JOIN sys_menu p ON p.path='/recording-center' AND p.type=1 AND p.deleted_at IS NULL
WHERE c.parent_id=p.id
AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=rm.role_id AND x.menu_id=p.id);

-- 5b) 新菜单默认只给启用的系统管理员（其它角色按需在角色管理里勾选）
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT r.id,m.id FROM sys_role r CROSS JOIN sys_menu m
WHERE r.id=1 AND r.status=1 AND r.deleted_at IS NULL AND m.deleted_at IS NULL
AND m.path IN ('/recording-center','/recording-center/device/:channelId?')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu rm WHERE rm.role_id=r.id AND rm.menu_id=m.id);
