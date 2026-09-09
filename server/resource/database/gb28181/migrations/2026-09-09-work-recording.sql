-- Additive work recording schema. Existing recording history is retained.
CREATE TABLE IF NOT EXISTS gb_work_recording (
 id VARCHAR(36) NOT NULL PRIMARY KEY,
 channel_id BIGINT NOT NULL,
 created_by BIGINT NOT NULL,
 request_id VARCHAR(128) NOT NULL,
 state VARCHAR(20) NOT NULL,
 desired_action VARCHAR(20) NOT NULL,
 version BIGINT NOT NULL,
 node_id BIGINT NOT NULL DEFAULT 0,
 v_host VARCHAR(128) NOT NULL DEFAULT '',
 app VARCHAR(64) NOT NULL DEFAULT '',
 stream VARCHAR(64) NOT NULL DEFAULT '',
 generation BIGINT NOT NULL DEFAULT 0,
 started_at DATETIME(6) NULL,
 stopped_at DATETIME(6) NULL,
 last_checked_at DATETIME(6) NULL,
 last_error VARCHAR(500) NOT NULL DEFAULT '',
 file_state VARCHAR(20) NOT NULL DEFAULT 'pending',
 form_state VARCHAR(20) NOT NULL DEFAULT 'draft',
 form_version BIGINT NOT NULL DEFAULT 0,
 schema_version BIGINT NOT NULL DEFAULT 1,
 device_id VARCHAR(20) NOT NULL DEFAULT '',
 form_json LONGTEXT NOT NULL,
 created_at DATETIME(6) NULL,
 updated_at DATETIME(6) NULL,
 CONSTRAINT uk_work_recording_request UNIQUE(created_by, request_id)
);

CREATE TABLE IF NOT EXISTS gb_recorder_claim (
 resource_key VARCHAR(64) NOT NULL PRIMARY KEY,
 owner_kind VARCHAR(20) NOT NULL,
 owner_id VARCHAR(128) NOT NULL,
 state VARCHAR(20) NOT NULL,
 version BIGINT NOT NULL,
 generation BIGINT NOT NULL DEFAULT 0,
 created_at DATETIME(6) NULL,
 updated_at DATETIME(6) NULL
);

CREATE TABLE IF NOT EXISTS gb_work_recording_file (
 file_id BIGINT NOT NULL PRIMARY KEY,
 work_recording_id VARCHAR(36) NOT NULL,
 evidence VARCHAR(500) NOT NULL DEFAULT '',
 created_at DATETIME(6) NULL
);
