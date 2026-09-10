-- Persist the original recorder owner and exact media identity.
-- Existing rows receive neutral defaults and are never adopted automatically.
-- SQL Server 2017+, repeatable.
IF COL_LENGTH(N'gb_recording_plan_channel_state', N'recorder_owner_kind') IS NULL
  ALTER TABLE [gb_recording_plan_channel_state]
    ADD [recorder_owner_kind] NVARCHAR(20) NOT NULL
      CONSTRAINT [df_gb_recording_plan_state_recorder_owner_kind] DEFAULT N'';
IF COL_LENGTH(N'gb_recording_plan_channel_state', N'recorder_owner_id') IS NULL
  ALTER TABLE [gb_recording_plan_channel_state]
    ADD [recorder_owner_id] NVARCHAR(128) NOT NULL
      CONSTRAINT [df_gb_recording_plan_state_recorder_owner_id] DEFAULT N'';
IF COL_LENGTH(N'gb_recording_plan_channel_state', N'recorder_claim_version') IS NULL
  ALTER TABLE [gb_recording_plan_channel_state]
    ADD [recorder_claim_version] BIGINT NOT NULL
      CONSTRAINT [df_gb_recording_plan_state_recorder_claim_version] DEFAULT 0;

IF COL_LENGTH(N'gb_recorder_claim', N'channel_id') IS NULL
  ALTER TABLE [gb_recorder_claim]
    ADD [channel_id] BIGINT NOT NULL
      CONSTRAINT [df_gb_recorder_claim_channel_id] DEFAULT 0;
IF COL_LENGTH(N'gb_recorder_claim', N'node_id') IS NULL
  ALTER TABLE [gb_recorder_claim]
    ADD [node_id] BIGINT NOT NULL
      CONSTRAINT [df_gb_recorder_claim_node_id] DEFAULT 0;
IF COL_LENGTH(N'gb_recorder_claim', N'v_host') IS NULL
  ALTER TABLE [gb_recorder_claim]
    ADD [v_host] NVARCHAR(128) NOT NULL
      CONSTRAINT [df_gb_recorder_claim_v_host] DEFAULT N'';
IF COL_LENGTH(N'gb_recorder_claim', N'app') IS NULL
  ALTER TABLE [gb_recorder_claim]
    ADD [app] NVARCHAR(64) NOT NULL
      CONSTRAINT [df_gb_recorder_claim_app] DEFAULT N'';
IF COL_LENGTH(N'gb_recorder_claim', N'stream') IS NULL
  ALTER TABLE [gb_recorder_claim]
    ADD [stream] NVARCHAR(64) NOT NULL
      CONSTRAINT [df_gb_recorder_claim_stream] DEFAULT N'';
