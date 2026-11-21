-- Existing SIP configurations default to the current callback address policy.
SET @table_exists := (
  SELECT COUNT(*) FROM information_schema.TABLES
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_sip_config'
);
SET @column_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_sip_config' AND COLUMN_NAME = 'hook_ip'
);
SET @ddl := IF(@table_exists = 1 AND @column_exists = 0,
  'ALTER TABLE `gb_sip_config` ADD COLUMN `hook_ip` varchar(45) NOT NULL DEFAULT '''' COMMENT ''平台默认 Hook 地址'' AFTER `advertise_ip_inferred`',
  'SELECT 1');
PREPARE sip_platform_hook_ip_stmt FROM @ddl;
EXECUTE sip_platform_hook_ip_stmt;
DEALLOCATE PREPARE sip_platform_hook_ip_stmt;
