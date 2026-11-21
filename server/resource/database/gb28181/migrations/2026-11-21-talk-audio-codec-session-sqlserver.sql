IF COL_LENGTH(N'dbo.gb_talk_session', N'audio_codec') IS NULL ALTER TABLE [dbo].[gb_talk_session] ADD [audio_codec] NVARCHAR(16) NOT NULL CONSTRAINT [df_gb_talk_session_audio_codec] DEFAULT N'pcma';
