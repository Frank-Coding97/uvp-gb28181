IF OBJECT_ID(N'gb_sip_trace_message', N'U') IS NULL
BEGIN
    CREATE TABLE gb_sip_trace_message (
        event_id VARCHAR(36) NOT NULL CONSTRAINT pk_gb_sip_trace_message PRIMARY KEY,
        occurred_at DATETIME2(6) NOT NULL,
        direction VARCHAR(16) NOT NULL,
        transport VARCHAR(16) NOT NULL,
        local_addr VARCHAR(255) NOT NULL,
        remote_addr VARCHAR(255) NOT NULL,
        device_id VARCHAR(64) NOT NULL,
        method VARCHAR(32) NOT NULL,
        status_code SMALLINT NOT NULL,
        call_id VARCHAR(255) NOT NULL,
        cseq INT NOT NULL,
        cseq_method VARCHAR(32) NOT NULL,
        from_uri VARCHAR(512) NOT NULL,
        to_uri VARCHAR(512) NOT NULL,
        from_id VARCHAR(64) NOT NULL CONSTRAINT df_sip_trace_from_id DEFAULT '',
        to_id VARCHAR(64) NOT NULL CONSTRAINT df_sip_trace_to_id DEFAULT '',
        business_code VARCHAR(64) NOT NULL CONSTRAINT df_sip_trace_business_code DEFAULT 'unknown',
        business_type NVARCHAR(64) NOT NULL CONSTRAINT df_sip_trace_business_type DEFAULT N'未知业务',
        business_confidence VARCHAR(16) NOT NULL CONSTRAINT df_sip_trace_business_confidence DEFAULT 'none',
        user_agent VARCHAR(512) NOT NULL,
        malformed BIT NOT NULL CONSTRAINT df_gb_sip_trace_malformed DEFAULT 0,
        parse_error VARCHAR(1024) NOT NULL,
        payload_nonce VARBINARY(MAX) NOT NULL,
        payload_ciphertext VARBINARY(MAX) NOT NULL,
        payload_algorithm VARCHAR(32) NOT NULL,
        payload_key_version VARCHAR(64) NOT NULL,
        payload_digest_sha256 CHAR(64) NOT NULL
    );
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_gb_sip_trace_occurred_event' AND object_id = OBJECT_ID(N'gb_sip_trace_message')) CREATE INDEX idx_gb_sip_trace_occurred_event ON gb_sip_trace_message (occurred_at, event_id);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_gb_sip_trace_device_occurred' AND object_id = OBJECT_ID(N'gb_sip_trace_message')) CREATE INDEX idx_gb_sip_trace_device_occurred ON gb_sip_trace_message (device_id, occurred_at, event_id);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_gb_sip_trace_call_occurred' AND object_id = OBJECT_ID(N'gb_sip_trace_message')) CREATE INDEX idx_gb_sip_trace_call_occurred ON gb_sip_trace_message (call_id, occurred_at, event_id);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_gb_sip_trace_business_occurred' AND object_id = OBJECT_ID(N'gb_sip_trace_message')) CREATE INDEX idx_gb_sip_trace_business_occurred ON gb_sip_trace_message (business_code, occurred_at);
