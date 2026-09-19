-- 回收设备自报事实列（PostgreSQL 12+）。删列会丢掉已落库的设备自报事实，只用于回滚。
ALTER TABLE IF EXISTS gb_device_control_state DROP COLUMN IF EXISTS alarm_input_count;
ALTER TABLE IF EXISTS gb_device_control_state DROP COLUMN IF EXISTS device_time;
ALTER TABLE IF EXISTS gb_device_control_state DROP COLUMN IF EXISTS encode_state;
ALTER TABLE IF EXISTS gb_device_control_state DROP COLUMN IF EXISTS selftest_state;
ALTER TABLE IF EXISTS gb_device_control_state DROP COLUMN IF EXISTS online_state;
