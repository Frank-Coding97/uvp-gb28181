-- 2026-07-24: GB28181 home-position response lifecycle (PostgreSQL).
-- Idempotent upgrade: legacy gb_ptz_state.home_* columns are intentionally retained.

CREATE TABLE IF NOT EXISTS gb_ptz_operation (
    id BIGSERIAL,
    operation_id VARCHAR(64) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    device_id BIGINT NOT NULL,
    device_code VARCHAR(20) NOT NULL,
    channel_id BIGINT NOT NULL,
    channel_code VARCHAR(20) NOT NULL,
    cmd_type VARCHAR(64) NOT NULL,
    action VARCHAR(64),
    payload_json TEXT,
    sn INTEGER NOT NULL,
    call_id VARCHAR(255),
    cseq VARCHAR(64),
    sip_status INTEGER NOT NULL DEFAULT 0,
    device_result VARCHAR(32),
    device_error TEXT,
    status VARCHAR(16) NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 1,
    response_required BOOLEAN NOT NULL DEFAULT FALSE,
    max_attempts INTEGER NOT NULL DEFAULT 1,
    error_code VARCHAR(64),
    error_message TEXT,
    actor_id BIGINT NOT NULL DEFAULT 0,
    actor_dept_id BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP(3) NOT NULL,
    sent_at TIMESTAMP(3),
    completed_at TIMESTAMP(3),
    queue_deadline_at TIMESTAMP(3),
    dispatch_started_at TIMESTAMP(3),
    transport_deadline_at TIMESTAMP(3),
    deadline_at TIMESTAMP(3),
    next_attempt_at TIMESTAMP(3),
    response_call_id VARCHAR(255),
    response_cseq VARCHAR(64),
    response_at TIMESTAMP(3),
    response_has_data BOOLEAN,
    trigger_operation_id VARCHAR(64),
    reconcile_operation_id VARCHAR(64),
    PRIMARY KEY (id),
    CONSTRAINT uk_ptz_operation_id UNIQUE (operation_id),
    CONSTRAINT uk_ptz_operation_idempotency UNIQUE (channel_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS gb_ptz_state (
    id BIGSERIAL,
    device_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    channel_code VARCHAR(20) NOT NULL,
    pan NUMERIC(18,6),
    tilt NUMERIC(18,6),
    zoom NUMERIC(18,6),
    focus NUMERIC(18,6),
    iris NUMERIC(18,6),
    device_time TIMESTAMP(3),
    received_at TIMESTAMP(3) NOT NULL,
    source_sn INTEGER NOT NULL DEFAULT 0,
    freshness VARCHAR(16) NOT NULL DEFAULT 'unknown',
    dedupe_key VARCHAR(128),
    raw_summary TEXT,
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_ptz_state_channel UNIQUE (channel_id)
);

CREATE TABLE IF NOT EXISTS gb_ptz_preset (
    id BIGSERIAL,
    device_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    preset_id INTEGER NOT NULL,
    name VARCHAR(255),
    status VARCHAR(16) NOT NULL DEFAULT 'unknown',
    last_operation_id VARCHAR(64),
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_ptz_preset_channel_number UNIQUE (channel_id, preset_id)
);

CREATE TABLE IF NOT EXISTS gb_ptz_cruise_track (
    id BIGSERIAL,
    device_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    track_id INTEGER NOT NULL,
    name VARCHAR(255),
    enabled BOOLEAN,
    detail_json TEXT,
    raw_summary TEXT,
    device_time TIMESTAMP(3),
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_ptz_cruise_channel_track UNIQUE (channel_id, track_id)
);

CREATE TABLE IF NOT EXISTS gb_ptz_operation_attempt (
    id BIGSERIAL,
    operation_id BIGINT NOT NULL,
    attempt_no INTEGER NOT NULL,
    sn INTEGER NOT NULL,
    status VARCHAR(16) NOT NULL,
    call_id VARCHAR(255),
    cseq VARCHAR(64),
    sip_status INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMP(3) NOT NULL,
    lease_until TIMESTAMP(3) NOT NULL,
    sent_at TIMESTAMP(3),
    completed_at TIMESTAMP(3),
    error_code VARCHAR(64),
    error_message TEXT,
    created_at TIMESTAMP(3) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_ptz_operation_attempt UNIQUE (operation_id, attempt_no)
);

CREATE TABLE IF NOT EXISTS gb_ptz_home_position (
    id BIGSERIAL,
    device_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    channel_code VARCHAR(20) NOT NULL,
    enabled BOOLEAN NOT NULL,
    reset_time INTEGER,
    preset_id INTEGER,
    enabled_encoding VARCHAR(32) NOT NULL DEFAULT 'numeric',
    confirmed_at TIMESTAMP(3) NOT NULL,
    source VARCHAR(32) NOT NULL,
    verification VARCHAR(16) NOT NULL,
    source_sn INTEGER NOT NULL DEFAULT 0,
    source_operation_id VARCHAR(64),
    source_operation_seq BIGINT NOT NULL DEFAULT 0,
    raw_summary TEXT,
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_ptz_home_position_channel UNIQUE (channel_id)
);

ALTER TABLE gb_ptz_operation
    ADD COLUMN IF NOT EXISTS response_required BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS queue_deadline_at TIMESTAMP(3),
    ADD COLUMN IF NOT EXISTS dispatch_started_at TIMESTAMP(3),
    ADD COLUMN IF NOT EXISTS transport_deadline_at TIMESTAMP(3),
    ADD COLUMN IF NOT EXISTS deadline_at TIMESTAMP(3),
    ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMP(3),
    ADD COLUMN IF NOT EXISTS response_call_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS response_cseq VARCHAR(64),
    ADD COLUMN IF NOT EXISTS response_at TIMESTAMP(3),
    ADD COLUMN IF NOT EXISTS response_has_data BOOLEAN,
    ADD COLUMN IF NOT EXISTS trigger_operation_id VARCHAR(64),
    ADD COLUMN IF NOT EXISTS reconcile_operation_id VARCHAR(64);

CREATE UNIQUE INDEX IF NOT EXISTS uk_ptz_operation_id ON gb_ptz_operation (operation_id);
CREATE UNIQUE INDEX IF NOT EXISTS uk_ptz_operation_idempotency ON gb_ptz_operation (channel_id, idempotency_key);
CREATE INDEX IF NOT EXISTS idx_ptz_operation_channel_time ON gb_ptz_operation (channel_id, created_at);
CREATE INDEX IF NOT EXISTS idx_ptz_operation_channel_cmd_id ON gb_ptz_operation (channel_id, cmd_type, id);
CREATE INDEX IF NOT EXISTS idx_ptz_operation_device_sn ON gb_ptz_operation (device_id, sn);
CREATE INDEX IF NOT EXISTS idx_ptz_operation_status_time ON gb_ptz_operation (status, created_at);
CREATE INDEX IF NOT EXISTS idx_ptz_operation_status_next_attempt ON gb_ptz_operation (status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_ptz_operation_status_queue_deadline ON gb_ptz_operation (status, queue_deadline_at);
CREATE INDEX IF NOT EXISTS idx_ptz_operation_status_transport_deadline ON gb_ptz_operation (status, transport_deadline_at);
CREATE INDEX IF NOT EXISTS idx_ptz_operation_status_deadline ON gb_ptz_operation (status, deadline_at);
CREATE INDEX IF NOT EXISTS idx_ptz_operation_call_id ON gb_ptz_operation (call_id);
CREATE UNIQUE INDEX IF NOT EXISTS uk_ptz_state_channel ON gb_ptz_state (channel_id);
CREATE INDEX IF NOT EXISTS idx_ptz_state_device ON gb_ptz_state (device_id);
CREATE INDEX IF NOT EXISTS idx_ptz_state_received ON gb_ptz_state (received_at);
CREATE UNIQUE INDEX IF NOT EXISTS uk_ptz_preset_channel_number ON gb_ptz_preset (channel_id, preset_id);
CREATE INDEX IF NOT EXISTS idx_ptz_preset_device ON gb_ptz_preset (device_id);
CREATE UNIQUE INDEX IF NOT EXISTS uk_ptz_cruise_channel_track ON gb_ptz_cruise_track (channel_id, track_id);
CREATE INDEX IF NOT EXISTS idx_ptz_cruise_device ON gb_ptz_cruise_track (device_id);
CREATE UNIQUE INDEX IF NOT EXISTS uk_ptz_operation_attempt ON gb_ptz_operation_attempt (operation_id, attempt_no);
CREATE INDEX IF NOT EXISTS idx_ptz_attempt_status_lease ON gb_ptz_operation_attempt (status, lease_until);
CREATE UNIQUE INDEX IF NOT EXISTS uk_ptz_home_position_channel ON gb_ptz_home_position (channel_id);
CREATE INDEX IF NOT EXISTS idx_ptz_home_position_device ON gb_ptz_home_position (device_id);

UPDATE gb_ptz_operation
SET status = 'unknown',
    error_code = 'TRANSPORT_UNKNOWN',
    error_message = COALESCE(NULLIF(error_message, ''), 'legacy home-position operation upgraded without response metadata'),
    completed_at = COALESCE(completed_at, CURRENT_TIMESTAMP)
WHERE action IN ('home_position', 'refresh_home_position')
  AND status IN ('queued', 'sent')
  AND response_required = FALSE
  AND completed_at IS NULL;
