CREATE TABLE IF NOT EXISTS gb_sip_trace_message (
    event_id VARCHAR(36) PRIMARY KEY,
    occurred_at TIMESTAMP(6) WITH TIME ZONE NOT NULL,
    direction VARCHAR(16) NOT NULL,
    transport VARCHAR(16) NOT NULL,
    local_addr VARCHAR(255) NOT NULL,
    remote_addr VARCHAR(255) NOT NULL,
    device_id VARCHAR(64) NOT NULL,
    method VARCHAR(32) NOT NULL,
    status_code SMALLINT NOT NULL,
    call_id VARCHAR(255) NOT NULL,
    cseq INTEGER NOT NULL,
    cseq_method VARCHAR(32) NOT NULL,
    from_uri VARCHAR(512) NOT NULL,
    to_uri VARCHAR(512) NOT NULL,
    from_id VARCHAR(64) NOT NULL DEFAULT '',
    to_id VARCHAR(64) NOT NULL DEFAULT '',
    business_code VARCHAR(64) NOT NULL DEFAULT 'unknown',
    business_type VARCHAR(64) NOT NULL DEFAULT '未知业务',
    business_confidence VARCHAR(16) NOT NULL DEFAULT 'none',
    user_agent VARCHAR(512) NOT NULL,
    malformed BOOLEAN NOT NULL DEFAULT FALSE,
    parse_error VARCHAR(1024) NOT NULL,
    payload_nonce BYTEA NOT NULL,
    payload_ciphertext BYTEA NOT NULL,
    payload_algorithm VARCHAR(32) NOT NULL,
    payload_key_version VARCHAR(64) NOT NULL,
    payload_digest_sha256 CHAR(64) NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_gb_sip_trace_occurred_event ON gb_sip_trace_message (occurred_at, event_id);
CREATE INDEX IF NOT EXISTS idx_gb_sip_trace_device_occurred ON gb_sip_trace_message (device_id, occurred_at, event_id);
CREATE INDEX IF NOT EXISTS idx_gb_sip_trace_call_occurred ON gb_sip_trace_message (call_id, occurred_at, event_id);
CREATE INDEX IF NOT EXISTS idx_gb_sip_trace_business_occurred ON gb_sip_trace_message (business_code, occurred_at);
