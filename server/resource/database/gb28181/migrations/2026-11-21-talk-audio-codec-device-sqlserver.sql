IF COL_LENGTH(N'dbo.gb_device', N'talk_audio_codec') IS NULL ALTER TABLE [dbo].[gb_device] ADD [talk_audio_codec] NVARCHAR(16) NOT NULL CONSTRAINT [df_gb_device_talk_audio_codec] DEFAULT N'pcma';
