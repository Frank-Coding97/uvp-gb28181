-- Persist the SSRC of the current live generation independently from stream_id.
-- PostgreSQL 12+, repeatable.
ALTER TABLE IF EXISTS gb_channel
  ADD COLUMN IF NOT EXISTS current_ssrc VARCHAR(10) NOT NULL DEFAULT '';

COMMENT ON COLUMN gb_channel.current_ssrc IS '当前实时媒体会话SSRC';
