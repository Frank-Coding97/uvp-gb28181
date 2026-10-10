-- Existing users default to no forced password change; only the fresh-install seed is true.
IF COL_LENGTH(N'dbo.sys_users', N'must_change_password') IS NULL
  ALTER TABLE [dbo].[sys_users]
    ADD [must_change_password] BIT NOT NULL DEFAULT (0);
