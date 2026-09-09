-- Persist the original recorder owner and exact media identity.
-- Existing rows receive neutral defaults and are never adopted automatically.
-- PostgreSQL 12+, repeatable.
ALTER TABLE gb_recording_plan_channel_state
  ADD COLUMN IF NOT EXISTS recorder_owner_kind VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE gb_recording_plan_channel_state
  ADD COLUMN IF NOT EXISTS recorder_owner_id VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE gb_recording_plan_channel_state
  ADD COLUMN IF NOT EXISTS recorder_claim_version BIGINT NOT NULL DEFAULT 0;

ALTER TABLE gb_recorder_claim
  ADD COLUMN IF NOT EXISTS channel_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE gb_recorder_claim
  ADD COLUMN IF NOT EXISTS node_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE gb_recorder_claim
  ADD COLUMN IF NOT EXISTS v_host VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE gb_recorder_claim
  ADD COLUMN IF NOT EXISTS app VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE gb_recorder_claim
  ADD COLUMN IF NOT EXISTS stream VARCHAR(64) NOT NULL DEFAULT '';
