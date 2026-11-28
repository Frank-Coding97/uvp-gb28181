IF COL_LENGTH(N'dbo.gb_sip_config', N'stream_ip') IS NULL
  ALTER TABLE [dbo].[gb_sip_config]
    ADD [stream_ip] VARCHAR(253) NOT NULL CONSTRAINT [df_gb_sip_config_stream_ip] DEFAULT ('');
