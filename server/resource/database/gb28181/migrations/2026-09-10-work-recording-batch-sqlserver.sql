IF COL_LENGTH(N'gb_work_recording', N'batch_id') IS NULL
ALTER TABLE gb_work_recording ADD batch_id NVARCHAR(36) NOT NULL CONSTRAINT df_work_recording_batch_id DEFAULT N'';

IF OBJECT_ID(N'gb_work_recording_batch', N'U') IS NULL
CREATE TABLE gb_work_recording_batch (
 id NVARCHAR(36) NOT NULL PRIMARY KEY,
 created_by BIGINT NOT NULL,
 request_id NVARCHAR(128) NOT NULL,
 state NVARCHAR(20) NOT NULL,
 version BIGINT NOT NULL DEFAULT 1,
 form_state NVARCHAR(20) NOT NULL DEFAULT N'draft',
 form_version BIGINT NOT NULL DEFAULT 0,
 schema_version BIGINT NOT NULL DEFAULT 1,
 device_id NVARCHAR(20) NOT NULL DEFAULT N'',
 form_json NVARCHAR(MAX) NOT NULL,
 last_error NVARCHAR(500) NOT NULL DEFAULT N'',
 created_at DATETIME2 NULL,
 updated_at DATETIME2 NULL,
 CONSTRAINT uk_work_recording_batch_request UNIQUE(created_by, request_id)
);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_work_recording_batch' AND object_id = OBJECT_ID(N'gb_work_recording'))
CREATE INDEX idx_work_recording_batch ON gb_work_recording(batch_id);
