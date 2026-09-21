-- 抓拍图像库：设备/平台抓拍产出的**图片文件事实表**（MySQL 5.7+）。
--
-- 背景见 docs/snapshot-flow-design.md §2.4。三件事：
--   1) 建 gb_channel_snapshot：一张图一行，三套抓拍的产出汇到同一张表；
--   2) 读接口权限：GET /api/gb28181/device-mgmt/snapshots/:id/content
--      绑 gb28181:device:snapshot（与抓拍会话面板同一个权限码 —— 能看到"这次抓拍的图"
--      的人，就是该能看到"这张图"的人；换成更宽的 view 码会让只读账号也能看全部抓拍图）。
--   3) 与 gb_channel.snapshot_url 的分工：那一列是"最近一张"的指针（覆盖式），
--      本表是全部历史（逐张一行）。两者不是一回事，不要合并。
--
-- 幂等：建表用 IF NOT EXISTS；权限三件事（sys_api / sys_menu_api / sys_casbin_rule）
--       全部 NOT EXISTS 守卫。
--
-- ⛔⛔ 建表**必须显式写 COLLATE=utf8mb4_general_ci**，不能只写 `DEFAULT CHARSET=utf8mb4`。
--   MySQL 的规则是"给了字符集但没给排序规则 ⇒ 取**该字符集的默认排序规则**"，
--   而 MySQL 8.0 里 utf8mb4 的默认是 `utf8mb4_0900_ai_ci` —— **不是**这个库的默认
--   （`uvp_gb28181` 的默认是 `utf8mb4_general_ci`，仓库里 73 张表也都显式声明了它）。
--   漏写这一段的后果不在建表时暴露，而在**第一次 JOIN** 上：
--     `ch.channel_id = s.channel_code` 两侧排序规则不同 ⇒ MySQL 1267
--     "Illegal mix of collations" ⇒ 图像库列表接口恒 400，且日志里只有一行 db.query_failed，
--     看不出是"建表时少写了一个子句"。（2026-09-20 实际踩到，见技能 §25。）

CREATE TABLE IF NOT EXISTS `gb_channel_snapshot` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` bigint unsigned NOT NULL DEFAULT 0,
  `channel_id` bigint unsigned NOT NULL DEFAULT 0,
  `channel_code` varchar(20) NOT NULL,
  `session_id` varchar(64) DEFAULT NULL,
  `file_name` varchar(64) NOT NULL,
  `rel_path` varchar(255) NOT NULL,
  `size` bigint NOT NULL DEFAULT 0,
  `md5` varchar(32) DEFAULT NULL,
  `captured_at` datetime(3) NOT NULL,
  `source` varchar(16) NOT NULL,
  `created_by` bigint unsigned DEFAULT NULL,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  `deleted_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_channel_snapshot_file` (`channel_code`,`file_name`),
  KEY `idx_channel_snapshot_channel_time` (`channel_id`,`captured_at`),
  KEY `idx_channel_snapshot_session` (`session_id`),
  KEY `idx_channel_snapshot_captured` (`captured_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- channel-snapshot-library-permissions:start
-- 以下为纯 DML（权限三件事 + 图像库一级菜单），不含方言特有的 DDL 语法 —— 行为测试会在
-- sqlite 上连跑两遍验幂等、再跑 down 验无残留，所以这一段的边界用注释标出来供测试切分。
--
-- ⛔ 本段用的 sys_menu 列必须是行为测试那份 sqlite 建表里**已有的列**
--    （parent_id/path/name/component/title/hide/disable/sort/type/permission/icon/…），
--    用上 redirect/is_full 之类没建的列会让测试在 SQL 层就碎掉，而不是断言失败。
--    真实 sys_menu 里这些列都有 DEFAULT，省略不影响插入。

-- ---- 读接口：GET /api/gb28181/device-mgmt/snapshots/:id/content ----
-- ⛔ 这是**稳定**读接口（按库里的行 id 取图，凭证是 JWT）：与上传那条
--    `/device-snapshots/uploads/:token/:filename` 是两条路，后者的凭证是会话 token 会过期，
--    只能给"刚下发的那一批"用；刷新页面或重启后端后，历史图只能走这一条。
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '读取抓拍图像','/api/gb28181/device-mgmt/snapshots/:id/content','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/snapshots/:id/content' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:device:snapshot' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/snapshots/:id/content' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:device:snapshot' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/snapshots/:id/content' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1='/api/gb28181/device-mgmt/snapshots/:id/content' AND p.v2='GET' AND p.v3='*');

-- ---- 列表接口：GET /api/gb28181/device-mgmt/snapshots ----
-- 与读图接口是两条路：本条出"有哪些图"（元数据 + 取图地址），那条出"这张图的字节"。
-- 二者都绑同一个权限码 `gb28181:device:snapshot` —— 能看一次抓拍的产出，就能看它的历史。
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查询抓拍图像库','/api/gb28181/device-mgmt/snapshots','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/snapshots' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:device:snapshot' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/snapshots' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:device:snapshot' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/snapshots' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1='/api/gb28181/device-mgmt/snapshots' AND p.v2='GET' AND p.v3='*');

-- ---- 图像库一级菜单：/gb28181/snapshot-library ----
-- component 是**前端组件路径契约**（web/src/router/route-output.ts 用
-- import.meta.glob("@/views/**/*.vue") 的 key 逐字比对）⇒ 这一行要求仓库里存在
-- web/src/views/gb28181/snapshot-library/index.vue。
-- ⚠️ 菜单随本次迁移入库、页面随前端发布：两者不在同一批次时，**菜单点开是空白**。
-- 这是设计上接受的短暂窗口（见 docs/snapshot-flow-design.md §3.3 / §5 的落地顺序），
-- 不是可以长期停留的状态。
--
-- ⛔ `sort=55`：**不能与别的同层菜单取同一个值**。菜单树由
-- `app/models/sysmenu.go` 的 `TreeSort()` 排序，它用 `sort.Slice`（**非稳定排序**）且
-- `aSort == bSort` 时直接回落成 `aSort < bSort` = false ⇒ 同 sort 的两项**顺序不确定**
-- （随查询返回顺序漂移），表现是"菜单栏位置偶尔会变"。
-- 55 是刻意选的：插在「云端录像」(50) 与「录像计划」(60) 之间 —— 图像库与云端录像同属
-- "历史媒体资源浏览"，语义同组；且 55 当前**没有任何邻居占用**（初版取 80 撞上了
-- 「SIP 接入信息」，已修）。
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,'/gb28181/snapshot-library','snapshot-library','gb28181/snapshot-library/index','图像库',0,0,55,2,'','lucide:Images',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/snapshot-library' AND deleted_at IS NULL);

-- ⛔ 菜单可见性**必须**显式写 sys_role_menu：本平台菜单树是 role_menu 驱动
-- （`app/controllers/sysmenu.go` 的 `GetRouters`：sys_user_role 取用户角色 → 角色祖先 →
--  sys_role_menu 取 menu_id → sys_menu），少了这些行，菜单行存在、谁都看不到。
-- ① 内置管理员（role 1，硬编码）。照 2026-09-06-openapi-client-menu.sql 的口径，
--    但**刻意不查 sys_role**（那份先例查了 r.status/r.deleted_at）：行为测试在 sqlite 上跑，
--    那里没有 sys_role 表；而 role 1 是内置管理员，授予它不需要额外的存活校验。
INSERT INTO sys_role_menu(role_id,menu_id)
SELECT 1,m.id FROM sys_menu m
WHERE m.path='/gb28181/snapshot-library' AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);

-- ② 已持有 `gb28181:device:snapshot` 按钮的角色 —— 与图像库两个接口**同权限码**。
--    能看到"这次抓拍的图"的人本来就该看到图像库入口；不给的话会变成
--    "有权限调接口、菜单里却找不到入口"。
-- ⛔ JOIN 必须带 `btn.type=3`：同 permission 的目录行（type=1）不是按钮，
--    把它当授权来源等于凭空扩权（同 permission 的目录通常是隐藏的导航壳）。
INSERT INTO sys_role_menu(role_id,menu_id)
SELECT DISTINCT rm.role_id,lib.id
FROM sys_role_menu rm
JOIN sys_menu btn ON btn.id=rm.menu_id AND btn.deleted_at IS NULL
  AND btn.permission='gb28181:device:snapshot' AND btn.type=3
CROSS JOIN sys_menu lib
WHERE lib.path='/gb28181/snapshot-library' AND lib.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=rm.role_id AND x.menu_id=lib.id);
-- channel-snapshot-library-permissions:end
