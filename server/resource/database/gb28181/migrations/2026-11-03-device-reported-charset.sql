-- 设备详情「上报字符集」：记录设备 MESSAGE 报文声明的字符集与平台实际判定值。
-- 见 models/gb_device.go UpdateReportedCharset 与 handler/message.go 的嗅探入口。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_device' AND COLUMN_NAME = 'reported_charset') = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `reported_charset` VARCHAR(32) NOT NULL DEFAULT '''' COMMENT ''最近一次上报报文声明的字符集''', 'SELECT 1');
PREPARE device_reported_charset_stmt FROM @ddl;
EXECUTE device_reported_charset_stmt;
DEALLOCATE PREPARE device_reported_charset_stmt;

SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_device' AND COLUMN_NAME = 'wire_charset') = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `wire_charset` VARCHAR(32) NOT NULL DEFAULT '''' COMMENT ''最近一次上报报文实际判定字符集''', 'SELECT 1');
PREPARE device_wire_charset_stmt FROM @ddl;
EXECUTE device_wire_charset_stmt;
DEALLOCATE PREPARE device_wire_charset_stmt;

SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_device' AND COLUMN_NAME = 'reported_charset_at') = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `reported_charset_at` DATETIME(3) NULL COMMENT ''当前字符集事实的确认时间''', 'SELECT 1');
PREPARE device_reported_charset_at_stmt FROM @ddl;
EXECUTE device_reported_charset_at_stmt;
DEALLOCATE PREPARE device_reported_charset_at_stmt;
