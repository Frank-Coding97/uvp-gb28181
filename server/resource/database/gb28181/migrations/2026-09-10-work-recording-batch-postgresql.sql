ALTER TABLE gb_work_recording ADD COLUMN IF NOT EXISTS batch_id VARCHAR(36) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS gb_work_recording_batch (
 id VARCHAR(36) NOT NULL PRIMARY KEY,
 created_by BIGINT NOT NULL,
 request_id VARCHAR(128) NOT NULL,
 state VARCHAR(20) NOT NULL,
 version BIGINT NOT NULL DEFAULT 1,
 form_state VARCHAR(20) NOT NULL DEFAULT 'draft',
 form_version BIGINT NOT NULL DEFAULT 0,
 schema_version BIGINT NOT NULL DEFAULT 1,
 device_id VARCHAR(20) NOT NULL DEFAULT '',
 form_json TEXT NOT NULL,
 last_error VARCHAR(500) NOT NULL DEFAULT '',
 created_at TIMESTAMP NULL,
 updated_at TIMESTAMP NULL,
 CONSTRAINT uk_work_recording_batch_request UNIQUE(created_by, request_id)
);

CREATE INDEX IF NOT EXISTS idx_work_recording_batch ON gb_work_recording(batch_id);
