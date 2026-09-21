-- GB/T 28181-2022 A.2.3.1.14「目标跟踪控制命令」（MySQL 5.7+）。
--
-- 1) 落库表 gb_device_target_track：**平台最近一次下发的意图**，(device_id, target_code) 一行
--    （见 models.GbDeviceTargetTrack）。
--    ⛔ 它**不是**设备状态：目标跟踪是无应答命令（9.3.1 d) + 表 1 序号 13），
--       而且 2022 全文里没有任何"目标跟踪状态查询/上报"的命令 ⇒
--       「设备现在在跟踪什么」在协议上不可回答，只有"平台让它跟踪什么"可答。
-- 2) 接口权限：读 GET /channel/:id/target-track → gb28181:ptz:view
--              写 POST /channel/:id/target-track → gb28181:ptz:control
--    照 video-params 的先例（同一路径、按方法分档），不是新造独立权限码：
--    目标跟踪是**可逆**的普通控制（不像存储卡格式化会清数据），不该比云台控制更严。
--
-- ⛔ 路由全路径 `/api/gb28181/device-mgmt/channel/:id/target-track` 三处必须同名
--    （迁移 / 路由 / 测试）。路由侧从 `models.TargetTrackRoutePath` 派生，
--    手写错一个字符的表现是"接口通但恒 403"，且两侧都不报错。
--
-- 幂等：建表 IF NOT EXISTS；sys_api / sys_menu_api / sys_casbin_rule 三处全部
--       NOT EXISTS 守卫，连跑两遍无副作用。
--
-- ⛔⛔ 建表**必须显式写 COLLATE=utf8mb4_general_ci**，不能只写 `DEFAULT CHARSET=utf8mb4`。
--   MySQL 的规则是"给了字符集但没给排序规则 ⇒ 取**该字符集的默认排序规则**"，
--   而 8.0 里 utf8mb4 的默认是 `utf8mb4_0900_ai_ci`，**不是**本库的 `utf8mb4_general_ci`。
--   漏写的后果不在建表时暴露，而在**第一次 JOIN** 上：两侧排序规则不同 ⇒ MySQL 1267
--   "Illegal mix of collations"，日志里只有一行 db.query_failed。
--   （2026-09-20 在 gb_channel_snapshot 上实际踩到，见 docs/snapshot-flow-design.md。）
--
-- ⛔ sys_casbin_rule 段的 NOT EXISTS 里**不许写列列比较**（`p.v1=a.path` 这类）：
--   `sys_casbin_rule.v*` 是 utf8mb4_0900_ai_ci、`sys_api.path` 是 utf8mb4_unicode_ci，
--   MySQL 会报 1267 → runner 遇错中止且不 MarkApplied → 后端起不来（2026-09-19 实测）。
--   外层 WHERE 已把 path/method 钉成常量，所以这里直接写字面量。

-- target-track:start

CREATE TABLE IF NOT EXISTS `gb_device_target_track` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` bigint unsigned NOT NULL,
  `channel_id` bigint unsigned NOT NULL DEFAULT 0,
  `target_code` varchar(20) NOT NULL,
  `mode` varchar(16) NOT NULL,
  `device_id2` varchar(20) NOT NULL DEFAULT '',
  `area_length` int DEFAULT NULL,
  `area_width` int DEFAULT NULL,
  `area_mid_point_x` int DEFAULT NULL,
  `area_mid_point_y` int DEFAULT NULL,
  `area_length_x` int DEFAULT NULL,
  `area_length_y` int DEFAULT NULL,
  `source_operation_seq` bigint unsigned NOT NULL DEFAULT 0,
  `source_sn` int NOT NULL DEFAULT 0,
  `source_operation_id` varchar(64) DEFAULT NULL,
  `commanded_by` bigint unsigned NOT NULL DEFAULT 0,
  `commanded_by_dept_id` bigint unsigned NOT NULL DEFAULT 0,
  `commanded_at` datetime(3) NOT NULL,
  `raw_summary` text,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_target_track_target` (`device_id`,`target_code`),
  KEY `idx_target_track_device` (`device_id`,`commanded_at`),
  KEY `idx_target_track_channel` (`channel_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- target-track:end

-- target-track-permissions:start
-- 以下为纯 DML（权限三件事），不含方言特有的 DDL 语法 —— 行为测试会在 sqlite 上
-- 连跑两遍验幂等、再跑 down 验无残留，所以这一段的边界用注释标出来供测试切分。

-- ---- 读接口：GET /api/gb28181/device-mgmt/channel/:id/target-track ----
-- 读的是**平台已下发的意图**（不是设备状态），所以挂在"看云台资源"那个权限码上。
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '读取目标跟踪已下发指令','/api/gb28181/device-mgmt/channel/:id/target-track','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/target-track' AND method='GET' AND deleted_at IS NULL);

-- ---- 写接口：POST /api/gb28181/device-mgmt/channel/:id/target-track ----
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '下发目标跟踪','/api/gb28181/device-mgmt/channel/:id/target-track','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/target-track' AND method='POST' AND deleted_at IS NULL);

-- ⛔ JOIN 条件必须带 `type=3`：同 permission 的**目录行（type=1/2）不是按钮**，
--    只按 permission 关联会把一个导航壳也挂上一条接口权限。
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/target-track' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/target-track' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),'/api/gb28181/device-mgmt/channel/:id/target-track','GET','*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/target-track' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1='/api/gb28181/device-mgmt/channel/:id/target-track' AND p.v2='GET' AND p.v3='*');

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),'/api/gb28181/device-mgmt/channel/:id/target-track','POST','*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/target-track' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1='/api/gb28181/device-mgmt/channel/:id/target-track' AND p.v2='POST' AND p.v3='*');

-- target-track-permissions:end
