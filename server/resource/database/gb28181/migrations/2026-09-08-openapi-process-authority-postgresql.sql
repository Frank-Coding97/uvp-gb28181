-- Root registers generations only while holding the protected local lifetime lock.
-- No seed owner or historical backfill. Retain this ledger across application rollback.
CREATE TABLE IF NOT EXISTS sys_openapi_process_generation (
    generation_id VARCHAR(32) COLLATE "C" NOT NULL PRIMARY KEY,
    domain_id VARCHAR(32) COLLATE "C" NOT NULL,
    started_at TIMESTAMP(6) NOT NULL,
    CONSTRAINT uk_openapi_generation_domain UNIQUE (domain_id, generation_id),
    CONSTRAINT ck_openapi_generation_identity CHECK (
        generation_id ~ '^[0-9a-f]{32}$' AND generation_id <> '00000000000000000000000000000000'
        AND domain_id ~ '^[0-9a-f]{32}$' AND domain_id <> '00000000000000000000000000000000'
    )
);
CREATE TABLE IF NOT EXISTS sys_openapi_process_authority (
    id BIGINT NOT NULL PRIMARY KEY,
    domain_id VARCHAR(32) COLLATE "C" NOT NULL,
    current_generation_id VARCHAR(32) COLLATE "C" NOT NULL,
    row_version BIGINT NOT NULL,
    CONSTRAINT ck_openapi_authority_singleton CHECK (id = 1 AND row_version > 0),
    CONSTRAINT fk_openapi_authority_generation FOREIGN KEY (domain_id, current_generation_id)
        REFERENCES sys_openapi_process_generation (domain_id, generation_id)
);
