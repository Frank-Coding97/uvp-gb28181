-- openapi-must-auth:begin
-- Append-only security commitment: never reset an existing row during upgrade.
CREATE TABLE IF NOT EXISTS sys_openapi_security_state (
    id BIGINT NOT NULL PRIMARY KEY,
    must_auth_locked TINYINT NOT NULL DEFAULT 0,
    locked_at DATETIME(6) NULL,
    lock_version BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT ck_openapi_security_singleton CHECK (id = 1),
    CONSTRAINT ck_openapi_security_state CHECK (
        (must_auth_locked = 0 AND lock_version = 0 AND locked_at IS NULL)
        OR (must_auth_locked = 1 AND lock_version > 0 AND locked_at IS NOT NULL)
    )
) ENGINE=InnoDB;
INSERT INTO sys_openapi_security_state (id, must_auth_locked, locked_at, lock_version)
SELECT 1, 0, NULL, 0 WHERE NOT EXISTS (SELECT 1 FROM sys_openapi_security_state WHERE id = 1);
-- openapi-must-auth:end
