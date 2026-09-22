IF OBJECT_ID(N'gb_play_lifecycle_event', N'U') IS NOT NULL DROP TABLE [gb_play_lifecycle_event];
IF OBJECT_ID(N'gb_play_attempt', N'U') IS NOT NULL AND COL_LENGTH(N'gb_play_attempt', N'lifecycle_state') IS NOT NULL
BEGIN
  DROP INDEX [idx_play_attempt_lifecycle_started] ON [gb_play_attempt];
  DROP INDEX [idx_play_attempt_stream_node] ON [gb_play_attempt];
  ALTER TABLE [gb_play_attempt] DROP CONSTRAINT [df_play_attempt_stream_id], [df_play_attempt_ssrc], [df_play_attempt_call_id],
    [df_play_attempt_cseq], [df_play_attempt_current_stage], [df_play_attempt_media_state], [df_play_attempt_client_state],
    [df_play_attempt_lifecycle_state], [df_play_attempt_reason_code], [df_play_attempt_reason_message], [df_play_attempt_client_error_code];
  ALTER TABLE [gb_play_attempt] DROP COLUMN [last_event_at], [client_error_code], [client_error_at], [client_first_frame_at],
    [reason_message], [reason_code], [lifecycle_state], [client_state], [media_state], [current_stage], [cseq], [call_id], [ssrc], [stream_id];
END;
