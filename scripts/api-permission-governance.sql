-- API 权限数据治理（一次性数据修正）
-- 生成源: tmp/perm_governance/patch_seeds.py —— 幂等可重跑
--
-- 背景: 三方对账（Go 路由表 ↔ sys_api 登记 ↔ sys_menu_api 挂载）后的收敛：
--   A. 删 4 条幽灵登记（id 3,4,64,202）：其 path 在当前路由表中已不存在，连带清挂载引用
--   B. 增 23 条漏登记（id 597..619）：protected 路由存在但库中无记录
--   C. 增 34 条挂载：真缺口 14（前端在用·已登记·未挂载）+ 漏登记 20（可归属节点）
--   D. casbin 策略重物化：补齐 C 段新增挂载隐含的策略（仅新增，不删除既有）
--
-- 生效方式:
--   sys_casbin_rule 是 casbin 的策略真源，casbin.autoloadpolicyseconds=120
--   ⇒ 落库后 ≤2 分钟内核自动重读生效，无需重启。
--   ⚠️ 但 casbin 不会从 sys_menu_api 自动重算，故 D 段显式补齐——
--      等价于在「角色管理」里把受影响的角色逐个重新保存一次菜单授权。
-- 幂等性: DELETE 天然幂等；INSERT IGNORE 依赖主键/唯一键去重，重复执行不报错。
--   ⚠️ sys_casbin_rule.v4/v5 现有行均为空串（列默认是 NULL），必须显式写 ''，
--      否则唯一键 (ptype,v0,v1,v2,v3,v4,v5) 对不上，去重会失效。

-- A. 删除幽灵登记（先清挂载引用，避免悬空）
DELETE FROM sys_menu_api WHERE api_id IN (3,4,64,202);
DELETE FROM sys_api WHERE id IN (3,4,64,202);

-- B. 新增漏登记
INSERT IGNORE INTO sys_api (id, title, path, method, api_group, created_at, updated_at, deleted_at, created_by) VALUES
(597, '查询设备列表', '/api/gb28181/device/list', 'GET', '设备管理', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(598, '查询设备详情', '/api/gb28181/device/:deviceId', 'GET', '设备管理', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(599, '查询设备的通道列表', '/api/gb28181/device/:deviceId/channels', 'GET', '设备管理', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(600, '查询SIP会话统计', '/api/gb28181/sip-traces/sessions/stats', 'GET', '日志中心', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(601, '订阅SIP报文实时流', '/api/gb28181/sip-traces/stream', 'GET', '日志中心', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(602, '读取通道默认流传输方式', '/api/gb28181/sip/service-config/default-channel-stream-transport', 'GET', '国标服务配置', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(603, '读取SDP扩展开关', '/api/gb28181/sip/service-config/sdp-extension', 'GET', '国标服务配置', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(604, '获取API分组清单', '/api/sysApi/groups', 'GET', '接口管理', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(605, '手动推送目录到上级平台', '/api/gb28181/cascade/platforms/:id/push-catalog', 'POST', '国标级联', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(606, '控制通道雨刷', '/api/gb28181/device-mgmt/channel/:id/ptz/wiper', 'POST', '设备控制', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(607, '查看级联平台通道', '/api/gb28181/cascade/platforms/:id/channels', 'GET', '国标级联', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(608, '查询目录异常数量', '/api/gb28181/device-mgmt/catalog/anomaly/count', 'GET', '设备管理', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(609, '查询目录子树', '/api/gb28181/device-mgmt/catalog/tree/:id/subtree', 'GET', '设备管理', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(610, '查询设备告警', '/api/gb28181/device-mgmt/device/:id/alarms', 'GET', '告警管理', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(611, '查询无坐标设备数量', '/api/gb28181/device-mgmt/map/no-coord-count', 'GET', '设备管理', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(612, '订阅SIP概览实时流', '/api/gb28181/sip/dashboard/stream', 'GET', 'SIP 接入信息', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(613, '查询定时任务执行结果详情', '/api/sysJobResults/:id', 'GET', '定时任务', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(614, '参数分页列表', '/api/sysParam/list', 'GET', '系统配置', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(615, '根据ID获取参数', '/api/sysParam/:id', 'GET', '系统配置', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(616, '根据编码获取参数', '/api/sysParam/getByCode/:code', 'GET', '系统配置', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(617, '共享级联通道', '/api/gb28181/cascade/platforms/:id/channels/share', 'POST', '国标级联', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(618, '取消共享级联通道', '/api/gb28181/cascade/platforms/:id/channels/unshare', 'POST', '国标级联', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1),
(619, '语音对讲上行推流', '/api/gb28181/device-mgmt/talk-sessions/:sessionId/uplink', 'POST', '设备控制', '2026-09-23 18:00:00.000000', '2026-09-23 18:00:00.000000', NULL, 1);

-- C. 新增挂载
INSERT IGNORE INTO sys_menu_api (menu_id, api_id) VALUES
(1005, 28),
(1005, 42),
(140235, 46),
(140235, 47),
(140239, 59),
(140265, 106),
(140265, 187),
(140371, 277),
(140371, 278),
(140371, 279),
(140371, 281),
(140371, 283),
(140371, 284),
(140371, 292),
(140452, 597),
(140452, 598),
(140452, 599),
(140362, 600),
(140362, 601),
(140369, 602),
(140369, 603),
(140225, 604),
(140377, 605),
(140459, 606),
(140376, 607),
(140452, 608),
(140452, 609),
(140452, 611),
(140366, 610),
(140359, 612),
(140483, 613),
(140379, 617),
(140379, 618),
(140475, 619);

-- D. casbin 策略重物化（补新增挂载隐含的策略）
--    口径与 app 一致：策略集 = sys_role_menu ⋈ sys_menu_api ⋈ sys_api（sys_api 未软删）
INSERT IGNORE INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
SELECT * FROM (
    SELECT DISTINCT 'p' AS ptype, CONCAT('role_', rm.role_id) AS v0,
           a.path AS v1, a.method AS v2, '*' AS v3, '' AS v4, '' AS v5
    FROM sys_role_menu rm
    JOIN sys_menu_api ma ON ma.menu_id = rm.menu_id
    JOIN sys_api a ON a.id = ma.api_id
    WHERE a.deleted_at IS NULL
      AND NOT EXISTS (
          SELECT 1 FROM sys_casbin_rule c
          WHERE c.ptype = 'p'
            AND c.v0 = CONCAT('role_', rm.role_id) COLLATE utf8mb4_unicode_ci
            AND c.v1 = a.path  COLLATE utf8mb4_unicode_ci
            AND c.v2 = a.method COLLATE utf8mb4_unicode_ci
      )
) x;
