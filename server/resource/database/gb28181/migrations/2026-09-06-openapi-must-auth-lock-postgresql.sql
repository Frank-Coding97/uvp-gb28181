-- openapi-must-auth:begin
-- Append-only security commitment: never reset an existing row during upgrade.
CREATE TABLE IF NOT EXISTS sys_openapi_security_state (
    id BIGINT NOT NULL PRIMARY KEY,
    must_auth_locked BOOLEAN NOT NULL DEFAULT FALSE,
    locked_at TIMESTAMPTZ NULL,
    lock_version BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT ck_openapi_security_singleton CHECK (id = 1),
    CONSTRAINT ck_openapi_security_state CHECK (
        (must_auth_locked = FALSE AND lock_version = 0 AND locked_at IS NULL)
        OR (must_auth_locked = TRUE AND lock_version > 0 AND locked_at IS NOT NULL)
    )
);
INSERT INTO sys_openapi_security_state (id, must_auth_locked, locked_at, lock_version)
SELECT 1, FALSE, NULL, 0 WHERE NOT EXISTS (SELECT 1 FROM sys_openapi_security_state WHERE id = 1);
-- openapi-must-auth:end
