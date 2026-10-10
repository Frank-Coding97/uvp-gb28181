IF COL_LENGTH(N'dbo.sys_users', N'must_change_password') IS NOT NULL
  ALTER TABLE [dbo].[sys_users] DROP COLUMN [must_change_password];
