-- Persist the SSRC of the current live generation independently from stream_id.
-- SQL Server 2017+, repeatable.
IF OBJECT_ID(N'gb_channel', N'U') IS NOT NULL
   AND COL_LENGTH(N'gb_channel', N'current_ssrc') IS NULL
  ALTER TABLE [gb_channel]
    ADD [current_ssrc] NVARCHAR(10) NOT NULL
      CONSTRAINT [df_gb_channel_current_ssrc] DEFAULT N'';
