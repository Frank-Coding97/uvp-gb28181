CREATE TABLE IF NOT EXISTS gb_sip_trace_capture (
    id CHAR(36) PRIMARY KEY,
    device_id BIGINT NOT NULL,
    device_code VARCHAR(20) NOT NULL,
    created_by BIGINT NOT NULL,
    started_at TIMESTAMP(3) WITH TIME ZONE NOT NULL,
    planned_end_at TIMESTAMP(3) WITH TIME ZONE NOT NULL,
    ended_at TIMESTAMP(3) WITH TIME ZONE NULL,
    end_reason VARCHAR(16) NOT NULL DEFAULT '',
    active_key VARCHAR(64) NULL,
    created_at TIMESTAMP(3) WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP(3) WITH TIME ZONE NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_sip_trace_capture_active ON gb_sip_trace_capture (active_key);
CREATE INDEX IF NOT EXISTS idx_sip_trace_capture_device_started ON gb_sip_trace_capture (device_id, started_at);
CREATE INDEX IF NOT EXISTS idx_sip_trace_capture_device_code ON gb_sip_trace_capture (device_code);
CREATE INDEX IF NOT EXISTS idx_sip_trace_capture_created_by ON gb_sip_trace_capture (created_by);
CREATE INDEX IF NOT EXISTS idx_sip_trace_capture_planned_end ON gb_sip_trace_capture (planned_end_at);
