IF COL_LENGTH(N'dbo.gb_sip_config', N'sdp_ip') IS NULL
  ALTER TABLE [dbo].[gb_sip_config]
    ADD [sdp_ip] VARCHAR(253) NOT NULL CONSTRAINT [df_gb_sip_config_sdp_ip] DEFAULT ('');