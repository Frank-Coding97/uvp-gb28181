-- Additive work recording schema. Existing recording history is retained.
IF OBJECT_ID(N'gb_work_recording', N'U') IS NULL
CREATE TABLE gb_work_recording (
 id NVARCHAR(36) NOT NULL PRIMARY KEY,
 channel_id BIGINT NOT NULL,
 created_by BIGINT NOT NULL,
 request_id NVARCHAR(128) NOT NULL,
 state NVARCHAR(20) NOT NULL,
 desired_action NVARCHAR(20) NOT NULL,
 version BIGINT NOT NULL,
 node_id BIGINT NOT NULL DEFAULT 0,
 v_host NVARCHAR(128) NOT NULL DEFAULT '',
 app NVARCHAR(64) NOT NULL DEFAULT '',
 stream NVARCHAR(64) NOT NULL DEFAULT '',
 generation BIGINT NOT NULL DEFAULT 0,
 started_at DATETIME2 NULL,
 stopped_at DATETIME2 NULL,
 last_checked_at DATETIME2 NULL,
 last_error NVARCHAR(500) NOT NULL DEFAULT '',
 file_state NVARCHAR(20) NOT NULL DEFAULT 'pending',
 form_state NVARCHAR(20) NOT NULL DEFAULT 'draft',
 form_version BIGINT NOT NULL DEFAULT 0,
 schema_version BIGINT NOT NULL DEFAULT 1,
 device_id NVARCHAR(20) NOT NULL DEFAULT '',
 form_json NVARCHAR(MAX) NOT NULL,
 created_at DATETIME2 NULL,
 updated_at DATETIME2 NULL,
 CONSTRAINT uk_work_recording_request UNIQUE(created_by, request_id)
);

IF OBJECT_ID(N'gb_recorder_claim', N'U') IS NULL
CREATE TABLE gb_recorder_claim (
 resource_key NVARCHAR(64) NOT NULL PRIMARY KEY,
 owner_kind NVARCHAR(20) NOT NULL,
 owner_id NVARCHAR(128) NOT NULL,
 state NVARCHAR(20) NOT NULL,
 version BIGINT NOT NULL,
 generation BIGINT NOT NULL DEFAULT 0,
 created_at DATETIME2 NULL,
 updated_at DATETIME2 NULL
);

IF OBJECT_ID(N'gb_work_recording_file', N'U') IS NULL
CREATE TABLE gb_work_recording_file (
 file_id BIGINT NOT NULL PRIMARY KEY,
 work_recording_id NVARCHAR(36) NOT NULL,
 evidence NVARCHAR(500) NOT NULL DEFAULT '',
 created_at DATETIME2 NULL
);
