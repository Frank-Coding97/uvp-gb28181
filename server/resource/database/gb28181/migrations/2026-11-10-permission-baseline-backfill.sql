-- 存量库权限基线补齐（幂等，只补不删）。
--
-- 背景：sys_api / sys_casbin_rule 的真源是 baseline/seeds/*.jsonl，全新安装导入
-- 全量脚本时权限齐全；但 2026-09~11 陆续新增的接口只进了 seeds、没有增量迁移，
-- 升级上来的老库一直缺登记与授权。管理员（role_1）访问设备列表时，
-- firmware-repository / map/center / map/channel-tree 等 GET 被 casbin 判 403
-- 「您没有权限访问此资源」，前端弹错，页面功能残缺。
-- 本迁移按 2026-11-10 线上库与 seeds 的逐行对账结果补齐；每条都带 NOT EXISTS，
-- 重复执行不会覆盖任何既有自定义授权。
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,v.api_group,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (
      SELECT '读取地图默认中心点' title,'/api/gb28181/device-mgmt/map/center' path,'GET' method,'设备管理' api_group
      UNION ALL SELECT '保存地图默认中心点' title,'/api/gb28181/device-mgmt/map/center' path,'PUT' method,'设备管理' api_group
      UNION ALL SELECT '查询可定位通道树' title,'/api/gb28181/device-mgmt/map/channel-tree' path,'GET' method,'设备管理' api_group
      UNION ALL SELECT '查询通道移动轨迹' title,'/api/gb28181/device-mgmt/map/trajectory' path,'GET' method,'设备管理' api_group
      UNION ALL SELECT '清空定时任务日志' title,'/api/sysJobResults/clear' path,'POST' method,'日志中心' api_group
) v
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE BINARY a.path=BINARY v.path AND BINARY a.method=BINARY v.method);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p',v.v0,v.v1,v.v2,'*','',''
FROM (
      SELECT 'role_1' v0,'/api/config/viewCache' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/cascade/platforms/:id/disable' v1,'POST' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/device-mgmt/device/:id/catalog/refresh/:operationId' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/device-mgmt/firmware-repository' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/device-mgmt/firmware-repository' v1,'POST' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/device-mgmt/firmware-repository/:id' v1,'DELETE' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/device-mgmt/firmware-repository/:id' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/device-mgmt/firmware-repository/:id/download-link' v1,'POST' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/device-mgmt/firmware-repository/:id/status' v1,'PATCH' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/device-mgmt/map/center' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/device-mgmt/map/center' v1,'PUT' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/device-mgmt/map/channel-tree' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/device-mgmt/map/trajectory' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/play/:deviceId/:channelId/authorization' v1,'POST' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/sip/service-config' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/sip/service-config' v1,'PUT' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/zlm/sessions/network' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/gb28181/zlm/sessions/viewers' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/sysJobResults/clear' v1,'POST' v2
      UNION ALL SELECT 'role_1' v0,'/api/sysMenu/:id' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/sysParam/:id' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/sysParam/add' v1,'POST' v2
      UNION ALL SELECT 'role_1' v0,'/api/sysParam/delete' v1,'DELETE' v2
      UNION ALL SELECT 'role_1' v0,'/api/sysParam/edit' v1,'PUT' v2
      UNION ALL SELECT 'role_1' v0,'/api/sysParam/getByCode/:code' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/sysParam/list' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/sysRole/:id' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/sysRole/list' v1,'GET' v2
      UNION ALL SELECT 'role_1' v0,'/api/users/switchTenant/:tenantld' v1,'GET' v2
      UNION ALL SELECT 'role_3' v0,'/api/gb28181/device-mgmt/map/center' v1,'GET' v2
      UNION ALL SELECT 'role_3' v0,'/api/gb28181/device-mgmt/map/channel-tree' v1,'GET' v2
      UNION ALL SELECT 'role_3' v0,'/api/gb28181/device-mgmt/map/trajectory' v1,'GET' v2
) v
WHERE NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE BINARY r.ptype=BINARY 'p' AND BINARY r.v0=BINARY v.v0 AND BINARY r.v1=BINARY v.v1 AND BINARY r.v2=BINARY v.v2);
