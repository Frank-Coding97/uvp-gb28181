-- Existing SIP configurations default to the current browser playback policy.
SET @table_exists := (
  SELECT COUNT(*) FROM information_schema.TABLES
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_sip_config'
);
SET @column_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_sip_config' AND COLUMN_NAME = 'stream_ip'
);
SET @ddl := IF(@table_exists = 1 AND @column_exists = 0,
  'ALTER TABLE `gb_sip_config` ADD COLUMN `stream_ip` varchar(253) NOT NULL DEFAULT '''' COMMENT ''平台默认 Stream 地址'' AFTER `hook_ip`',
  'SELECT 1');
PREPARE sip_platform_stream_ip_stmt FROM @ddl;
EXECUTE sip_platform_stream_ip_stmt;
DEALLOCATE PREPARE sip_platform_stream_ip_stmt;
