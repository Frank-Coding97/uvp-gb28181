-- 存量 SQLite 库补齐「图像库按钮权限 + 一键清理 + 批量下载」等种子。
--
-- 背景：2026-11-04 那批改动（图像库按钮权限、批量下载 zip、清空定时任务日志）
-- 只写了**仅 MySQL 方言**的增量脚本（2026-11-04-snapshot-*.sql），注释里写明
-- 「全新环境由三方言全量脚本建库」—— 当时 SQLite 单机版还没纳入交付面。
-- 结果：全新装的 SQLite 库（基线重生成后）有这些行，而**升级上来的单机版没有**：
--   · 图像库菜单 140509 的 permission 是空串 ⇒ 没有任何权限码，角色配不出图像库；
--   · 「批量下载图像」「删除图像」两个按钮节点不存在 ⇒ 页面上没有这两个按钮；
--   · 「一键清理」(140619) 不存在 ⇒ 任务日志页的清理按钮不显示；
--   · 批量下载接口 686 没登记，casbin 也没授权 ⇒ 即便手工调也 403；
--   · sys_openapi_security_state 没有初始行 ⇒ OpenAPI 鉴权锁状态读取依赖默认值。
--
-- ⚠️ 与 MySQL 版（2026-11-04-snapshot-*.sql）同口径：**只补不删**，
--    每条都带 NOT EXISTS，重复执行不覆盖任何既有自定义授权。
-- ⚠️ 定位 sys_api 一律按 (path, method)，不写死自增 id —— 存量库的 id 早已被占，
--    MySQL 版能写 686/687 是因为那是活库真实 id，SQLite 库不一定对得上。

-- 1) sys_api：批量下载接口 -------------------------------------------------
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","deleted_at","created_by")
SELECT '批量下载抓拍图像','/api/gb28181/device-mgmt/snapshots/archive','GET','图像库',
       '2026-11-04 16:41:24.000000','2026-11-04 16:41:24.000000',NULL,1
WHERE NOT EXISTS (
    SELECT 1 FROM "sys_api"
    WHERE "path"='/api/gb28181/device-mgmt/snapshots/archive' AND "method"='GET');

-- 2) 图像库页面菜单补查看权限码（只在还是空的时候改） ----------------------
-- ⛔ 页面菜单行也带 permission：userservice 收集权限码取 type IN (2,3)，
--    所以「勾了菜单 = 有查看权」，存量角色不必再补一行。
UPDATE "sys_menu"
SET "permission"='gb28181:snapshot:view', "updated_at"='2026-11-04 19:14:43.000000'
WHERE "id"=140509 AND ("permission" IS NULL OR "permission"='');

-- 3) 三个按钮节点 ----------------------------------------------------------
INSERT INTO "sys_menu" ("id","parent_id","path","name","redirect","component","title",
                        "is_full","hide","disable","keep_alive","affix","link","iframe",
                        "svg_icon","icon","sort","type","is_link","permission",
                        "created_at","updated_at","deleted_at","created_by")
SELECT 140619,140347,'','SysJobResultsClear',NULL,'','一键清理',0,0,0,0,0,'',0,'','',1,3,0,
       'system:sysjobresults:clear','2026-11-04 18:55:00.000000','2026-11-04 18:55:00.000000',NULL,1
WHERE NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "id"=140619);

INSERT INTO "sys_menu" ("id","parent_id","path","name","redirect","component","title",
                        "is_full","hide","disable","keep_alive","affix","link","iframe",
                        "svg_icon","icon","sort","type","is_link","permission",
                        "created_at","updated_at","deleted_at","created_by")
SELECT 140620,140509,'','',NULL,'','批量下载图像',0,0,0,1,0,'',0,'','',1,3,0,
       'gb28181:snapshot:download','2026-11-04 19:14:43.000000','2026-11-04 19:14:43.000000',NULL,1
WHERE NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "id"=140620);

INSERT INTO "sys_menu" ("id","parent_id","path","name","redirect","component","title",
                        "is_full","hide","disable","keep_alive","affix","link","iframe",
                        "svg_icon","icon","sort","type","is_link","permission",
                        "created_at","updated_at","deleted_at","created_by")
SELECT 140621,140509,'','',NULL,'','删除图像',0,0,0,1,0,'',0,'','',2,3,0,
       'gb28181:snapshot:delete','2026-11-04 19:14:43.000000','2026-11-04 19:14:43.000000',NULL,1
WHERE NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "id"=140621);

-- 4) 接口重挂到图像库自己名下（先挂新的，再摘旧的） -------------------------
--    ⛔ 缩略图/取图走的 587 必须挂在 type=2 的页面菜单上，**不能**归到下载按钮：
--       同一个接口既喂列表缩略图又喂单张下载，归到下载会让「只看不下载」的账号整页白图。
INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT 140509, a."id" FROM "sys_api" a
WHERE ((a."path"='/api/gb28181/device-mgmt/snapshots' AND a."method"='GET')
    OR (a."path"='/api/gb28181/device-mgmt/snapshots/:id/content' AND a."method"='GET'))
  AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" ma WHERE ma."menu_id"=140509 AND ma."api_id"=a."id");

INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT 140620, a."id" FROM "sys_api" a
WHERE a."path"='/api/gb28181/device-mgmt/snapshots/archive' AND a."method"='GET'
  AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" ma WHERE ma."menu_id"=140620 AND ma."api_id"=a."id");

INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT 140621, a."id" FROM "sys_api" a
WHERE a."path"='/api/gb28181/device-mgmt/snapshots' AND a."method"='DELETE'
  AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" ma WHERE ma."menu_id"=140621 AND ma."api_id"=a."id");

INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT 140619, a."id" FROM "sys_api" a
WHERE a."path"='/api/sysJobResults/clear' AND a."method"='POST'
  AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" ma WHERE ma."menu_id"=140619 AND ma."api_id"=a."id");

-- 5) 摘掉 140450「设备图像抓拍会话」上的图像库接口 -------------------------
--    ⛔ SQLite 不支持 `DELETE ma FROM ... JOIN`，只能先选出 api_id 再删。
DELETE FROM "sys_menu_api"
WHERE "menu_id"=140450
  AND "api_id" IN (
      SELECT "id" FROM "sys_api"
      WHERE ("path"='/api/gb28181/device-mgmt/snapshots' AND "method" IN ('GET','DELETE'))
         OR ("path"='/api/gb28181/device-mgmt/snapshots/:id/content' AND "method"='GET')
         OR ("path"='/api/gb28181/device-mgmt/snapshots/archive' AND "method"='GET'));

-- 6) casbin：批量下载只给本来就调得动图像库的管理员 ------------------------
INSERT INTO "sys_casbin_rule" ("ptype","v0","v1","v2","v3","v4","v5")
SELECT 'p','role_1','/api/gb28181/device-mgmt/snapshots/archive','GET','*','',''
WHERE NOT EXISTS (
    SELECT 1 FROM "sys_casbin_rule"
    WHERE "ptype"='p' AND "v0"='role_1'
      AND "v1"='/api/gb28181/device-mgmt/snapshots/archive' AND "v2"='GET');

-- 7) 存量角色补按钮：按 casbin 已有策略反查 --------------------------------
--    ⛔ 不能写成「所有勾了图像库菜单的角色都补」—— 那等于凭空发删除权：
--       改造前 639 没挂任何菜单，除了 casbin 里显式写过 DELETE 的角色，谁也删不了。
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT DISTINCT r."id", 140620 FROM "sys_casbin_rule" c
JOIN "sys_role" r ON c."v0" = 'role_' || r."id"
WHERE c."v1"='/api/gb28181/device-mgmt/snapshots/archive' AND c."v2"='GET'
  AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" rm WHERE rm."role_id"=r."id" AND rm."menu_id"=140620);

INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT DISTINCT r."id", 140621 FROM "sys_casbin_rule" c
JOIN "sys_role" r ON c."v0" = 'role_' || r."id"
WHERE c."v1"='/api/gb28181/device-mgmt/snapshots' AND c."v2"='DELETE'
  AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" rm WHERE rm."role_id"=r."id" AND rm."menu_id"=140621);

INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT DISTINCT r."id", 140619 FROM "sys_casbin_rule" c
JOIN "sys_role" r ON c."v0" = 'role_' || r."id"
WHERE c."v1"='/api/sysJobResults/clear' AND c."v2"='POST'
  AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" rm WHERE rm."role_id"=r."id" AND rm."menu_id"=140619);

-- 8) OpenAPI 鉴权锁初始行 -------------------------------------------------
INSERT INTO "sys_openapi_security_state" ("id","must_auth_locked","locked_at","lock_version")
SELECT 1,0,NULL,0
WHERE NOT EXISTS (SELECT 1 FROM "sys_openapi_security_state");
