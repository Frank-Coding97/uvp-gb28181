IF OBJECT_ID(N'gb_sip_trace_capture', N'U') IS NULL
BEGIN
    CREATE TABLE gb_sip_trace_capture (
        id CHAR(36) NOT NULL CONSTRAINT pk_gb_sip_trace_capture PRIMARY KEY,
        device_id BIGINT NOT NULL,
        device_code VARCHAR(20) NOT NULL,
        created_by BIGINT NOT NULL,
        started_at DATETIME2(3) NOT NULL,
        planned_end_at DATETIME2(3) NOT NULL,
        ended_at DATETIME2(3) NULL,
        end_reason VARCHAR(16) NOT NULL CONSTRAINT df_gb_sip_trace_capture_reason DEFAULT '',
        active_key VARCHAR(64) NULL,
        created_at DATETIME2(3) NOT NULL,
        updated_at DATETIME2(3) NOT NULL
    );
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'uk_sip_trace_capture_active' AND object_id = OBJECT_ID(N'gb_sip_trace_capture')) CREATE UNIQUE INDEX uk_sip_trace_capture_active ON gb_sip_trace_capture (active_key) WHERE active_key IS NOT NULL;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_sip_trace_capture_device_started' AND object_id = OBJECT_ID(N'gb_sip_trace_capture')) CREATE INDEX idx_sip_trace_capture_device_started ON gb_sip_trace_capture (device_id, started_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_sip_trace_capture_device_code' AND object_id = OBJECT_ID(N'gb_sip_trace_capture')) CREATE INDEX idx_sip_trace_capture_device_code ON gb_sip_trace_capture (device_code);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_sip_trace_capture_created_by' AND object_id = OBJECT_ID(N'gb_sip_trace_capture')) CREATE INDEX idx_sip_trace_capture_created_by ON gb_sip_trace_capture (created_by);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_sip_trace_capture_planned_end' AND object_id = OBJECT_ID(N'gb_sip_trace_capture')) CREATE INDEX idx_sip_trace_capture_planned_end ON gb_sip_trace_capture (planned_end_at);
