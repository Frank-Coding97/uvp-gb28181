-- ⛔⛔ SQLite 的索引名是**库级唯一**（sqlite_master.name 有 UNIQUE 约束），
--    而 MySQL 只要求**表内**唯一。基线是从 MySQL 脚本转来的，同一个索引名
--    在好几张表上各声明了一次，于是执行到第二张表就被
--    `CREATE INDEX IF NOT EXISTS` **静默跳过** —— 不报错、也不管库里那个同名
--    索引建在哪张表上。实测 6 组重名，每组只有第一张表真的有索引：
--
--      idx_status             → sys_job_results / sys_jobs
--      idx_deleted_at         → gb_catalog_node / gb_channel / gb_device /
--                               gb_device_grant / gb_device_status_event
--      idx_owner_dept_deleted → gb_channel / gb_device
--      idx_channel_id         → gb_alarm_event / gb_mobile_position_latest
--      idx_user_id            → sys_operation_logs / sys_user_sessions
--      idx_created_at         → sys_job_results / sys_jobs（后者从未建出来）
--
--    后果不是「少个索引无所谓」：会话鉴权按 user_id 查、设备列表按
--    owner_dept_id + deleted_at 过滤、作业列表按 created_at 排序，
--    在 SQLite 单机版上全部退化成全表扫。
--
-- 修法与主生成器（baseline/generate_sql.py 的 SCHEMA_WIDE_INDEX_NAMES）
-- 和 PostgreSQL 基线保持一致：重名索引一律改名成 `<表名>__<原名>`。
-- 组内**每张表都重建**，不能只补被跳过的那几张 —— 「谁被跳过」取决于语句顺序，
-- 只补后面的会让名字随基线重生成的顺序漂移。
--
-- ⚠️ SQLite 不支持 ALTER INDEX RENAME，所以先 DROP 掉占用短名的那个再建新名；
--    否则旧名字留在原地，白搭一份写放大。
-- ⚠️ gb_device_status_event / gb_alarm_event 建索引各约 1~3 秒（按线上量级），
--    整条迁移在升级窗口内完成。

-- idx_status ---------------------------------------------------------------
DROP INDEX IF EXISTS "idx_status";
CREATE INDEX IF NOT EXISTS "sys_job_results__idx_status" ON "sys_job_results" ("status");
CREATE INDEX IF NOT EXISTS "sys_jobs__idx_status" ON "sys_jobs" ("status");

-- idx_deleted_at -----------------------------------------------------------
DROP INDEX IF EXISTS "idx_deleted_at";
CREATE INDEX IF NOT EXISTS "gb_catalog_node__idx_deleted_at" ON "gb_catalog_node" ("deleted_at");
CREATE INDEX IF NOT EXISTS "gb_channel__idx_deleted_at" ON "gb_channel" ("deleted_at");
CREATE INDEX IF NOT EXISTS "gb_device__idx_deleted_at" ON "gb_device" ("deleted_at");
CREATE INDEX IF NOT EXISTS "gb_device_grant__idx_deleted_at" ON "gb_device_grant" ("deleted_at");
CREATE INDEX IF NOT EXISTS "gb_device_status_event__idx_deleted_at" ON "gb_device_status_event" ("deleted_at");

-- idx_owner_dept_deleted ---------------------------------------------------
DROP INDEX IF EXISTS "idx_owner_dept_deleted";
CREATE INDEX IF NOT EXISTS "gb_channel__idx_owner_dept_deleted" ON "gb_channel" ("owner_dept_id", "deleted_at");
CREATE INDEX IF NOT EXISTS "gb_device__idx_owner_dept_deleted" ON "gb_device" ("owner_dept_id", "deleted_at");

-- idx_channel_id -----------------------------------------------------------
DROP INDEX IF EXISTS "idx_channel_id";
CREATE INDEX IF NOT EXISTS "gb_alarm_event__idx_channel_id" ON "gb_alarm_event" ("channel_id");
CREATE INDEX IF NOT EXISTS "gb_mobile_position_latest__idx_channel_id" ON "gb_mobile_position_latest" ("channel_id");

-- idx_user_id --------------------------------------------------------------
DROP INDEX IF EXISTS "idx_user_id";
CREATE INDEX IF NOT EXISTS "sys_operation_logs__idx_user_id" ON "sys_operation_logs" ("user_id");
CREATE INDEX IF NOT EXISTS "sys_user_sessions__idx_user_id" ON "sys_user_sessions" ("user_id");

-- idx_created_at -----------------------------------------------------------
-- 这一组特殊：sys_job_results 上的那个是死索引（没有任何查询按它的 created_at
-- 过滤），上一条迁移 2026-11-21-job-result-drop-created-at-index-sqlite 已经删了它；
-- 而真正需要它的 sys_jobs 因为重名被跳过，从来没建出来。这里把它建到正确的表上，
-- 名字保持裸名（去重后已不冲突，与 MySQL/PG 基线一致）。
-- ⚠️ 再 DROP 一次是为了幂等：全新安装的库在跑迁移之前，基线已经在 sys_jobs 上
--    建过这个索引（job-result 那条迁移会把它误删）。
DROP INDEX IF EXISTS "idx_created_at";
CREATE INDEX IF NOT EXISTS "idx_created_at" ON "sys_jobs" ("created_at");
