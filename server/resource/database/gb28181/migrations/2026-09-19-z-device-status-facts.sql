-- 设备自报事实落库（MySQL 5.7+）。
--
-- DeviceStatus 应答里的 Online / Status / Encode / DeviceTime 四项，以及设备声明的
-- 报警输入数量，此前被解析器整段丢弃（见 app/gb28181/manscdp/device_status.go）。
-- 取值约定：NULL = 本次应答没有这一项；0 = 设备明确指出自己一个报警输入都没有。
-- 二者语义不同（未知 vs 已知的能力缺失），所以 alarm_input_count 不做 NOT NULL DEFAULT 0。
SET @sql := IF(NOT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='gb_device_control_state' AND column_name='online_state'
), 'ALTER TABLE `gb_device_control_state` ADD COLUMN `online_state` varchar(8) NULL AFTER `guard_state`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(NOT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='gb_device_control_state' AND column_name='selftest_state'
), 'ALTER TABLE `gb_device_control_state` ADD COLUMN `selftest_state` varchar(16) NULL AFTER `online_state`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(NOT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='gb_device_control_state' AND column_name='encode_state'
), 'ALTER TABLE `gb_device_control_state` ADD COLUMN `encode_state` varchar(8) NULL AFTER `selftest_state`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(NOT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='gb_device_control_state' AND column_name='device_time'
), 'ALTER TABLE `gb_device_control_state` ADD COLUMN `device_time` datetime(3) NULL AFTER `encode_state`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(NOT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='gb_device_control_state' AND column_name='alarm_input_count'
), 'ALTER TABLE `gb_device_control_state` ADD COLUMN `alarm_input_count` int NULL AFTER `device_time`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
