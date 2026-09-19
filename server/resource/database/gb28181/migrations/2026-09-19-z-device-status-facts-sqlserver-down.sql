-- 回收设备自报事实列（SQL Server 2017+）。删列会丢掉已落库的设备自报事实，只用于回滚。
IF OBJECT_ID(N'gb_device_control_state', N'U') IS NOT NULL AND COL_LENGTH(N'gb_device_control_state', N'alarm_input_count') IS NOT NULL
  ALTER TABLE [gb_device_control_state] DROP COLUMN [alarm_input_count];
IF OBJECT_ID(N'gb_device_control_state', N'U') IS NOT NULL AND COL_LENGTH(N'gb_device_control_state', N'device_time') IS NOT NULL
  ALTER TABLE [gb_device_control_state] DROP COLUMN [device_time];
IF OBJECT_ID(N'gb_device_control_state', N'U') IS NOT NULL AND COL_LENGTH(N'gb_device_control_state', N'encode_state') IS NOT NULL
  ALTER TABLE [gb_device_control_state] DROP COLUMN [encode_state];
IF OBJECT_ID(N'gb_device_control_state', N'U') IS NOT NULL AND COL_LENGTH(N'gb_device_control_state', N'selftest_state') IS NOT NULL
  ALTER TABLE [gb_device_control_state] DROP COLUMN [selftest_state];
IF OBJECT_ID(N'gb_device_control_state', N'U') IS NOT NULL AND COL_LENGTH(N'gb_device_control_state', N'online_state') IS NOT NULL
  ALTER TABLE [gb_device_control_state] DROP COLUMN [online_state];
