CREATE TABLE IF NOT EXISTS gb_sip_trace_session_diagnosis (
    id BIGSERIAL PRIMARY KEY,
    session_day DATE NOT NULL,
    observed_at TIMESTAMP(6) WITH TIME ZONE NOT NULL,
    correlation_key VARCHAR(128) NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'active',
    category VARCHAR(32) NOT NULL,
    code VARCHAR(64) NOT NULL,
    stage VARCHAR(32) NOT NULL,
    source VARCHAR(32) NOT NULL,
    device_id VARCHAR(64) NOT NULL DEFAULT '',
    channel_id VARCHAR(64) NOT NULL DEFAULT '',
    call_id VARCHAR(255) NOT NULL DEFAULT '',
    cseq INTEGER NOT NULL DEFAULT 0,
    method VARCHAR(32) NOT NULL DEFAULT '',
    status_code SMALLINT NOT NULL DEFAULT 0,
    stream_id VARCHAR(255) NOT NULL DEFAULT '',
    resolved_at TIMESTAMP(6) WITH TIME ZONE NULL,
    CONSTRAINT uk_sip_trace_diagnosis_session UNIQUE (session_day, category, correlation_key)
);
CREATE INDEX IF NOT EXISTS idx_sip_trace_diagnosis_category_state_observed
    ON gb_sip_trace_session_diagnosis (session_day, category, state, observed_at);
CREATE INDEX IF NOT EXISTS idx_sip_trace_diagnosis_device_observed
    ON gb_sip_trace_session_diagnosis (device_id, observed_at);
CREATE INDEX IF NOT EXISTS idx_sip_trace_diagnosis_call_cseq
    ON gb_sip_trace_session_diagnosis (call_id, cseq);
