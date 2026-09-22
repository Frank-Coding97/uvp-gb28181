IF OBJECT_ID(N'gb_play_attempt', N'U') IS NOT NULL AND COL_LENGTH(N'gb_play_attempt', N'lifecycle_state') IS NULL
BEGIN
  ALTER TABLE [gb_play_attempt] ADD
    [stream_id] nvarchar(128) NOT NULL CONSTRAINT [df_play_attempt_stream_id] DEFAULT N'' WITH VALUES,
    [ssrc] nvarchar(32) NOT NULL CONSTRAINT [df_play_attempt_ssrc] DEFAULT N'' WITH VALUES,
    [call_id] nvarchar(128) NOT NULL CONSTRAINT [df_play_attempt_call_id] DEFAULT N'' WITH VALUES,
    [cseq] nvarchar(32) NOT NULL CONSTRAINT [df_play_attempt_cseq] DEFAULT N'' WITH VALUES,
    [current_stage] nvarchar(32) NOT NULL CONSTRAINT [df_play_attempt_current_stage] DEFAULT N'unknown' WITH VALUES,
    [media_state] nvarchar(32) NOT NULL CONSTRAINT [df_play_attempt_media_state] DEFAULT N'unknown' WITH VALUES,
    [client_state] nvarchar(32) NOT NULL CONSTRAINT [df_play_attempt_client_state] DEFAULT N'unknown' WITH VALUES,
    [lifecycle_state] nvarchar(32) NOT NULL CONSTRAINT [df_play_attempt_lifecycle_state] DEFAULT N'in_progress' WITH VALUES,
    [reason_code] nvarchar(64) NOT NULL CONSTRAINT [df_play_attempt_reason_code] DEFAULT N'' WITH VALUES,
    [reason_message] nvarchar(256) NOT NULL CONSTRAINT [df_play_attempt_reason_message] DEFAULT N'' WITH VALUES,
    [client_first_frame_at] datetime2(3) NULL, [client_error_at] datetime2(3) NULL,
    [client_error_code] nvarchar(64) NOT NULL CONSTRAINT [df_play_attempt_client_error_code] DEFAULT N'' WITH VALUES,
    [last_event_at] datetime2(3) NULL;
  CREATE INDEX [idx_play_attempt_lifecycle_started] ON [gb_play_attempt] ([lifecycle_state],[started_at]);
  CREATE INDEX [idx_play_attempt_stream_node] ON [gb_play_attempt] ([stream_id],[node_id]);
END;

IF OBJECT_ID(N'gb_play_lifecycle_event', N'U') IS NULL
BEGIN
  CREATE TABLE [gb_play_lifecycle_event] (
    [id] bigint IDENTITY(1,1) NOT NULL PRIMARY KEY, [event_id] nvarchar(64) NOT NULL,
    [lifecycle_id] nvarchar(64) NOT NULL, [sequence] bigint NOT NULL, [event_at] datetime2(3) NOT NULL,
    [elapsed_ms] bigint NOT NULL CONSTRAINT [df_play_lifecycle_elapsed] DEFAULT 0, [stage] nvarchar(32) NOT NULL,
    [event_name] nvarchar(64) NOT NULL, [fact_state] nvarchar(32) NOT NULL, [source] nvarchar(32) NOT NULL,
    [device_code] nvarchar(20) NOT NULL, [channel_code] nvarchar(20) NOT NULL,
    [stream_id] nvarchar(128) NOT NULL CONSTRAINT [df_play_lifecycle_stream] DEFAULT N'',
    [node_id] bigint NOT NULL CONSTRAINT [df_play_lifecycle_node] DEFAULT 0,
    [ssrc] nvarchar(32) NOT NULL CONSTRAINT [df_play_lifecycle_ssrc] DEFAULT N'',
    [reused] bit NOT NULL CONSTRAINT [df_play_lifecycle_reused] DEFAULT 0,
    [call_id] nvarchar(128) NOT NULL CONSTRAINT [df_play_lifecycle_call] DEFAULT N'',
    [cseq] nvarchar(32) NOT NULL CONSTRAINT [df_play_lifecycle_cseq] DEFAULT N'',
    [reason_code] nvarchar(64) NOT NULL CONSTRAINT [df_play_lifecycle_reason_code] DEFAULT N'',
    [reason_message] nvarchar(256) NOT NULL CONSTRAINT [df_play_lifecycle_reason_message] DEFAULT N'',
    [metadata_json] nvarchar(max) NULL, [created_at] datetime2(3) NOT NULL CONSTRAINT [df_play_lifecycle_created] DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [uk_play_lifecycle_event_id] UNIQUE ([event_id]),
    CONSTRAINT [uk_play_lifecycle_sequence] UNIQUE ([lifecycle_id],[sequence])
  );
  CREATE INDEX [idx_play_lifecycle_event_lifecycle_sequence] ON [gb_play_lifecycle_event] ([lifecycle_id],[sequence]);
  CREATE INDEX [idx_play_lifecycle_event_at] ON [gb_play_lifecycle_event] ([event_at]);
  CREATE INDEX [idx_play_lifecycle_event_device_at] ON [gb_play_lifecycle_event] ([device_code],[event_at]);
  CREATE INDEX [idx_play_lifecycle_event_stream_node] ON [gb_play_lifecycle_event] ([stream_id],[node_id]);
  CREATE INDEX [idx_play_lifecycle_event_stage] ON [gb_play_lifecycle_event] ([stage],[fact_state]);
END;
