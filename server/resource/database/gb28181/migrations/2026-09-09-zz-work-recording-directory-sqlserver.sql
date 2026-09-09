-- Persist the independent server-side recording directory and claim version.
-- Existing work-recording and recorder-claim tables are required; no table is created here.
-- SQL Server 2017+, repeatable.
IF OBJECT_ID(N'gb_work_recording', N'U') IS NULL BEGIN THROW 51000, 'gb_work_recording must exist before work-recording directory migration', 1; END;
IF OBJECT_ID(N'gb_recorder_claim', N'U') IS NULL BEGIN THROW 51000, 'gb_recorder_claim must exist before work-recording directory migration', 1; END;

IF COL_LENGTH(N'gb_work_recording', N'recording_root') IS NULL
  ALTER TABLE [gb_work_recording]
    ADD [recording_root] NVARCHAR(1024) NOT NULL
      CONSTRAINT [df_gb_work_recording_recording_root] DEFAULT N'';
IF COL_LENGTH(N'gb_work_recording', N'recorder_claim_version') IS NULL
  ALTER TABLE [gb_work_recording]
    ADD [recorder_claim_version] BIGINT NOT NULL
      CONSTRAINT [df_gb_work_recording_recorder_claim_version] DEFAULT 0;
IF COL_LENGTH(N'gb_recorder_claim', N'recording_root') IS NULL
  ALTER TABLE [gb_recorder_claim]
    ADD [recording_root] NVARCHAR(1024) NOT NULL
      CONSTRAINT [df_gb_recorder_claim_recording_root] DEFAULT N'';
