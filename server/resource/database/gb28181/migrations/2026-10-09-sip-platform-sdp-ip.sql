-- Platform SDP announcement address: the address written into the INVITE SDP
-- c= line, telling the device where to push RTP.
--
-- Required from now on: an empty value makes the device push at the platform
-- itself (typically 127.0.0.1), which surfaces as "signalling succeeds but no
-- picture" rather than as a configuration error.
SET @table_exists := (
  SELECT COUNT(*) FROM information_schema.TABLES
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_sip_config'
);
SET @column_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_sip_config' AND COLUMN_NAME = 'sdp_ip'
);
SET @ddl := IF(@table_exists = 1 AND @column_exists = 0,
  'ALTER TABLE `gb_sip_config` ADD COLUMN `sdp_ip` varchar(253) NOT NULL DEFAULT '''' COMMENT ''平台默认 SDP 地址(设备回推RTP 目标)'' AFTER `hook_ip`',
  'SELECT 1');
PREPARE sip_platform_sdp_ip_stmt FROM @ddl;
EXECUTE sip_platform_sdp_ip_stmt;
DEALLOCATE PREPARE sip_platform_sdp_ip_stmt;