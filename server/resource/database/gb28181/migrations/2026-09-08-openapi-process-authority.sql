-- Root registers generations only while holding the protected local lifetime lock.
-- No seed owner or historical backfill. Retain this ledger across application rollback.
CREATE TABLE IF NOT EXISTS sys_openapi_process_generation (
    generation_id VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    domain_id VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at DATETIME(6) NOT NULL,
    CONSTRAINT uk_openapi_generation_domain UNIQUE (domain_id, generation_id),
    CONSTRAINT ck_openapi_generation_identity CHECK (
        LENGTH(generation_id) = 32 AND generation_id NOT REGEXP '[^0-9a-f]'
        AND generation_id <> '00000000000000000000000000000000'
        AND LENGTH(domain_id) = 32 AND domain_id NOT REGEXP '[^0-9a-f]'
        AND domain_id <> '00000000000000000000000000000000'
    )
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS sys_openapi_process_authority (
    id BIGINT NOT NULL PRIMARY KEY,
    domain_id VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    current_generation_id VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    row_version BIGINT NOT NULL,
    CONSTRAINT ck_openapi_authority_singleton CHECK (id = 1 AND row_version > 0),
    CONSTRAINT fk_openapi_authority_generation FOREIGN KEY (domain_id, current_generation_id)
        REFERENCES sys_openapi_process_generation (domain_id, generation_id)
) ENGINE=InnoDB;
