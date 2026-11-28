-- 回滚到「每组只有第一张表有索引」的原状：删掉带前缀的新索引，
-- 把短名还给组内第一张表（也就是 SQLite 当初真正建出来的那一张）。
-- ⚠️ 顺序与 up 相反：先删新名再建短名，否则 DROP IF EXISTS "idx_status"
--    会把刚建好的东西删掉。

-- idx_status ---------------------------------------------------------------
DROP INDEX IF EXISTS "sys_job_results__idx_status";
DROP INDEX IF EXISTS "sys_jobs__idx_status";
CREATE INDEX IF NOT EXISTS "idx_status" ON "sys_job_results" ("status");

-- idx_deleted_at -----------------------------------------------------------
DROP INDEX IF EXISTS "gb_catalog_node__idx_deleted_at";
DROP INDEX IF EXISTS "gb_channel__idx_deleted_at";
DROP INDEX IF EXISTS "gb_device__idx_deleted_at";
DROP INDEX IF EXISTS "gb_device_grant__idx_deleted_at";
DROP INDEX IF EXISTS "gb_device_status_event__idx_deleted_at";
CREATE INDEX IF NOT EXISTS "idx_deleted_at" ON "gb_catalog_node" ("deleted_at");

-- idx_owner_dept_deleted ---------------------------------------------------
DROP INDEX IF EXISTS "gb_channel__idx_owner_dept_deleted";
DROP INDEX IF EXISTS "gb_device__idx_owner_dept_deleted";
CREATE INDEX IF NOT EXISTS "idx_owner_dept_deleted" ON "gb_channel" ("owner_dept_id", "deleted_at");

-- idx_channel_id -----------------------------------------------------------
DROP INDEX IF EXISTS "gb_alarm_event__idx_channel_id";
DROP INDEX IF EXISTS "gb_mobile_position_latest__idx_channel_id";
CREATE INDEX IF NOT EXISTS "idx_channel_id" ON "gb_alarm_event" ("channel_id");

-- idx_user_id --------------------------------------------------------------
DROP INDEX IF EXISTS "sys_operation_logs__idx_user_id";
DROP INDEX IF EXISTS "sys_user_sessions__idx_user_id";
CREATE INDEX IF NOT EXISTS "idx_user_id" ON "sys_operation_logs" ("user_id");

-- idx_created_at -----------------------------------------------------------
-- 迁移前 sys_jobs 没有这个索引；短名会在 2026-11-21-job-result-drop-created-at-index-sqlite
-- 的 down 里还给 sys_job_results。
DROP INDEX IF EXISTS "idx_created_at";
