-- 设备类型（ipc / nvr / platform）：由用户在设备编辑里手动维护，默认 IPC。
-- 只用于展示（卡片图标）与筛选，不参与任何协议逻辑 —— 注册报文里没有可靠的形态判据。
-- 见 models/gb_device.go 的 DeviceType 字段与 NormalizeDeviceType。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_device' AND COLUMN_NAME = 'device_type') = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `device_type` VARCHAR(16) NOT NULL DEFAULT ''ipc'' COMMENT ''设备类型 ipc/nvr/platform(用户维护)''', 'SELECT 1');
PREPARE device_type_stmt FROM @ddl;
EXECUTE device_type_stmt;
DEALLOCATE PREPARE device_type_stmt;
