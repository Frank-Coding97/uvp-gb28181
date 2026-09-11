-- 2026-06-26 catalog B+ DOWN(回滚)— 反向 DDL
-- A5 backfill 后才允许跑此 down;之前跑会丢 catalog/挂载/anomaly 数据
DROP TABLE IF EXISTS `sys_civil_code`;
DROP TABLE IF EXISTS `gb_anomaly_record`;
DROP TABLE IF EXISTS `gb_channel_mount`;
DROP TABLE IF EXISTS `gb_catalog_node`;

ALTER TABLE `gb_channel`
  DROP COLUMN `capabilities`;

ALTER TABLE `gb_device`
  DROP INDEX `idx_subscribe_capability`,
  DROP COLUMN `subscribe_expires_at`,
  DROP COLUMN `subscribe_last_test`,
  DROP COLUMN `subscribe_capability`;
