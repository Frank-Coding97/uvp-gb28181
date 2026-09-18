-- 2026-06-16 在线状态模型重构:keepalive_time 作唯一真相,status 降为派生缓存
-- 对已存在的 gb_device 表加列(新建环境用 gb_device.sql 即含这些列)
-- 幂等:版本表丢失的既有安装会重放本迁移,列已存在时跳过
SET @online_model_cols_missing = (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_device'
    AND COLUMN_NAME IN ('register_expire_at','keepalive_interval','offline_at')
);
SET @online_model_ddl = IF(@online_model_cols_missing = 0, 'SELECT 1', 'ALTER TABLE `gb_device` ADD COLUMN `register_expire_at` datetime DEFAULT NULL COMMENT ''注册到期时刻'' AFTER `register_time`, ADD COLUMN `keepalive_interval` int DEFAULT 60 COMMENT ''【事实】该设备期望心跳周期(秒)'' AFTER `keepalive_time`, ADD COLUMN `offline_at` datetime DEFAULT NULL COMMENT ''最近被判离线的时刻'' AFTER `status`');
PREPARE online_model_stmt FROM @online_model_ddl;
EXECUTE online_model_stmt;
DEALLOCATE PREPARE online_model_stmt;

SET @online_model_idx_missing = (
  SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_device' AND INDEX_NAME = 'idx_status_keepalive'
);
SET @online_model_idx_ddl = IF(@online_model_idx_missing = 0, 'SELECT 1', 'ALTER TABLE `gb_device` ADD INDEX `idx_status_keepalive` (`status`, `keepalive_time`)');
PREPARE online_model_idx_stmt FROM @online_model_idx_ddl;
EXECUTE online_model_idx_stmt;
DEALLOCATE PREPARE online_model_idx_stmt;
