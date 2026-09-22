DROP TABLE IF EXISTS gb_play_lifecycle_event;
DROP INDEX IF EXISTS idx_play_attempt_lifecycle_started;
DROP INDEX IF EXISTS idx_play_attempt_stream_node;
ALTER TABLE IF EXISTS gb_play_attempt DROP COLUMN IF EXISTS last_event_at, DROP COLUMN IF EXISTS client_error_code,
  DROP COLUMN IF EXISTS client_error_at, DROP COLUMN IF EXISTS client_first_frame_at, DROP COLUMN IF EXISTS reason_message,
  DROP COLUMN IF EXISTS reason_code, DROP COLUMN IF EXISTS lifecycle_state, DROP COLUMN IF EXISTS client_state,
  DROP COLUMN IF EXISTS media_state, DROP COLUMN IF EXISTS current_stage, DROP COLUMN IF EXISTS cseq,
  DROP COLUMN IF EXISTS call_id, DROP COLUMN IF EXISTS ssrc, DROP COLUMN IF EXISTS stream_id;
