-- 设备自报事实落库（PostgreSQL 12+）。列语义见 MySQL 版本的注释：
-- NULL = 本次应答没有这一项；0 = 设备明确指出自己一个报警输入都没有。
ALTER TABLE IF EXISTS gb_device_control_state ADD COLUMN IF NOT EXISTS online_state VARCHAR(8);
ALTER TABLE IF EXISTS gb_device_control_state ADD COLUMN IF NOT EXISTS selftest_state VARCHAR(16);
ALTER TABLE IF EXISTS gb_device_control_state ADD COLUMN IF NOT EXISTS encode_state VARCHAR(8);
ALTER TABLE IF EXISTS gb_device_control_state ADD COLUMN IF NOT EXISTS device_time TIMESTAMP(3);
ALTER TABLE IF EXISTS gb_device_control_state ADD COLUMN IF NOT EXISTS alarm_input_count INTEGER;
