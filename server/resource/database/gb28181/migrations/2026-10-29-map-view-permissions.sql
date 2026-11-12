-- 地图视图 7 个接口的权限登记（幂等）。
--
-- 背景：这批接口是 2026-09-05（markers/clusters/no-coord-count）与 2026-10-21
--      （channel-tree/trajectory/center）随手加进**全量初始化种子** baseline/seeds 的，
--      但从来没写过增量迁移 —— 结果是：
--        · 全新安装（导入 baseline）：权限齐全，地图正常；
--        · 升级上来的老库（只跑 migrations）：一条都没有，管理员进地图视图也吃
--          403「您没有权限访问此资源」。
--      本迁移只做"补齐缺失行"，不覆盖任何既有的自定义授权：每一步都带 NOT EXISTS。
--
-- 归属：全部挂到 gb28181:device:view 菜单下（与 seed 里 sys_menu_api 的口径一致）。
-- 档位：GET 给 role_1（系统管理员）+ role_3（游客只读）；
--      PUT /map/center 是**写**动作（改全平台默认视野），只给 role_1。

INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (SELECT '查询地图标记点' title,'/api/gb28181/device-mgmt/map/markers' path,'GET' method
      UNION ALL SELECT '查询地图聚合点','/api/gb28181/device-mgmt/map/clusters','GET'
      UNION ALL SELECT '查询无坐标设备数量','/api/gb28181/device-mgmt/map/no-coord-count','GET'
      UNION ALL SELECT '查询可定位通道树','/api/gb28181/device-mgmt/map/channel-tree','GET'
      UNION ALL SELECT '查询通道移动轨迹','/api/gb28181/device-mgmt/map/trajectory','GET'
      UNION ALL SELECT '读取地图默认中心点','/api/gb28181/device-mgmt/map/center','GET'
      UNION ALL SELECT '保存地图默认中心点','/api/gb28181/device-mgmt/map/center','PUT') v
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE BINARY a.path=BINARY v.path AND BINARY a.method=BINARY v.method);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id
FROM sys_menu m CROSS JOIN sys_api a
WHERE BINARY m.permission=BINARY 'gb28181:device:view'
  AND BINARY a.path IN (BINARY '/api/gb28181/device-mgmt/map/markers',BINARY '/api/gb28181/device-mgmt/map/clusters',BINARY '/api/gb28181/device-mgmt/map/no-coord-count',BINARY '/api/gb28181/device-mgmt/map/channel-tree',BINARY '/api/gb28181/device-mgmt/map/trajectory',BINARY '/api/gb28181/device-mgmt/map/center')
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','',''
FROM sys_api a
WHERE BINARY a.path IN (BINARY '/api/gb28181/device-mgmt/map/markers',BINARY '/api/gb28181/device-mgmt/map/clusters',BINARY '/api/gb28181/device-mgmt/map/no-coord-count',BINARY '/api/gb28181/device-mgmt/map/channel-tree',BINARY '/api/gb28181/device-mgmt/map/trajectory',BINARY '/api/gb28181/device-mgmt/map/center')
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE BINARY r.ptype=BINARY 'p' AND BINARY r.v0=BINARY 'role_1' AND BINARY r.v1=BINARY a.path AND BINARY r.v2=BINARY a.method);

-- 游客只读：除 PUT /map/center 之外的 6 条。
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_3',a.path,a.method,'*','',''
FROM sys_api a
WHERE BINARY a.path IN (BINARY '/api/gb28181/device-mgmt/map/markers',BINARY '/api/gb28181/device-mgmt/map/clusters',BINARY '/api/gb28181/device-mgmt/map/no-coord-count',BINARY '/api/gb28181/device-mgmt/map/channel-tree',BINARY '/api/gb28181/device-mgmt/map/trajectory',BINARY '/api/gb28181/device-mgmt/map/center')
  AND BINARY a.method=BINARY 'GET'
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE BINARY r.ptype=BINARY 'p' AND BINARY r.v0=BINARY 'role_3' AND BINARY r.v1=BINARY a.path AND BINARY r.v2=BINARY a.method);
