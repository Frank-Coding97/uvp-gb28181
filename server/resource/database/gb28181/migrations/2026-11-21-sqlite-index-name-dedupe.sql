-- MySQL 的索引名按表各自唯一，不存在跨表重名问题：
-- 基线与线上这 5 组同名索引（idx_status / idx_deleted_at / idx_channel_id /
-- idx_owner_dept_deleted / idx_user_id）在每张表上**都已各自存在**。
--
-- 本迁移的实质内容只针对 SQLite（它的索引名是**库级唯一**，同名索引在第二张表
-- 上会被 `CREATE INDEX IF NOT EXISTS` 静默跳过）。这里保留一条无副作用的语句，
-- 只为让四方言脚本成套（契约见 migration_contract_test.go）。
SELECT 1;
