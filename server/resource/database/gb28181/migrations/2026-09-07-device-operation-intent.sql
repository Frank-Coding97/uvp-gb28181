-- Durable reservation only. dispatched means may-have-dispatched, not success.
-- Append-only safety history; no cascading deletion or automatic completion.
CREATE TABLE IF NOT EXISTS gb_device_operation_intent (
    operation_id VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    contract_version BIGINT NOT NULL,
    device_pk BIGINT NOT NULL,
    device_code VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    device_epoch BIGINT NOT NULL,
    target_scope VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_pk BIGINT NOT NULL,
    target_code VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    kind VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    state VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    row_version BIGINT NOT NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    dispatch_started_at DATETIME(6) NULL,
    cancelled_at DATETIME(6) NULL,
    CONSTRAINT ck_device_intent_identity CHECK (
        contract_version = 1 AND device_pk > 0 AND device_epoch > 0 AND target_pk > 0
        AND REGEXP_LIKE(operation_id, '^[0-9a-f]{32}$', 'c')
        AND REGEXP_LIKE(device_code, '^[0-9]{20}$', 'c')
        AND REGEXP_LIKE(target_code, '^[0-9]{20}$', 'c')
        AND (target_scope = 'channel' OR
            (target_scope = 'device' AND target_pk = device_pk AND target_code = device_code))
        AND kind IN ('live', 'playback', 'download', 'talk', 'ptz')
    ),
    CONSTRAINT ck_device_intent_phase CHECK (
        updated_at >= created_at AND (
            (state = 'reserved' AND row_version = 1 AND dispatch_started_at IS NULL AND cancelled_at IS NULL)
            OR (state = 'dispatched' AND row_version >= 2 AND cancelled_at IS NULL
                AND dispatch_started_at IS NOT NULL AND dispatch_started_at >= created_at AND dispatch_started_at <= updated_at)
            OR (state = 'cancelled' AND row_version >= 2 AND dispatch_started_at IS NULL
                AND cancelled_at IS NOT NULL AND cancelled_at >= created_at AND cancelled_at <= updated_at)
        )
    ),
    INDEX ix_device_intent_recovery (device_pk, device_epoch, state, operation_id)
) ENGINE=InnoDB;
