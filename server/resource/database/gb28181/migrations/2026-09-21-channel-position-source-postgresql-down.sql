-- 回滚 2026-09-21-channel-position-source-postgresql.sql（PostgreSQL）。
-- 语义与 MySQL 版 down 一致，见 2026-09-21-channel-position-source-down.sql。

ALTER TABLE gb_channel DROP COLUMN IF EXISTS position_updated_at;
ALTER TABLE gb_channel DROP COLUMN IF EXISTS position_source;
