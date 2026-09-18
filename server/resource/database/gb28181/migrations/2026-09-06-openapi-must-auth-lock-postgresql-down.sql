-- Isolated empty test databases only, through the Go Down safety guard.
-- A latched commitment is never eligible for rollback or implicit unlock.
DROP TABLE IF EXISTS sys_openapi_security_state;
