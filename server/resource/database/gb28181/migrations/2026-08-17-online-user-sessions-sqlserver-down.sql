-- 回滚在线用户会话表(SQL Server)。只删除本功能资产。
IF OBJECT_ID(N'sys_user_sessions', N'U') IS NOT NULL DROP TABLE [sys_user_sessions];
