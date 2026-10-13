IF COL_LENGTH(N'dbo.gb_sip_config', N'hook_ip') IS NULL
  ALTER TABLE [dbo].[gb_sip_config]
    ADD [hook_ip] VARCHAR(45) NOT NULL CONSTRAINT [df_gb_sip_config_hook_ip] DEFAULT ('');
