-- PostgreSQL 的索引名也是 schema 级唯一，但**主生成器早就处理过了**：
-- baseline/generate_sql.py 的 SCHEMA_WIDE_INDEX_NAMES 会把重名索引加
-- `<表名>__` 前缀（见 postgresql_converted.sql 里的 gb_device__idx_deleted_at）。
-- 所以 PG 基线/线上本来就带着正确名字，无需变更。
SELECT 1;
