-- Root registers generations only while holding the protected local lifetime lock.
-- No seed owner or historical backfill. Retain this ledger across application rollback.
IF OBJECT_ID(N'dbo.sys_openapi_process_generation', N'U') IS NULL
BEGIN
CREATE TABLE dbo.sys_openapi_process_generation (
    generation_id VARCHAR(32) COLLATE Latin1_General_100_BIN2 NOT NULL PRIMARY KEY,
    domain_id VARCHAR(32) COLLATE Latin1_General_100_BIN2 NOT NULL,
    started_at DATETIME2(6) NOT NULL,
    CONSTRAINT uk_openapi_generation_domain UNIQUE (domain_id, generation_id),
    CONSTRAINT ck_openapi_generation_identity CHECK (
        DATALENGTH(generation_id) = 32 AND generation_id NOT LIKE '%[^0-9a-f]%'
        AND generation_id <> '00000000000000000000000000000000'
        AND DATALENGTH(domain_id) = 32 AND domain_id NOT LIKE '%[^0-9a-f]%'
        AND domain_id <> '00000000000000000000000000000000'
    )
);
END;
IF OBJECT_ID(N'dbo.sys_openapi_process_authority', N'U') IS NULL
BEGIN
CREATE TABLE dbo.sys_openapi_process_authority (
    id BIGINT NOT NULL PRIMARY KEY,
    domain_id VARCHAR(32) COLLATE Latin1_General_100_BIN2 NOT NULL,
    current_generation_id VARCHAR(32) COLLATE Latin1_General_100_BIN2 NOT NULL,
    row_version BIGINT NOT NULL,
    CONSTRAINT ck_openapi_authority_singleton CHECK (id = 1 AND row_version > 0),
    CONSTRAINT fk_openapi_authority_generation FOREIGN KEY (domain_id, current_generation_id)
        REFERENCES dbo.sys_openapi_process_generation (domain_id, generation_id)
);
END;
