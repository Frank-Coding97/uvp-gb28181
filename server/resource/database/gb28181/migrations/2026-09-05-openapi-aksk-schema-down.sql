-- Isolated test databases only. Never run on live authorization state.
DROP TABLE IF EXISTS sys_openapi_audit;
DROP TABLE IF EXISTS sys_openapi_nonce;
DROP TABLE IF EXISTS sys_openapi_client_scope;
DROP TABLE IF EXISTS sys_openapi_client;
