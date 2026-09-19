-- 回收设备自报事实列（MySQL 5.7+）。删列会丢掉已落库的设备自报事实，只用于回滚。
SET @sql := IF(EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='gb_device_control_state' AND column_name='alarm_input_count'
), 'ALTER TABLE `gb_device_control_state` DROP COLUMN `alarm_input_count`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='gb_device_control_state' AND column_name='device_time'
), 'ALTER TABLE `gb_device_control_state` DROP COLUMN `device_time`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='gb_device_control_state' AND column_name='encode_state'
), 'ALTER TABLE `gb_device_control_state` DROP COLUMN `encode_state`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='gb_device_control_state' AND column_name='selftest_state'
), 'ALTER TABLE `gb_device_control_state` DROP COLUMN `selftest_state`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='gb_device_control_state' AND column_name='online_state'
), 'ALTER TABLE `gb_device_control_state` DROP COLUMN `online_state`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
