-- 设备自报事实落库（SQL Server 2017+）。列语义见 MySQL 版本的注释：
-- NULL = 本次应答没有这一项；0 = 设备明确指出自己一个报警输入都没有。
IF OBJECT_ID(N'gb_device_control_state', N'U') IS NOT NULL AND COL_LENGTH(N'gb_device_control_state', N'online_state') IS NULL
  ALTER TABLE [gb_device_control_state] ADD [online_state] NVARCHAR(8) NULL;
IF OBJECT_ID(N'gb_device_control_state', N'U') IS NOT NULL AND COL_LENGTH(N'gb_device_control_state', N'selftest_state') IS NULL
  ALTER TABLE [gb_device_control_state] ADD [selftest_state] NVARCHAR(16) NULL;
IF OBJECT_ID(N'gb_device_control_state', N'U') IS NOT NULL AND COL_LENGTH(N'gb_device_control_state', N'encode_state') IS NULL
  ALTER TABLE [gb_device_control_state] ADD [encode_state] NVARCHAR(8) NULL;
IF OBJECT_ID(N'gb_device_control_state', N'U') IS NOT NULL AND COL_LENGTH(N'gb_device_control_state', N'device_time') IS NULL
  ALTER TABLE [gb_device_control_state] ADD [device_time] DATETIME2(3) NULL;
IF OBJECT_ID(N'gb_device_control_state', N'U') IS NOT NULL AND COL_LENGTH(N'gb_device_control_state', N'alarm_input_count') IS NULL
  ALTER TABLE [gb_device_control_state] ADD [alarm_input_count] INT NULL;
