-- Isolated empty test databases only. Never run on live authorization state or
-- use this file to delete business rows. The Down executor must verify the
-- media/device baseline before executing these feature-only inverse operations.
DROP TABLE IF EXISTS gb_openapi_viewer;
DROP TABLE IF EXISTS gb_openapi_play_grant;

ALTER TABLE IF EXISTS meta_node DROP COLUMN IF EXISTS runtime_identity_status;
ALTER TABLE IF EXISTS meta_node DROP COLUMN IF EXISTS runtime_confirmed_at;
ALTER TABLE IF EXISTS meta_node DROP COLUMN IF EXISTS runtime_confirmed_revision;
ALTER TABLE IF EXISTS meta_node DROP COLUMN IF EXISTS runtime_protocol_version;
ALTER TABLE IF EXISTS meta_node DROP COLUMN IF EXISTS runtime_epoch;
ALTER TABLE IF EXISTS meta_node DROP COLUMN IF EXISTS retired_boot_history;
ALTER TABLE IF EXISTS meta_node DROP COLUMN IF EXISTS current_boot_nonce;
ALTER TABLE IF EXISTS gb_device DROP COLUMN IF EXISTS legacy_revoked_before;
ALTER TABLE IF EXISTS gb_device DROP COLUMN IF EXISTS access_epoch;

DROP TABLE IF EXISTS sys_openapi_audit;
DROP TABLE IF EXISTS sys_openapi_nonce;
DROP TABLE IF EXISTS sys_openapi_client_scope;
DROP TABLE IF EXISTS sys_openapi_client;
