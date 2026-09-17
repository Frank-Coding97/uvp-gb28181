-- 日志中心：把散落的日志菜单收进新一级目录 /log-center（PostgreSQL，幂等）
--
-- 背景：后端 GetRouters 取「角色授权的 menu_id 扁平集合」再 BuildTree；父目录不在授权集合里
--   → 子树整体变孤儿 → 从侧栏静默消失。因此新增目录必须同步补角色授权。
-- 一律按 path 定位：menu_id 由各环境自增分配，跨环境会漂移。
-- 收录 5 项：SIP 日志 / 实时日志控制台 / 登录日志 / 操作日志 / 定时任务日志

INSERT INTO sys_menu (parent_id,title,path,name,sort,icon,type,hide,disable,keep_alive,created_at,updated_at)
SELECT 0,'日志中心','/log-center','LogCenter',145,'lucide:FileClock',1,false,false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/log-center' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/log-center' AND deleted_at IS NULL), sort=10, updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/sip-traces' AND deleted_at IS NULL AND type IN (1,2) AND (sort IS NULL OR sort<>10);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/log-center' AND deleted_at IS NULL), sort=20, updated_at=CURRENT_TIMESTAMP
WHERE path='/system/realtime-log' AND deleted_at IS NULL AND type IN (1,2) AND (sort IS NULL OR sort<>20);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/log-center' AND deleted_at IS NULL), sort=30, updated_at=CURRENT_TIMESTAMP
WHERE path='/system/login-log' AND deleted_at IS NULL AND type IN (1,2) AND (sort IS NULL OR sort<>30);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/log-center' AND deleted_at IS NULL), sort=40, updated_at=CURRENT_TIMESTAMP
WHERE path='/system/log' AND deleted_at IS NULL AND type IN (1,2) AND (sort IS NULL OR sort<>40);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/log-center' AND deleted_at IS NULL), sort=50, updated_at=CURRENT_TIMESTAMP
WHERE path='/system/joblog' AND deleted_at IS NULL AND type IN (1,2) AND (sort IS NULL OR sort<>50);

UPDATE sys_menu SET title='操作日志', updated_at=CURRENT_TIMESTAMP
WHERE path='/system/log' AND deleted_at IS NULL AND title<>'操作日志';

UPDATE sys_menu SET title='定时任务日志', updated_at=CURRENT_TIMESTAMP
WHERE path='/system/joblog' AND deleted_at IS NULL AND title<>'定时任务日志';

INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id, lc.id
FROM sys_role_menu rm
JOIN sys_menu m  ON m.id=rm.menu_id
JOIN sys_menu lc ON lc.path='/log-center' AND lc.deleted_at IS NULL
WHERE m.deleted_at IS NULL
  AND m.path IN ('/gb28181/sip-traces','/system/realtime-log','/system/login-log','/system/log','/system/joblog')
ON CONFLICT DO NOTHING;
