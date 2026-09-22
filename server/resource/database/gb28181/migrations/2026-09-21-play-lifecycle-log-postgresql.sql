ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS stream_id varchar(128) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS ssrc varchar(32) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS call_id varchar(128) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS cseq varchar(32) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS current_stage varchar(32) NOT NULL DEFAULT 'unknown';
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS media_state varchar(32) NOT NULL DEFAULT 'unknown';
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS client_state varchar(32) NOT NULL DEFAULT 'unknown';
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS lifecycle_state varchar(32) NOT NULL DEFAULT 'in_progress';
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS reason_code varchar(64) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS reason_message varchar(256) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS client_first_frame_at timestamp(3) NULL;
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS client_error_at timestamp(3) NULL;
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS client_error_code varchar(64) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS gb_play_attempt ADD COLUMN IF NOT EXISTS last_event_at timestamp(3) NULL;
CREATE INDEX IF NOT EXISTS idx_play_attempt_lifecycle_started ON gb_play_attempt (lifecycle_state,started_at);
CREATE INDEX IF NOT EXISTS idx_play_attempt_stream_node ON gb_play_attempt (stream_id,node_id);

CREATE TABLE IF NOT EXISTS gb_play_lifecycle_event (
  id bigserial PRIMARY KEY, event_id varchar(64) NOT NULL, lifecycle_id varchar(64) NOT NULL, sequence bigint NOT NULL,
  event_at timestamp(3) NOT NULL, elapsed_ms bigint NOT NULL DEFAULT 0, stage varchar(32) NOT NULL,
  event_name varchar(64) NOT NULL, fact_state varchar(32) NOT NULL, source varchar(32) NOT NULL,
  device_code varchar(20) NOT NULL, channel_code varchar(20) NOT NULL, stream_id varchar(128) NOT NULL DEFAULT '',
  node_id bigint NOT NULL DEFAULT 0, ssrc varchar(32) NOT NULL DEFAULT '', reused boolean NOT NULL DEFAULT false,
  call_id varchar(128) NOT NULL DEFAULT '', cseq varchar(32) NOT NULL DEFAULT '', reason_code varchar(64) NOT NULL DEFAULT '',
  reason_message varchar(256) NOT NULL DEFAULT '', metadata_json jsonb NULL, created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT uk_play_lifecycle_event_id UNIQUE (event_id),
  CONSTRAINT uk_play_lifecycle_sequence UNIQUE (lifecycle_id,sequence)
);
CREATE INDEX IF NOT EXISTS idx_play_lifecycle_event_lifecycle_sequence ON gb_play_lifecycle_event (lifecycle_id,sequence);
CREATE INDEX IF NOT EXISTS idx_play_lifecycle_event_at ON gb_play_lifecycle_event (event_at);
CREATE INDEX IF NOT EXISTS idx_play_lifecycle_event_device_at ON gb_play_lifecycle_event (device_code,event_at);
CREATE INDEX IF NOT EXISTS idx_play_lifecycle_event_stream_node ON gb_play_lifecycle_event (stream_id,node_id);
CREATE INDEX IF NOT EXISTS idx_play_lifecycle_event_stage ON gb_play_lifecycle_event (stage,fact_state);
