-- 与主迁移相同的 ANSI 语句；SQLite 没有 BINARY 前缀操作符，按默认的二进制比较即可。
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (SELECT '查询地图标记点' title,'/api/gb28181/device-mgmt/map/markers' path,'GET' method
      UNION ALL SELECT '查询地图聚合点','/api/gb28181/device-mgmt/map/clusters','GET'
      UNION ALL SELECT '查询无坐标设备数量','/api/gb28181/device-mgmt/map/no-coord-count','GET'
      UNION ALL SELECT '查询可定位通道树','/api/gb28181/device-mgmt/map/channel-tree','GET'
      UNION ALL SELECT '查询通道移动轨迹','/api/gb28181/device-mgmt/map/trajectory','GET'
      UNION ALL SELECT '读取地图默认中心点','/api/gb28181/device-mgmt/map/center','GET'
      UNION ALL SELECT '保存地图默认中心点','/api/gb28181/device-mgmt/map/center','PUT') v
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id
FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:device:view'
  AND a.path IN ('/api/gb28181/device-mgmt/map/markers','/api/gb28181/device-mgmt/map/clusters','/api/gb28181/device-mgmt/map/no-coord-count','/api/gb28181/device-mgmt/map/channel-tree','/api/gb28181/device-mgmt/map/trajectory','/api/gb28181/device-mgmt/map/center')
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','',''
FROM sys_api a
WHERE a.path IN ('/api/gb28181/device-mgmt/map/markers','/api/gb28181/device-mgmt/map/clusters','/api/gb28181/device-mgmt/map/no-coord-count','/api/gb28181/device-mgmt/map/channel-tree','/api/gb28181/device-mgmt/map/trajectory','/api/gb28181/device-mgmt/map/center')
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE r.ptype='p' AND r.v0='role_1' AND r.v1=a.path AND r.v2=a.method);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_3',a.path,a.method,'*','',''
FROM sys_api a
WHERE a.path IN ('/api/gb28181/device-mgmt/map/markers','/api/gb28181/device-mgmt/map/clusters','/api/gb28181/device-mgmt/map/no-coord-count','/api/gb28181/device-mgmt/map/channel-tree','/api/gb28181/device-mgmt/map/trajectory','/api/gb28181/device-mgmt/map/center')
  AND a.method='GET'
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE r.ptype='p' AND r.v0='role_3' AND r.v1=a.path AND r.v2=a.method);
