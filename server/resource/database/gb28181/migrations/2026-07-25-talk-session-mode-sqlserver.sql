-- 2026-07-25: persist the user-selected GB28181 audio session mode.
-- SQL Server 2017+; repeatable for both fresh and upgraded installations.

IF OBJECT_ID(N'gb_talk_session', N'U') IS NOT NULL
   AND COL_LENGTH(N'gb_talk_session', N'mode') IS NULL
    ALTER TABLE [gb_talk_session]
        ADD [mode] NVARCHAR(16) NOT NULL
            CONSTRAINT [df_gb_talk_session_mode] DEFAULT N'talk';

IF OBJECT_ID(N'gb_talk_session', N'U') IS NOT NULL
    UPDATE [gb_talk_session]
    SET [mode] = N'talk'
    WHERE [mode] IS NULL OR [mode] NOT IN (N'broadcast', N'talk');
