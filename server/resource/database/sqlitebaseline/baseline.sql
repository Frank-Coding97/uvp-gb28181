-- SQLite standalone baseline generated from release source e07857cc.
-- SQLite storage mapping: integer ids/counters -> INTEGER; DATE/DATETIME/TIMESTAMP keep their declared type; JSON -> TEXT with json_valid checks; decimal -> NUMERIC.
-- MySQL numeric ranges and VARCHAR/CHAR/VARBINARY lengths are represented by explicit checks where SQLite can preserve them.
-- Application code owns timestamp updates; no environment rows or credentials are seeded here.
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS "gb_alarm_binding" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" INTEGER NOT NULL,
  "channel_code" TEXT NOT NULL,
  "alarm_resource_id" INTEGER NOT NULL,
  "source" TEXT NOT NULL DEFAULT 'manual',
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_code" IS NULL OR length("channel_code") <= 20),
  CHECK ("alarm_resource_id" IS NULL OR (typeof("alarm_resource_id") = 'integer' AND "alarm_resource_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("source" IS NULL OR length("source") <= 16)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_alarm_binding_channel" ON "gb_alarm_binding" ("device_id", "channel_code");
CREATE INDEX IF NOT EXISTS "idx_alarm_binding_device" ON "gb_alarm_binding" ("device_id");
CREATE INDEX IF NOT EXISTS "idx_alarm_binding_resource" ON "gb_alarm_binding" ("alarm_resource_id");

CREATE TABLE IF NOT EXISTS "gb_alarm_event" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" INTEGER NOT NULL,
  "channel_id" INTEGER DEFAULT NULL,
  "source_code" TEXT NOT NULL,
  "sn" TEXT DEFAULT NULL,
  "alarm_time" DATETIME DEFAULT NULL,
  "priority" INTEGER DEFAULT NULL,
  "method" INTEGER DEFAULT NULL,
  "alarm_type" INTEGER DEFAULT NULL,
  "alarm_type_param" TEXT DEFAULT NULL,
  "description" TEXT,
  "longitude" NUMERIC DEFAULT NULL,
  "latitude" NUMERIC DEFAULT NULL,
  "call_id" TEXT DEFAULT NULL,
  "cseq" TEXT DEFAULT NULL,
  "dedupe_key" TEXT NOT NULL,
  "raw_digest" TEXT DEFAULT NULL,
  "raw_summary" TEXT,
  "received_at" DATETIME NOT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 4294967295)),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 4294967295)),
  CHECK ("source_code" IS NULL OR length("source_code") <= 20),
  CHECK ("sn" IS NULL OR length("sn") <= 64),
  CHECK ("priority" IS NULL OR (typeof("priority") = 'integer' AND "priority" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("method" IS NULL OR (typeof("method") = 'integer' AND "method" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("alarm_type" IS NULL OR (typeof("alarm_type") = 'integer' AND "alarm_type" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("alarm_type_param" IS NULL OR length("alarm_type_param") <= 255),
  CHECK ("call_id" IS NULL OR length("call_id") <= 255),
  CHECK ("cseq" IS NULL OR length("cseq") <= 64),
  CHECK ("dedupe_key" IS NULL OR length("dedupe_key") <= 128),
  CHECK ("raw_digest" IS NULL OR length("raw_digest") <= 64)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_dedupe_key" ON "gb_alarm_event" ("dedupe_key");
CREATE INDEX IF NOT EXISTS "idx_alarm_device_time" ON "gb_alarm_event" ("device_id", "alarm_time");
CREATE INDEX IF NOT EXISTS "gb_alarm_event__idx_channel_id" ON "gb_alarm_event" ("channel_id");
CREATE INDEX IF NOT EXISTS "idx_received_at" ON "gb_alarm_event" ("received_at");
CREATE INDEX IF NOT EXISTS "idx_alarm_time" ON "gb_alarm_event" ("alarm_time");

CREATE TABLE IF NOT EXISTS "gb_alarm_resource" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "owner_dept_id" INTEGER NOT NULL,
  "device_id" INTEGER NOT NULL DEFAULT 0,
  "device_code" TEXT NOT NULL,
  "alarm_code" TEXT NOT NULL,
  "resource_type" TEXT NOT NULL,
  "type_code" TEXT NOT NULL,
  "name" TEXT NOT NULL,
  "raw_parent_ids" TEXT NOT NULL DEFAULT '',
  "status" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_code" IS NULL OR length("device_code") <= 20),
  CHECK ("alarm_code" IS NULL OR length("alarm_code") <= 20),
  CHECK ("resource_type" IS NULL OR length("resource_type") <= 16),
  CHECK ("type_code" IS NULL OR length("type_code") <= 3),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("raw_parent_ids" IS NULL OR length("raw_parent_ids") <= 512),
  CHECK ("status" IS NULL OR (typeof("status") = 'integer' AND "status" BETWEEN -128 AND 127))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_alarm_resource_code" ON "gb_alarm_resource" ("owner_dept_id", "device_code", "alarm_code");
CREATE INDEX IF NOT EXISTS "idx_alarm_resource_device" ON "gb_alarm_resource" ("owner_dept_id", "device_code");
CREATE INDEX IF NOT EXISTS "idx_alarm_resource_device_id" ON "gb_alarm_resource" ("device_id");
CREATE INDEX IF NOT EXISTS "idx_alarm_resource_alarm_code" ON "gb_alarm_resource" ("alarm_code");
CREATE INDEX IF NOT EXISTS "idx_alarm_resource_type" ON "gb_alarm_resource" ("resource_type");
CREATE INDEX IF NOT EXISTS "idx_alarm_resource_deleted_at" ON "gb_alarm_resource" ("deleted_at");

CREATE TABLE IF NOT EXISTS "gb_alarm_resource_parent" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "alarm_resource_id" INTEGER NOT NULL,
  "parent_code" TEXT NOT NULL,
  "created_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("alarm_resource_id" IS NULL OR (typeof("alarm_resource_id") = 'integer' AND "alarm_resource_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("parent_code" IS NULL OR length("parent_code") <= 20)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_alarm_resource_parent" ON "gb_alarm_resource_parent" ("alarm_resource_id", "parent_code");
CREATE INDEX IF NOT EXISTS "idx_alarm_parent_resource" ON "gb_alarm_resource_parent" ("alarm_resource_id");
CREATE INDEX IF NOT EXISTS "idx_alarm_parent_code" ON "gb_alarm_resource_parent" ("parent_code");

CREATE TABLE IF NOT EXISTS "gb_anomaly_record" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "catalog_node_id" INTEGER NOT NULL,
  "raw_code" TEXT NOT NULL,
  "guessed_type" TEXT DEFAULT NULL,
  "fallback_type" TEXT NOT NULL,
  "source_device_id" INTEGER DEFAULT NULL,
  "reason" TEXT DEFAULT NULL,
  "resolved" INTEGER NOT NULL DEFAULT 0,
  "resolved_by" INTEGER DEFAULT NULL,
  "resolved_at" DATETIME DEFAULT NULL,
  "resolved_action" TEXT DEFAULT NULL,
  "created_at" DATETIME NOT NULL,
  "owner_dept_id" INTEGER NOT NULL DEFAULT 0,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("catalog_node_id" IS NULL OR (typeof("catalog_node_id") = 'integer' AND "catalog_node_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("raw_code" IS NULL OR length("raw_code") <= 64),
  CHECK ("guessed_type" IS NULL OR length("guessed_type") <= 32),
  CHECK ("fallback_type" IS NULL OR length("fallback_type") <= 16),
  CHECK ("source_device_id" IS NULL OR (typeof("source_device_id") = 'integer' AND "source_device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("reason" IS NULL OR length("reason") <= 255),
  CHECK ("resolved" IS NULL OR (typeof("resolved") = 'integer' AND "resolved" BETWEEN -128 AND 127)),
  CHECK ("resolved_by" IS NULL OR (typeof("resolved_by") = 'integer' AND "resolved_by" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("resolved_action" IS NULL OR length("resolved_action") <= 64),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 4294967295))
);
CREATE INDEX IF NOT EXISTS "idx_node" ON "gb_anomaly_record" ("catalog_node_id");
CREATE INDEX IF NOT EXISTS "idx_owner_dept_resolved" ON "gb_anomaly_record" ("owner_dept_id", "resolved", "created_at");

CREATE TABLE IF NOT EXISTS "gb_cascade_channel_projection" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "platform_id" INTEGER NOT NULL,
  "device_projection_id" INTEGER NOT NULL,
  "source_channel_id" INTEGER NOT NULL,
  "published_channel_id" TEXT NOT NULL,
  "name" TEXT DEFAULT NULL,
  "parent_override" TEXT DEFAULT NULL,
  "ptz_allowed" INTEGER NOT NULL DEFAULT 0,
  "active" INTEGER NOT NULL DEFAULT 1,
  "revision" INTEGER NOT NULL DEFAULT 1,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("platform_id" IS NULL OR (typeof("platform_id") = 'integer' AND "platform_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_projection_id" IS NULL OR (typeof("device_projection_id") = 'integer' AND "device_projection_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("source_channel_id" IS NULL OR (typeof("source_channel_id") = 'integer' AND "source_channel_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("published_channel_id" IS NULL OR length("published_channel_id") <= 20),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("parent_override" IS NULL OR length("parent_override") <= 20),
  CHECK ("ptz_allowed" IS NULL OR (typeof("ptz_allowed") = 'integer' AND "ptz_allowed" BETWEEN -128 AND 127)),
  CHECK ("active" IS NULL OR (typeof("active") = 'integer' AND "active" BETWEEN -128 AND 127)),
  CHECK ("revision" IS NULL OR (typeof("revision") = 'integer' AND "revision" BETWEEN 0 AND 9223372036854775807))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_cascade_channel_source" ON "gb_cascade_channel_projection" ("platform_id", "source_channel_id");
CREATE UNIQUE INDEX IF NOT EXISTS "uk_cascade_channel_published" ON "gb_cascade_channel_projection" ("platform_id", "published_channel_id");
CREATE INDEX IF NOT EXISTS "idx_cascade_channel_platform_active" ON "gb_cascade_channel_projection" ("platform_id", "active");
CREATE INDEX IF NOT EXISTS "idx_cascade_channel_device_projection" ON "gb_cascade_channel_projection" ("device_projection_id");
CREATE INDEX IF NOT EXISTS "idx_cascade_channel_deleted_at" ON "gb_cascade_channel_projection" ("deleted_at");

CREATE TABLE IF NOT EXISTS "gb_cascade_device_projection" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "platform_id" INTEGER NOT NULL,
  "source_device_id" INTEGER NOT NULL,
  "published_device_id" TEXT NOT NULL,
  "name" TEXT DEFAULT NULL,
  "manufacturer" TEXT DEFAULT NULL,
  "model" TEXT DEFAULT NULL,
  "owner" TEXT DEFAULT NULL,
  "civil_code" TEXT DEFAULT NULL,
  "address" TEXT DEFAULT NULL,
  "parental" INTEGER NOT NULL DEFAULT 0,
  "secrecy" INTEGER NOT NULL DEFAULT 0,
  "active" INTEGER NOT NULL DEFAULT 1,
  "revision" INTEGER NOT NULL DEFAULT 1,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("platform_id" IS NULL OR (typeof("platform_id") = 'integer' AND "platform_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("source_device_id" IS NULL OR (typeof("source_device_id") = 'integer' AND "source_device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("published_device_id" IS NULL OR length("published_device_id") <= 20),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("manufacturer" IS NULL OR length("manufacturer") <= 255),
  CHECK ("model" IS NULL OR length("model") <= 255),
  CHECK ("owner" IS NULL OR length("owner") <= 255),
  CHECK ("civil_code" IS NULL OR length("civil_code") <= 32),
  CHECK ("address" IS NULL OR length("address") <= 255),
  CHECK ("parental" IS NULL OR (typeof("parental") = 'integer' AND "parental" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("secrecy" IS NULL OR (typeof("secrecy") = 'integer' AND "secrecy" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("active" IS NULL OR (typeof("active") = 'integer' AND "active" BETWEEN -128 AND 127)),
  CHECK ("revision" IS NULL OR (typeof("revision") = 'integer' AND "revision" BETWEEN 0 AND 9223372036854775807))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_cascade_device_source" ON "gb_cascade_device_projection" ("platform_id", "source_device_id");
CREATE UNIQUE INDEX IF NOT EXISTS "uk_cascade_device_published" ON "gb_cascade_device_projection" ("platform_id", "published_device_id");
CREATE INDEX IF NOT EXISTS "idx_cascade_device_platform_active" ON "gb_cascade_device_projection" ("platform_id", "active");
CREATE INDEX IF NOT EXISTS "idx_cascade_device_deleted_at" ON "gb_cascade_device_projection" ("deleted_at");

CREATE TABLE IF NOT EXISTS "gb_cascade_media_session" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "platform_id" INTEGER NOT NULL,
  "dialog_key" TEXT NOT NULL,
  "call_id" TEXT NOT NULL,
  "local_tag" TEXT DEFAULT NULL,
  "remote_tag" TEXT DEFAULT NULL,
  "cseq" INTEGER NOT NULL DEFAULT 0,
  "source_device_id" INTEGER NOT NULL,
  "source_channel_id" INTEGER NOT NULL,
  "published_channel_id" TEXT NOT NULL,
  "profile_version" TEXT DEFAULT NULL,
  "profile_charset" TEXT DEFAULT NULL,
  "sdp_summary" TEXT,
  "zlm_node_id" INTEGER NOT NULL DEFAULT 0,
  "zlm_vhost" TEXT DEFAULT NULL,
  "zlm_app" TEXT DEFAULT NULL,
  "zlm_stream" TEXT DEFAULT NULL,
  "sender_ssrc" TEXT DEFAULT NULL,
  "transport" TEXT DEFAULT NULL,
  "remote_ip" TEXT DEFAULT NULL,
  "remote_port" INTEGER NOT NULL DEFAULT 0,
  "state" TEXT NOT NULL DEFAULT 'received',
  "failure_code" TEXT DEFAULT NULL,
  "failure_message" TEXT,
  "received_at" DATETIME DEFAULT NULL,
  "answered_at" DATETIME DEFAULT NULL,
  "active_at" DATETIME DEFAULT NULL,
  "closed_at" DATETIME DEFAULT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("platform_id" IS NULL OR (typeof("platform_id") = 'integer' AND "platform_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("dialog_key" IS NULL OR length("dialog_key") <= 512),
  CHECK ("call_id" IS NULL OR length("call_id") <= 255),
  CHECK ("local_tag" IS NULL OR length("local_tag") <= 255),
  CHECK ("remote_tag" IS NULL OR length("remote_tag") <= 255),
  CHECK ("cseq" IS NULL OR (typeof("cseq") = 'integer' AND "cseq" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("source_device_id" IS NULL OR (typeof("source_device_id") = 'integer' AND "source_device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("source_channel_id" IS NULL OR (typeof("source_channel_id") = 'integer' AND "source_channel_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("published_channel_id" IS NULL OR length("published_channel_id") <= 20),
  CHECK ("profile_version" IS NULL OR length("profile_version") <= 16),
  CHECK ("profile_charset" IS NULL OR length("profile_charset") <= 32),
  CHECK ("zlm_node_id" IS NULL OR (typeof("zlm_node_id") = 'integer')),
  CHECK ("zlm_vhost" IS NULL OR length("zlm_vhost") <= 128),
  CHECK ("zlm_app" IS NULL OR length("zlm_app") <= 64),
  CHECK ("zlm_stream" IS NULL OR length("zlm_stream") <= 128),
  CHECK ("sender_ssrc" IS NULL OR length("sender_ssrc") <= 32),
  CHECK ("transport" IS NULL OR length("transport") <= 16),
  CHECK ("remote_ip" IS NULL OR length("remote_ip") <= 45),
  CHECK ("remote_port" IS NULL OR (typeof("remote_port") = 'integer' AND "remote_port" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("state" IS NULL OR length("state") <= 16),
  CHECK ("failure_code" IS NULL OR length("failure_code") <= 64)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_cascade_media_dialog" ON "gb_cascade_media_session" ("dialog_key");
CREATE INDEX IF NOT EXISTS "idx_cascade_media_platform_state" ON "gb_cascade_media_session" ("platform_id", "state");
CREATE INDEX IF NOT EXISTS "idx_cascade_media_call_id" ON "gb_cascade_media_session" ("call_id");

CREATE TABLE IF NOT EXISTS "gb_cascade_platform" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT NOT NULL,
  "upstream_server_id" TEXT NOT NULL,
  "upstream_domain" TEXT NOT NULL,
  "host" TEXT NOT NULL,
  "port" INTEGER NOT NULL,
  "local_device_id" TEXT NOT NULL,
  "local_domain" TEXT NOT NULL,
  "local_sip_ip" TEXT NOT NULL,
  "local_sip_port" INTEGER NOT NULL,
  "media_advertise_ip" TEXT DEFAULT NULL,
  "auth_username" TEXT DEFAULT NULL,
  "secret_nonce" TEXT DEFAULT NULL,
  "secret_ciphertext" blob,
  "secret_alg" TEXT DEFAULT NULL,
  "secret_key_version" TEXT DEFAULT NULL,
  "profile_override" TEXT NOT NULL DEFAULT 'auto',
  "reported_gb_version" TEXT DEFAULT NULL,
  "reported_gb_version_at" DATETIME DEFAULT NULL,
  "effective_version" TEXT NOT NULL DEFAULT 2016,
  "effective_version_source" TEXT NOT NULL DEFAULT 'default',
  "effective_version_at" DATETIME DEFAULT NULL,
  "charset_override" TEXT DEFAULT NULL,
  "register_expires" INTEGER NOT NULL DEFAULT 3600,
  "keepalive_interval" INTEGER NOT NULL DEFAULT 60,
  "retry_policy" TEXT,
  "transport" TEXT NOT NULL DEFAULT 'UDP',
  "catalog_batch_size" INTEGER NOT NULL DEFAULT 100,
  "publish_platform" INTEGER NOT NULL DEFAULT 0,
  "publish_civil" INTEGER NOT NULL DEFAULT 0,
  "publish_group" INTEGER NOT NULL DEFAULT 0,
  "max_streams" INTEGER NOT NULL DEFAULT 1,
  "ptz_enabled" INTEGER NOT NULL DEFAULT 0,
  "register_at" DATETIME DEFAULT NULL,
  "register_expires_at" DATETIME DEFAULT NULL,
  "heartbeat_at" DATETIME DEFAULT NULL,
  "last_error_code" TEXT DEFAULT NULL,
  "last_error_message" TEXT,
  "last_error_at" DATETIME DEFAULT NULL,
  "enabled" INTEGER NOT NULL DEFAULT 0,
  "config_revision" INTEGER NOT NULL DEFAULT 1,
  "projection_revision" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("name" IS NULL OR length("name") <= 128),
  CHECK ("upstream_server_id" IS NULL OR length("upstream_server_id") <= 20),
  CHECK ("upstream_domain" IS NULL OR length("upstream_domain") <= 255),
  CHECK ("host" IS NULL OR length("host") <= 255),
  CHECK ("port" IS NULL OR (typeof("port") = 'integer' AND "port" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("local_device_id" IS NULL OR length("local_device_id") <= 20),
  CHECK ("local_domain" IS NULL OR length("local_domain") <= 255),
  CHECK ("local_sip_ip" IS NULL OR length("local_sip_ip") <= 45),
  CHECK ("local_sip_port" IS NULL OR (typeof("local_sip_port") = 'integer' AND "local_sip_port" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("media_advertise_ip" IS NULL OR length("media_advertise_ip") <= 45),
  CHECK ("auth_username" IS NULL OR length("auth_username") <= 255),
  CHECK ("secret_nonce" IS NULL OR length(CAST("secret_nonce" AS BLOB)) <= 64),
  CHECK ("secret_alg" IS NULL OR length("secret_alg") <= 32),
  CHECK ("secret_key_version" IS NULL OR length("secret_key_version") <= 64),
  CHECK ("profile_override" IS NULL OR length("profile_override") <= 16),
  CHECK ("reported_gb_version" IS NULL OR length("reported_gb_version") <= 16),
  CHECK ("effective_version" IS NULL OR length("effective_version") <= 16),
  CHECK ("effective_version_source" IS NULL OR length("effective_version_source") <= 32),
  CHECK ("charset_override" IS NULL OR length("charset_override") <= 32),
  CHECK ("register_expires" IS NULL OR (typeof("register_expires") = 'integer' AND "register_expires" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("keepalive_interval" IS NULL OR (typeof("keepalive_interval") = 'integer' AND "keepalive_interval" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("transport" IS NULL OR length("transport") <= 16),
  CHECK ("catalog_batch_size" IS NULL OR (typeof("catalog_batch_size") = 'integer' AND "catalog_batch_size" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("publish_platform" IS NULL OR (typeof("publish_platform") = 'integer' AND "publish_platform" BETWEEN -128 AND 127)),
  CHECK ("publish_civil" IS NULL OR (typeof("publish_civil") = 'integer' AND "publish_civil" BETWEEN -128 AND 127)),
  CHECK ("publish_group" IS NULL OR (typeof("publish_group") = 'integer' AND "publish_group" BETWEEN -128 AND 127)),
  CHECK ("max_streams" IS NULL OR (typeof("max_streams") = 'integer' AND "max_streams" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("ptz_enabled" IS NULL OR (typeof("ptz_enabled") = 'integer' AND "ptz_enabled" BETWEEN -128 AND 127)),
  CHECK ("last_error_code" IS NULL OR length("last_error_code") <= 64),
  CHECK ("enabled" IS NULL OR (typeof("enabled") = 'integer' AND "enabled" BETWEEN -128 AND 127)),
  CHECK ("config_revision" IS NULL OR (typeof("config_revision") = 'integer' AND "config_revision" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("projection_revision" IS NULL OR (typeof("projection_revision") = 'integer' AND "projection_revision" BETWEEN 0 AND 9223372036854775807))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_cascade_platform_name" ON "gb_cascade_platform" ("name");
CREATE UNIQUE INDEX IF NOT EXISTS "uk_cascade_platform_local_identity" ON "gb_cascade_platform" ("local_device_id", "local_domain");
CREATE INDEX IF NOT EXISTS "idx_cascade_platform_enabled" ON "gb_cascade_platform" ("enabled");
CREATE INDEX IF NOT EXISTS "idx_cascade_platform_deleted_at" ON "gb_cascade_platform" ("deleted_at");

CREATE TABLE IF NOT EXISTS "gb_catalog_node" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "node_type" TEXT NOT NULL,
  "parent_id" INTEGER DEFAULT NULL,
  "path" TEXT NOT NULL DEFAULT '/',
  "depth" INTEGER NOT NULL DEFAULT 0,
  "name" TEXT NOT NULL,
  "code" TEXT DEFAULT NULL,
  "civil_code" TEXT DEFAULT NULL,
  "device_id" INTEGER DEFAULT NULL,
  "channel_id" INTEGER DEFAULT NULL,
  "source" TEXT NOT NULL DEFAULT 'catalog',
  "sort_order" INTEGER NOT NULL DEFAULT 0,
  "anomaly" INTEGER NOT NULL DEFAULT 0,
  "anomaly_reason" TEXT DEFAULT NULL,
  "raw_code" TEXT DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "owner_dept_id" INTEGER NOT NULL DEFAULT 0,
  "alarm_resource_id" INTEGER DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("node_type" IS NULL OR length("node_type") <= 16),
  CHECK ("parent_id" IS NULL OR (typeof("parent_id") = 'integer' AND "parent_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("path" IS NULL OR length("path") <= 512),
  CHECK ("depth" IS NULL OR (typeof("depth") = 'integer' AND "depth" BETWEEN 0 AND 255)),
  CHECK ("name" IS NULL OR length("name") <= 128),
  CHECK ("code" IS NULL OR length("code") <= 32),
  CHECK ("civil_code" IS NULL OR length("civil_code") <= 6),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("source" IS NULL OR length("source") <= 16),
  CHECK ("sort_order" IS NULL OR (typeof("sort_order") = 'integer' AND "sort_order" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("anomaly" IS NULL OR (typeof("anomaly") = 'integer' AND "anomaly" BETWEEN -128 AND 127)),
  CHECK ("anomaly_reason" IS NULL OR length("anomaly_reason") <= 255),
  CHECK ("raw_code" IS NULL OR length("raw_code") <= 64),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 4294967295)),
  CHECK ("alarm_resource_id" IS NULL OR (typeof("alarm_resource_id") = 'integer' AND "alarm_resource_id" BETWEEN 0 AND 9223372036854775807))
);
CREATE INDEX IF NOT EXISTS "idx_code" ON "gb_catalog_node" ("code");
CREATE INDEX IF NOT EXISTS "gb_catalog_node__idx_deleted_at" ON "gb_catalog_node" ("deleted_at");
CREATE INDEX IF NOT EXISTS "idx_owner_dept_parent" ON "gb_catalog_node" ("owner_dept_id", "parent_id");
CREATE INDEX IF NOT EXISTS "idx_owner_dept_path" ON "gb_catalog_node" ("owner_dept_id", "path");
CREATE INDEX IF NOT EXISTS "idx_owner_dept_type" ON "gb_catalog_node" ("owner_dept_id", "node_type");
CREATE INDEX IF NOT EXISTS "idx_owner_dept_anomaly" ON "gb_catalog_node" ("owner_dept_id", "anomaly");
CREATE INDEX IF NOT EXISTS "idx_owner_dept_civil_code" ON "gb_catalog_node" ("owner_dept_id", "civil_code");
CREATE INDEX IF NOT EXISTS "idx_catalog_alarm_resource" ON "gb_catalog_node" ("alarm_resource_id");

CREATE TABLE IF NOT EXISTS "gb_channel" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "channel_id" TEXT NOT NULL DEFAULT '',
  "device_id" TEXT NOT NULL DEFAULT '',
  "name" TEXT NOT NULL DEFAULT '',
  "alias" TEXT NOT NULL DEFAULT '',
  "manufacturer" TEXT NOT NULL DEFAULT '',
  "model" TEXT NOT NULL DEFAULT '',
  "owner" TEXT NOT NULL DEFAULT '',
  "civil_code" TEXT NOT NULL DEFAULT '',
  "parent_id" TEXT NOT NULL DEFAULT '',
  "ptz_type" INTEGER DEFAULT 0,
  "longitude" NUMERIC DEFAULT '0.000000',
  "latitude" NUMERIC DEFAULT '0.000000',
  "status" INTEGER DEFAULT 0,
  "stream_id" TEXT NOT NULL DEFAULT '',
  "current_ssrc" TEXT NOT NULL DEFAULT '',
  "on_demand_live" INTEGER NOT NULL DEFAULT 1,
  "cloud_recording_enabled" INTEGER NOT NULL DEFAULT 0,
  "cloud_recording_state" TEXT NOT NULL DEFAULT 'disabled',
  "cloud_recording_error" TEXT NOT NULL DEFAULT '',
  "cloud_recording_updated_at" DATETIME DEFAULT NULL,
  "audio_enabled" INTEGER NOT NULL DEFAULT 1,
  "recording_mode" TEXT NOT NULL DEFAULT 'off',
  "stream_transport" TEXT NOT NULL DEFAULT 'TCP-Passive',
  "capabilities" TEXT DEFAULT NULL,
  "snapshot_url" TEXT NOT NULL DEFAULT '',
  "snapshot_at" DATETIME DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "owner_dept_id" INTEGER NOT NULL DEFAULT 0,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("channel_id" IS NULL OR length("channel_id") <= 20),
  CHECK ("device_id" IS NULL OR length("device_id") <= 20),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("alias" IS NULL OR length("alias") <= 255),
  CHECK ("manufacturer" IS NULL OR length("manufacturer") <= 255),
  CHECK ("model" IS NULL OR length("model") <= 255),
  CHECK ("owner" IS NULL OR length("owner") <= 64),
  CHECK ("civil_code" IS NULL OR length("civil_code") <= 32),
  CHECK ("parent_id" IS NULL OR length("parent_id") <= 20),
  CHECK ("ptz_type" IS NULL OR (typeof("ptz_type") = 'integer' AND "ptz_type" BETWEEN -128 AND 127)),
  CHECK ("status" IS NULL OR (typeof("status") = 'integer' AND "status" BETWEEN -128 AND 127)),
  CHECK ("stream_id" IS NULL OR length("stream_id") <= 64),
  CHECK ("current_ssrc" IS NULL OR length("current_ssrc") <= 10),
  CHECK ("on_demand_live" IS NULL OR (typeof("on_demand_live") = 'integer' AND "on_demand_live" BETWEEN -128 AND 127)),
  CHECK ("cloud_recording_enabled" IS NULL OR (typeof("cloud_recording_enabled") = 'integer' AND "cloud_recording_enabled" BETWEEN -128 AND 127)),
  CHECK ("cloud_recording_state" IS NULL OR length("cloud_recording_state") <= 20),
  CHECK ("cloud_recording_error" IS NULL OR length("cloud_recording_error") <= 500),
  CHECK ("audio_enabled" IS NULL OR (typeof("audio_enabled") = 'integer' AND "audio_enabled" BETWEEN -128 AND 127)),
  CHECK ("recording_mode" IS NULL OR length("recording_mode") <= 16),
  CHECK ("stream_transport" IS NULL OR length("stream_transport") <= 16),
  CHECK ("capabilities" IS NULL OR json_valid("capabilities")),
  CHECK ("snapshot_url" IS NULL OR length("snapshot_url") <= 500),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 4294967295))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_device_channel" ON "gb_channel" ("device_id", "channel_id");
CREATE INDEX IF NOT EXISTS "idx_device_id" ON "gb_channel" ("device_id");
CREATE INDEX IF NOT EXISTS "gb_channel__idx_deleted_at" ON "gb_channel" ("deleted_at");
CREATE INDEX IF NOT EXISTS "gb_channel__idx_owner_dept_deleted" ON "gb_channel" ("owner_dept_id", "deleted_at");
CREATE INDEX IF NOT EXISTS "idx_stream_transport" ON "gb_channel" ("stream_transport");
CREATE INDEX IF NOT EXISTS "idx_stream_id" ON "gb_channel" ("stream_id");

CREATE TABLE IF NOT EXISTS "gb_channel_mount" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "channel_id" INTEGER NOT NULL,
  "parent_node_id" INTEGER NOT NULL,
  "display_name" TEXT DEFAULT NULL,
  "is_primary" INTEGER NOT NULL DEFAULT 0,
  "mount_source" TEXT NOT NULL DEFAULT 'catalog',
  "sort_order" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "owner_dept_id" INTEGER NOT NULL DEFAULT 0,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("parent_node_id" IS NULL OR (typeof("parent_node_id") = 'integer' AND "parent_node_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("display_name" IS NULL OR length("display_name") <= 128),
  CHECK ("is_primary" IS NULL OR (typeof("is_primary") = 'integer' AND "is_primary" BETWEEN -128 AND 127)),
  CHECK ("mount_source" IS NULL OR length("mount_source") <= 16),
  CHECK ("sort_order" IS NULL OR (typeof("sort_order") = 'integer' AND "sort_order" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 4294967295))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_channel_parent" ON "gb_channel_mount" ("channel_id", "parent_node_id");
CREATE INDEX IF NOT EXISTS "idx_parent_sort" ON "gb_channel_mount" ("parent_node_id", "sort_order");
CREATE INDEX IF NOT EXISTS "idx_owner_dept" ON "gb_channel_mount" ("owner_dept_id");

CREATE TABLE IF NOT EXISTS "gb_custom_group" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "owner_dept_id" INTEGER NOT NULL,
  "parent_id" INTEGER NOT NULL DEFAULT 0,
  "path" TEXT NOT NULL,
  "depth" INTEGER NOT NULL DEFAULT 0,
  "name" TEXT NOT NULL,
  "created_by" INTEGER NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("parent_id" IS NULL OR (typeof("parent_id") = 'integer' AND "parent_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("path" IS NULL OR length("path") <= 1024),
  CHECK ("depth" IS NULL OR (typeof("depth") = 'integer' AND "depth" BETWEEN 0 AND 255)),
  CHECK ("name" IS NULL OR length("name") <= 64),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 9223372036854775807))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_custom_group_sibling_name" ON "gb_custom_group" ("owner_dept_id", "parent_id", "name");
CREATE INDEX IF NOT EXISTS "idx_custom_group_parent" ON "gb_custom_group" ("parent_id");
CREATE INDEX IF NOT EXISTS "idx_custom_group_dept_path" ON "gb_custom_group" ("owner_dept_id", "path");

CREATE TABLE IF NOT EXISTS "gb_channel_favorite_item" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "group_id" INTEGER NOT NULL,
  "device_code" TEXT NOT NULL,
  "channel_code" TEXT NOT NULL,
  "device_name" TEXT NOT NULL DEFAULT '',
  "channel_name" TEXT NOT NULL DEFAULT '',
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("group_id" IS NULL OR (typeof("group_id") = 'integer' AND "group_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_code" IS NULL OR length("device_code") <= 64),
  CHECK ("channel_code" IS NULL OR length("channel_code") <= 64),
  CHECK ("device_name" IS NULL OR length("device_name") <= 255),
  CHECK ("channel_name" IS NULL OR length("channel_name") <= 255)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_gb_channel_favorite_item_code" ON "gb_channel_favorite_item" ("group_id","device_code","channel_code");
CREATE INDEX IF NOT EXISTS "idx_gb_channel_favorite_item_group" ON "gb_channel_favorite_item" ("group_id");

CREATE TABLE IF NOT EXISTS "gb_channel_favorite_group" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "owner_user_id" INTEGER NOT NULL,
  "name" TEXT NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("owner_user_id" IS NULL OR (typeof("owner_user_id") = 'integer' AND "owner_user_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("name" IS NULL OR length("name") <= 64)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_gb_channel_favorite_group_owner_name" ON "gb_channel_favorite_group" ("owner_user_id","name");
CREATE INDEX IF NOT EXISTS "idx_gb_channel_favorite_group_owner" ON "gb_channel_favorite_group" ("owner_user_id");

CREATE TABLE IF NOT EXISTS "gb_custom_group_device" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "group_id" INTEGER NOT NULL,
  "device_id" INTEGER NOT NULL,
  "created_by" INTEGER NOT NULL,
  "created_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("group_id" IS NULL OR (typeof("group_id") = 'integer' AND "group_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 9223372036854775807))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_custom_group_device" ON "gb_custom_group_device" ("group_id", "device_id");
CREATE INDEX IF NOT EXISTS "idx_custom_group_device_group" ON "gb_custom_group_device" ("group_id");
CREATE INDEX IF NOT EXISTS "idx_custom_group_device_device" ON "gb_custom_group_device" ("device_id");

CREATE TABLE IF NOT EXISTS "gb_device" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" TEXT NOT NULL DEFAULT '',
  "name" TEXT NOT NULL DEFAULT '',
  "alias" TEXT NOT NULL DEFAULT '',
  "password" TEXT NOT NULL DEFAULT '',
  "transport" TEXT NOT NULL DEFAULT '',
  "manufacturer" TEXT NOT NULL DEFAULT '',
  "model" TEXT NOT NULL DEFAULT '',
  "firmware" TEXT NOT NULL DEFAULT '',
  "ip" TEXT NOT NULL DEFAULT '',
  "port" INTEGER DEFAULT 0,
  "register_time" DATETIME DEFAULT NULL,
  "register_expire_at" DATETIME DEFAULT NULL,
  "keepalive_time" DATETIME DEFAULT NULL,
  "keepalive_interval" INTEGER DEFAULT 60,
  "expires" INTEGER DEFAULT 0,
  "status" INTEGER DEFAULT 0,
  "offline_at" DATETIME DEFAULT NULL,
  "subscribe_capability" TEXT NOT NULL DEFAULT 'unknown',
  "subscribe_last_test" DATETIME DEFAULT NULL,
  "subscribe_expires_at" DATETIME DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "created_by" INTEGER DEFAULT 0,
  "owner_dept_id" INTEGER NOT NULL DEFAULT 0,
  "reported_version" TEXT NOT NULL DEFAULT '',
  "reported_version_at" DATETIME DEFAULT NULL,
  "protocol_override" TEXT NOT NULL DEFAULT 'auto',
  "effective_version" TEXT NOT NULL DEFAULT 2016,
  "effective_version_source" TEXT NOT NULL DEFAULT 'default',
  "effective_version_at" DATETIME DEFAULT NULL,
  "zlm_node_id" INTEGER NOT NULL DEFAULT 0,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("device_id" IS NULL OR length("device_id") <= 20),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("alias" IS NULL OR length("alias") <= 255),
  CHECK ("password" IS NULL OR length("password") <= 255),
  CHECK ("transport" IS NULL OR length("transport") <= 8),
  CHECK ("manufacturer" IS NULL OR length("manufacturer") <= 255),
  CHECK ("model" IS NULL OR length("model") <= 255),
  CHECK ("firmware" IS NULL OR length("firmware") <= 255),
  CHECK ("ip" IS NULL OR length("ip") <= 64),
  CHECK ("port" IS NULL OR (typeof("port") = 'integer' AND "port" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("keepalive_interval" IS NULL OR (typeof("keepalive_interval") = 'integer' AND "keepalive_interval" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("expires" IS NULL OR (typeof("expires") = 'integer' AND "expires" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("status" IS NULL OR (typeof("status") = 'integer' AND "status" BETWEEN -128 AND 127)),
  CHECK ("subscribe_capability" IS NULL OR length("subscribe_capability") <= 16),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295)),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 4294967295)),
  CHECK ("reported_version" IS NULL OR length("reported_version") <= 8),
  CHECK ("protocol_override" IS NULL OR length("protocol_override") <= 8),
  CHECK ("effective_version" IS NULL OR length("effective_version") <= 8),
  CHECK ("effective_version_source" IS NULL OR length("effective_version_source") <= 16),
  CHECK ("zlm_node_id" IS NULL OR (typeof("zlm_node_id") = 'integer'))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_device_id" ON "gb_device" ("device_id");
CREATE INDEX IF NOT EXISTS "gb_device__idx_deleted_at" ON "gb_device" ("deleted_at");
CREATE INDEX IF NOT EXISTS "idx_status_keepalive" ON "gb_device" ("status", "keepalive_time");
CREATE INDEX IF NOT EXISTS "idx_subscribe_capability" ON "gb_device" ("subscribe_capability", "subscribe_last_test");
CREATE INDEX IF NOT EXISTS "gb_device__idx_owner_dept_deleted" ON "gb_device" ("owner_dept_id", "deleted_at");
CREATE INDEX IF NOT EXISTS "idx_gb_device_zlm_node" ON "gb_device" ("zlm_node_id");

CREATE TABLE IF NOT EXISTS "gb_device_control_state" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" INTEGER NOT NULL,
  "channel_id" INTEGER NOT NULL DEFAULT 0,
  "target_scope" TEXT NOT NULL,
  "target_code" TEXT NOT NULL,
  "record_state" TEXT NOT NULL DEFAULT 'unknown',
  "guard_state" TEXT NOT NULL DEFAULT 'unknown',
  "freshness" TEXT NOT NULL DEFAULT 'unknown',
  "observed_at" DATETIME NOT NULL,
  "source" TEXT NOT NULL DEFAULT 'device_status',
  "source_sn" INTEGER NOT NULL DEFAULT 0,
  "source_operation_id" TEXT DEFAULT NULL,
  "source_operation_seq" INTEGER NOT NULL DEFAULT 0,
  "raw_summary" TEXT,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("target_scope" IS NULL OR length("target_scope") <= 16),
  CHECK ("target_code" IS NULL OR length("target_code") <= 20),
  CHECK ("record_state" IS NULL OR length("record_state") <= 8),
  CHECK ("guard_state" IS NULL OR length("guard_state") <= 8),
  CHECK ("freshness" IS NULL OR length("freshness") <= 8),
  CHECK ("source" IS NULL OR length("source") <= 32),
  CHECK ("source_sn" IS NULL OR (typeof("source_sn") = 'integer' AND "source_sn" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("source_operation_id" IS NULL OR length("source_operation_id") <= 64),
  CHECK ("source_operation_seq" IS NULL OR (typeof("source_operation_seq") = 'integer' AND "source_operation_seq" BETWEEN 0 AND 9223372036854775807))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_control_state_target" ON "gb_device_control_state" ("device_id", "target_scope", "target_code");
CREATE INDEX IF NOT EXISTS "idx_control_state_device_target" ON "gb_device_control_state" ("device_id", "target_scope", "target_code");
CREATE INDEX IF NOT EXISTS "idx_control_state_channel" ON "gb_device_control_state" ("channel_id");

CREATE TABLE IF NOT EXISTS "gb_device_status_event" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" INTEGER NOT NULL,
  "device_code" TEXT NOT NULL,
  "event_type" TEXT NOT NULL,
  "from_status" INTEGER DEFAULT NULL,
  "to_status" INTEGER NOT NULL,
  "occurred_at" DATETIME NOT NULL,
  "source" TEXT NOT NULL,
  "register_expires" INTEGER DEFAULT NULL,
  "keepalive_interval" INTEGER DEFAULT NULL,
  "ip" TEXT NOT NULL DEFAULT '',
  "port" INTEGER NOT NULL DEFAULT 0,
  "transport" TEXT NOT NULL DEFAULT '',
  "detail" TEXT,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 4294967295)),
  CHECK ("device_code" IS NULL OR length("device_code") <= 20),
  CHECK ("event_type" IS NULL OR length("event_type") <= 32),
  CHECK ("from_status" IS NULL OR (typeof("from_status") = 'integer' AND "from_status" BETWEEN -128 AND 127)),
  CHECK ("to_status" IS NULL OR (typeof("to_status") = 'integer' AND "to_status" BETWEEN -128 AND 127)),
  CHECK ("source" IS NULL OR length("source") <= 32),
  CHECK ("register_expires" IS NULL OR (typeof("register_expires") = 'integer' AND "register_expires" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("keepalive_interval" IS NULL OR (typeof("keepalive_interval") = 'integer' AND "keepalive_interval" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("ip" IS NULL OR length("ip") <= 64),
  CHECK ("port" IS NULL OR (typeof("port") = 'integer' AND "port" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("transport" IS NULL OR length("transport") <= 8)
);
CREATE INDEX IF NOT EXISTS "idx_device_occurred" ON "gb_device_status_event" ("device_id", "occurred_at", "id");
CREATE INDEX IF NOT EXISTS "idx_event_type_occurred" ON "gb_device_status_event" ("event_type", "occurred_at");
CREATE INDEX IF NOT EXISTS "idx_device_code" ON "gb_device_status_event" ("device_code");
CREATE INDEX IF NOT EXISTS "gb_device_status_event__idx_deleted_at" ON "gb_device_status_event" ("deleted_at");

CREATE TABLE IF NOT EXISTS "gb_device_subscription" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" INTEGER NOT NULL,
  "kind" TEXT NOT NULL,
  "enabled" INTEGER NOT NULL DEFAULT 0,
  "status" TEXT NOT NULL DEFAULT 'disabled',
  "expires_seconds" INTEGER NOT NULL DEFAULT 3600,
  "interval_seconds" INTEGER NOT NULL DEFAULT 0,
  "event" TEXT NOT NULL,
  "call_id" TEXT DEFAULT NULL,
  "local_tag" TEXT DEFAULT NULL,
  "remote_tag" TEXT DEFAULT NULL,
  "cseq" INTEGER NOT NULL DEFAULT 0,
  "expires_at" DATETIME DEFAULT NULL,
  "last_subscribe_at" DATETIME DEFAULT NULL,
  "last_notify_at" DATETIME DEFAULT NULL,
  "next_action_at" DATETIME DEFAULT NULL,
  "retry_count" INTEGER NOT NULL DEFAULT 0,
  "last_status_code" INTEGER NOT NULL DEFAULT 0,
  "last_error" TEXT,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 4294967295)),
  CHECK ("kind" IS NULL OR length("kind") <= 32),
  CHECK ("enabled" IS NULL OR (typeof("enabled") = 'integer' AND "enabled" BETWEEN -128 AND 127)),
  CHECK ("status" IS NULL OR length("status") <= 16),
  CHECK ("expires_seconds" IS NULL OR (typeof("expires_seconds") = 'integer' AND "expires_seconds" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("interval_seconds" IS NULL OR (typeof("interval_seconds") = 'integer' AND "interval_seconds" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("event" IS NULL OR length("event") <= 32),
  CHECK ("call_id" IS NULL OR length("call_id") <= 255),
  CHECK ("local_tag" IS NULL OR length("local_tag") <= 128),
  CHECK ("remote_tag" IS NULL OR length("remote_tag") <= 128),
  CHECK ("cseq" IS NULL OR (typeof("cseq") = 'integer' AND "cseq" BETWEEN 0 AND 4294967295)),
  CHECK ("retry_count" IS NULL OR (typeof("retry_count") = 'integer' AND "retry_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("last_status_code" IS NULL OR (typeof("last_status_code") = 'integer' AND "last_status_code" BETWEEN -2147483648 AND 2147483647))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_device_subscription_kind" ON "gb_device_subscription" ("device_id", "kind");
CREATE INDEX IF NOT EXISTS "idx_subscription_call_id" ON "gb_device_subscription" ("call_id");
CREATE INDEX IF NOT EXISTS "idx_subscription_due" ON "gb_device_subscription" ("enabled", "next_action_at");

CREATE TABLE IF NOT EXISTS "gb_mobile_position_history" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" INTEGER NOT NULL,
  "source_code" TEXT NOT NULL,
  "channel_id" INTEGER DEFAULT NULL,
  "event_time" DATETIME NOT NULL,
  "received_at" DATETIME NOT NULL,
  "longitude" NUMERIC NOT NULL,
  "latitude" NUMERIC NOT NULL,
  "speed" REAL DEFAULT NULL,
  "direction" REAL DEFAULT NULL,
  "altitude" REAL DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 4294967295)),
  CHECK ("source_code" IS NULL OR length("source_code") <= 20),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 4294967295))
);
CREATE INDEX IF NOT EXISTS "idx_position_history_device_source_time" ON "gb_mobile_position_history" ("device_id", "source_code", "event_time");
CREATE INDEX IF NOT EXISTS "idx_position_history_channel_time" ON "gb_mobile_position_history" ("channel_id", "event_time");
CREATE INDEX IF NOT EXISTS "idx_position_history_received_at" ON "gb_mobile_position_history" ("received_at");

CREATE TABLE IF NOT EXISTS "gb_mobile_position_latest" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" INTEGER NOT NULL,
  "source_code" TEXT NOT NULL,
  "channel_id" INTEGER DEFAULT NULL,
  "event_time" DATETIME NOT NULL,
  "received_at" DATETIME NOT NULL,
  "longitude" NUMERIC NOT NULL,
  "latitude" NUMERIC NOT NULL,
  "speed" REAL DEFAULT NULL,
  "direction" REAL DEFAULT NULL,
  "altitude" REAL DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 4294967295)),
  CHECK ("source_code" IS NULL OR length("source_code") <= 20),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 4294967295))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_position_device_source" ON "gb_mobile_position_latest" ("device_id", "source_code");
CREATE INDEX IF NOT EXISTS "gb_mobile_position_latest__idx_channel_id" ON "gb_mobile_position_latest" ("channel_id");
CREATE INDEX IF NOT EXISTS "idx_event_time" ON "gb_mobile_position_latest" ("event_time");

CREATE TABLE IF NOT EXISTS "gb_playback_scheme" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "owner_user_id" INTEGER NOT NULL,
  "owner_dept_id" INTEGER NOT NULL,
  "name" TEXT NOT NULL,
  "layout_size" INTEGER NOT NULL,
  "slot_count" INTEGER NOT NULL DEFAULT 0,
  "created_by" INTEGER NOT NULL,
  "updated_by" INTEGER NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("owner_user_id" IS NULL OR (typeof("owner_user_id") = 'integer' AND "owner_user_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("name" IS NULL OR length("name") <= 64),
  CHECK ("layout_size" IS NULL OR (typeof("layout_size") = 'integer' AND "layout_size" BETWEEN -32768 AND 32767)),
  CHECK ("slot_count" IS NULL OR (typeof("slot_count") = 'integer' AND "slot_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("updated_by" IS NULL OR (typeof("updated_by") = 'integer' AND "updated_by" BETWEEN 0 AND 9223372036854775807))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_playback_scheme_owner_name" ON "gb_playback_scheme" ("owner_user_id", "name");
CREATE INDEX IF NOT EXISTS "idx_playback_scheme_owner_updated" ON "gb_playback_scheme" ("owner_user_id", "updated_at");
CREATE INDEX IF NOT EXISTS "idx_playback_scheme_dept" ON "gb_playback_scheme" ("owner_dept_id");

CREATE TABLE IF NOT EXISTS "gb_playback_scheme_slot" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "scheme_id" INTEGER NOT NULL,
  "slot_index" INTEGER NOT NULL,
  "device_code" TEXT NOT NULL,
  "channel_code" TEXT NOT NULL,
  "device_name_snapshot" TEXT NOT NULL,
  "channel_name_snapshot" TEXT NOT NULL,
  "created_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("scheme_id" IS NULL OR (typeof("scheme_id") = 'integer' AND "scheme_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("slot_index" IS NULL OR (typeof("slot_index") = 'integer' AND "slot_index" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("device_code" IS NULL OR length("device_code") <= 20),
  CHECK ("channel_code" IS NULL OR length("channel_code") <= 20),
  CHECK ("device_name_snapshot" IS NULL OR length("device_name_snapshot") <= 255),
  CHECK ("channel_name_snapshot" IS NULL OR length("channel_name_snapshot") <= 255)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_playback_scheme_slot" ON "gb_playback_scheme_slot" ("scheme_id", "slot_index");
CREATE INDEX IF NOT EXISTS "idx_playback_scheme_slot_scheme" ON "gb_playback_scheme_slot" ("scheme_id");

CREATE TABLE IF NOT EXISTS "gb_ptz_cruise_track" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" INTEGER NOT NULL,
  "channel_id" INTEGER NOT NULL,
  "track_id" INTEGER NOT NULL,
  "name" TEXT DEFAULT NULL,
  "enabled" INTEGER DEFAULT NULL,
  "detail_json" TEXT,
  "last_operation_id" TEXT DEFAULT NULL,
  "raw_summary" TEXT,
  "device_time" DATETIME DEFAULT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("track_id" IS NULL OR (typeof("track_id") = 'integer' AND "track_id" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("enabled" IS NULL OR (typeof("enabled") = 'integer' AND "enabled" BETWEEN -128 AND 127)),
  CHECK ("last_operation_id" IS NULL OR length("last_operation_id") <= 64)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_ptz_cruise_channel_track" ON "gb_ptz_cruise_track" ("channel_id", "track_id");
CREATE INDEX IF NOT EXISTS "idx_ptz_cruise_device" ON "gb_ptz_cruise_track" ("device_id");

CREATE TABLE IF NOT EXISTS "gb_ptz_home_position" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" INTEGER NOT NULL,
  "channel_id" INTEGER NOT NULL,
  "channel_code" TEXT NOT NULL,
  "enabled" INTEGER NOT NULL,
  "reset_time" INTEGER DEFAULT NULL,
  "preset_id" INTEGER DEFAULT NULL,
  "enabled_encoding" TEXT NOT NULL DEFAULT 'numeric',
  "confirmed_at" DATETIME NOT NULL,
  "source" TEXT NOT NULL,
  "verification" TEXT NOT NULL,
  "source_sn" INTEGER NOT NULL DEFAULT 0,
  "source_operation_id" TEXT DEFAULT NULL,
  "source_operation_seq" INTEGER NOT NULL DEFAULT 0,
  "raw_summary" TEXT,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_code" IS NULL OR length("channel_code") <= 20),
  CHECK ("enabled" IS NULL OR (typeof("enabled") = 'integer' AND "enabled" BETWEEN -128 AND 127)),
  CHECK ("reset_time" IS NULL OR (typeof("reset_time") = 'integer' AND "reset_time" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("preset_id" IS NULL OR (typeof("preset_id") = 'integer' AND "preset_id" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("enabled_encoding" IS NULL OR length("enabled_encoding") <= 32),
  CHECK ("source" IS NULL OR length("source") <= 32),
  CHECK ("verification" IS NULL OR length("verification") <= 16),
  CHECK ("source_sn" IS NULL OR (typeof("source_sn") = 'integer' AND "source_sn" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("source_operation_id" IS NULL OR length("source_operation_id") <= 64),
  CHECK ("source_operation_seq" IS NULL OR (typeof("source_operation_seq") = 'integer' AND "source_operation_seq" BETWEEN 0 AND 9223372036854775807))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_ptz_home_position_channel" ON "gb_ptz_home_position" ("channel_id");
CREATE INDEX IF NOT EXISTS "idx_ptz_home_position_device" ON "gb_ptz_home_position" ("device_id");

CREATE TABLE IF NOT EXISTS "gb_ptz_operation" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "operation_id" TEXT NOT NULL,
  "idempotency_key" TEXT NOT NULL,
  "device_id" INTEGER NOT NULL,
  "device_code" TEXT NOT NULL,
  "channel_id" INTEGER NOT NULL,
  "channel_code" TEXT NOT NULL,
  "cmd_type" TEXT NOT NULL,
  "action" TEXT DEFAULT NULL,
  "payload_json" TEXT,
  "sn" INTEGER NOT NULL,
  "call_id" TEXT DEFAULT NULL,
  "cseq" TEXT DEFAULT NULL,
  "sip_status" INTEGER NOT NULL DEFAULT 0,
  "device_result" TEXT DEFAULT NULL,
  "device_error" TEXT,
  "status" TEXT NOT NULL,
  "attempt" INTEGER NOT NULL DEFAULT 1,
  "error_code" TEXT DEFAULT NULL,
  "error_message" TEXT,
  "actor_id" INTEGER NOT NULL DEFAULT 0,
  "actor_dept_id" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL,
  "sent_at" DATETIME DEFAULT NULL,
  "completed_at" DATETIME DEFAULT NULL,
  "response_required" INTEGER NOT NULL DEFAULT 0,
  "max_attempts" INTEGER NOT NULL DEFAULT 1,
  "queue_deadline_at" DATETIME DEFAULT NULL,
  "dispatch_started_at" DATETIME DEFAULT NULL,
  "transport_deadline_at" DATETIME DEFAULT NULL,
  "deadline_at" DATETIME DEFAULT NULL,
  "next_attempt_at" DATETIME DEFAULT NULL,
  "response_call_id" TEXT DEFAULT NULL,
  "response_cseq" TEXT DEFAULT NULL,
  "response_at" DATETIME DEFAULT NULL,
  "response_has_data" INTEGER DEFAULT NULL,
  "trigger_operation_id" TEXT DEFAULT NULL,
  "reconcile_operation_id" TEXT DEFAULT NULL,
  "profile_version" TEXT DEFAULT NULL,
  "profile_charset" TEXT DEFAULT NULL,
  "target_scope" TEXT DEFAULT NULL,
  "target_code" TEXT DEFAULT NULL,
  "scope_key" TEXT DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("operation_id" IS NULL OR length("operation_id") <= 64),
  CHECK ("idempotency_key" IS NULL OR length("idempotency_key") <= 128),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_code" IS NULL OR length("device_code") <= 20),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_code" IS NULL OR length("channel_code") <= 20),
  CHECK ("cmd_type" IS NULL OR length("cmd_type") <= 64),
  CHECK ("action" IS NULL OR length("action") <= 64),
  CHECK ("sn" IS NULL OR (typeof("sn") = 'integer' AND "sn" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("call_id" IS NULL OR length("call_id") <= 255),
  CHECK ("cseq" IS NULL OR length("cseq") <= 64),
  CHECK ("sip_status" IS NULL OR (typeof("sip_status") = 'integer' AND "sip_status" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("device_result" IS NULL OR length("device_result") <= 32),
  CHECK ("status" IS NULL OR length("status") <= 16),
  CHECK ("attempt" IS NULL OR (typeof("attempt") = 'integer' AND "attempt" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("error_code" IS NULL OR length("error_code") <= 64),
  CHECK ("actor_id" IS NULL OR (typeof("actor_id") = 'integer' AND "actor_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("actor_dept_id" IS NULL OR (typeof("actor_dept_id") = 'integer' AND "actor_dept_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("response_required" IS NULL OR (typeof("response_required") = 'integer' AND "response_required" BETWEEN -128 AND 127)),
  CHECK ("max_attempts" IS NULL OR (typeof("max_attempts") = 'integer' AND "max_attempts" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("response_call_id" IS NULL OR length("response_call_id") <= 255),
  CHECK ("response_cseq" IS NULL OR length("response_cseq") <= 64),
  CHECK ("response_has_data" IS NULL OR (typeof("response_has_data") = 'integer' AND "response_has_data" BETWEEN -128 AND 127)),
  CHECK ("trigger_operation_id" IS NULL OR length("trigger_operation_id") <= 64),
  CHECK ("reconcile_operation_id" IS NULL OR length("reconcile_operation_id") <= 64),
  CHECK ("profile_version" IS NULL OR length("profile_version") <= 8),
  CHECK ("profile_charset" IS NULL OR length("profile_charset") <= 16),
  CHECK ("target_scope" IS NULL OR length("target_scope") <= 16),
  CHECK ("target_code" IS NULL OR length("target_code") <= 20),
  CHECK ("scope_key" IS NULL OR length("scope_key") <= 64)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_ptz_operation_id" ON "gb_ptz_operation" ("operation_id");
CREATE UNIQUE INDEX IF NOT EXISTS "uk_ptz_operation_idempotency" ON "gb_ptz_operation" ("channel_id", "idempotency_key");
CREATE INDEX IF NOT EXISTS "idx_ptz_operation_channel_time" ON "gb_ptz_operation" ("channel_id", "created_at");
CREATE INDEX IF NOT EXISTS "idx_ptz_operation_device_sn" ON "gb_ptz_operation" ("device_id", "sn");
CREATE INDEX IF NOT EXISTS "idx_ptz_operation_status_time" ON "gb_ptz_operation" ("status", "created_at");
CREATE INDEX IF NOT EXISTS "idx_ptz_operation_call_id" ON "gb_ptz_operation" ("call_id");
CREATE INDEX IF NOT EXISTS "idx_ptz_operation_channel_cmd_id" ON "gb_ptz_operation" ("channel_id", "cmd_type", "id");
CREATE INDEX IF NOT EXISTS "idx_ptz_operation_status_next_attempt" ON "gb_ptz_operation" ("status", "next_attempt_at");
CREATE INDEX IF NOT EXISTS "idx_ptz_operation_status_queue_deadline" ON "gb_ptz_operation" ("status", "queue_deadline_at");
CREATE INDEX IF NOT EXISTS "idx_ptz_operation_status_transport_deadline" ON "gb_ptz_operation" ("status", "transport_deadline_at");
CREATE INDEX IF NOT EXISTS "idx_ptz_operation_status_deadline" ON "gb_ptz_operation" ("status", "deadline_at");
CREATE INDEX IF NOT EXISTS "idx_ptz_operation_target" ON "gb_ptz_operation" ("device_code", "target_scope", "target_code", "status");
CREATE INDEX IF NOT EXISTS "idx_ptz_operation_device_scope_time" ON "gb_ptz_operation" ("device_id", "scope_key", "created_at");

CREATE TABLE IF NOT EXISTS "gb_ptz_operation_attempt" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "operation_id" INTEGER NOT NULL,
  "attempt_no" INTEGER NOT NULL,
  "sn" INTEGER NOT NULL,
  "status" TEXT NOT NULL,
  "call_id" TEXT DEFAULT NULL,
  "cseq" TEXT DEFAULT NULL,
  "sip_status" INTEGER NOT NULL DEFAULT 0,
  "started_at" DATETIME NOT NULL,
  "lease_until" DATETIME NOT NULL,
  "sent_at" DATETIME DEFAULT NULL,
  "completed_at" DATETIME DEFAULT NULL,
  "error_code" TEXT DEFAULT NULL,
  "error_message" TEXT,
  "created_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("operation_id" IS NULL OR (typeof("operation_id") = 'integer' AND "operation_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("attempt_no" IS NULL OR (typeof("attempt_no") = 'integer' AND "attempt_no" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("sn" IS NULL OR (typeof("sn") = 'integer' AND "sn" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("status" IS NULL OR length("status") <= 16),
  CHECK ("call_id" IS NULL OR length("call_id") <= 255),
  CHECK ("cseq" IS NULL OR length("cseq") <= 64),
  CHECK ("sip_status" IS NULL OR (typeof("sip_status") = 'integer' AND "sip_status" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("error_code" IS NULL OR length("error_code") <= 64)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_ptz_operation_attempt" ON "gb_ptz_operation_attempt" ("operation_id", "attempt_no");
CREATE INDEX IF NOT EXISTS "idx_ptz_attempt_status_lease" ON "gb_ptz_operation_attempt" ("status", "lease_until");

CREATE TABLE IF NOT EXISTS "gb_ptz_preset" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" INTEGER NOT NULL,
  "channel_id" INTEGER NOT NULL,
  "preset_id" INTEGER NOT NULL,
  "name" TEXT DEFAULT NULL,
  "status" TEXT NOT NULL DEFAULT 'unknown',
  "last_operation_id" TEXT DEFAULT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("preset_id" IS NULL OR (typeof("preset_id") = 'integer' AND "preset_id" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("status" IS NULL OR length("status") <= 16),
  CHECK ("last_operation_id" IS NULL OR length("last_operation_id") <= 64)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_ptz_preset_channel_number" ON "gb_ptz_preset" ("channel_id", "preset_id");
CREATE INDEX IF NOT EXISTS "idx_ptz_preset_device" ON "gb_ptz_preset" ("device_id");

CREATE TABLE IF NOT EXISTS "gb_ptz_state" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" INTEGER NOT NULL,
  "device_code" TEXT NOT NULL,
  "channel_id" INTEGER NOT NULL,
  "channel_code" TEXT NOT NULL,
  "pan" NUMERIC DEFAULT NULL,
  "tilt" NUMERIC DEFAULT NULL,
  "zoom" NUMERIC DEFAULT NULL,
  "focus" NUMERIC DEFAULT NULL,
  "iris" NUMERIC DEFAULT NULL,
  "device_time" DATETIME DEFAULT NULL,
  "received_at" DATETIME NOT NULL,
  "source_sn" INTEGER NOT NULL DEFAULT 0,
  "freshness" TEXT NOT NULL DEFAULT 'unknown',
  "dedupe_key" TEXT DEFAULT NULL,
  "raw_summary" TEXT,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_code" IS NULL OR length("device_code") <= 20),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_code" IS NULL OR length("channel_code") <= 20),
  CHECK ("source_sn" IS NULL OR (typeof("source_sn") = 'integer' AND "source_sn" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("freshness" IS NULL OR length("freshness") <= 16),
  CHECK ("dedupe_key" IS NULL OR length("dedupe_key") <= 128)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_ptz_state_channel" ON "gb_ptz_state" ("channel_id");
CREATE INDEX IF NOT EXISTS "idx_ptz_state_device" ON "gb_ptz_state" ("device_id");
CREATE INDEX IF NOT EXISTS "idx_ptz_state_received" ON "gb_ptz_state" ("received_at");

CREATE TABLE IF NOT EXISTS "gb_recording_plan_gap" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "plan_id" INTEGER DEFAULT NULL,
  "channel_id" INTEGER NOT NULL,
  "started_at" DATETIME NOT NULL,
  "ended_at" DATETIME DEFAULT NULL,
  "duration_ms" INTEGER NOT NULL DEFAULT 0,
  "reason_code" TEXT NOT NULL,
  "reason_message" TEXT NOT NULL DEFAULT '',
  "recovered" INTEGER NOT NULL DEFAULT 0,
  "execution_id" INTEGER DEFAULT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("plan_id" IS NULL OR (typeof("plan_id") = 'integer' AND "plan_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 4294967295)),
  CHECK ("duration_ms" IS NULL OR (typeof("duration_ms") = 'integer')),
  CHECK ("reason_code" IS NULL OR length("reason_code") <= 64),
  CHECK ("reason_message" IS NULL OR length("reason_message") <= 500),
  CHECK ("recovered" IS NULL OR (typeof("recovered") = 'integer' AND "recovered" BETWEEN -128 AND 127)),
  CHECK ("execution_id" IS NULL OR (typeof("execution_id") = 'integer' AND "execution_id" BETWEEN 0 AND 9223372036854775807))
);
CREATE INDEX IF NOT EXISTS "idx_recording_plan_gap_channel" ON "gb_recording_plan_gap" ("channel_id","started_at");
CREATE INDEX IF NOT EXISTS "idx_recording_plan_gap_plan" ON "gb_recording_plan_gap" ("plan_id","started_at");

CREATE TABLE IF NOT EXISTS "gb_recording_plan_execution" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "plan_id" INTEGER DEFAULT NULL,
  "channel_id" INTEGER NOT NULL,
  "device_id" TEXT NOT NULL DEFAULT '',
  "action" TEXT NOT NULL,
  "trigger_source" TEXT NOT NULL,
  "stage" TEXT NOT NULL DEFAULT '',
  "attempt" INTEGER NOT NULL DEFAULT 1,
  "result" TEXT NOT NULL,
  "reason_code" TEXT NOT NULL DEFAULT '',
  "reason_message" TEXT NOT NULL DEFAULT '',
  "stream_id" TEXT NOT NULL DEFAULT '',
  "node_id" TEXT NOT NULL DEFAULT '',
  "recording_session_id" INTEGER DEFAULT NULL,
  "generation" INTEGER NOT NULL DEFAULT 0,
  "started_at" DATETIME NOT NULL,
  "ended_at" DATETIME DEFAULT NULL,
  "duration_ms" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("plan_id" IS NULL OR (typeof("plan_id") = 'integer' AND "plan_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 4294967295)),
  CHECK ("device_id" IS NULL OR length("device_id") <= 20),
  CHECK ("action" IS NULL OR length("action") <= 32),
  CHECK ("trigger_source" IS NULL OR length("trigger_source") <= 32),
  CHECK ("stage" IS NULL OR length("stage") <= 32),
  CHECK ("attempt" IS NULL OR (typeof("attempt") = 'integer' AND "attempt" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("result" IS NULL OR length("result") <= 24),
  CHECK ("reason_code" IS NULL OR length("reason_code") <= 64),
  CHECK ("reason_message" IS NULL OR length("reason_message") <= 500),
  CHECK ("stream_id" IS NULL OR length("stream_id") <= 64),
  CHECK ("node_id" IS NULL OR length("node_id") <= 64),
  CHECK ("recording_session_id" IS NULL OR (typeof("recording_session_id") = 'integer' AND "recording_session_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("generation" IS NULL OR (typeof("generation") = 'integer' AND "generation" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("duration_ms" IS NULL OR (typeof("duration_ms") = 'integer'))
);
CREATE INDEX IF NOT EXISTS "idx_recording_plan_execution_channel" ON "gb_recording_plan_execution" ("channel_id","started_at");
CREATE INDEX IF NOT EXISTS "idx_recording_plan_execution_plan" ON "gb_recording_plan_execution" ("plan_id","started_at");

CREATE TABLE IF NOT EXISTS "gb_recording_plan_channel_state" (
  "channel_id" INTEGER NOT NULL,
  "plan_id" INTEGER DEFAULT NULL,
  "plan_version" INTEGER NOT NULL DEFAULT 0,
  "desired_state" TEXT NOT NULL,
  "actual_state" TEXT NOT NULL,
  "reason_code" TEXT NOT NULL DEFAULT '',
  "reason_message" TEXT NOT NULL DEFAULT '',
  "next_transition_at" DATETIME DEFAULT NULL,
  "next_retry_at" DATETIME DEFAULT NULL,
  "reconcile_at" DATETIME NOT NULL,
  "attempt_count" INTEGER NOT NULL DEFAULT 0,
  "generation" INTEGER NOT NULL DEFAULT 0,
  "stream_id" TEXT NOT NULL DEFAULT '',
  "recording_session_id" INTEGER DEFAULT NULL,
  "node_id" TEXT NOT NULL DEFAULT '',
  "last_media_at" DATETIME DEFAULT NULL,
  "last_success_at" DATETIME DEFAULT NULL,
  "lease_owner" TEXT NOT NULL DEFAULT '',
  "lease_until" DATETIME DEFAULT NULL,
  "state_version" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 4294967295)),
  CHECK ("plan_id" IS NULL OR (typeof("plan_id") = 'integer' AND "plan_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("plan_version" IS NULL OR (typeof("plan_version") = 'integer' AND "plan_version" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("desired_state" IS NULL OR length("desired_state") <= 24),
  CHECK ("actual_state" IS NULL OR length("actual_state") <= 32),
  CHECK ("reason_code" IS NULL OR length("reason_code") <= 64),
  CHECK ("reason_message" IS NULL OR length("reason_message") <= 500),
  CHECK ("attempt_count" IS NULL OR (typeof("attempt_count") = 'integer' AND "attempt_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("generation" IS NULL OR (typeof("generation") = 'integer' AND "generation" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("stream_id" IS NULL OR length("stream_id") <= 64),
  CHECK ("recording_session_id" IS NULL OR (typeof("recording_session_id") = 'integer' AND "recording_session_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("node_id" IS NULL OR length("node_id") <= 64),
  CHECK ("lease_owner" IS NULL OR length("lease_owner") <= 128),
  CHECK ("state_version" IS NULL OR (typeof("state_version") = 'integer' AND "state_version" BETWEEN 0 AND 9223372036854775807)),
  PRIMARY KEY ("channel_id")
);
CREATE INDEX IF NOT EXISTS "idx_recording_plan_state_reconcile" ON "gb_recording_plan_channel_state" ("reconcile_at","channel_id");
CREATE INDEX IF NOT EXISTS "idx_recording_plan_state_retry" ON "gb_recording_plan_channel_state" ("next_retry_at","channel_id");
CREATE INDEX IF NOT EXISTS "idx_recording_plan_state_plan_actual" ON "gb_recording_plan_channel_state" ("plan_id","actual_state");

CREATE TABLE IF NOT EXISTS "gb_recording_plan_binding" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "plan_id" INTEGER NOT NULL,
  "channel_id" INTEGER NOT NULL,
  "owner_dept_id" INTEGER NOT NULL,
  "assigned_by" INTEGER NOT NULL DEFAULT 0,
  "assigned_at" DATETIME NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("plan_id" IS NULL OR (typeof("plan_id") = 'integer' AND "plan_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 4294967295)),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 4294967295)),
  CHECK ("assigned_by" IS NULL OR (typeof("assigned_by") = 'integer' AND "assigned_by" BETWEEN 0 AND 4294967295))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_recording_plan_binding_channel" ON "gb_recording_plan_binding" ("channel_id");
CREATE INDEX IF NOT EXISTS "idx_recording_plan_binding_plan" ON "gb_recording_plan_binding" ("plan_id");

CREATE TABLE IF NOT EXISTS "gb_recording_plan_period" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "plan_id" INTEGER NOT NULL,
  "weekday" INTEGER NOT NULL,
  "start_slot" INTEGER NOT NULL,
  "end_slot" INTEGER NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("plan_id" IS NULL OR (typeof("plan_id") = 'integer' AND "plan_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("weekday" IS NULL OR (typeof("weekday") = 'integer' AND "weekday" BETWEEN -128 AND 127)),
  CHECK ("start_slot" IS NULL OR (typeof("start_slot") = 'integer' AND "start_slot" BETWEEN -32768 AND 32767)),
  CHECK ("end_slot" IS NULL OR (typeof("end_slot") = 'integer' AND "end_slot" BETWEEN -32768 AND 32767))
);
CREATE INDEX IF NOT EXISTS "idx_recording_plan_period_plan_weekday" ON "gb_recording_plan_period" ("plan_id","weekday");

CREATE TABLE IF NOT EXISTS "gb_recording_plan" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT NOT NULL,
  "description" TEXT NOT NULL DEFAULT '',
  "status" INTEGER NOT NULL DEFAULT 1,
  "version" INTEGER NOT NULL DEFAULT 1,
  "owner_dept_id" INTEGER NOT NULL,
  "created_by" INTEGER NOT NULL DEFAULT 0,
  "updated_by" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("name" IS NULL OR length("name") <= 128),
  CHECK ("description" IS NULL OR length("description") <= 500),
  CHECK ("status" IS NULL OR (typeof("status") = 'integer' AND "status" BETWEEN -128 AND 127)),
  CHECK ("version" IS NULL OR (typeof("version") = 'integer' AND "version" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 4294967295)),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295)),
  CHECK ("updated_by" IS NULL OR (typeof("updated_by") = 'integer' AND "updated_by" BETWEEN 0 AND 4294967295))
);
CREATE INDEX IF NOT EXISTS "idx_recording_plan_dept_deleted" ON "gb_recording_plan" ("owner_dept_id","deleted_at");

CREATE TABLE IF NOT EXISTS "gb_recording_file" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "session_id" INTEGER DEFAULT NULL,
  "channel_id" INTEGER NOT NULL,
  "device_id" TEXT NOT NULL,
  "node_id" INTEGER NOT NULL,
  "vhost" TEXT NOT NULL,
  "app" TEXT NOT NULL,
  "stream" TEXT NOT NULL,
  "file_name" TEXT NOT NULL,
  "file_path" TEXT NOT NULL,
  "folder" TEXT NOT NULL DEFAULT '',
  "url" TEXT NOT NULL DEFAULT '',
  "start_time" DATETIME DEFAULT NULL,
  "time_len" NUMERIC DEFAULT NULL,
  "file_size" INTEGER DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "channel_code" TEXT NOT NULL DEFAULT '',
  "channel_name" TEXT NOT NULL DEFAULT '',
  "device_name" TEXT NOT NULL DEFAULT '',
  "owner_dept_id" INTEGER NOT NULL DEFAULT 0,
  "file_key" TEXT NOT NULL,
  "source" TEXT NOT NULL DEFAULT 'hook',
  "metadata_state" TEXT NOT NULL DEFAULT 'complete',
  "record_date" DATE DEFAULT NULL,
  "discovered_at" DATETIME NOT NULL,
  "last_seen_at" DATETIME DEFAULT NULL,
  "missing_at" DATETIME DEFAULT NULL,
  "reconcile_miss_count" INTEGER NOT NULL DEFAULT 0,
  "updated_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("session_id" IS NULL OR (typeof("session_id") = 'integer' AND "session_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 4294967295)),
  CHECK ("device_id" IS NULL OR length("device_id") <= 20),
  CHECK ("node_id" IS NULL OR (typeof("node_id") = 'integer')),
  CHECK ("vhost" IS NULL OR length("vhost") <= 128),
  CHECK ("app" IS NULL OR length("app") <= 64),
  CHECK ("stream" IS NULL OR length("stream") <= 64),
  CHECK ("file_name" IS NULL OR length("file_name") <= 255),
  CHECK ("file_path" IS NULL OR length("file_path") <= 1000),
  CHECK ("folder" IS NULL OR length("folder") <= 1000),
  CHECK ("url" IS NULL OR length("url") <= 1000),
  CHECK ("file_size" IS NULL OR (typeof("file_size") = 'integer' AND "file_size" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_code" IS NULL OR length("channel_code") <= 20),
  CHECK ("channel_name" IS NULL OR length("channel_name") <= 255),
  CHECK ("device_name" IS NULL OR length("device_name") <= 255),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("file_key" IS NULL OR length("file_key") <= 64),
  CHECK ("source" IS NULL OR length("source") <= 16),
  CHECK ("metadata_state" IS NULL OR length("metadata_state") <= 16),
  CHECK ("reconcile_miss_count" IS NULL OR (typeof("reconcile_miss_count") = 'integer' AND "reconcile_miss_count" BETWEEN -2147483648 AND 2147483647))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_recording_file_key" ON "gb_recording_file" ("file_key");
CREATE INDEX IF NOT EXISTS "idx_recording_file_channel_start" ON "gb_recording_file" ("channel_id", "start_time");
CREATE INDEX IF NOT EXISTS "idx_recording_file_device_start" ON "gb_recording_file" ("device_id", "start_time");
CREATE INDEX IF NOT EXISTS "idx_recording_file_date_tuple" ON "gb_recording_file" ("node_id", "vhost", "app", "stream", "record_date");
CREATE INDEX IF NOT EXISTS "idx_recording_file_missing" ON "gb_recording_file" ("missing_at", "reconcile_miss_count");

CREATE TABLE IF NOT EXISTS "gb_recording_reconcile_state" (
  "node_id" INTEGER NOT NULL,
  "status" TEXT NOT NULL DEFAULT 'queued',
  "trigger_source" TEXT NOT NULL DEFAULT 'scheduled',
  "requested_start" DATETIME DEFAULT NULL,
  "requested_end" DATETIME DEFAULT NULL,
  "effective_start" DATETIME DEFAULT NULL,
  "effective_end" DATETIME DEFAULT NULL,
  "started_at" DATETIME DEFAULT NULL,
  "finished_at" DATETIME DEFAULT NULL,
  "candidate_count" INTEGER NOT NULL DEFAULT 0,
  "success_count" INTEGER NOT NULL DEFAULT 0,
  "failure_count" INTEGER NOT NULL DEFAULT 0,
  "discovered_count" INTEGER NOT NULL DEFAULT 0,
  "inserted_count" INTEGER NOT NULL DEFAULT 0,
  "updated_count" INTEGER NOT NULL DEFAULT 0,
  "missing_count" INTEGER NOT NULL DEFAULT 0,
  "unattributed_count" INTEGER NOT NULL DEFAULT 0,
  "last_error" TEXT NOT NULL DEFAULT '',
  "updated_at" DATETIME NOT NULL,
  CHECK ("node_id" IS NULL OR (typeof("node_id") = 'integer')),
  CHECK ("status" IS NULL OR length("status") <= 16),
  CHECK ("trigger_source" IS NULL OR length("trigger_source") <= 16),
  CHECK ("candidate_count" IS NULL OR (typeof("candidate_count") = 'integer' AND "candidate_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("success_count" IS NULL OR (typeof("success_count") = 'integer' AND "success_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("failure_count" IS NULL OR (typeof("failure_count") = 'integer' AND "failure_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("discovered_count" IS NULL OR (typeof("discovered_count") = 'integer' AND "discovered_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("inserted_count" IS NULL OR (typeof("inserted_count") = 'integer' AND "inserted_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("updated_count" IS NULL OR (typeof("updated_count") = 'integer' AND "updated_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("missing_count" IS NULL OR (typeof("missing_count") = 'integer' AND "missing_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("unattributed_count" IS NULL OR (typeof("unattributed_count") = 'integer' AND "unattributed_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("last_error" IS NULL OR length("last_error") <= 500),
  PRIMARY KEY ("node_id")
);

CREATE TABLE IF NOT EXISTS "gb_recording_session" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "channel_id" INTEGER NOT NULL,
  "device_id" TEXT NOT NULL,
  "node_id" INTEGER NOT NULL,
  "vhost" TEXT NOT NULL DEFAULT '__defaultVhost__',
  "app" TEXT NOT NULL DEFAULT 'rtp',
  "stream" TEXT NOT NULL,
  "state" TEXT NOT NULL,
  "started_at" DATETIME DEFAULT NULL,
  "stopped_at" DATETIME DEFAULT NULL,
  "last_checked_at" DATETIME DEFAULT NULL,
  "last_error" TEXT NOT NULL DEFAULT '',
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 4294967295)),
  CHECK ("device_id" IS NULL OR length("device_id") <= 20),
  CHECK ("node_id" IS NULL OR (typeof("node_id") = 'integer')),
  CHECK ("vhost" IS NULL OR length("vhost") <= 128),
  CHECK ("app" IS NULL OR length("app") <= 64),
  CHECK ("stream" IS NULL OR length("stream") <= 64),
  CHECK ("state" IS NULL OR length("state") <= 20),
  CHECK ("last_error" IS NULL OR length("last_error") <= 500)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_recording_session_media" ON "gb_recording_session" ("node_id", "vhost", "app", "stream");
CREATE INDEX IF NOT EXISTS "idx_recording_session_channel_state" ON "gb_recording_session" ("channel_id", "state");

CREATE TABLE IF NOT EXISTS "gb_sip_config" (
  "id" INTEGER NOT NULL,
  "deployment_mode" TEXT NOT NULL,
  "listen_ip" TEXT NOT NULL,
  "advertise_ip" TEXT NOT NULL,
  "advertise_ip_inferred" INTEGER NOT NULL DEFAULT 0,
  "port" INTEGER NOT NULL,
  "domain" TEXT NOT NULL,
  "server_id" TEXT NOT NULL,
  "password" TEXT NOT NULL,
  "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 255)),
  CHECK ("deployment_mode" IS NULL OR length("deployment_mode") <= 8),
  CHECK ("listen_ip" IS NULL OR length("listen_ip") <= 45),
  CHECK ("advertise_ip" IS NULL OR length("advertise_ip") <= 45),
  CHECK ("advertise_ip_inferred" IS NULL OR (typeof("advertise_ip_inferred") = 'integer' AND "advertise_ip_inferred" BETWEEN -128 AND 127)),
  CHECK ("port" IS NULL OR (typeof("port") = 'integer' AND "port" BETWEEN 0 AND 4294967295)),
  CHECK ("domain" IS NULL OR length("domain") <= 10),
  CHECK ("server_id" IS NULL OR length("server_id") <= 20),
  CHECK ("password" IS NULL OR length("password") <= 255),
  PRIMARY KEY ("id"),
  CONSTRAINT "chk_gb_sip_config_deployment_mode" CHECK (("deployment_mode" in ('lan','public'))),
  CONSTRAINT "chk_gb_sip_config_port" CHECK (("port" between 1 and 65535)),
  CONSTRAINT "chk_gb_sip_config_singleton" CHECK (("id" = 1))
);

CREATE TABLE IF NOT EXISTS "gb_sip_security_access_rule" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "list_type" TEXT NOT NULL,
  "match_type" TEXT NOT NULL,
  "match_value" TEXT NOT NULL,
  "scope" TEXT NOT NULL DEFAULT 'all_sip',
  "status" TEXT NOT NULL DEFAULT 'enabled',
  "expires_at" DATETIME DEFAULT NULL,
  "note" TEXT NOT NULL DEFAULT '',
  "created_by" TEXT NOT NULL DEFAULT '',
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer')),
  CHECK ("list_type" IS NULL OR length("list_type") <= 16),
  CHECK ("match_type" IS NULL OR length("match_type") <= 16),
  CHECK ("match_value" IS NULL OR length("match_value") <= 255),
  CHECK ("scope" IS NULL OR length("scope") <= 32),
  CHECK ("status" IS NULL OR length("status") <= 16),
  CHECK ("note" IS NULL OR length("note") <= 255),
  CHECK ("created_by" IS NULL OR length("created_by") <= 64)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_gb_sip_security_access_rule_match" ON "gb_sip_security_access_rule" ("list_type", "match_type", "match_value");
CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_access_rule_list_status" ON "gb_sip_security_access_rule" ("list_type", "status");

CREATE TABLE IF NOT EXISTS "gb_sip_security_audit" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "actor" TEXT NOT NULL,
  "action" TEXT NOT NULL,
  "target" TEXT NOT NULL,
  "reason" TEXT NOT NULL,
  "decision_id" TEXT NOT NULL DEFAULT '',
  "created_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer')),
  CHECK ("actor" IS NULL OR length("actor") <= 64),
  CHECK ("action" IS NULL OR length("action") <= 32),
  CHECK ("target" IS NULL OR length("target") <= 128),
  CHECK ("reason" IS NULL OR length("reason") <= 255),
  CHECK ("decision_id" IS NULL OR length("decision_id") <= 64)
);
CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_audit_time" ON "gb_sip_security_audit" ("created_at");

CREATE TABLE IF NOT EXISTS "gb_sip_security_ban" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "source_ip" TEXT NOT NULL,
  "device_id" TEXT DEFAULT NULL,
  "risk_scope" TEXT DEFAULT NULL,
  "address_family" TEXT NOT NULL,
  "status" TEXT NOT NULL,
  "reason" TEXT NOT NULL,
  "rule_id" TEXT NOT NULL,
  "score" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL,
  "expires_at" DATETIME DEFAULT NULL,
  "unbanned_at" DATETIME DEFAULT NULL,
  "unbanned_by" TEXT NOT NULL DEFAULT '',
  "origin" TEXT NOT NULL,
  "agent_state" TEXT NOT NULL,
  "decision_id" TEXT NOT NULL,
  "last_error" TEXT NOT NULL DEFAULT '',
  "trigger_method" TEXT NOT NULL DEFAULT '',
  "trigger_count" INTEGER NOT NULL DEFAULT 0,
  "trigger_threshold" INTEGER NOT NULL DEFAULT 0,
  "window_seconds" INTEGER NOT NULL DEFAULT 0,
  "policy_mode" TEXT NOT NULL DEFAULT '',
  "firewall_applied_at" DATETIME DEFAULT NULL,
  "blocked_count_after_ban" INTEGER NOT NULL DEFAULT 0,
  "last_blocked_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer')),
  CHECK ("source_ip" IS NULL OR length("source_ip") <= 64),
  CHECK ("device_id" IS NULL OR length("device_id") <= 64),
  CHECK ("risk_scope" IS NULL OR length("risk_scope") <= 16),
  CHECK ("address_family" IS NULL OR length("address_family") <= 8),
  CHECK ("status" IS NULL OR length("status") <= 16),
  CHECK ("reason" IS NULL OR length("reason") <= 32),
  CHECK ("rule_id" IS NULL OR length("rule_id") <= 64),
  CHECK ("score" IS NULL OR (typeof("score") = 'integer' AND "score" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("unbanned_by" IS NULL OR length("unbanned_by") <= 64),
  CHECK ("origin" IS NULL OR length("origin") <= 16),
  CHECK ("agent_state" IS NULL OR length("agent_state") <= 16),
  CHECK ("decision_id" IS NULL OR length("decision_id") <= 64),
  CHECK ("last_error" IS NULL OR length("last_error") <= 512),
  CHECK ("trigger_method" IS NULL OR length("trigger_method") <= 16),
  CHECK ("trigger_count" IS NULL OR (typeof("trigger_count") = 'integer' AND "trigger_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("trigger_threshold" IS NULL OR (typeof("trigger_threshold") = 'integer' AND "trigger_threshold" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("window_seconds" IS NULL OR (typeof("window_seconds") = 'integer' AND "window_seconds" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("policy_mode" IS NULL OR length("policy_mode") <= 16),
  CHECK ("blocked_count_after_ban" IS NULL OR (typeof("blocked_count_after_ban") = 'integer'))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_gb_sip_security_ban_decision" ON "gb_sip_security_ban" ("decision_id");
CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_ban_source_status" ON "gb_sip_security_ban" ("source_ip", "status");
CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_ban_attribution_status" ON "gb_sip_security_ban" ("risk_scope", "device_id", "status");
CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_ban_expiry" ON "gb_sip_security_ban" ("expires_at");

CREATE TABLE IF NOT EXISTS "gb_sip_security_event" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "bucket_at" DATETIME NOT NULL,
  "source_ip" TEXT NOT NULL,
  "device_id" TEXT DEFAULT NULL,
  "risk_scope" TEXT DEFAULT NULL,
  "address_family" TEXT NOT NULL,
  "transport" TEXT NOT NULL,
  "method" TEXT NOT NULL,
  "user_agent" TEXT NOT NULL DEFAULT '',
  "reason" TEXT NOT NULL,
  "action" TEXT NOT NULL,
  "count" INTEGER NOT NULL DEFAULT 0,
  "score_delta" INTEGER NOT NULL DEFAULT 0,
  "first_seen_at" DATETIME NOT NULL,
  "last_seen_at" DATETIME NOT NULL,
  "sample_event_id" TEXT NOT NULL DEFAULT '',
  CHECK ("id" IS NULL OR (typeof("id") = 'integer')),
  CHECK ("source_ip" IS NULL OR length("source_ip") <= 64),
  CHECK ("device_id" IS NULL OR length("device_id") <= 64),
  CHECK ("risk_scope" IS NULL OR length("risk_scope") <= 16),
  CHECK ("address_family" IS NULL OR length("address_family") <= 8),
  CHECK ("transport" IS NULL OR length("transport") <= 8),
  CHECK ("method" IS NULL OR length("method") <= 16),
  CHECK ("user_agent" IS NULL OR length("user_agent") <= 255),
  CHECK ("reason" IS NULL OR length("reason") <= 32),
  CHECK ("action" IS NULL OR length("action") <= 16),
  CHECK ("count" IS NULL OR (typeof("count") = 'integer')),
  CHECK ("score_delta" IS NULL OR (typeof("score_delta") = 'integer')),
  CHECK ("sample_event_id" IS NULL OR length("sample_event_id") <= 64)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_gb_sip_security_event" ON "gb_sip_security_event" ("bucket_at", "source_ip", "device_id", "risk_scope", "transport", "method", "reason", "action");
CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_event_source_time" ON "gb_sip_security_event" ("source_ip", "last_seen_at");
CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_event_attribution_time" ON "gb_sip_security_event" ("risk_scope", "device_id", "last_seen_at");
CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_event_reason_time" ON "gb_sip_security_event" ("reason", "last_seen_at");

CREATE TABLE IF NOT EXISTS "gb_sip_security_policy" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "scope_key" TEXT NOT NULL,
  "mode" TEXT NOT NULL,
  "window_seconds" INTEGER NOT NULL,
  "ban_score" INTEGER NOT NULL,
  "max_packet_bytes" INTEGER NOT NULL,
  "max_udp_per_window" INTEGER NOT NULL,
  "max_tcp_connections" INTEGER NOT NULL,
  "sample_per_source" INTEGER NOT NULL,
  "nonce_ttl_seconds" INTEGER NOT NULL,
  "ban_ttl_steps" TEXT NOT NULL,
  "allowlist_text" TEXT NOT NULL,
  "updated_by" INTEGER NOT NULL DEFAULT 0,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer')),
  CHECK ("scope_key" IS NULL OR length("scope_key") <= 32),
  CHECK ("mode" IS NULL OR length("mode") <= 16),
  CHECK ("window_seconds" IS NULL OR (typeof("window_seconds") = 'integer' AND "window_seconds" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("ban_score" IS NULL OR (typeof("ban_score") = 'integer' AND "ban_score" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("max_packet_bytes" IS NULL OR (typeof("max_packet_bytes") = 'integer' AND "max_packet_bytes" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("max_udp_per_window" IS NULL OR (typeof("max_udp_per_window") = 'integer' AND "max_udp_per_window" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("max_tcp_connections" IS NULL OR (typeof("max_tcp_connections") = 'integer' AND "max_tcp_connections" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("sample_per_source" IS NULL OR (typeof("sample_per_source") = 'integer' AND "sample_per_source" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("nonce_ttl_seconds" IS NULL OR (typeof("nonce_ttl_seconds") = 'integer' AND "nonce_ttl_seconds" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("ban_ttl_steps" IS NULL OR length("ban_ttl_steps") <= 1024),
  CHECK ("allowlist_text" IS NULL OR length("allowlist_text") <= 4096),
  CHECK ("updated_by" IS NULL OR (typeof("updated_by") = 'integer'))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_gb_sip_security_policy_scope" ON "gb_sip_security_policy" ("scope_key");

CREATE TABLE IF NOT EXISTS "gb_sip_trace_capture" (
  "id" TEXT NOT NULL,
  "device_id" INTEGER NOT NULL,
  "device_code" TEXT NOT NULL,
  "created_by" INTEGER NOT NULL,
  "started_at" DATETIME NOT NULL,
  "planned_end_at" DATETIME NOT NULL,
  "ended_at" DATETIME DEFAULT NULL,
  "end_reason" TEXT NOT NULL DEFAULT '',
  "active_key" TEXT DEFAULT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR length("id") <= 36),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 4294967295)),
  CHECK ("device_code" IS NULL OR length("device_code") <= 20),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295)),
  CHECK ("end_reason" IS NULL OR length("end_reason") <= 16),
  CHECK ("active_key" IS NULL OR length("active_key") <= 64),
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_sip_trace_capture_active" ON "gb_sip_trace_capture" ("active_key");
CREATE INDEX IF NOT EXISTS "idx_sip_trace_capture_device_started" ON "gb_sip_trace_capture" ("device_id", "started_at");
CREATE INDEX IF NOT EXISTS "idx_sip_trace_capture_device_code" ON "gb_sip_trace_capture" ("device_code");
CREATE INDEX IF NOT EXISTS "idx_sip_trace_capture_created_by" ON "gb_sip_trace_capture" ("created_by");
CREATE INDEX IF NOT EXISTS "idx_sip_trace_capture_planned_end" ON "gb_sip_trace_capture" ("planned_end_at");

CREATE TABLE IF NOT EXISTS "gb_sip_trace_message" (
  "event_id" TEXT NOT NULL,
  "occurred_at" DATETIME NOT NULL,
  "direction" TEXT NOT NULL,
  "transport" TEXT NOT NULL,
  "local_addr" TEXT NOT NULL,
  "remote_addr" TEXT NOT NULL,
  "device_id" TEXT NOT NULL,
  "method" TEXT NOT NULL,
  "status_code" INTEGER NOT NULL,
  "call_id" TEXT NOT NULL,
  "cseq" INTEGER NOT NULL,
  "cseq_method" TEXT NOT NULL,
  "from_uri" TEXT NOT NULL,
  "to_uri" TEXT NOT NULL,
  "from_id" TEXT NOT NULL DEFAULT '',
  "to_id" TEXT NOT NULL DEFAULT '',
  "business_code" TEXT NOT NULL DEFAULT 'unknown',
  "business_type" TEXT NOT NULL DEFAULT '未知业务',
  "business_confidence" TEXT NOT NULL DEFAULT 'none',
  "user_agent" TEXT NOT NULL,
  "malformed" INTEGER NOT NULL DEFAULT 0,
  "parse_error" TEXT NOT NULL,
  "payload_nonce" blob NOT NULL,
  "payload_ciphertext" mediumblob NOT NULL,
  "payload_algorithm" TEXT NOT NULL,
  "payload_key_version" TEXT NOT NULL,
  "payload_digest_sha256" TEXT NOT NULL,
  CHECK ("event_id" IS NULL OR length("event_id") <= 36),
  CHECK ("direction" IS NULL OR length("direction") <= 16),
  CHECK ("transport" IS NULL OR length("transport") <= 16),
  CHECK ("local_addr" IS NULL OR length("local_addr") <= 255),
  CHECK ("remote_addr" IS NULL OR length("remote_addr") <= 255),
  CHECK ("device_id" IS NULL OR length("device_id") <= 64),
  CHECK ("method" IS NULL OR length("method") <= 32),
  CHECK ("status_code" IS NULL OR (typeof("status_code") = 'integer' AND "status_code" BETWEEN 0 AND 65535)),
  CHECK ("call_id" IS NULL OR length("call_id") <= 255),
  CHECK ("cseq" IS NULL OR (typeof("cseq") = 'integer' AND "cseq" BETWEEN 0 AND 4294967295)),
  CHECK ("cseq_method" IS NULL OR length("cseq_method") <= 32),
  CHECK ("from_uri" IS NULL OR length("from_uri") <= 512),
  CHECK ("to_uri" IS NULL OR length("to_uri") <= 512),
  CHECK ("from_id" IS NULL OR length("from_id") <= 64),
  CHECK ("to_id" IS NULL OR length("to_id") <= 64),
  CHECK ("business_code" IS NULL OR length("business_code") <= 64),
  CHECK ("business_type" IS NULL OR length("business_type") <= 64),
  CHECK ("business_confidence" IS NULL OR length("business_confidence") <= 16),
  CHECK ("user_agent" IS NULL OR length("user_agent") <= 512),
  CHECK ("malformed" IS NULL OR (typeof("malformed") = 'integer' AND "malformed" BETWEEN -128 AND 127)),
  CHECK ("parse_error" IS NULL OR length("parse_error") <= 1024),
  CHECK ("payload_algorithm" IS NULL OR length("payload_algorithm") <= 32),
  CHECK ("payload_key_version" IS NULL OR length("payload_key_version") <= 64),
  CHECK ("payload_digest_sha256" IS NULL OR length("payload_digest_sha256") <= 64),
  PRIMARY KEY ("event_id")
);
CREATE INDEX IF NOT EXISTS "idx_gb_sip_trace_occurred_event" ON "gb_sip_trace_message" ("occurred_at", "event_id");
CREATE INDEX IF NOT EXISTS "idx_gb_sip_trace_device_occurred" ON "gb_sip_trace_message" ("device_id", "occurred_at", "event_id");
CREATE INDEX IF NOT EXISTS "idx_gb_sip_trace_call_occurred" ON "gb_sip_trace_message" ("call_id", "occurred_at", "event_id");
CREATE INDEX IF NOT EXISTS "idx_gb_sip_trace_business_occurred" ON "gb_sip_trace_message" ("business_code", "occurred_at");

CREATE TABLE IF NOT EXISTS "gb_sip_trace_session_diagnosis" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "session_day" DATE NOT NULL,
  "observed_at" DATETIME NOT NULL,
  "correlation_key" TEXT NOT NULL,
  "state" TEXT NOT NULL DEFAULT 'active',
  "category" TEXT NOT NULL,
  "code" TEXT NOT NULL,
  "stage" TEXT NOT NULL,
  "source" TEXT NOT NULL,
  "device_id" TEXT NOT NULL DEFAULT '',
  "channel_id" TEXT NOT NULL DEFAULT '',
  "call_id" TEXT NOT NULL DEFAULT '',
  "cseq" INTEGER NOT NULL DEFAULT 0,
  "method" TEXT NOT NULL DEFAULT '',
  "status_code" INTEGER NOT NULL DEFAULT 0,
  "stream_id" TEXT NOT NULL DEFAULT '',
  "resolved_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("correlation_key" IS NULL OR length("correlation_key") <= 128),
  CHECK ("state" IS NULL OR length("state") <= 16),
  CHECK ("category" IS NULL OR length("category") <= 32),
  CHECK ("code" IS NULL OR length("code") <= 64),
  CHECK ("stage" IS NULL OR length("stage") <= 32),
  CHECK ("source" IS NULL OR length("source") <= 32),
  CHECK ("device_id" IS NULL OR length("device_id") <= 64),
  CHECK ("channel_id" IS NULL OR length("channel_id") <= 64),
  CHECK ("call_id" IS NULL OR length("call_id") <= 255),
  CHECK ("cseq" IS NULL OR (typeof("cseq") = 'integer' AND "cseq" BETWEEN 0 AND 4294967295)),
  CHECK ("method" IS NULL OR length("method") <= 32),
  CHECK ("status_code" IS NULL OR (typeof("status_code") = 'integer' AND "status_code" BETWEEN 0 AND 65535)),
  CHECK ("stream_id" IS NULL OR length("stream_id") <= 255)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_sip_trace_diagnosis_session" ON "gb_sip_trace_session_diagnosis" ("session_day", "category", "correlation_key");
CREATE INDEX IF NOT EXISTS "idx_sip_trace_diagnosis_category_state_observed" ON "gb_sip_trace_session_diagnosis" ("session_day", "category", "state", "observed_at");
CREATE INDEX IF NOT EXISTS "idx_sip_trace_diagnosis_device_observed" ON "gb_sip_trace_session_diagnosis" ("device_id", "observed_at");
CREATE INDEX IF NOT EXISTS "idx_sip_trace_diagnosis_call_cseq" ON "gb_sip_trace_session_diagnosis" ("call_id", "cseq");

CREATE TABLE IF NOT EXISTS "gb_talk_session" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "session_id" TEXT NOT NULL,
  "channel_id" INTEGER NOT NULL,
  "device_id" TEXT NOT NULL,
  "target_id" TEXT NOT NULL DEFAULT '',
  "actor_id" INTEGER NOT NULL DEFAULT 0,
  "actor_dept_id" INTEGER NOT NULL DEFAULT 0,
  "mode" TEXT NOT NULL DEFAULT 'talk',
  "broadcast_sn" INTEGER NOT NULL DEFAULT 0,
  "broadcast_reply_status" TEXT NOT NULL DEFAULT '',
  "signal_phase" TEXT NOT NULL DEFAULT '',
  "node_id" INTEGER NOT NULL DEFAULT 0,
  "app" TEXT NOT NULL DEFAULT 'talk',
  "source_stream" TEXT NOT NULL,
  "recv_stream" TEXT NOT NULL DEFAULT '',
  "ssrc" TEXT NOT NULL DEFAULT '',
  "state" TEXT NOT NULL,
  "lease_key" INTEGER DEFAULT NULL,
  "source_key" TEXT DEFAULT NULL,
  "recv_key" TEXT DEFAULT NULL,
  "ssrc_key" TEXT DEFAULT NULL,
  "expires_at" DATETIME NOT NULL,
  "publish_token_hash" TEXT NOT NULL,
  "token_consumed_at" DATETIME DEFAULT NULL,
  "publish_id" TEXT NOT NULL DEFAULT '',
  "local_port" INTEGER NOT NULL DEFAULT 0,
  "remote_media_ip" TEXT NOT NULL DEFAULT '',
  "remote_media_port" INTEGER NOT NULL DEFAULT 0,
  "media_transport" TEXT NOT NULL DEFAULT '',
  "sender_mode" TEXT NOT NULL DEFAULT '',
  "call_id" TEXT NOT NULL DEFAULT '',
  "dialog_local_tag" TEXT NOT NULL DEFAULT '',
  "dialog_remote_tag" TEXT NOT NULL DEFAULT '',
  "dialog_remote_uri" TEXT NOT NULL DEFAULT '',
  "dialog_cseq" INTEGER NOT NULL DEFAULT 0,
  "error" TEXT NOT NULL DEFAULT '',
  "started_at" DATETIME DEFAULT NULL,
  "ended_at" DATETIME DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("session_id" IS NULL OR length("session_id") <= 64),
  CHECK ("channel_id" IS NULL OR (typeof("channel_id") = 'integer' AND "channel_id" BETWEEN 0 AND 4294967295)),
  CHECK ("device_id" IS NULL OR length("device_id") <= 20),
  CHECK ("target_id" IS NULL OR length("target_id") <= 20),
  CHECK ("actor_id" IS NULL OR (typeof("actor_id") = 'integer' AND "actor_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("actor_dept_id" IS NULL OR (typeof("actor_dept_id") = 'integer' AND "actor_dept_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("mode" IS NULL OR length("mode") <= 16),
  CHECK ("broadcast_sn" IS NULL OR (typeof("broadcast_sn") = 'integer' AND "broadcast_sn" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("broadcast_reply_status" IS NULL OR length("broadcast_reply_status") <= 32),
  CHECK ("signal_phase" IS NULL OR length("signal_phase") <= 40),
  CHECK ("node_id" IS NULL OR (typeof("node_id") = 'integer')),
  CHECK ("app" IS NULL OR length("app") <= 64),
  CHECK ("source_stream" IS NULL OR length("source_stream") <= 128),
  CHECK ("recv_stream" IS NULL OR length("recv_stream") <= 128),
  CHECK ("ssrc" IS NULL OR length("ssrc") <= 32),
  CHECK ("state" IS NULL OR length("state") <= 20),
  CHECK ("lease_key" IS NULL OR (typeof("lease_key") = 'integer' AND "lease_key" BETWEEN 0 AND 4294967295)),
  CHECK ("source_key" IS NULL OR length("source_key") <= 128),
  CHECK ("recv_key" IS NULL OR length("recv_key") <= 128),
  CHECK ("ssrc_key" IS NULL OR length("ssrc_key") <= 32),
  CHECK ("publish_token_hash" IS NULL OR length("publish_token_hash") <= 64),
  CHECK ("publish_id" IS NULL OR length("publish_id") <= 255),
  CHECK ("local_port" IS NULL OR (typeof("local_port") = 'integer' AND "local_port" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("remote_media_ip" IS NULL OR length("remote_media_ip") <= 64),
  CHECK ("remote_media_port" IS NULL OR (typeof("remote_media_port") = 'integer' AND "remote_media_port" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("media_transport" IS NULL OR length("media_transport") <= 16),
  CHECK ("sender_mode" IS NULL OR length("sender_mode") <= 24),
  CHECK ("call_id" IS NULL OR length("call_id") <= 255),
  CHECK ("dialog_local_tag" IS NULL OR length("dialog_local_tag") <= 128),
  CHECK ("dialog_remote_tag" IS NULL OR length("dialog_remote_tag") <= 128),
  CHECK ("dialog_remote_uri" IS NULL OR length("dialog_remote_uri") <= 512),
  CHECK ("dialog_cseq" IS NULL OR (typeof("dialog_cseq") = 'integer' AND "dialog_cseq" BETWEEN 0 AND 4294967295)),
  CHECK ("error" IS NULL OR length("error") <= 500)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_talk_session_session_id" ON "gb_talk_session" ("session_id");
CREATE UNIQUE INDEX IF NOT EXISTS "uk_talk_session_lease" ON "gb_talk_session" ("lease_key");
CREATE UNIQUE INDEX IF NOT EXISTS "uk_talk_session_source_key" ON "gb_talk_session" ("source_key");
CREATE UNIQUE INDEX IF NOT EXISTS "uk_talk_session_recv_key" ON "gb_talk_session" ("recv_key");
CREATE UNIQUE INDEX IF NOT EXISTS "uk_talk_session_ssrc_key" ON "gb_talk_session" ("ssrc_key");
CREATE INDEX IF NOT EXISTS "idx_talk_session_channel_created" ON "gb_talk_session" ("channel_id", "created_at");
CREATE INDEX IF NOT EXISTS "idx_talk_session_state_expires" ON "gb_talk_session" ("state", "expires_at");
CREATE INDEX IF NOT EXISTS "idx_talk_session_source" ON "gb_talk_session" ("node_id", "app", "source_stream");
CREATE INDEX IF NOT EXISTS "idx_talk_session_recv" ON "gb_talk_session" ("node_id", "recv_stream");
CREATE INDEX IF NOT EXISTS "idx_talk_session_call_id" ON "gb_talk_session" ("call_id");
CREATE INDEX IF NOT EXISTS "idx_talk_session_broadcast_match" ON "gb_talk_session" ("device_id", "target_id", "state");
CREATE INDEX IF NOT EXISTS "idx_talk_session_broadcast_sn" ON "gb_talk_session" ("broadcast_sn");

CREATE TABLE IF NOT EXISTS "meta_node" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "revision" INTEGER NOT NULL DEFAULT '1',
  "name" TEXT NOT NULL DEFAULT '',
  "host" TEXT NOT NULL DEFAULT '',
  "receive_host" TEXT NOT NULL DEFAULT '',
  "playback_host" TEXT NOT NULL DEFAULT '',
  "api_port" INTEGER NOT NULL DEFAULT 18080,
  "api_secret" TEXT NOT NULL DEFAULT '',
  "media_server_uuid" TEXT NOT NULL DEFAULT '',
  "weight" INTEGER NOT NULL DEFAULT 50,
  "tags_json" TEXT,
  "state" TEXT NOT NULL DEFAULT 'active',
  "recovery_required" INTEGER NOT NULL DEFAULT '0',
  "recovery_reason" TEXT NOT NULL DEFAULT '',
  "recovery_fingerprint" TEXT NOT NULL DEFAULT '',
  "rtp_port_start" INTEGER NOT NULL DEFAULT 30000,
  "rtp_port_end" INTEGER NOT NULL DEFAULT 35000,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("revision" IS NULL OR (typeof("revision") = 'integer' AND "revision" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("name" IS NULL OR length("name") <= 64),
  CHECK ("host" IS NULL OR length("host") <= 64),
  CHECK ("receive_host" IS NULL OR length("receive_host") <= 255),
  CHECK ("playback_host" IS NULL OR length("playback_host") <= 255),
  CHECK ("api_port" IS NULL OR (typeof("api_port") = 'integer' AND "api_port" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("api_secret" IS NULL OR length("api_secret") <= 128),
  CHECK ("media_server_uuid" IS NULL OR length("media_server_uuid") <= 64),
  CHECK ("weight" IS NULL OR (typeof("weight") = 'integer' AND "weight" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("state" IS NULL OR length("state") <= 16),
  CHECK ("recovery_required" IS NULL OR (typeof("recovery_required") = 'integer' AND "recovery_required" BETWEEN -128 AND 127)),
  CHECK ("recovery_reason" IS NULL OR length("recovery_reason") <= 255),
  CHECK ("recovery_fingerprint" IS NULL OR length("recovery_fingerprint") <= 64),
  CHECK ("rtp_port_start" IS NULL OR (typeof("rtp_port_start") = 'integer' AND "rtp_port_start" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("rtp_port_end" IS NULL OR (typeof("rtp_port_end") = 'integer' AND "rtp_port_end" BETWEEN -2147483648 AND 2147483647))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_media_server_uuid" ON "meta_node" ("media_server_uuid");
CREATE INDEX IF NOT EXISTS "idx_state" ON "meta_node" ("state");
CREATE INDEX IF NOT EXISTS "idx_recovery_required" ON "meta_node" ("recovery_required");

CREATE TABLE IF NOT EXISTS "gb_zlm_managed_resource" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "node_id" INTEGER NOT NULL,
  "resource_type" TEXT NOT NULL,
  "resource_key" TEXT NOT NULL,
  "app" TEXT NOT NULL DEFAULT '',
  "stream" TEXT NOT NULL DEFAULT '',
  "identity_fingerprint" TEXT NOT NULL,
  "summary" TEXT NOT NULL DEFAULT '',
  "created_by" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL,
  "last_observed_at" DATETIME DEFAULT NULL,
  "tombstoned_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("node_id" IS NULL OR (typeof("node_id") = 'integer')),
  CHECK ("resource_type" IS NULL OR length("resource_type") <= 32),
  CHECK ("resource_key" IS NULL OR length("resource_key") <= 255),
  CHECK ("app" IS NULL OR length("app") <= 64),
  CHECK ("stream" IS NULL OR length("stream") <= 255),
  CHECK ("identity_fingerprint" IS NULL OR length("identity_fingerprint") <= 64),
  CHECK ("summary" IS NULL OR length("summary") <= 512),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 9223372036854775807))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_gb_zlm_managed_resource_identity" ON "gb_zlm_managed_resource" ("node_id","resource_type","resource_key");
CREATE INDEX IF NOT EXISTS "idx_gb_zlm_managed_resource_observed" ON "gb_zlm_managed_resource" ("node_id","last_observed_at");
CREATE INDEX IF NOT EXISTS "idx_gb_zlm_managed_resource_tombstone" ON "gb_zlm_managed_resource" ("node_id","tombstoned_at");

CREATE TABLE IF NOT EXISTS "scheduler_log" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "happened_at" DATETIME NOT NULL,
  "algorithm" TEXT NOT NULL DEFAULT '',
  "node_id" INTEGER NOT NULL DEFAULT 0,
  "node_name" TEXT NOT NULL DEFAULT '',
  "stream_id" TEXT NOT NULL DEFAULT '',
  "device_id" TEXT NOT NULL DEFAULT '',
  "channel_id" TEXT NOT NULL DEFAULT '',
  "error_message" TEXT NOT NULL DEFAULT '',
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("algorithm" IS NULL OR length("algorithm") <= 32),
  CHECK ("node_id" IS NULL OR (typeof("node_id") = 'integer')),
  CHECK ("node_name" IS NULL OR length("node_name") <= 64),
  CHECK ("stream_id" IS NULL OR length("stream_id") <= 64),
  CHECK ("device_id" IS NULL OR length("device_id") <= 64),
  CHECK ("channel_id" IS NULL OR length("channel_id") <= 64),
  CHECK ("error_message" IS NULL OR length("error_message") <= 255)
);
CREATE INDEX IF NOT EXISTS "idx_happened_at" ON "scheduler_log" ("happened_at");

CREATE TABLE IF NOT EXISTS "scheduler_setting" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "algorithm" TEXT NOT NULL DEFAULT 'roundrobin',
  "config_json" TEXT,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("algorithm" IS NULL OR length("algorithm") <= 32)
);

CREATE TABLE IF NOT EXISTS "sys_affix" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT DEFAULT NULL,
  "path" TEXT DEFAULT NULL,
  "url" TEXT DEFAULT NULL,
  "file_md5" TEXT DEFAULT '',
  "size" INTEGER DEFAULT NULL,
  "ftype" TEXT DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "created_by" INTEGER DEFAULT NULL,
  "suffix" TEXT DEFAULT NULL,
  "thumbnail_path" TEXT DEFAULT NULL,
  "thumbnail_name" TEXT DEFAULT NULL,
  "thumbnail_url" TEXT DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("path" IS NULL OR length("path") <= 255),
  CHECK ("url" IS NULL OR length("url") <= 255),
  CHECK ("file_md5" IS NULL OR length("file_md5") <= 32),
  CHECK ("size" IS NULL OR (typeof("size") = 'integer' AND "size" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("ftype" IS NULL OR length("ftype") <= 100),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("suffix" IS NULL OR length("suffix") <= 100),
  CHECK ("thumbnail_path" IS NULL OR length("thumbnail_path") <= 255),
  CHECK ("thumbnail_name" IS NULL OR length("thumbnail_name") <= 255),
  CHECK ("thumbnail_url" IS NULL OR length("thumbnail_url") <= 255)
);
CREATE INDEX IF NOT EXISTS "idx_sys_affix_file_md5" ON "sys_affix" ("file_md5");

CREATE TABLE IF NOT EXISTS "sys_affix_chunk" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "upload_id" TEXT NOT NULL,
  "file_md5" TEXT NOT NULL,
  "file_name" TEXT DEFAULT NULL,
  "file_size" INTEGER DEFAULT NULL,
  "chunk_size" INTEGER DEFAULT NULL,
  "total_chunks" INTEGER DEFAULT NULL,
  "chunk_index" INTEGER NOT NULL,
  "chunk_path" TEXT DEFAULT NULL,
  "status" INTEGER DEFAULT 0,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "created_by" INTEGER DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("upload_id" IS NULL OR length("upload_id") <= 64),
  CHECK ("file_md5" IS NULL OR length("file_md5") <= 32),
  CHECK ("file_name" IS NULL OR length("file_name") <= 255),
  CHECK ("file_size" IS NULL OR (typeof("file_size") = 'integer')),
  CHECK ("chunk_size" IS NULL OR (typeof("chunk_size") = 'integer' AND "chunk_size" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("total_chunks" IS NULL OR (typeof("total_chunks") = 'integer' AND "total_chunks" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("chunk_index" IS NULL OR (typeof("chunk_index") = 'integer' AND "chunk_index" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("chunk_path" IS NULL OR length("chunk_path") <= 255),
  CHECK ("status" IS NULL OR (typeof("status") = 'integer' AND "status" BETWEEN -128 AND 127)),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN -2147483648 AND 2147483647))
);
CREATE INDEX IF NOT EXISTS "idx_upload_id" ON "sys_affix_chunk" ("upload_id");
CREATE INDEX IF NOT EXISTS "idx_file_md5" ON "sys_affix_chunk" ("file_md5");

CREATE TABLE IF NOT EXISTS "sys_api" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "title" TEXT DEFAULT NULL,
  "path" TEXT DEFAULT NULL,
  "method" TEXT DEFAULT NULL,
  "api_group" TEXT DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "created_by" INTEGER DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("title" IS NULL OR length("title") <= 255),
  CHECK ("path" IS NULL OR length("path") <= 255),
  CHECK ("method" IS NULL OR length("method") <= 32),
  CHECK ("api_group" IS NULL OR length("api_group") <= 255),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295))
);

CREATE TABLE IF NOT EXISTS "sys_casbin_rule" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "ptype" TEXT DEFAULT NULL,
  "v0" TEXT DEFAULT NULL,
  "v1" TEXT DEFAULT NULL,
  "v2" TEXT DEFAULT NULL,
  "v3" TEXT DEFAULT NULL,
  "v4" TEXT DEFAULT NULL,
  "v5" TEXT DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("ptype" IS NULL OR length("ptype") <= 100),
  CHECK ("v0" IS NULL OR length("v0") <= 100),
  CHECK ("v1" IS NULL OR length("v1") <= 100),
  CHECK ("v2" IS NULL OR length("v2") <= 100),
  CHECK ("v3" IS NULL OR length("v3") <= 100),
  CHECK ("v4" IS NULL OR length("v4") <= 100),
  CHECK ("v5" IS NULL OR length("v5") <= 100)
);
CREATE UNIQUE INDEX IF NOT EXISTS "idx_casbin_rule" ON "sys_casbin_rule" ("ptype", "v0", "v1", "v2", "v3", "v4", "v5");
CREATE UNIQUE INDEX IF NOT EXISTS "idx_sys_casbin_rule" ON "sys_casbin_rule" ("ptype", "v0", "v1", "v2", "v3", "v4", "v5");

CREATE TABLE IF NOT EXISTS "sys_civil_code" (
  "code" TEXT NOT NULL,
  "name" TEXT NOT NULL,
  "short_name" TEXT DEFAULT NULL,
  "parent_code" TEXT DEFAULT NULL,
  "level" INTEGER NOT NULL,
  "pinyin" TEXT DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  CHECK ("code" IS NULL OR length("code") <= 6),
  CHECK ("name" IS NULL OR length("name") <= 64),
  CHECK ("short_name" IS NULL OR length("short_name") <= 32),
  CHECK ("parent_code" IS NULL OR length("parent_code") <= 6),
  CHECK ("level" IS NULL OR (typeof("level") = 'integer' AND "level" BETWEEN -128 AND 127)),
  CHECK ("pinyin" IS NULL OR length("pinyin") <= 64),
  PRIMARY KEY ("code")
);
CREATE INDEX IF NOT EXISTS "idx_parent_code" ON "sys_civil_code" ("parent_code", "level");
CREATE INDEX IF NOT EXISTS "idx_pinyin" ON "sys_civil_code" ("pinyin");

CREATE TABLE IF NOT EXISTS "sys_department" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "parent_id" INTEGER DEFAULT 0,
  "name" TEXT DEFAULT NULL,
  "status" INTEGER DEFAULT NULL,
  "leader" TEXT DEFAULT NULL,
  "phone" TEXT DEFAULT NULL,
  "email" TEXT DEFAULT NULL,
  "sort" INTEGER DEFAULT 0,
  "describe" TEXT DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "created_by" INTEGER DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("parent_id" IS NULL OR (typeof("parent_id") = 'integer' AND "parent_id" BETWEEN 0 AND 4294967295)),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("status" IS NULL OR (typeof("status") = 'integer' AND "status" BETWEEN -128 AND 127)),
  CHECK ("leader" IS NULL OR length("leader") <= 255),
  CHECK ("phone" IS NULL OR length("phone") <= 255),
  CHECK ("email" IS NULL OR length("email") <= 255),
  CHECK ("sort" IS NULL OR (typeof("sort") = 'integer' AND "sort" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("describe" IS NULL OR length("describe") <= 255),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295))
);

CREATE TABLE IF NOT EXISTS "sys_dict" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT DEFAULT NULL,
  "code" TEXT DEFAULT NULL,
  "status" INTEGER DEFAULT NULL,
  "description" TEXT DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "created_by" INTEGER DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("code" IS NULL OR length("code") <= 255),
  CHECK ("status" IS NULL OR (typeof("status") = 'integer' AND "status" BETWEEN -128 AND 127)),
  CHECK ("description" IS NULL OR length("description") <= 500),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295))
);

CREATE TABLE IF NOT EXISTS "sys_dict_item" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT DEFAULT NULL,
  "value" TEXT DEFAULT NULL,
  "status" INTEGER DEFAULT NULL,
  "dict_id" INTEGER DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("value" IS NULL OR length("value") <= 255),
  CHECK ("status" IS NULL OR (typeof("status") = 'integer' AND "status" BETWEEN -128 AND 127)),
  CHECK ("dict_id" IS NULL OR (typeof("dict_id") = 'integer' AND "dict_id" BETWEEN 0 AND 4294967295))
);

CREATE TABLE IF NOT EXISTS "sys_gen" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "db_type" TEXT DEFAULT NULL,
  "database" TEXT DEFAULT NULL,
  "name" TEXT DEFAULT NULL,
  "module_name" TEXT DEFAULT NULL,
  "file_name" TEXT DEFAULT NULL,
  "describe" TEXT DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "created_by" INTEGER DEFAULT NULL,
  "is_cover" INTEGER DEFAULT 0,
  "is_menu" INTEGER DEFAULT 0,
  "is_tree" INTEGER DEFAULT 0,
  "is_relation_tree" INTEGER DEFAULT 0,
  "relation_tree_table" INTEGER DEFAULT 0,
  "relation_field" INTEGER DEFAULT 0,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("db_type" IS NULL OR length("db_type") <= 255),
  CHECK ("database" IS NULL OR length("database") <= 255),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("module_name" IS NULL OR length("module_name") <= 255),
  CHECK ("file_name" IS NULL OR length("file_name") <= 255),
  CHECK ("describe" IS NULL OR length("describe") <= 1000),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295)),
  CHECK ("is_cover" IS NULL OR (typeof("is_cover") = 'integer' AND "is_cover" BETWEEN -128 AND 127)),
  CHECK ("is_menu" IS NULL OR (typeof("is_menu") = 'integer' AND "is_menu" BETWEEN -128 AND 127)),
  CHECK ("is_tree" IS NULL OR (typeof("is_tree") = 'integer' AND "is_tree" BETWEEN -128 AND 127)),
  CHECK ("is_relation_tree" IS NULL OR (typeof("is_relation_tree") = 'integer' AND "is_relation_tree" BETWEEN -128 AND 127)),
  CHECK ("relation_tree_table" IS NULL OR (typeof("relation_tree_table") = 'integer' AND "relation_tree_table" BETWEEN 0 AND 4294967295)),
  CHECK ("relation_field" IS NULL OR (typeof("relation_field") = 'integer' AND "relation_field" BETWEEN 0 AND 4294967295))
);

CREATE TABLE IF NOT EXISTS "sys_gen_field" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "gen_id" INTEGER DEFAULT NULL,
  "data_name" TEXT DEFAULT NULL,
  "data_type" TEXT DEFAULT NULL,
  "data_comment" TEXT DEFAULT NULL,
  "data_extra" TEXT DEFAULT NULL,
  "data_column_key" TEXT DEFAULT NULL,
  "data_unsigned" INTEGER DEFAULT 0,
  "is_primary" INTEGER DEFAULT 0,
  "go_type" TEXT DEFAULT NULL,
  "front_type" TEXT DEFAULT NULL,
  "custom_name" TEXT DEFAULT '',
  "require" INTEGER DEFAULT 0,
  "list_show" INTEGER DEFAULT 0,
  "form_show" INTEGER DEFAULT 0,
  "query_show" INTEGER DEFAULT 0,
  "query_type" TEXT DEFAULT NULL,
  "form_type" TEXT DEFAULT NULL,
  "dict_type" TEXT DEFAULT NULL,
  "gorm_tag" TEXT DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("gen_id" IS NULL OR (typeof("gen_id") = 'integer' AND "gen_id" BETWEEN 0 AND 4294967295)),
  CHECK ("data_name" IS NULL OR length("data_name") <= 255),
  CHECK ("data_type" IS NULL OR length("data_type") <= 255),
  CHECK ("data_comment" IS NULL OR length("data_comment") <= 255),
  CHECK ("data_extra" IS NULL OR length("data_extra") <= 255),
  CHECK ("data_column_key" IS NULL OR length("data_column_key") <= 255),
  CHECK ("data_unsigned" IS NULL OR (typeof("data_unsigned") = 'integer' AND "data_unsigned" BETWEEN -128 AND 127)),
  CHECK ("is_primary" IS NULL OR (typeof("is_primary") = 'integer' AND "is_primary" BETWEEN -128 AND 127)),
  CHECK ("go_type" IS NULL OR length("go_type") <= 255),
  CHECK ("front_type" IS NULL OR length("front_type") <= 255),
  CHECK ("custom_name" IS NULL OR length("custom_name") <= 255),
  CHECK ("require" IS NULL OR (typeof("require") = 'integer' AND "require" BETWEEN -128 AND 127)),
  CHECK ("list_show" IS NULL OR (typeof("list_show") = 'integer' AND "list_show" BETWEEN -128 AND 127)),
  CHECK ("form_show" IS NULL OR (typeof("form_show") = 'integer' AND "form_show" BETWEEN -128 AND 127)),
  CHECK ("query_show" IS NULL OR (typeof("query_show") = 'integer' AND "query_show" BETWEEN -128 AND 127)),
  CHECK ("query_type" IS NULL OR length("query_type") <= 255),
  CHECK ("form_type" IS NULL OR length("form_type") <= 255),
  CHECK ("dict_type" IS NULL OR length("dict_type") <= 255),
  CHECK ("gorm_tag" IS NULL OR length("gorm_tag") <= 255)
);

CREATE TABLE IF NOT EXISTS "sys_job_results" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "job_id" TEXT NOT NULL,
  "status" TEXT NOT NULL,
  "error" TEXT,
  "start_time" DATETIME NOT NULL,
  "end_time" DATETIME NOT NULL,
  "duration" INTEGER NOT NULL,
  "retry_count" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("job_id" IS NULL OR length("job_id") <= 255),
  CHECK ("status" IS NULL OR length("status") <= 20),
  CHECK ("duration" IS NULL OR (typeof("duration") = 'integer')),
  CHECK ("retry_count" IS NULL OR (typeof("retry_count") = 'integer' AND "retry_count" BETWEEN -2147483648 AND 2147483647)),
  CONSTRAINT "sys_job_results_ibfk_1" FOREIGN KEY ("job_id") REFERENCES "sys_jobs" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_job_id" ON "sys_job_results" ("job_id");
CREATE INDEX IF NOT EXISTS "sys_job_results__idx_status" ON "sys_job_results" ("status");
CREATE INDEX IF NOT EXISTS "idx_start_time" ON "sys_job_results" ("start_time");
CREATE INDEX IF NOT EXISTS "sys_job_results__idx_created_at" ON "sys_job_results" ("created_at");

CREATE TABLE IF NOT EXISTS "sys_jobs" (
  "id" TEXT NOT NULL,
  "group" TEXT NOT NULL,
  "name" TEXT NOT NULL,
  "description" TEXT,
  "executor_name" TEXT NOT NULL,
  "execution_policy" INTEGER NOT NULL DEFAULT 1,
  "status" INTEGER NOT NULL DEFAULT 1,
  "cron_expression" TEXT NOT NULL,
  "parameters" TEXT DEFAULT NULL,
  "blocking_policy" INTEGER NOT NULL DEFAULT 0,
  "timeout" INTEGER NOT NULL DEFAULT 30000000000,
  "max_retry" INTEGER NOT NULL DEFAULT 0,
  "retry_interval" INTEGER NOT NULL DEFAULT 10000000000,
  "parallel_num" INTEGER NOT NULL DEFAULT 1,
  "running_count" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "deleted_at" DATETIME DEFAULT NULL,
  "created_by" INTEGER DEFAULT NULL,
  CHECK ("id" IS NULL OR length("id") <= 255),
  CHECK ("group" IS NULL OR length("group") <= 100),
  CHECK ("name" IS NULL OR length("name") <= 200),
  CHECK ("executor_name" IS NULL OR length("executor_name") <= 100),
  CHECK ("execution_policy" IS NULL OR (typeof("execution_policy") = 'integer' AND "execution_policy" BETWEEN -128 AND 127)),
  CHECK ("status" IS NULL OR (typeof("status") = 'integer' AND "status" BETWEEN -128 AND 127)),
  CHECK ("cron_expression" IS NULL OR length("cron_expression") <= 100),
  CHECK ("parameters" IS NULL OR json_valid("parameters")),
  CHECK ("blocking_policy" IS NULL OR (typeof("blocking_policy") = 'integer' AND "blocking_policy" BETWEEN -128 AND 127)),
  CHECK ("timeout" IS NULL OR (typeof("timeout") = 'integer')),
  CHECK ("max_retry" IS NULL OR (typeof("max_retry") = 'integer' AND "max_retry" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("retry_interval" IS NULL OR (typeof("retry_interval") = 'integer')),
  CHECK ("parallel_num" IS NULL OR (typeof("parallel_num") = 'integer' AND "parallel_num" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("running_count" IS NULL OR (typeof("running_count") = 'integer' AND "running_count" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295)),
  PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_group" ON "sys_jobs" ("group");
CREATE INDEX IF NOT EXISTS "sys_jobs__idx_status" ON "sys_jobs" ("status");
CREATE INDEX IF NOT EXISTS "idx_executor_name" ON "sys_jobs" ("executor_name");
CREATE INDEX IF NOT EXISTS "sys_jobs__idx_created_at" ON "sys_jobs" ("created_at");

CREATE TABLE IF NOT EXISTS "sys_menu" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "parent_id" INTEGER NOT NULL DEFAULT 0,
  "path" TEXT NOT NULL,
  "name" TEXT NOT NULL,
  "redirect" TEXT DEFAULT NULL,
  "component" TEXT DEFAULT NULL,
  "title" TEXT DEFAULT NULL,
  "is_full" INTEGER DEFAULT 0,
  "hide" INTEGER DEFAULT 0,
  "disable" INTEGER DEFAULT 0,
  "keep_alive" INTEGER DEFAULT 0,
  "affix" INTEGER DEFAULT 0,
  "link" TEXT DEFAULT '',
  "iframe" INTEGER DEFAULT 0,
  "svg_icon" TEXT DEFAULT '',
  "icon" TEXT DEFAULT '',
  "sort" INTEGER DEFAULT 0,
  "type" INTEGER DEFAULT 2,
  "is_link" INTEGER DEFAULT 0,
  "permission" TEXT DEFAULT '',
  "created_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
  "updated_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
  "deleted_at" DATETIME DEFAULT NULL,
  "created_by" INTEGER DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("parent_id" IS NULL OR (typeof("parent_id") = 'integer' AND "parent_id" BETWEEN 0 AND 4294967295)),
  CHECK ("path" IS NULL OR length("path") <= 255),
  CHECK ("name" IS NULL OR length("name") <= 100),
  CHECK ("redirect" IS NULL OR length("redirect") <= 255),
  CHECK ("component" IS NULL OR length("component") <= 255),
  CHECK ("title" IS NULL OR length("title") <= 100),
  CHECK ("is_full" IS NULL OR (typeof("is_full") = 'integer' AND "is_full" BETWEEN -128 AND 127)),
  CHECK ("hide" IS NULL OR (typeof("hide") = 'integer' AND "hide" BETWEEN -128 AND 127)),
  CHECK ("disable" IS NULL OR (typeof("disable") = 'integer' AND "disable" BETWEEN -128 AND 127)),
  CHECK ("keep_alive" IS NULL OR (typeof("keep_alive") = 'integer' AND "keep_alive" BETWEEN -128 AND 127)),
  CHECK ("affix" IS NULL OR (typeof("affix") = 'integer' AND "affix" BETWEEN -128 AND 127)),
  CHECK ("link" IS NULL OR length("link") <= 500),
  CHECK ("iframe" IS NULL OR (typeof("iframe") = 'integer' AND "iframe" BETWEEN -128 AND 127)),
  CHECK ("svg_icon" IS NULL OR length("svg_icon") <= 100),
  CHECK ("icon" IS NULL OR length("icon") <= 100),
  CHECK ("sort" IS NULL OR (typeof("sort") = 'integer' AND "sort" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("type" IS NULL OR (typeof("type") = 'integer' AND "type" BETWEEN -128 AND 127)),
  CHECK ("is_link" IS NULL OR (typeof("is_link") = 'integer' AND "is_link" BETWEEN -128 AND 127)),
  CHECK ("permission" IS NULL OR length("permission") <= 255),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295))
);
CREATE INDEX IF NOT EXISTS "idx_parent_id" ON "sys_menu" ("parent_id");
CREATE INDEX IF NOT EXISTS "idx_sort" ON "sys_menu" ("sort");
CREATE INDEX IF NOT EXISTS "idx_type" ON "sys_menu" ("type");

CREATE TABLE IF NOT EXISTS "sys_menu_api" (
  "menu_id" INTEGER NOT NULL,
  "api_id" INTEGER NOT NULL,
  CHECK ("menu_id" IS NULL OR (typeof("menu_id") = 'integer' AND "menu_id" BETWEEN 0 AND 4294967295)),
  CHECK ("api_id" IS NULL OR (typeof("api_id") = 'integer' AND "api_id" BETWEEN 0 AND 4294967295)),
  PRIMARY KEY ("menu_id","api_id")
);

CREATE TABLE IF NOT EXISTS "sys_operation_logs" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "user_id" INTEGER DEFAULT NULL,
  "username" TEXT DEFAULT NULL,
  "module" TEXT DEFAULT NULL,
  "operation" TEXT DEFAULT NULL,
  "method" TEXT DEFAULT NULL,
  "path" TEXT DEFAULT NULL,
  "ip" TEXT DEFAULT NULL,
  "user_agent" TEXT DEFAULT NULL,
  "request_data" TEXT,
  "response_data" TEXT,
  "status_code" INTEGER DEFAULT NULL,
  "duration" INTEGER DEFAULT NULL,
  "error_msg" TEXT,
  "location" TEXT DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("user_id" IS NULL OR (typeof("user_id") = 'integer' AND "user_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("username" IS NULL OR length("username") <= 50),
  CHECK ("module" IS NULL OR length("module") <= 100),
  CHECK ("operation" IS NULL OR length("operation") <= 100),
  CHECK ("method" IS NULL OR length("method") <= 10),
  CHECK ("path" IS NULL OR length("path") <= 500),
  CHECK ("ip" IS NULL OR length("ip") <= 50),
  CHECK ("user_agent" IS NULL OR length("user_agent") <= 500),
  CHECK ("status_code" IS NULL OR (typeof("status_code") = 'integer' AND "status_code" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("duration" IS NULL OR (typeof("duration") = 'integer')),
  CHECK ("location" IS NULL OR length("location") <= 100)
);
CREATE INDEX IF NOT EXISTS "idx_sys_operation_logs_deleted_at" ON "sys_operation_logs" ("deleted_at");
CREATE INDEX IF NOT EXISTS "sys_operation_logs__idx_user_id" ON "sys_operation_logs" ("user_id");

CREATE TABLE IF NOT EXISTS "sys_param" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT DEFAULT NULL,
  "code" TEXT NOT NULL,
  "value" TEXT,
  "status" INTEGER DEFAULT 1,
  "description" TEXT DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "created_by" INTEGER DEFAULT 0,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("code" IS NULL OR length("code") <= 255),
  CHECK ("status" IS NULL OR (typeof("status") = 'integer' AND "status" BETWEEN -128 AND 127)),
  CHECK ("description" IS NULL OR length("description") <= 500),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295))
);
CREATE UNIQUE INDEX IF NOT EXISTS "idx_sys_param_code" ON "sys_param" ("code");
CREATE INDEX IF NOT EXISTS "idx_sys_param_deleted_at" ON "sys_param" ("deleted_at");

CREATE TABLE IF NOT EXISTS "sys_role" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT DEFAULT '',
  "sort" INTEGER DEFAULT 0,
  "status" INTEGER DEFAULT 0,
  "description" TEXT DEFAULT NULL,
  "parent_id" INTEGER DEFAULT 0,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "created_by" INTEGER DEFAULT NULL,
  "data_scope" INTEGER DEFAULT 0,
  "checked_depts" TEXT DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("name" IS NULL OR length("name") <= 255),
  CHECK ("sort" IS NULL OR (typeof("sort") = 'integer' AND "sort" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("status" IS NULL OR (typeof("status") = 'integer' AND "status" BETWEEN -128 AND 127)),
  CHECK ("description" IS NULL OR length("description") <= 255),
  CHECK ("parent_id" IS NULL OR (typeof("parent_id") = 'integer' AND "parent_id" BETWEEN 0 AND 4294967295)),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295)),
  CHECK ("data_scope" IS NULL OR (typeof("data_scope") = 'integer' AND "data_scope" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("checked_depts" IS NULL OR length("checked_depts") <= 1000)
);

CREATE TABLE IF NOT EXISTS "sys_role_menu" (
  "role_id" INTEGER NOT NULL,
  "menu_id" INTEGER NOT NULL,
  CHECK ("role_id" IS NULL OR (typeof("role_id") = 'integer' AND "role_id" BETWEEN 0 AND 4294967295)),
  CHECK ("menu_id" IS NULL OR (typeof("menu_id") = 'integer' AND "menu_id" BETWEEN 0 AND 4294967295)),
  PRIMARY KEY ("role_id","menu_id")
);

CREATE TABLE IF NOT EXISTS "sys_user_role" (
  "user_id" INTEGER NOT NULL DEFAULT 0,
  "role_id" INTEGER NOT NULL DEFAULT 0,
  CHECK ("user_id" IS NULL OR (typeof("user_id") = 'integer' AND "user_id" BETWEEN 0 AND 4294967295)),
  CHECK ("role_id" IS NULL OR (typeof("role_id") = 'integer' AND "role_id" BETWEEN 0 AND 4294967295)),
  PRIMARY KEY ("user_id","role_id")
);

CREATE TABLE IF NOT EXISTS "sys_users" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "username" TEXT NOT NULL DEFAULT '',
  "password" TEXT NOT NULL DEFAULT '',
  "email" TEXT DEFAULT '',
  "status" INTEGER DEFAULT 1,
  "dept_id" INTEGER DEFAULT 0,
  "phone" TEXT DEFAULT '',
  "sex" TEXT DEFAULT '',
  "nick_name" TEXT DEFAULT '',
  "avatar" TEXT DEFAULT '',
  "description" TEXT DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  "created_by" INTEGER DEFAULT 0,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 4294967295)),
  CHECK ("username" IS NULL OR length("username") <= 50),
  CHECK ("password" IS NULL OR length("password") <= 255),
  CHECK ("email" IS NULL OR length("email") <= 100),
  CHECK ("status" IS NULL OR (typeof("status") = 'integer' AND "status" BETWEEN -128 AND 127)),
  CHECK ("dept_id" IS NULL OR (typeof("dept_id") = 'integer' AND "dept_id" BETWEEN 0 AND 4294967295)),
  CHECK ("phone" IS NULL OR length("phone") <= 64),
  CHECK ("sex" IS NULL OR length("sex") <= 64),
  CHECK ("nick_name" IS NULL OR length("nick_name") <= 100),
  CHECK ("avatar" IS NULL OR length("avatar") <= 255),
  CHECK ("description" IS NULL OR length("description") <= 500),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295))
);
CREATE UNIQUE INDEX IF NOT EXISTS "username" ON "sys_users" ("username");

CREATE TABLE IF NOT EXISTS "sys_user_sessions" (
  "sid" TEXT NOT NULL,
  "user_id" INTEGER NOT NULL,
  "refresh_token_hash" TEXT DEFAULT NULL,
  "refresh_jti" TEXT DEFAULT NULL,
  "client_ip" TEXT NOT NULL DEFAULT '',
  "login_location" TEXT NOT NULL DEFAULT '未知',
  "user_agent" TEXT NOT NULL DEFAULT '',
  "browser" TEXT NOT NULL DEFAULT '未知',
  "os" TEXT NOT NULL DEFAULT '未知',
  "login_at" DATETIME NOT NULL,
  "last_active_at" DATETIME NOT NULL,
  "session_expires_at" DATETIME NOT NULL,
  "revoked_at" DATETIME DEFAULT NULL,
  "revoke_reason" TEXT DEFAULT NULL,
  "revoked_by" INTEGER DEFAULT NULL,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  CHECK ("sid" IS NULL OR length("sid") <= 36),
  CHECK ("user_id" IS NULL OR (typeof("user_id") = 'integer' AND "user_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("refresh_token_hash" IS NULL OR length("refresh_token_hash") <= 64),
  CHECK ("refresh_jti" IS NULL OR length("refresh_jti") <= 36),
  CHECK ("client_ip" IS NULL OR length("client_ip") <= 50),
  CHECK ("login_location" IS NULL OR length("login_location") <= 100),
  CHECK ("user_agent" IS NULL OR length("user_agent") <= 500),
  CHECK ("browser" IS NULL OR length("browser") <= 100),
  CHECK ("os" IS NULL OR length("os") <= 100),
  CHECK ("revoke_reason" IS NULL OR length("revoke_reason") <= 32),
  CHECK ("revoked_by" IS NULL OR (typeof("revoked_by") = 'integer' AND "revoked_by" BETWEEN 0 AND 9223372036854775807)),
  PRIMARY KEY ("sid")
);
CREATE INDEX IF NOT EXISTS "sys_user_sessions__idx_user_id" ON "sys_user_sessions" ("user_id");
CREATE INDEX IF NOT EXISTS "idx_session_valid" ON "sys_user_sessions" ("revoked_at","session_expires_at","login_at");
CREATE INDEX IF NOT EXISTS "idx_client_ip" ON "sys_user_sessions" ("client_ip");

CREATE TABLE IF NOT EXISTS "sys_login_logs" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "user_id" INTEGER DEFAULT NULL,
  "username" TEXT NOT NULL,
  "result" TEXT NOT NULL,
  "failure_reason" TEXT DEFAULT NULL,
  "ip" TEXT NOT NULL DEFAULT '',
  "location" TEXT NOT NULL DEFAULT '未知',
  "user_agent" TEXT NOT NULL DEFAULT '',
  "browser" TEXT NOT NULL DEFAULT '未知',
  "os" TEXT NOT NULL DEFAULT '未知',
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("user_id" IS NULL OR (typeof("user_id") = 'integer' AND "user_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("username" IS NULL OR length("username") <= 100),
  CHECK ("result" IS NULL OR length("result") <= 16),
  CHECK ("failure_reason" IS NULL OR length("failure_reason") <= 48),
  CHECK ("ip" IS NULL OR length("ip") <= 50),
  CHECK ("location" IS NULL OR length("location") <= 100),
  CHECK ("user_agent" IS NULL OR length("user_agent") <= 500),
  CHECK ("browser" IS NULL OR length("browser") <= 100),
  CHECK ("os" IS NULL OR length("os") <= 100)
);
CREATE INDEX IF NOT EXISTS "idx_login_logs_user_id" ON "sys_login_logs" ("user_id");
CREATE INDEX IF NOT EXISTS "idx_login_logs_username" ON "sys_login_logs" ("username");
CREATE INDEX IF NOT EXISTS "idx_login_logs_result" ON "sys_login_logs" ("result");
CREATE INDEX IF NOT EXISTS "idx_login_logs_failure_reason" ON "sys_login_logs" ("failure_reason");
CREATE INDEX IF NOT EXISTS "idx_login_logs_ip" ON "sys_login_logs" ("ip");
CREATE INDEX IF NOT EXISTS "idx_login_logs_created_at" ON "sys_login_logs" ("created_at");

CREATE TABLE IF NOT EXISTS "gb_device_firmware_upgrade" (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  operation_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  device_id INTEGER NOT NULL,
  device_code TEXT NOT NULL,
  firmware TEXT NOT NULL,
  file_url TEXT NOT NULL,
  manufacturer TEXT NOT NULL,
  session_id TEXT NOT NULL,
  sn INTEGER NOT NULL,
  profile_version TEXT NOT NULL,
  profile_charset TEXT NOT NULL,
  sip_status INTEGER DEFAULT 0 NOT NULL,
  sip_call_id TEXT NULL,
  sip_cseq TEXT NULL,
  device_result TEXT NULL,
  device_error TEXT NULL,
  status TEXT NOT NULL,
  error_code TEXT NULL,
  error_message TEXT NULL,
  failed_reason TEXT NULL,
  current_firmware TEXT NULL,
  actor_id INTEGER DEFAULT 0 NOT NULL,
  actor_dept_id INTEGER DEFAULT 0 NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  sent_at DATETIME NULL,
  accepted_at DATETIME NULL,
  completed_at DATETIME NULL,
  deadline_at DATETIME NULL,
  response_at DATETIME NULL,
  response_call_id TEXT NULL,
  response_cseq TEXT NULL,
  CONSTRAINT uk_firmware_upgrade_operation UNIQUE (operation_id),
  CONSTRAINT uk_firmware_upgrade_device_idempotency UNIQUE (device_id,idempotency_key),
  CONSTRAINT uk_firmware_upgrade_device_session UNIQUE (device_id,session_id),
  CONSTRAINT uk_firmware_upgrade_sn UNIQUE (sn),
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("operation_id" IS NULL OR length("operation_id") <= 64),
  CHECK ("idempotency_key" IS NULL OR length("idempotency_key") <= 128),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer')),
  CHECK ("device_code" IS NULL OR length("device_code") <= 20),
  CHECK ("firmware" IS NULL OR length("firmware") <= 255),
  CHECK ("file_url" IS NULL OR length("file_url") <= 2048),
  CHECK ("manufacturer" IS NULL OR length("manufacturer") <= 255),
  CHECK ("session_id" IS NULL OR length("session_id") <= 128),
  CHECK ("sn" IS NULL OR (typeof("sn") = 'integer')),
  CHECK ("profile_version" IS NULL OR length("profile_version") <= 8),
  CHECK ("profile_charset" IS NULL OR length("profile_charset") <= 16),
  CHECK ("sip_status" IS NULL OR (typeof("sip_status") = 'integer' AND "sip_status" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("sip_call_id" IS NULL OR length("sip_call_id") <= 255),
  CHECK ("sip_cseq" IS NULL OR length("sip_cseq") <= 64),
  CHECK ("device_result" IS NULL OR length("device_result") <= 16),
  CHECK ("status" IS NULL OR length("status") <= 16),
  CHECK ("error_code" IS NULL OR length("error_code") <= 64),
  CHECK ("failed_reason" IS NULL OR length("failed_reason") <= 8),
  CHECK ("current_firmware" IS NULL OR length("current_firmware") <= 255),
  CHECK ("actor_id" IS NULL OR (typeof("actor_id") = 'integer')),
  CHECK ("actor_dept_id" IS NULL OR (typeof("actor_dept_id") = 'integer')),
  CHECK ("response_call_id" IS NULL OR length("response_call_id") <= 255),
  CHECK ("response_cseq" IS NULL OR length("response_cseq") <= 64)
);
CREATE INDEX IF NOT EXISTS "idx_firmware_upgrade_device_sn" ON "gb_device_firmware_upgrade" (device_code,sn);
CREATE INDEX IF NOT EXISTS "idx_firmware_upgrade_device_session" ON "gb_device_firmware_upgrade" (device_code,session_id);
CREATE INDEX IF NOT EXISTS "idx_firmware_upgrade_device_status" ON "gb_device_firmware_upgrade" (device_id,status);
CREATE INDEX IF NOT EXISTS "idx_firmware_upgrade_device_time" ON "gb_device_firmware_upgrade" (device_id,created_at);
CREATE INDEX IF NOT EXISTS "idx_firmware_upgrade_deadline" ON "gb_device_firmware_upgrade" (deadline_at);

CREATE TABLE IF NOT EXISTS "gb_device_grant" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "device_id" INTEGER NOT NULL,
  "target_type" TEXT NOT NULL DEFAULT '',
  "target_id" INTEGER NOT NULL DEFAULT 0,
  "created_by" INTEGER DEFAULT 0,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  "deleted_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_id" IS NULL OR (typeof("device_id") = 'integer' AND "device_id" BETWEEN 0 AND 4294967295)),
  CHECK ("target_type" IS NULL OR length("target_type") <= 16),
  CHECK ("target_id" IS NULL OR (typeof("target_id") = 'integer' AND "target_id" BETWEEN 0 AND 4294967295)),
  CHECK ("created_by" IS NULL OR (typeof("created_by") = 'integer' AND "created_by" BETWEEN 0 AND 4294967295))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_device_target" ON "gb_device_grant" ("device_id","target_type","target_id");
CREATE INDEX IF NOT EXISTS "idx_target" ON "gb_device_grant" ("target_type","target_id");
CREATE INDEX IF NOT EXISTS "idx_device" ON "gb_device_grant" ("device_id");
CREATE INDEX IF NOT EXISTS "gb_device_grant__idx_deleted_at" ON "gb_device_grant" ("deleted_at");

CREATE TABLE IF NOT EXISTS "gb_device_traffic_session" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "business_key" TEXT NOT NULL,
  "node_id" INTEGER NOT NULL,
  "media_server_uuid" TEXT NOT NULL DEFAULT '',
  "zlm_session_id" TEXT NOT NULL DEFAULT '',
  "direction" TEXT NOT NULL,
  "device_code" TEXT NOT NULL,
  "channel_code" TEXT NOT NULL DEFAULT '',
  "owner_dept_id" INTEGER NOT NULL DEFAULT 0,
  "media_kind" TEXT NOT NULL DEFAULT '',
  "schema" TEXT NOT NULL DEFAULT '',
  "vhost" TEXT NOT NULL DEFAULT '',
  "app" TEXT NOT NULL DEFAULT '',
  "stream" TEXT NOT NULL DEFAULT '',
  "create_stamp" INTEGER NOT NULL DEFAULT 0,
  "last_total_bytes" INTEGER NOT NULL DEFAULT 0,
  "settled_total_bytes" INTEGER NOT NULL DEFAULT 0,
  "duration_seconds" INTEGER NOT NULL DEFAULT 0,
  "state" TEXT NOT NULL,
  "started_at" DATETIME NULL,
  "last_seen_at" DATETIME NULL,
  "ended_at" DATETIME NULL,
  "unattributed_reason" TEXT NOT NULL DEFAULT '',
  "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("business_key" IS NULL OR length("business_key") <= 255),
  CHECK ("node_id" IS NULL OR (typeof("node_id") = 'integer')),
  CHECK ("media_server_uuid" IS NULL OR length("media_server_uuid") <= 128),
  CHECK ("zlm_session_id" IS NULL OR length("zlm_session_id") <= 128),
  CHECK ("direction" IS NULL OR length("direction") <= 16),
  CHECK ("device_code" IS NULL OR length("device_code") <= 64),
  CHECK ("channel_code" IS NULL OR length("channel_code") <= 64),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("media_kind" IS NULL OR length("media_kind") <= 32),
  CHECK ("schema" IS NULL OR length("schema") <= 32),
  CHECK ("vhost" IS NULL OR length("vhost") <= 128),
  CHECK ("app" IS NULL OR length("app") <= 64),
  CHECK ("stream" IS NULL OR length("stream") <= 255),
  CHECK ("create_stamp" IS NULL OR (typeof("create_stamp") = 'integer' AND "create_stamp" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("last_total_bytes" IS NULL OR (typeof("last_total_bytes") = 'integer' AND "last_total_bytes" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("settled_total_bytes" IS NULL OR (typeof("settled_total_bytes") = 'integer' AND "settled_total_bytes" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("duration_seconds" IS NULL OR (typeof("duration_seconds") = 'integer')),
  CHECK ("state" IS NULL OR length("state") <= 16),
  CHECK ("unattributed_reason" IS NULL OR length("unattributed_reason") <= 255)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_traffic_session_business" ON "gb_device_traffic_session" ("business_key");
CREATE INDEX IF NOT EXISTS "idx_traffic_session_node_state" ON "gb_device_traffic_session" ("node_id","state");
CREATE INDEX IF NOT EXISTS "idx_traffic_session_zlm" ON "gb_device_traffic_session" ("zlm_session_id");
CREATE INDEX IF NOT EXISTS "idx_traffic_session_direction" ON "gb_device_traffic_session" ("direction");
CREATE INDEX IF NOT EXISTS "idx_traffic_session_device_started" ON "gb_device_traffic_session" ("device_code","channel_code","started_at");
CREATE INDEX IF NOT EXISTS "idx_traffic_session_channel_started" ON "gb_device_traffic_session" ("channel_code","started_at");
CREATE INDEX IF NOT EXISTS "idx_traffic_session_owner_dept" ON "gb_device_traffic_session" ("owner_dept_id");

CREATE TABLE IF NOT EXISTS "gb_device_traffic_daily" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "stat_date" DATE NOT NULL,
  "device_code" TEXT NOT NULL,
  "channel_code" TEXT NOT NULL DEFAULT '',
  "owner_dept_id" INTEGER NOT NULL DEFAULT 0,
  "upstream_bytes" INTEGER NOT NULL DEFAULT 0,
  "downstream_bytes" INTEGER NOT NULL DEFAULT 0,
  "upstream_duration_seconds" INTEGER NOT NULL DEFAULT 0,
  "downstream_duration_seconds" INTEGER NOT NULL DEFAULT 0,
  "upstream_sessions" INTEGER NOT NULL DEFAULT 0,
  "downstream_sessions" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_code" IS NULL OR length("device_code") <= 64),
  CHECK ("channel_code" IS NULL OR length("channel_code") <= 64),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("upstream_bytes" IS NULL OR (typeof("upstream_bytes") = 'integer' AND "upstream_bytes" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("downstream_bytes" IS NULL OR (typeof("downstream_bytes") = 'integer' AND "downstream_bytes" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("upstream_duration_seconds" IS NULL OR (typeof("upstream_duration_seconds") = 'integer')),
  CHECK ("downstream_duration_seconds" IS NULL OR (typeof("downstream_duration_seconds") = 'integer')),
  CHECK ("upstream_sessions" IS NULL OR (typeof("upstream_sessions") = 'integer')),
  CHECK ("downstream_sessions" IS NULL OR (typeof("downstream_sessions") = 'integer'))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_traffic_daily_scope" ON "gb_device_traffic_daily" ("stat_date","device_code","channel_code");
CREATE INDEX IF NOT EXISTS "idx_traffic_daily_device" ON "gb_device_traffic_daily" ("device_code");
CREATE INDEX IF NOT EXISTS "idx_traffic_daily_owner_dept" ON "gb_device_traffic_daily" ("owner_dept_id");

CREATE TABLE IF NOT EXISTS "gb_device_traffic_gap" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "node_id" INTEGER NOT NULL,
  "reason" TEXT NOT NULL,
  "state" TEXT NOT NULL,
  "started_at" DATETIME NOT NULL,
  "ended_at" DATETIME NULL,
  "detail" TEXT NOT NULL DEFAULT '',
  "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("node_id" IS NULL OR (typeof("node_id") = 'integer')),
  CHECK ("reason" IS NULL OR length("reason") <= 32),
  CHECK ("state" IS NULL OR length("state") <= 16),
  CHECK ("detail" IS NULL OR length("detail") <= 500)
);
CREATE INDEX IF NOT EXISTS "idx_traffic_gap_node_state" ON "gb_device_traffic_gap" ("node_id","reason","state");
CREATE INDEX IF NOT EXISTS "idx_traffic_gap_started" ON "gb_device_traffic_gap" ("started_at");

CREATE TABLE IF NOT EXISTS "gb_device_traffic_hourly" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "stat_hour" DATETIME NOT NULL,
  "device_code" TEXT NOT NULL,
  "channel_code" TEXT NOT NULL DEFAULT '',
  "owner_dept_id" INTEGER NOT NULL DEFAULT 0,
  "upstream_bytes" INTEGER NOT NULL DEFAULT 0,
  "downstream_bytes" INTEGER NOT NULL DEFAULT 0,
  "upstream_duration_seconds" INTEGER NOT NULL DEFAULT 0,
  "downstream_duration_seconds" INTEGER NOT NULL DEFAULT 0,
  "upstream_sessions" INTEGER NOT NULL DEFAULT 0,
  "downstream_sessions" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME DEFAULT NULL,
  "updated_at" DATETIME DEFAULT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_code" IS NULL OR length("device_code") <= 64),
  CHECK ("channel_code" IS NULL OR length("channel_code") <= 64),
  CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("upstream_bytes" IS NULL OR (typeof("upstream_bytes") = 'integer' AND "upstream_bytes" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("downstream_bytes" IS NULL OR (typeof("downstream_bytes") = 'integer' AND "downstream_bytes" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("upstream_duration_seconds" IS NULL OR (typeof("upstream_duration_seconds") = 'integer')),
  CHECK ("downstream_duration_seconds" IS NULL OR (typeof("downstream_duration_seconds") = 'integer')),
  CHECK ("upstream_sessions" IS NULL OR (typeof("upstream_sessions") = 'integer')),
  CHECK ("downstream_sessions" IS NULL OR (typeof("downstream_sessions") = 'integer'))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_traffic_hourly_scope" ON "gb_device_traffic_hourly" ("stat_hour","device_code","channel_code");
CREATE INDEX IF NOT EXISTS "idx_traffic_hourly_device" ON "gb_device_traffic_hourly" ("device_code","stat_hour");
CREATE INDEX IF NOT EXISTS "idx_traffic_hourly_owner_dept" ON "gb_device_traffic_hourly" ("owner_dept_id");

CREATE TABLE IF NOT EXISTS "gb_dashboard_layout" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "user_id" INTEGER NOT NULL,
  "dashboard_key" TEXT NOT NULL,
  "schema_version" INTEGER NOT NULL,
  "revision" INTEGER NOT NULL DEFAULT 1,
  "layout_json" TEXT NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("user_id" IS NULL OR (typeof("user_id") = 'integer' AND "user_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("dashboard_key" IS NULL OR length("dashboard_key") <= 32),
  CHECK ("schema_version" IS NULL OR (typeof("schema_version") = 'integer' AND "schema_version" BETWEEN -2147483648 AND 2147483647)),
  CHECK ("revision" IS NULL OR (typeof("revision") = 'integer' AND "revision" BETWEEN 0 AND 9223372036854775807))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_dashboard_layout_user_key" ON "gb_dashboard_layout" ("user_id","dashboard_key");
CREATE INDEX IF NOT EXISTS "idx_dashboard_layout_updated" ON "gb_dashboard_layout" ("updated_at");

CREATE TABLE IF NOT EXISTS "gb_sip_metric_minute" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "bucket_start" DATETIME NOT NULL,
  "method" TEXT NOT NULL,
  "direction" TEXT NOT NULL,
  "request_count" INTEGER NOT NULL DEFAULT 0,
  "transaction_count" INTEGER NOT NULL DEFAULT 0,
  "transaction_success" INTEGER NOT NULL DEFAULT 0,
  "transaction_failure" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("method" IS NULL OR length("method") <= 16),
  CHECK ("direction" IS NULL OR length("direction") <= 8),
  CHECK ("request_count" IS NULL OR (typeof("request_count") = 'integer' AND "request_count" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("transaction_count" IS NULL OR (typeof("transaction_count") = 'integer' AND "transaction_count" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("transaction_success" IS NULL OR (typeof("transaction_success") = 'integer' AND "transaction_success" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("transaction_failure" IS NULL OR (typeof("transaction_failure") = 'integer' AND "transaction_failure" BETWEEN 0 AND 9223372036854775807))
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_sip_metric_minute_bucket" ON "gb_sip_metric_minute" ("bucket_start","method","direction");
CREATE INDEX IF NOT EXISTS "idx_sip_metric_minute_bucket" ON "gb_sip_metric_minute" ("bucket_start");

CREATE TABLE IF NOT EXISTS "gb_sip_metric_flush" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "flush_id" TEXT NOT NULL,
  "created_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("flush_id" IS NULL OR length("flush_id") <= 64)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_sip_metric_flush_id" ON "gb_sip_metric_flush" ("flush_id");
CREATE INDEX IF NOT EXISTS "idx_sip_metric_flush_created" ON "gb_sip_metric_flush" ("created_at");

CREATE TABLE IF NOT EXISTS "gb_sip_metric_gap" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "started_at" DATETIME NOT NULL,
  "ended_at" DATETIME NOT NULL,
  "reason" TEXT NOT NULL,
  "dropped_count" INTEGER NOT NULL DEFAULT 0,
  "created_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("reason" IS NULL OR length("reason") <= 32),
  CHECK ("dropped_count" IS NULL OR (typeof("dropped_count") = 'integer' AND "dropped_count" BETWEEN 0 AND 9223372036854775807))
);
CREATE INDEX IF NOT EXISTS "idx_sip_metric_gap_window" ON "gb_sip_metric_gap" ("started_at","ended_at");

CREATE TABLE IF NOT EXISTS "gb_play_attempt" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "correlation_id" TEXT NOT NULL,
  "user_id" INTEGER NOT NULL,
  "device_code" TEXT NOT NULL,
  "channel_code" TEXT NOT NULL,
  "node_id" INTEGER NOT NULL DEFAULT 0,
  "reused" INTEGER NOT NULL DEFAULT 0,
  "outcome" TEXT NOT NULL,
  "failure_stage" TEXT NOT NULL DEFAULT '',
  "started_at" DATETIME NOT NULL,
  "finished_at" DATETIME NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("correlation_id" IS NULL OR length("correlation_id") <= 64),
  CHECK ("user_id" IS NULL OR (typeof("user_id") = 'integer' AND "user_id" BETWEEN 0 AND 9223372036854775807)),
  CHECK ("device_code" IS NULL OR length("device_code") <= 20),
  CHECK ("channel_code" IS NULL OR length("channel_code") <= 20),
  CHECK ("node_id" IS NULL OR (typeof("node_id") = 'integer')),
  CHECK ("reused" IS NULL OR (typeof("reused") = 'integer' AND "reused" BETWEEN -128 AND 127)),
  CHECK ("outcome" IS NULL OR length("outcome") <= 32),
  CHECK ("failure_stage" IS NULL OR length("failure_stage") <= 32)
);
CREATE UNIQUE INDEX IF NOT EXISTS "uk_play_attempt_correlation" ON "gb_play_attempt" ("correlation_id");
CREATE INDEX IF NOT EXISTS "idx_play_attempt_user_started" ON "gb_play_attempt" ("user_id","started_at");
CREATE INDEX IF NOT EXISTS "idx_play_attempt_device_started" ON "gb_play_attempt" ("device_code","started_at");
CREATE INDEX IF NOT EXISTS "idx_play_attempt_outcome_started" ON "gb_play_attempt" ("outcome","started_at");

-- Deterministic system seed and permission metadata. Civil-code rows are seeded by the Go initializer.
INSERT INTO "sys_department" ("id", "parent_id", "name", "status", "leader", "phone", "email", "sort", "describe", "created_at", "updated_at", "deleted_at", "created_by") VALUES (1, 0, '总部', 1, '', '', '', 1, '默认根部门', '2026-09-07 00:00:00', '2026-09-07 00:00:00', NULL, 1);
INSERT INTO "sys_role" VALUES ('1', '系统管理员', '0', '1', '最高权限管理员角色', '0', '2025-09-01 17:32:12', '2025-09-30 15:53:24', null, '1', '1', '');
INSERT INTO "sys_api" ("id", "title", "path", "method", "api_group", "created_at", "updated_at", "deleted_at", "created_by") VALUES
(1, '用户登录', '/api/login', 'POST', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(2, '刷新Token', '/api/refreshToken', 'POST', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(3, '生成验证码ID', '/api/captcha/id', 'GET', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(4, '获取验证码图片', '/api/captcha/image', 'GET', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(5, '用户登出', '/api/users/logout', 'POST', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(6, '获取当前用户信息', '/api/users/profile', 'GET', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(7, '根据ID获取用户信息', '/api/users/:id', 'GET', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(8, '用户列表', '/api/users/list', 'GET', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(9, '新增用户', '/api/users/add', 'POST', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(10, '更新用户信息', '/api/users/edit', 'PUT', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(11, '删除用户', '/api/users/delete', 'DELETE', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(12, '获取用户权限菜单', '/api/sysMenu/getRouters', 'GET', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(13, '获取完整菜单列表', '/api/sysMenu/getMenuList', 'GET', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(14, '根据ID获取菜单信息', '/api/sysMenu/:id', 'GET', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(15, '新增菜单', '/api/sysMenu/add', 'POST', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(16, '更新菜单', '/api/sysMenu/edit', 'PUT', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(17, '删除菜单', '/api/sysMenu/delete', 'DELETE', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(18, '获取部门列表', '/api/sysDepartment/getDivision', 'GET', '部门管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(19, '获取所有角色数据', '/api/sysRole/getRoles', 'GET', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(20, '根据角色ID获取角色菜单权限', '/api/sysRole/getUserPermission/:roleId', 'GET', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(21, '添加角色的菜单权限', '/api/sysRole/addRoleMenu', 'POST', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(22, '角色分页列表', '/api/sysRole/list', 'GET', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(23, '根据ID获取角色信息', '/api/sysRole/:id', 'GET', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(24, '新增角色', '/api/sysRole/add', 'POST', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(25, '更新角色', '/api/sysRole/edit', 'PUT', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(26, '删除角色', '/api/sysRole/delete', 'DELETE', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(27, '获取所有字典数据', '/api/sysDict/getAllDicts', 'GET', '字典管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(28, '根据字典编码获取字典', '/api/sysDict/getByCode/:code', 'GET', '字典管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(29, 'API列表', '/api/sysApi/list', 'GET', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(30, '根据ID获取API信息', '/api/sysApi/:id', 'GET', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(31, '新增API', '/api/sysApi/add', 'POST', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(32, '更新API', '/api/sysApi/edit', 'PUT', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(33, '删除API', '/api/sysApi/delete', 'DELETE', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1),
(35, '根据菜单ID获取API的ID集合', '/api/sysMenu/apis/:id', 'GET', '菜单管理', '2025-09-04 17:25:14', '2025-09-04 17:25:14', NULL, 1),
(36, '设置菜单API权限', '/api/sysMenu/setApis', 'POST', '菜单管理', '2025-09-04 17:26:04', '2025-09-04 17:26:04', NULL, 1),
(37, '根据ID获取部门信息', '/api/sysDepartment/:id', 'GET', '部门管理', '2025-09-12 14:46:42', '2025-09-12 14:46:42', NULL, 1),
(38, '新增部门', '/api/sysDepartment/add', 'POST', '部门管理', '2025-09-12 14:47:27', '2025-09-12 14:47:27', NULL, 1),
(39, '更新部门', '/api/sysDepartment/edit', 'PUT', '部门管理', '2025-09-12 14:48:15', '2025-09-12 14:48:27', NULL, 1),
(40, '删除部门', '/api/sysDepartment/delete', 'DELETE', '部门管理', '2025-09-12 14:49:15', '2025-09-12 14:49:15', NULL, 1),
(41, '字典分页列表', '/api/sysDict/list', 'GET', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1),
(42, '根据ID获取字典信息', '/api/sysDict/:id', 'GET', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1),
(43, '新增字典', '/api/sysDict/add', 'POST', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1),
(44, '更新字典', '/api/sysDict/edit', 'PUT', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1),
(45, '删除字典', '/api/sysDict/delete', 'DELETE', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1),
(46, '字典项列表', '/api/sysDictItem/list', 'GET', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1),
(47, '根据ID获取字典项信息', '/api/sysDictItem/:id', 'GET', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1),
(48, '根据字典ID获取字典项列表', '/api/sysDictItem/getByDictId/:dictId', 'GET', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1),
(49, '根据字典编码获取字典项列表', '/api/sysDictItem/getByDictCode/:dictCode', 'GET', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1),
(50, '新增字典项', '/api/sysDictItem/add', 'POST', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1),
(51, '更新字典项', '/api/sysDictItem/edit', 'PUT', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1),
(52, '删除字典项', '/api/sysDictItem/delete', 'DELETE', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1),
(53, '修改用户密码、手机号及邮箱', '/api/users/updateAccount', 'PUT', '用户管理', '2025-09-18 18:11:01', '2025-09-18 18:11:01', NULL, 1),
(54, '头像上传', '/api/users/uploadAvatar', 'POST', '用户管理', '2025-09-24 17:01:05', '2025-09-24 17:01:05', NULL, 1),
(55, '上传文件', '/api/sysAffix/upload', 'POST', '文件管理', '2025-09-25 15:51:04', '2025-09-25 15:51:04', NULL, 1),
(56, '删除文件', '/api/sysAffix/delete', 'DELETE', '文件管理', '2025-09-25 15:51:38', '2025-09-25 15:51:38', NULL, 1),
(57, '修改文件名', '/api/sysAffix/updateName', 'PUT', '文件管理', '2025-09-25 15:52:31', '2025-09-25 15:52:31', NULL, 1),
(58, '文件列表', '/api/sysAffix/list', 'GET', '文件管理', '2025-09-25 15:54:03', '2025-09-25 15:54:03', NULL, 1),
(59, '获取文件详情', '/api/sysAffix/:id', 'GET', '文件管理', '2025-09-25 15:54:55', '2025-09-25 15:54:55', NULL, 1),
(60, '下载文件', '/api/sysAffix/download/:id', 'GET', '文件管理', '2025-09-25 15:56:15', '2025-09-25 15:58:06', NULL, 1),
(61, '设置数据权限', '/api/sysRole/dataScope', 'PUT', '角色管理', '2025-09-26 17:04:15', '2025-09-26 17:04:15', NULL, 1),
(62, '读取系统配置', '/api/config/get', 'GET', '系统配置', '2025-10-09 16:21:29', '2025-10-09 16:21:29', NULL, 1),
(63, '修改系统配置', '/api/config/update', 'PUT', '系统配置', '2025-10-09 16:21:59', '2025-10-09 16:22:09', NULL, 1),
(64, '查看内存缓存', '/api/config/viewCache', 'GET', '系统配置', '2025-10-10 17:41:33', '2025-10-10 17:41:33', NULL, 1),
(70, '日志列表', '/api/sysOperationLog/list', 'GET', '日志管理', '2025-10-20 10:10:58', '2025-10-20 10:10:58', NULL, 1),
(72, '日志删除', '/api/sysOperationLog/delete', 'DELETE', '日志管理', '2025-10-20 10:13:19', '2025-10-20 10:13:19', NULL, 1),
(73, '日志导出', '/api/sysOperationLog/export', 'GET', '日志管理', '2025-10-20 10:14:11', '2025-10-20 10:14:11', NULL, 1),
(74, '导出菜单', '/api/sysMenu/export', 'GET', '菜单管理', '2025-10-20 17:17:07', '2025-10-20 17:17:07', NULL, 1),
(75, '导入菜单', '/api/sysMenu/import', 'POST', '菜单管理', '2025-10-21 11:30:34', '2025-10-24 08:59:44', NULL, 1),
(89, '修改用户基本信息', '/api/users/updateBasicInfo', 'PUT', '用户管理', '2025-10-31 09:05:00', '2025-10-31 09:05:00', NULL, 1),
(105, '生成代码文件', '/api/codegen/generate', 'POST', '代码生成', '2025-11-07 15:32:53', '2025-11-07 15:32:53', NULL, 1),
(106, '获取表的字段信息', '/api/codegen/columns', 'GET', '代码生成', '2025-11-07 15:33:52', '2025-11-07 15:33:52', NULL, 1),
(187, '获取数据库列表', '/api/codegen/databases', 'GET', '代码生成', '2025-11-17 15:12:26', '2025-11-17 15:12:26', NULL, 1),
(188, '获取指定数据库中的表集合', '/api/codegen/tables', 'GET', '代码生成', '2025-11-17 15:13:38', '2025-11-17 15:13:38', NULL, 1),
(189, '代码预览', '/api/codegen/preview', 'GET', '代码生成', '2025-11-17 15:14:25', '2025-11-17 15:14:25', NULL, 1),
(190, '代码生成配置列表', '/api/sysGen/list', 'GET', '代码生成', '2025-11-17 15:15:20', '2025-11-17 15:15:20', NULL, 1),
(191, ' 批量创建代码生成配置', '/api/sysGen/batchInsert', 'POST', '代码生成', '2025-11-17 15:22:46', '2025-11-17 15:22:46', NULL, 1),
(192, '获取代码生成配置详情', '/api/sysGen/:id', 'GET', '代码生成', '2025-11-17 15:23:29', '2025-11-17 15:23:29', NULL, 1),
(193, '更新代码生成配置和字段信息', '/api/sysGen/update', 'PUT', '代码生成', '2025-11-17 15:24:41', '2025-11-17 15:24:41', NULL, 1),
(194, '删除代码生成配置和字段信息', '/api/sysGen/:id', 'DELETE', '代码生成', '2025-11-17 15:26:44', '2025-11-17 15:26:44', NULL, 1),
(195, '刷新代码生成配置的字段信息', '/api/sysGen/refreshFields', 'PUT', '代码生成', '2025-11-17 15:27:33', '2025-11-17 15:27:33', NULL, 1),
(196, '生成菜单', '/api/codegen/insertmenuandapi', 'POST', '代码生成', '2025-11-26 15:12:56', '2025-11-26 15:12:56', NULL, 1),
(197, '批量删除', '/api/sysMenu/batchDelete', 'DELETE', '菜单管理', '2025-12-05 17:48:52', '2025-12-05 17:48:52', NULL, 1),
(198, '获取插件列表', '/api/pluginsmanager/exports', 'GET', '插件管理', '2025-12-08 16:38:26', '2025-12-08 16:38:26', NULL, 1),
(199, '导出插件', '/api/pluginsmanager/export', 'POST', '插件管理', '2025-12-08 16:39:19', '2025-12-08 16:44:36', NULL, 1),
(200, '导入插件', '/api/pluginsmanager/import', 'POST', '插件管理', '2025-12-08 16:47:11', '2025-12-08 16:47:11', NULL, 1),
(201, '卸载插件', '/api/pluginsmanager/uninstall', 'DELETE', '插件管理', '2025-12-08 16:48:07', '2025-12-08 16:48:07', NULL, 1),
(202, '切换租户', '/api/users/switchTenant/:tenantld', 'GET', '用户管理', '2026-01-09 16:29:37', '2026-01-09 16:29:37', NULL, 1),
(203, '定时任务列表', '/api/sysJobs/list', 'GET', '任务调度', '2026-02-11 11:56:54', '2026-02-11 11:56:54', NULL, 1),
(204, '定时任务获取所有执行器列表', '/api/sysJobs/executors', 'GET', '任务调度', '2026-02-12 17:57:47', '2026-02-12 17:57:47', NULL, 1),
(205, '定时任务新增', '/api/sysJobs/add', 'POST', '任务调度', '2026-02-11 11:57:33', '2026-02-11 11:57:33', NULL, 1),
(206, '定时任务编辑', '/api/sysJobs/edit', 'PUT', '任务调度', '2026-02-11 11:57:58', '2026-02-11 11:57:58', NULL, 1),
(207, '定时任务获取数据', '/api/sysJobs/:id', 'GET', '任务调度', '2026-02-11 11:59:54', '2026-02-11 11:59:54', NULL, 1),
(208, '定时任务设置任务状态', '/api/sysJobs/setStatus', 'PUT', '任务调度', '2026-02-12 17:56:33', '2026-02-12 17:56:33', NULL, 1),
(209, '定时任务删除', '/api/sysJobs/delete', 'DELETE', '任务调度', '2026-02-11 11:59:05', '2026-02-11 11:59:05', NULL, 1),
(210, '定时任务立即执行任务', '/api/sysJobs/executeNow', 'POST', '任务调度', '2026-02-12 17:57:07', '2026-02-12 17:57:07', NULL, 1),
(211, '定时任务日志', '/api/sysJobResults/list', 'GET', '任务调度', '2026-02-11 12:00:54', '2026-02-11 12:00:54', NULL, 1),
(212, '定时任务日志删除', '/api/sysJobResults/delete', 'DELETE', '任务调度', '2026-02-11 12:01:22', '2026-02-11 12:01:22', NULL, 1),
(213, '分片上传初始化', '/api/sysAffix/chunk/init', 'POST', '文件管理', '2026-04-09 15:12:48', '2026-04-09 15:12:48', NULL, 1),
(214, '分片上传上传分片', '/api/sysAffix/chunk/upload', 'POST', '文件管理', '2026-04-09 15:37:44', '2026-04-09 15:37:44', NULL, 1),
(215, '分片上传合并分片', '/api/sysAffix/chunk/merge', 'POST', '文件管理', '2026-04-09 15:39:26', '2026-04-09 15:39:26', NULL, 1),
(216, '分片上传取消上传', '/api/sysAffix/chunk/cancel', 'DELETE', '文件管理', '2026-04-09 15:43:38', '2026-04-09 15:43:38', NULL, 1),
(217, '读取 SIP 配置状态', '/api/gb28181/sip/setup/status', 'GET', 'GB28181 SIP 配置', '2026-07-20 09:24:46', '2026-07-20 09:24:46', NULL, 1),
(218, '读取本机网络接口', '/api/gb28181/sip/setup/network-interfaces', 'GET', 'GB28181 SIP 配置', '2026-07-20 09:24:46', '2026-07-20 09:24:46', NULL, 1),
(219, '读取 SIP 平台信息', '/api/gb28181/sip/platform', 'GET', 'GB28181 SIP 配置', '2026-07-20 09:24:46', '2026-07-20 09:24:46', NULL, 1),
(220, '保存 SIP 配置', '/api/gb28181/sip/setup/config', 'PUT', 'GB28181 SIP 配置', '2026-07-20 09:24:46', '2026-07-20 09:24:46', NULL, 1),
(221, '暂缓 SIP 配置', '/api/gb28181/sip/setup/skip', 'POST', 'GB28181 SIP 配置', '2026-07-20 09:24:46', '2026-07-20 09:24:46', NULL, 1),
(222, 'SIP Trace 健康状态', '/api/gb28181/sip-traces/health', 'GET', 'SIP 日志', '2026-07-20 13:46:36', '2026-07-20 13:46:36', NULL, 1),
(223, 'SIP Trace 报文列表', '/api/gb28181/sip-traces/messages', 'GET', 'SIP 日志', '2026-07-20 13:46:36', '2026-07-20 13:46:36', NULL, 1),
(224, 'SIP Trace 报文详情', '/api/gb28181/sip-traces/messages/:id', 'GET', 'SIP 日志', '2026-07-20 13:46:36', '2026-07-20 13:46:36', NULL, 1),
(225, 'SIP Trace 会话列表', '/api/gb28181/sip-traces/sessions', 'GET', 'SIP 日志', '2026-07-20 13:46:36', '2026-07-20 13:46:36', NULL, 1),
(226, 'SIP Trace 会话报文', '/api/gb28181/sip-traces/sessions/:callId/messages', 'GET', 'SIP 日志', '2026-07-20 13:46:36', '2026-07-20 13:46:36', NULL, 1),
(227, '设备 SIP 诊断状态', '/api/gb28181/device-mgmt/device/:id/sip-trace-capture', 'GET', 'SIP 日志', '2026-07-20 13:46:36', '2026-07-20 13:46:36', NULL, 1),
(228, '启动设备 SIP 诊断', '/api/gb28181/device-mgmt/device/:id/sip-trace-captures', 'POST', 'SIP 日志', '2026-07-20 13:46:36', '2026-07-20 13:46:36', NULL, 1),
(229, '停止设备 SIP 诊断', '/api/gb28181/sip-traces/captures/:id/stop', 'POST', 'SIP 日志', '2026-07-20 13:46:36', '2026-07-20 13:46:36', NULL, 1),
(237, '生成设备接入二维码', '/api/gb28181/sip/qr/token', 'POST', 'GB28181 SIP 配置', '2026-07-26 20:49:53', '2026-07-26 20:49:53', NULL, 1),
(238, '创建自定义分组', '/api/gb28181/device-mgmt/custom-groups', 'POST', 'GB28181 设备分组', '2026-08-02 16:32:06', '2026-08-02 16:32:06', NULL, 1),
(239, '修改自定义分组', '/api/gb28181/device-mgmt/custom-groups/:id', 'PATCH', 'GB28181 设备分组', '2026-08-02 16:32:06', '2026-08-02 16:32:06', NULL, 1),
(240, '移动自定义分组', '/api/gb28181/device-mgmt/custom-groups/:id/move', 'POST', 'GB28181 设备分组', '2026-08-02 16:32:06', '2026-08-02 16:32:06', NULL, 1),
(241, '删除自定义分组', '/api/gb28181/device-mgmt/custom-groups/:id', 'DELETE', 'GB28181 设备分组', '2026-08-02 16:32:06', '2026-08-02 16:32:06', NULL, 1),
(242, '添加分组设备', '/api/gb28181/device-mgmt/custom-groups/:id/devices', 'POST', 'GB28181 设备分组', '2026-08-02 16:32:06', '2026-08-02 16:32:06', NULL, 1),
(243, '移除分组设备', '/api/gb28181/device-mgmt/custom-groups/:id/devices/remove', 'POST', 'GB28181 设备分组', '2026-08-02 16:32:06', '2026-08-02 16:32:06', NULL, 1),
(245, '查询告警列表', '/api/gb28181/alarms', 'GET', 'GB28181 告警管理', '2026-08-04 22:41:38', '2026-08-04 22:41:38', NULL, 1),
(246, '查询告警详情', '/api/gb28181/alarms/:id', 'GET', 'GB28181 告警管理', '2026-08-04 22:41:38', '2026-08-04 22:41:38', NULL, 1),
(247, '物理删除单条告警', '/api/gb28181/alarms/:id', 'DELETE', 'GB28181 告警管理', '2026-08-04 22:41:38', '2026-08-04 22:41:38', NULL, 1),
(248, '批量物理删除告警', '/api/gb28181/alarms/batch-delete', 'POST', 'GB28181 告警管理', '2026-08-04 22:41:38', '2026-08-04 22:41:38', NULL, 1),
(252, '读取移动位置历史轨迹配置', '/api/gb28181/sip/service-config/position-history', 'GET', 'GB28181 SIP 配置', '2026-08-07 14:46:22', '2026-08-07 14:46:22', NULL, 1),
(253, '修改移动位置历史轨迹配置', '/api/gb28181/sip/service-config/position-history', 'PUT', 'GB28181 SIP 配置', '2026-08-07 14:46:22', '2026-08-07 14:46:22', NULL, 1),
(254, '查询播放方案', '/api/gb28181/playback-schemes', 'GET', 'GB28181 多屏播放', '2026-08-07 17:23:46', '2026-08-07 17:23:46', NULL, 1),
(255, '查看播放方案', '/api/gb28181/playback-schemes/:id', 'GET', 'GB28181 多屏播放', '2026-08-07 17:23:46', '2026-08-07 17:23:46', NULL, 1),
(256, '创建播放方案', '/api/gb28181/playback-schemes', 'POST', 'GB28181 多屏播放', '2026-08-07 17:23:46', '2026-08-07 17:23:46', NULL, 1),
(257, '重命名播放方案', '/api/gb28181/playback-schemes/:id', 'PATCH', 'GB28181 多屏播放', '2026-08-07 17:23:46', '2026-08-07 17:23:46', NULL, 1),
(258, '覆盖播放方案', '/api/gb28181/playback-schemes/:id/layout', 'PUT', 'GB28181 多屏播放', '2026-08-07 17:23:46', '2026-08-07 17:23:46', NULL, 1),
(259, '删除播放方案', '/api/gb28181/playback-schemes/:id', 'DELETE', 'GB28181 多屏播放', '2026-08-07 17:23:46', '2026-08-07 17:23:46', NULL, 1),
(261, '读取云台默认速度配置', '/api/gb28181/sip/service-config/ptz-default-speed', 'GET', 'GB28181 SIP 配置', '2026-08-09 16:03:32', '2026-08-09 16:03:32', NULL, 1),
(262, '修改云台默认速度配置', '/api/gb28181/sip/service-config/ptz-default-speed', 'PUT', 'GB28181 SIP 配置', '2026-08-09 16:03:32', '2026-08-09 16:03:32', NULL, 1),
(263, '读取设备上线同步通道配置', '/api/gb28181/sip/service-config/sync-channels-on-online', 'GET', 'GB28181 SIP 配置', '2026-08-09 16:57:04', '2026-08-09 16:57:04', NULL, 1),
(264, '修改设备上线同步通道配置', '/api/gb28181/sip/service-config/sync-channels-on-online', 'PUT', 'GB28181 SIP 配置', '2026-08-09 16:57:04', '2026-08-09 16:57:04', NULL, 1),
(265, '读取 SIP 日志配置', '/api/gb28181/sip/service-config/sip-log', 'GET', 'GB28181 SIP 配置', '2026-08-09 17:36:24', '2026-08-09 17:36:24', NULL, 1),
(266, '修改 SIP 日志配置', '/api/gb28181/sip/service-config/sip-log', 'PUT', 'GB28181 SIP 配置', '2026-08-09 17:36:24', '2026-08-09 17:36:24', NULL, 1),
(267, '读取忽略通道离线异常通知配置', '/api/gb28181/sip/service-config/ignore-channel-offline-status-notify', 'GET', 'GB28181 SIP 配置', '2026-08-09 18:29:09', '2026-08-09 18:29:09', NULL, 1),
(268, '修改忽略通道离线异常通知配置', '/api/gb28181/sip/service-config/ignore-channel-offline-status-notify', 'PUT', 'GB28181 SIP 配置', '2026-08-09 18:29:09', '2026-08-09 18:29:09', NULL, 1),
(269, '读取收到心跳恢复设备上线配置', '/api/gb28181/sip/service-config/online-on-heartbeat', 'GET', 'GB28181 SIP 配置', '2026-08-09 19:08:02', '2026-08-09 19:08:02', NULL, 1),
(270, '修改收到心跳恢复设备上线配置', '/api/gb28181/sip/service-config/online-on-heartbeat', 'PUT', 'GB28181 SIP 配置', '2026-08-09 19:08:02', '2026-08-09 19:08:02', NULL, 1),
(271, '读取报警消息存储配置', '/api/gb28181/sip/service-config/save-alarm-messages', 'GET', 'GB28181 SIP 配置', '2026-08-09 21:39:23', '2026-08-09 21:39:23', NULL, 1),
(272, '修改报警消息存储配置', '/api/gb28181/sip/service-config/save-alarm-messages', 'PUT', 'GB28181 SIP 配置', '2026-08-09 21:39:23', '2026-08-09 21:39:23', NULL, 1),
(273, '读取 SIP 命令超时时间', '/api/gb28181/sip/service-config/sip-command-timeout', 'GET', 'GB28181 SIP 配置', '2026-08-09 21:39:23', '2026-08-09 21:39:23', NULL, 1),
(274, '修改 SIP 命令超时时间', '/api/gb28181/sip/service-config/sip-command-timeout', 'PUT', 'GB28181 SIP 配置', '2026-08-09 21:39:23', '2026-08-09 21:39:23', NULL, 1),
(275, '读取预分配模式', '/api/gb28181/sip/service-config/preallocation-mode', 'GET', 'GB28181 SIP 配置', '2026-08-09 21:39:23', '2026-08-09 21:39:23', NULL, 1),
(276, '修改预分配模式', '/api/gb28181/sip/service-config/preallocation-mode', 'PUT', 'GB28181 SIP 配置', '2026-08-09 21:39:24', '2026-08-09 21:39:24', NULL, 1),
(277, '查看安全快照', '/api/gb28181/security/snapshot', 'GET', 'GB28181 接入安全', '2026-08-10 09:27:17', '2026-08-10 09:27:17', NULL, 1),
(278, '查看安全事件', '/api/gb28181/security/events', 'GET', 'GB28181 接入安全', '2026-08-10 09:27:17', '2026-08-10 09:27:17', NULL, 1),
(279, '查看安全封禁', '/api/gb28181/security/bans', 'GET', 'GB28181 接入安全', '2026-08-10 09:27:17', '2026-08-10 09:27:17', NULL, 1),
(280, '手工解封安全封禁', '/api/gb28181/security/bans/:id/unban', 'POST', 'GB28181 接入安全', '2026-08-10 09:27:17', '2026-08-10 09:27:17', NULL, 1),
(281, '读取安全策略', '/api/gb28181/security/policy', 'GET', 'GB28181 接入安全', '2026-08-10 09:27:17', '2026-08-10 09:27:17', NULL, 1),
(282, '修改安全策略', '/api/gb28181/security/policy', 'PUT', 'GB28181 接入安全', '2026-08-10 09:27:17', '2026-08-10 09:27:17', NULL, 1),
(283, '查看防火墙 Agent 健康', '/api/gb28181/security/agent/health', 'GET', 'GB28181 接入安全', '2026-08-10 09:27:17', '2026-08-10 09:27:17', NULL, 1),
(284, '订阅安全事件', '/api/gb28181/security/stream', 'GET', 'GB28181 接入安全', '2026-08-10 09:27:17', '2026-08-10 09:27:17', NULL, 1),
(292, '查看安全访问名单', '/api/gb28181/security/access-rules', 'GET', 'GB28181 接入安全', '2026-08-10 09:27:18', '2026-08-10 09:27:18', NULL, 1),
(293, '创建安全访问规则', '/api/gb28181/security/access-rules', 'POST', 'GB28181 接入安全', '2026-08-10 09:27:18', '2026-08-10 09:27:18', NULL, 1),
(294, '修改安全访问规则', '/api/gb28181/security/access-rules/:id', 'PUT', 'GB28181 接入安全', '2026-08-10 09:27:18', '2026-08-10 09:27:18', NULL, 1),
(295, '删除安全访问规则', '/api/gb28181/security/access-rules/:id', 'DELETE', 'GB28181 接入安全', '2026-08-10 09:27:18', '2026-08-10 09:27:18', NULL, 1),
(299, '读取全局订阅项目', '/api/gb28181/sip/service-config/global-subscriptions', 'GET', 'GB28181 SIP 配置', '2026-08-10 11:13:31', '2026-08-10 11:13:31', NULL, 1),
(300, '修改全局订阅项目', '/api/gb28181/sip/service-config/global-subscriptions', 'PUT', 'GB28181 SIP 配置', '2026-08-10 11:13:31', '2026-08-10 11:13:31', NULL, 1),
(301, '读取全局通道音频配置', '/api/gb28181/sip/service-config/default-channel-audio', 'GET', 'GB28181 SIP 配置', '2026-08-10 11:13:31', '2026-08-10 11:13:31', NULL, 1),
(302, '修改全局通道音频配置', '/api/gb28181/sip/service-config/default-channel-audio', 'PUT', 'GB28181 SIP 配置', '2026-08-10 11:13:31', '2026-08-10 11:13:31', NULL, 1),
(306, '读取默认播放协议', '/api/gb28181/sip/service-config/default-playback-protocol', 'GET', 'GB28181 SIP 配置', '2026-08-10 18:56:34', '2026-08-10 18:56:34', NULL, 1),
(307, '修改默认播放协议', '/api/gb28181/sip/service-config/default-playback-protocol', 'PUT', 'GB28181 SIP 配置', '2026-08-10 18:56:34', '2026-08-10 18:56:34', NULL, 1),
(308, '读取固定地址播放配置', '/api/gb28181/sip/service-config/fixed-address-playback', 'GET', 'GB28181 SIP 配置', '2026-08-11 08:41:09', '2026-08-11 08:41:09', NULL, 1),
(309, '修改固定地址播放配置', '/api/gb28181/sip/service-config/fixed-address-playback', 'PUT', 'GB28181 SIP 配置', '2026-08-11 08:41:09', '2026-08-11 08:41:09', NULL, 1),
(310, '查询云端录像列表', '/api/gb28181/cloud-recordings/files', 'GET', 'GB28181 云端录像', '2026-08-11 08:43:35', '2026-08-11 08:43:35', NULL, 1),
(311, '查询云端录像选项', '/api/gb28181/cloud-recordings/files/options', 'GET', 'GB28181 云端录像', '2026-08-11 08:43:35', '2026-08-11 08:43:35', NULL, 1),
(312, '查询云端录像详情', '/api/gb28181/cloud-recordings/files/:id', 'GET', 'GB28181 云端录像', '2026-08-11 08:43:35', '2026-08-11 08:43:35', NULL, 1),
(313, '申请云端录像访问', '/api/gb28181/cloud-recordings/files/:id/access', 'POST', 'GB28181 云端录像', '2026-08-11 08:43:35', '2026-08-11 08:43:35', NULL, 1),
(314, '查询正在录像会话', '/api/gb28181/cloud-recordings/active', 'GET', 'GB28181 云端录像', '2026-08-11 08:43:35', '2026-08-11 08:43:35', NULL, 1),
(315, '查询录像对账状态', '/api/gb28181/cloud-recordings/reconciliations', 'GET', 'GB28181 云端录像', '2026-08-11 08:43:35', '2026-08-11 08:43:35', NULL, 1),
(316, '触发录像对账', '/api/gb28181/cloud-recordings/reconciliations', 'POST', 'GB28181 云端录像', '2026-08-11 08:43:35', '2026-08-11 08:43:35', NULL, 1),
(317, '查看级联平台列表', '/api/gb28181/cascade/platforms', 'GET', 'GB28181 国标级联', '2026-08-11 08:49:48', '2026-08-11 08:49:48', NULL, 1),
(318, '创建级联平台', '/api/gb28181/cascade/platforms', 'POST', 'GB28181 国标级联', '2026-08-11 08:49:48', '2026-08-11 08:49:48', NULL, 1),
(319, '查看级联平台', '/api/gb28181/cascade/platforms/:id', 'GET', 'GB28181 国标级联', '2026-08-11 08:49:48', '2026-08-11 08:49:48', NULL, 1),
(320, '修改级联平台', '/api/gb28181/cascade/platforms/:id', 'PUT', 'GB28181 国标级联', '2026-08-11 08:49:48', '2026-08-11 08:49:48', NULL, 1),
(321, '删除级联平台', '/api/gb28181/cascade/platforms/:id', 'DELETE', 'GB28181 国标级联', '2026-08-11 08:49:48', '2026-08-11 08:49:48', NULL, 1),
(322, '启用级联平台', '/api/gb28181/cascade/platforms/:id/enable', 'POST', 'GB28181 国标级联', '2026-08-11 08:49:48', '2026-08-11 08:49:48', NULL, 1),
(323, '停用级联平台', '/api/gb28181/cascade/platforms/:id/disable', 'POST', 'GB28181 国标级联', '2026-08-11 08:49:48', '2026-08-11 08:49:48', NULL, 1),
(324, '更新级联启用状态', '/api/gb28181/cascade/platforms/:id/enabled', 'PUT', 'GB28181 国标级联', '2026-08-11 08:49:48', '2026-08-11 08:49:48', NULL, 1),
(325, '重连级联平台', '/api/gb28181/cascade/platforms/:id/reconnect', 'POST', 'GB28181 国标级联', '2026-08-11 08:49:48', '2026-08-11 08:49:48', NULL, 1),
(326, '查看级联共享', '/api/gb28181/cascade/platforms/:id/shares', 'GET', 'GB28181 国标级联', '2026-08-11 08:49:48', '2026-08-11 08:49:48', NULL, 1),
(327, '更新级联共享', '/api/gb28181/cascade/platforms/:id/shares', 'PUT', 'GB28181 国标级联', '2026-08-11 08:49:48', '2026-08-11 08:49:48', NULL, 1),
(332, '读取播放鉴权配置', '/api/gb28181/sip/service-config/play-auth', 'GET', 'GB28181 SIP 配置', '2026-08-11 15:06:39', '2026-08-11 15:06:39', NULL, 1),
(333, '修改播放鉴权配置', '/api/gb28181/sip/service-config/play-auth', 'PUT', 'GB28181 SIP 配置', '2026-08-11 15:06:39', '2026-08-11 15:06:39', NULL, 1),
(334, '申请固定播放地址授权', '/api/gb28181/play/:deviceId/:channelId/authorization', 'POST', 'GB28181 播放鉴权', '2026-08-11 16:01:51', '2026-08-11 16:01:51', NULL, 1),
(335, '发起实时点播', '/api/gb28181/play/:deviceId/:channelId', 'POST', 'GB28181 播放鉴权', '2026-08-11 19:39:57', '2026-08-11 19:39:57', NULL, 1),
(336, '创建云端录像下载', '/api/gb28181/cloud-recordings/files/:id/downloads', 'POST', 'GB28181 云端录像下载', '2026-08-13 08:45:38', '2026-08-13 08:45:38', NULL, 1),
(337, '查询云端录像下载', '/api/gb28181/cloud-recordings/downloads/:taskId', 'GET', 'GB28181 云端录像下载', '2026-08-13 08:45:38', '2026-08-13 08:45:38', NULL, 1),
(338, '取消云端录像下载', '/api/gb28181/cloud-recordings/downloads/:taskId', 'DELETE', 'GB28181 云端录像下载', '2026-08-13 08:45:38', '2026-08-13 08:45:38', NULL, 1);
INSERT INTO "sys_menu" ("id", "parent_id", "path", "name", "redirect", "component", "title", "is_full", "hide", "disable", "keep_alive", "affix", "link", "iframe", "svg_icon", "icon", "sort", "type", "is_link", "permission", "created_at", "updated_at", "deleted_at", "created_by") VALUES
(1, 0, '/home', 'home', '', 'home/home', 'home', 0, 0, 0, 0, 1, '', 0, '', 'lucide:Gauge', 1, 2, 0, '', '2025-08-27 09:09:44', '2026-08-09 22:41:04', NULL, 1),
(10, 0, '/system', 'system', '', '', 'system', 0, 0, 0, 1, 0, '', 0, '', 'lucide:Settings', 0, 1, 0, '', '2025-08-27 09:09:44', '2026-07-08 10:46:25', NULL, 1),
(1001, 10, '/system/account', 'account', '', 'system/account/account', 'account', 0, 0, 0, 1, 0, '', 0, '', 'lucide:UserRound', 0, 2, 0, '', '2025-08-27 09:09:44', '2026-07-08 10:46:25', NULL, 1),
(1002, 10, '/system/role', 'role', '', 'system/role/role', 'role', 0, 0, 0, 1, 0, '', 0, '', 'lucide:Shield', 0, 2, 0, '', '2025-08-27 09:09:44', '2026-07-08 10:46:25', NULL, 1),
(1003, 10, '/system/menu', 'menu', '', 'system/menu/menu', 'menu', 0, 0, 0, 1, 0, '', 0, '', 'lucide:Menu', 0, 2, 0, '', '2025-08-27 09:09:44', '2026-07-08 10:46:25', NULL, 1),
(1004, 10, '/system/division', 'division', '', 'system/division/division', 'division', 0, 0, 0, 1, 0, '', 0, '', 'lucide:Building2', 0, 2, 0, '', '2025-08-27 09:09:44', '2026-07-08 10:46:25', NULL, 1),
(1005, 10, '/system/dictionary', 'dictionary', '', 'system/dictionary/dictionary', 'dictionary', 0, 0, 0, 1, 0, '', 0, '', 'lucide:BookOpen', 0, 2, 0, '', '2025-08-27 09:09:44', '2026-07-08 10:46:25', NULL, 1),
(1006, 10, '/system/log', 'log', '', 'system/log/log', 'log', 0, 0, 0, 1, 0, '', 0, '', 'lucide:FileText', 0, 2, 0, '', '2025-08-27 09:09:44', '2026-07-08 10:46:25', NULL, 1),
(1007, 10, '/system/userinfo', 'userinfo', '', 'system/userinfo/userinfo', 'userinfo', 0, 1, 0, 1, 0, '', 0, '', 'lucide:UserCog', 0, 2, 0, '', '2025-08-27 09:09:44', '2026-07-08 10:46:25', NULL, 1),
(140213, 10, '/system/api', 'SystemApi', '', 'system/sysapi/sysapi', 'api-management', 0, 0, 0, 1, 0, '', 0, '', 'lucide:Network', 0, 2, 0, '', '2025-09-03 10:53:57', '2026-07-08 10:46:25', NULL, 1),
(140214, 1001, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:account:add', '2025-09-03 16:11:58', '2025-09-03 16:11:58', NULL, 1),
(140215, 1001, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:account:edit', '2025-09-03 17:11:24', '2025-09-03 17:11:24', NULL, 1),
(140216, 1001, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:account:delete', '2025-09-03 17:12:22', '2025-09-03 17:12:22', NULL, 1),
(140218, 1002, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:role:add', '2025-09-04 16:43:54', '2025-09-04 16:43:54', NULL, 1),
(140219, 1002, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:role:edit', '2025-09-04 16:47:15', '2025-09-04 16:47:15', NULL, 1),
(140220, 1002, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:role:delete', '2025-09-04 16:50:19', '2025-09-04 16:50:19', NULL, 1),
(140221, 1002, '', '', '', '', '分配权限', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:role:addRoleMenu', '2025-09-04 16:53:09', '2025-09-04 16:53:09', NULL, 1),
(140222, 1003, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:menu:add', '2025-09-04 17:07:16', '2025-09-04 17:07:16', NULL, 1),
(140223, 1003, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:menu:edit', '2025-09-04 17:11:51', '2025-09-04 17:11:51', NULL, 1),
(140224, 1003, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:menu:delete', '2025-09-04 17:12:24', '2025-09-04 17:12:24', NULL, 1),
(140225, 1003, '', '', '', '', '分配权限', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:menu:setMenuApis', '2025-09-04 17:20:09', '2025-09-04 17:20:09', NULL, 1),
(140226, 140213, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:api:add', '2025-09-04 17:30:56', '2025-09-04 17:30:56', NULL, 1),
(140227, 140213, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:api:edit', '2025-09-04 17:31:20', '2025-09-04 17:31:20', NULL, 1),
(140228, 140213, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:api:delete', '2025-09-04 17:31:38', '2025-09-04 17:31:38', NULL, 1),
(140229, 1004, '', '', '', '', '新增部门', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:division:add', '2025-09-12 14:50:55', '2025-09-12 14:50:55', NULL, 1),
(140230, 1004, '', '', '', '', '编辑部门', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:division:edit', '2025-09-12 14:51:17', '2025-09-12 14:51:17', NULL, 1),
(140231, 1004, '', '', '', '', '删除部门', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:division:delete', '2025-09-12 14:51:51', '2025-09-12 14:51:51', NULL, 1),
(140232, 1005, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dict:add', '2025-09-16 16:38:06', '2025-09-16 16:38:06', NULL, 1),
(140233, 1005, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dict:edit', '2025-09-16 16:39:58', '2025-09-16 16:39:58', NULL, 1),
(140234, 1005, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dict:delete', '2025-09-16 16:40:19', '2025-09-16 16:40:19', NULL, 1),
(140235, 1005, '', '', '', '', '字典项管理', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dictitem:list', '2025-09-16 17:09:58', '2025-09-16 17:31:35', NULL, 1),
(140236, 1005, '', '', '', '', '新增字典项', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dictitem:add', '2025-09-16 17:32:06', '2025-09-16 17:32:06', NULL, 1),
(140237, 1005, '', '', '', '', '编辑字典项', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dictitem:edit', '2025-09-16 17:33:16', '2025-09-16 17:33:16', NULL, 1),
(140238, 1005, '', '', '', '', '删除字典项', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dictitem:delete', '2025-09-16 17:33:41', '2025-09-16 17:33:41', NULL, 1),
(140239, 10, '/system/affix', 'SystemAffix', '', 'system/affix/affix', 'file-manager', 0, 0, 0, 1, 0, '', 0, '', 'lucide:Folder', 0, 2, 0, '', '2025-09-25 15:17:00', '2026-07-08 10:46:25', NULL, 1),
(140240, 140239, '', '', '', '', '文件上传', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:affix:upload', '2025-09-25 15:45:29', '2025-09-25 15:46:29', NULL, 1),
(140241, 140239, '', '', '', '', '删除文件', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:affix:delete', '2025-09-25 15:46:52', '2025-09-25 15:46:52', NULL, 1),
(140242, 140239, '', '', '', '', '修改文件名', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:affix:updateName', '2025-09-25 15:47:41', '2025-09-25 15:47:41', NULL, 1),
(140243, 140239, '', '', '', '', '下载文件', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:affix:download', '2025-09-25 15:48:56', '2025-09-25 15:48:56', NULL, 1),
(140244, 1002, '', '', '', '', '数据权限', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:role:dataScope', '2025-09-26 17:07:16', '2025-09-26 17:07:16', NULL, 1),
(140245, 10, '/system/sysconfig', 'SystemSysconfig', '', 'system/sysconfig/sysconfig', 'system-config', 0, 0, 0, 1, 0, '', 0, '', 'lucide:SlidersHorizontal', 0, 2, 0, '', '2025-10-09 16:15:21', '2026-07-08 10:46:25', NULL, 1),
(140246, 140245, '', '', '', '', '修改系统配置', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:config:update', '2025-10-09 16:24:33', '2025-10-09 16:24:33', NULL, 1),
(140252, 1007, '', '', '', '', '修改密码、手机号等', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:userinfo:updateAccount', '2025-10-17 11:12:56', '2025-10-17 11:12:56', NULL, 1),
(140254, 140239, '', '', '', '', '复制链接', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:affix:copy', '2025-10-17 11:38:09', '2025-10-17 11:38:09', NULL, 1),
(140255, 1006, '', '', '', '', '导出', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:log:export', '2025-10-20 10:16:51', '2025-10-20 10:16:51', NULL, 1),
(140256, 1006, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:log:delete', '2025-10-20 10:17:19', '2025-10-20 10:17:19', NULL, 1),
(140257, 1003, '', '', '', '', '导出', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:menu:export', '2025-10-20 17:18:01', '2025-10-20 17:18:13', NULL, 1),
(140258, 1003, '', '', '', '', '导入', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:menu:import', '2025-10-21 11:29:45', '2025-10-21 11:29:45', NULL, 1),
(140264, 1007, '', '', '', '', '修改用户基本信息', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:userinfo:updateBasicInfo', '2025-10-31 09:26:42', '2025-10-31 09:26:42', NULL, 1),
(140265, 10, '/system/codegen', 'SystemCodegen', '', 'system/codegen/codegen', 'codegen', 0, 0, 0, 1, 0, '', 0, '', 'lucide:CodeXml', 0, 2, 0, '', '2025-11-04 11:45:49', '2026-07-08 10:46:25', NULL, 1),
(140329, 140265, '', '', '', '', '导入表', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'system:codegen:batchInsert', '2025-11-17 15:32:25', '2025-11-17 15:32:25', NULL, 1),
(140330, 140265, '', '', '', '', '配置', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'system:codegen:update', '2025-11-17 15:33:57', '2025-11-17 15:33:57', NULL, 1),
(140331, 140265, '', '', '', '', '预览', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'system:codegen:preview', '2025-11-17 15:34:24', '2025-11-17 15:34:24', NULL, 1),
(140332, 140265, '', '', '', '', '生成代码文件', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'system:codegen:generate', '2025-11-17 15:35:00', '2025-11-17 15:35:00', NULL, 1),
(140333, 140265, '', '', '', '', '同步数据库', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'system:codegen:refreshFields', '2025-11-17 15:35:51', '2025-11-17 15:35:51', NULL, 1),
(140334, 140265, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'system:codegen:delete', '2025-11-17 15:36:50', '2025-11-17 15:36:50', NULL, 1),
(140335, 140265, '', '', '', '', '生成菜单', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:codegen:insertmenuandapi', '2025-11-26 15:16:32', '2025-11-26 15:16:32', NULL, 1),
(140336, 10, '/system/pluginsmanager', 'SystemPluginsmanager', '', 'system/pluginsmanager/pluginsmanager', 'plugins-manager', 0, 0, 0, 1, 0, '', 0, '', 'lucide:Blocks', 0, 2, 0, '', '2025-12-05 17:59:34', '2026-07-08 10:46:25', NULL, 1),
(140338, 140336, '', '', '', '', '导出插件', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:pluginsmanager:export', '2025-12-08 16:33:32', '2025-12-08 16:33:32', NULL, 1),
(140339, 140336, '', '', '', '', '导入插件', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:pluginsmanager:import', '2025-12-08 16:33:51', '2025-12-08 16:33:51', NULL, 1),
(140340, 140336, '', '', '', '', '插件卸载', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:pluginsmanager:uninstall', '2025-12-08 16:34:53', '2025-12-08 16:34:53', NULL, 1),
(140341, 0, '/sysjobs', 'Sysjobs', '', '', 'sysjobs', 0, 0, 0, 1, 0, '', 0, '', 'lucide:CalendarClock', 0, 1, 0, '', '2026-02-11 11:29:40', '2026-07-08 10:46:25', NULL, 1),
(140342, 140341, '/system/sysjobslist', 'SystemSysjobslist', '', 'system/sysjobs/sysjobslist', 'jobslist', 0, 0, 0, 1, 0, '', 0, '', 'lucide:ListTodo', 0, 2, 0, '', '2026-02-11 11:36:54', '2026-07-08 10:46:25', NULL, 1),
(140343, 140342, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:sysjobs:add', '2026-02-11 11:43:35', '2026-02-11 11:43:35', NULL, 1),
(140344, 140342, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:sysjobs:edit', '2026-02-11 11:44:00', '2026-02-11 11:44:00', NULL, 1),
(140345, 140342, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:sysjobs:delete', '2026-02-11 11:44:22', '2026-02-11 11:44:22', NULL, 1),
(140346, 140342, '', '', '', '', '执行一次', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:sysjobs:executeNow', '2026-02-12 17:59:02', '2026-02-12 17:59:02', NULL, 1),
(140347, 140341, '/system/joblog', 'SystemJoblog', '', 'system/sysjobresults/sysjobresultslist', 'joblog', 0, 0, 0, 1, 0, '', 0, '', 'lucide:History', 0, 2, 0, '', '2026-02-11 11:41:27', '2026-07-08 10:46:25', NULL, 1),
(140348, 140347, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:sysjobresults:delete', '2026-02-11 11:45:18', '2026-02-11 11:45:18', NULL, 1),
(140349, 140239, '', '', '', '', '大文件上传', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:affix:bigupload', '2026-04-09 15:47:39', '2026-04-09 15:47:39', NULL, 1),
(140351, 140355, '/gb28181/zlm/nodes', 'gb28181-zlm-nodes', '', 'gb28181/zlm/NodeList', '流媒体节点', 0, 0, 0, 0, 0, '', 0, '', 'lucide:Server', 10, 2, 0, '', '2026-06-26 15:00:19', '2026-08-09 22:41:04', NULL, 0),
(140352, 0, '/gb28181/zlm/nodes/:id', 'gb28181-zlm-node-detail', '', 'gb28181/zlm/NodeDetail', '节点详情', 0, 1, 0, 0, 0, '', 0, '', 'lucide:Server', 6, 2, 0, '', '2026-06-26 15:00:19', '2026-08-09 22:41:04', NULL, 0),
(140353, 140355, '/gb28181/zlm/scheduler', 'gb28181-zlm-scheduler-strategy', '', 'gb28181/zlm/SchedulerStrategy', '调度算法', 0, 0, 0, 0, 0, '', 0, '', 'lucide:Workflow', 11, 2, 0, '', '2026-06-27 18:10:12', '2026-08-09 22:41:04', NULL, 0),
(140354, 140355, '/gb28181/zlm/scheduler/logs', 'gb28181-zlm-scheduler-log', '', 'gb28181/zlm/SchedulerLog', '调度日志', 0, 0, 0, 0, 0, '', 0, '', 'lucide:History', 12, 2, 0, '', '2026-06-27 18:10:12', '2026-08-09 22:41:04', NULL, 0),
(140355, 0, '/media', 'Media', '', '', '流媒体管理', 0, 0, 0, 1, 0, '', 0, '', 'lucide:Clapperboard', 36, 1, 0, '', '2026-06-30 09:59:39', '2026-08-11 14:18:57', NULL, 1),
(140357, 0, '/gb28181/device-mgmt/anomaly', 'device-mgmt-anomaly', '', 'gb28181/device-mgmt/anomaly/index', '目录异常', 0, 1, 0, 0, 0, '', 0, '', 'lucide:Cctv', 6, 2, 0, '', '2026-06-30 13:33:58', '2026-08-09 22:41:04', NULL, 0),
(140358, 0, '/gb28181/device-mgmt/index', 'device-mgmt-list', '', 'gb28181/device-mgmt/index', '设备列表', 0, 0, 0, 0, 0, '', 0, '', 'lucide:Cctv', 2, 2, 0, '', '2026-06-30 13:37:04', '2026-08-09 22:41:04', NULL, 0),
(140359, 0, '/gb28181/sip/platform', 'gb28181-sip-platform', NULL, 'gb28181/sip/PlatformInfo', 'SIP 接入信息', 0, 0, 0, 0, 0, '', 0, '', 'lucide:Router', 6, 2, 0, '', '2026-07-09 19:06:03', '2026-08-09 22:41:04', NULL, NULL),
(140360, 140355, '', '', NULL, '', '查看 SIP 配置', 0, 1, 0, 0, 0, '', 0, '', '', 0, 3, 0, 'gb28181:sip:config:view', '2026-07-20 09:24:46', '2026-07-20 09:24:46', NULL, 1),
(140361, 140355, '', '', NULL, '', '修改 SIP 配置', 0, 1, 0, 0, 0, '', 0, '', '', 0, 3, 0, 'gb28181:sip:config:update', '2026-07-20 09:24:46', '2026-07-20 09:24:46', NULL, 1),
(140362, 0, '/gb28181/sip-traces', 'gb28181-sip-traces', '', 'gb28181/sip-log-v2/index', 'SIP 日志', 0, 0, 0, 0, 0, '', 0, '', 'lucide:FileText', 8, 2, 0, '', '2026-07-20 13:46:36', '2026-08-09 22:41:04', NULL, 0),
(140364, 140355, '', '', NULL, NULL, '管理自定义分组', 0, 1, 0, 0, 0, '', 0, '', '', 0, 3, 0, 'gb28181:device-group:manage', '2026-08-02 16:32:06', '2026-08-02 16:32:06', NULL, 1),
(140365, 0, '/gb28181/device-record-playback/:channelId', 'gb28181-device-record-playback', NULL, 'gb28181/device-record-playback/index', '设备录像回放', 0, 1, 0, 0, 0, '', 0, '', 'lucide:History', 99, 2, 0, '', '2026-08-04 15:41:09', '2026-08-09 22:41:04', NULL, 1),
(140366, 0, '/gb28181/alarm-management', 'gb28181-alarm-management', NULL, 'gb28181/alarm-management/index', '告警管理', 0, 0, 0, 0, 0, '', 0, '', 'lucide:BellRing', 4, 2, 0, 'gb28181:alarm:view', '2026-08-04 22:41:38', '2026-08-09 22:41:04', NULL, 1),
(140367, 140366, '', '', NULL, '', '物理删除告警', 0, 1, 0, 0, 0, '', 0, '', '', 0, 3, 0, 'gb28181:alarm:delete', '2026-08-04 22:41:38', '2026-08-04 22:41:38', NULL, 1),
(140368, 0, '/gb28181/multi-screen-playback', 'gb28181-multi-screen-playback', NULL, 'gb28181/multi-screen-playback/index', '多屏播放', 0, 0, 0, 0, 0, '', 0, '', 'lucide:MonitorPlay', 3, 2, 0, '', '2026-08-06 17:33:14', '2026-08-09 22:41:04', NULL, 1),
(140369, 0, '/gb28181/sip/config', 'gb28181-sip-service-config', '', 'gb28181/sip/ServiceConfig', '国标服务配置', 0, 0, 0, 0, 0, '', 0, '', 'lucide:ServerCog', 7, 2, 0, '', '2026-08-07 10:42:06', '2026-08-09 22:41:04', NULL, 1),
(140370, 140355, '', '', NULL, NULL, '管理播放方案', 0, 1, 0, 0, 0, '', 0, '', '', 0, 3, 0, 'gb28181:playback-scheme:manage', '2026-08-07 17:23:46', '2026-08-07 17:23:46', NULL, 1),
(140371, 0, '/security-preview', 'security-preview', '', 'gb28181/security/preview', '国标接入安全', 0, 0, 0, 0, 0, '', 0, '', 'lucide:Shield', 5, 2, 0, '', '2026-08-09 11:15:46', '2026-08-10 10:59:35', NULL, 1),
(140372, 0, '/gb28181/security', 'gb28181-security', '', 'gb28181/security/index', '国标接入安全', 0, 0, 1, 0, 0, '', 0, '', 'lucide:ShieldCheck', 5, 2, 0, '', '2026-08-10 09:27:17', '2026-08-10 10:59:28', NULL, 1),
(140373, 0, '/gb28181/cloud-recordings', 'gb28181-cloud-recordings', NULL, 'gb28181/cloud-recordings/index', '云端录像', 0, 0, 0, 0, 0, '', 0, '', 'lucide:Cloud', 35, 2, 0, 'gb28181:recording:view', '2026-08-11 08:43:15', '2026-08-11 08:47:36', NULL, 1),
(140374, 140373, '', '', NULL, '', '执行录像对账', 0, 1, 0, 0, 0, '', 0, '', '', 0, 3, 0, 'gb28181:recording:reconcile', '2026-08-11 08:43:22', '2026-08-11 08:43:22', NULL, 1),
(140375, 0, '/gb28181/cascade', 'gb28181-cascade', NULL, 'gb28181/cascade/index', '国标级联', 0, 0, 0, 0, 0, '', 0, '', 'lucide:GitBranch', 13, 2, 0, '', '2026-08-11 08:49:48', '2026-08-11 08:49:48', NULL, 1),
(140376, 140375, '', 'gb28181-cascade-view', NULL, '', '查看国标级联', 0, 1, 0, 0, 0, '', 0, '', '', 0, 3, 0, 'gb28181:cascade:view', '2026-08-11 08:49:48', '2026-08-11 08:51:00', NULL, 1),
(140377, 140375, '', 'gb28181-cascade-manage', NULL, '', '管理国标级联', 0, 1, 0, 0, 0, '', 0, '', '', 0, 3, 0, 'gb28181:cascade:manage', '2026-08-11 08:49:48', '2026-08-11 08:51:00', NULL, 1),
(140378, 140375, '', 'gb28181-cascade-enable', NULL, '', '启停国标级联', 0, 1, 0, 0, 0, '', 0, '', '', 0, 3, 0, 'gb28181:cascade:enable', '2026-08-11 08:49:48', '2026-08-11 08:51:00', NULL, 1),
(140379, 140375, '', 'gb28181-cascade-share', NULL, '', '共享国标级联资源', 0, 1, 0, 0, 0, '', 0, '', '', 0, 3, 0, 'gb28181:cascade:share', '2026-08-11 08:49:48', '2026-08-11 08:51:00', NULL, 1),
(140380, 140375, '', 'gb28181-cascade-reconnect', NULL, '', '重连国标级联', 0, 1, 0, 0, 0, '', 0, '', '', 0, 3, 0, 'gb28181:cascade:reconnect', '2026-08-11 08:49:48', '2026-08-11 08:51:00', NULL, 1),
(140381, 140355, '', '', NULL, NULL, '发起实时点播', 0, 1, 0, 0, 0, '', 0, '', '', 0, 3, 0, 'gb28181:play:start', '2026-08-11 19:39:57', '2026-08-11 19:39:57', NULL, 1);
INSERT INTO "sys_menu_api" ("menu_id", "api_id") VALUES
(10, 5),
(10, 6),
(10, 7),
(10, 12),
(10, 27),
(10, 54),
(10, 202),
(1001, 7),
(1001, 8),
(1001, 18),
(1001, 19),
(1002, 19),
(1003, 13),
(1004, 18),
(1004, 37),
(1005, 41),
(1006, 70),
(1007, 6),
(140213, 29),
(140214, 9),
(140215, 10),
(140216, 11),
(140218, 24),
(140219, 25),
(140220, 26),
(140221, 13),
(140221, 20),
(140221, 21),
(140222, 15),
(140223, 16),
(140224, 17),
(140224, 197),
(140225, 29),
(140225, 35),
(140225, 36),
(140226, 31),
(140227, 30),
(140227, 32),
(140228, 33),
(140229, 38),
(140230, 39),
(140231, 40),
(140232, 43),
(140233, 44),
(140234, 45),
(140235, 48),
(140236, 50),
(140237, 51),
(140238, 52),
(140239, 58),
(140240, 55),
(140241, 56),
(140242, 57),
(140243, 60),
(140244, 61),
(140245, 62),
(140245, 64),
(140246, 63),
(140252, 53),
(140254, 60),
(140255, 73),
(140256, 72),
(140257, 74),
(140258, 75),
(140264, 89),
(140265, 190),
(140329, 188),
(140329, 191),
(140330, 192),
(140330, 193),
(140331, 189),
(140332, 105),
(140333, 195),
(140334, 194),
(140335, 196),
(140336, 198),
(140338, 199),
(140339, 200),
(140340, 201),
(140342, 203),
(140342, 204),
(140343, 205),
(140344, 206),
(140344, 207),
(140344, 208),
(140345, 209),
(140346, 210),
(140347, 211),
(140348, 212),
(140349, 55),
(140349, 213),
(140349, 214),
(140349, 215),
(140349, 216),
(140360, 49),
(140360, 217),
(140360, 218),
(140360, 219),
(140360, 237),
(140360, 261),
(140360, 263),
(140360, 265),
(140360, 267),
(140360, 269),
(140360, 271),
(140360, 273),
(140360, 275),
(140360, 299),
(140360, 301),
(140360, 306),
(140360, 308),
(140360, 332),
(140361, 220),
(140361, 221),
(140361, 262),
(140361, 264),
(140361, 266),
(140361, 268),
(140361, 270),
(140361, 272),
(140361, 274),
(140361, 276),
(140361, 300),
(140361, 302),
(140361, 307),
(140361, 309),
(140361, 333),
(140362, 222),
(140362, 223),
(140362, 224),
(140362, 225),
(140362, 226),
(140362, 227),
(140362, 228),
(140362, 229),
(140364, 238),
(140364, 239),
(140364, 240),
(140364, 241),
(140364, 242),
(140364, 243),
(140366, 245),
(140366, 246),
(140367, 247),
(140367, 248),
(140369, 217),
(140369, 218),
(140369, 220),
(140369, 252),
(140369, 253),
(140370, 254),
(140370, 255),
(140370, 256),
(140370, 257),
(140370, 258),
(140370, 259),
(140372, 277),
(140372, 278),
(140372, 279),
(140372, 280),
(140372, 281),
(140372, 282),
(140372, 283),
(140372, 284),
(140372, 292),
(140372, 293),
(140372, 294),
(140372, 295),
(140373, 310),
(140373, 311),
(140373, 312),
(140373, 313),
(140373, 314),
(140373, 336),
(140373, 337),
(140373, 338),
(140374, 315),
(140374, 316),
(140376, 317),
(140376, 319),
(140376, 326),
(140377, 318),
(140377, 320),
(140377, 321),
(140378, 322),
(140378, 324),
(140379, 326),
(140379, 327),
(140380, 325),
(140381, 334),
(140381, 335);
INSERT INTO "sys_role_menu" ("role_id", "menu_id") VALUES
(1, 1),
(1, 10),
(1, 1001),
(1, 1002),
(1, 1003),
(1, 1004),
(1, 1005),
(1, 1006),
(1, 1007),
(1, 140213),
(1, 140214),
(1, 140215),
(1, 140216),
(1, 140218),
(1, 140219),
(1, 140220),
(1, 140221),
(1, 140222),
(1, 140223),
(1, 140224),
(1, 140225),
(1, 140226),
(1, 140227),
(1, 140228),
(1, 140229),
(1, 140230),
(1, 140231),
(1, 140232),
(1, 140233),
(1, 140234),
(1, 140235),
(1, 140236),
(1, 140237),
(1, 140238),
(1, 140239),
(1, 140240),
(1, 140241),
(1, 140242),
(1, 140243),
(1, 140244),
(1, 140245),
(1, 140246),
(1, 140252),
(1, 140254),
(1, 140255),
(1, 140256),
(1, 140257),
(1, 140258),
(1, 140264),
(1, 140265),
(1, 140329),
(1, 140330),
(1, 140331),
(1, 140332),
(1, 140333),
(1, 140334),
(1, 140335),
(1, 140336),
(1, 140338),
(1, 140339),
(1, 140340),
(1, 140341),
(1, 140342),
(1, 140343),
(1, 140344),
(1, 140345),
(1, 140346),
(1, 140347),
(1, 140348),
(1, 140349),
(1, 140351),
(1, 140352),
(1, 140353),
(1, 140354),
(1, 140355),
(1, 140357),
(1, 140358),
(1, 140359),
(1, 140360),
(1, 140361),
(1, 140362),
(1, 140364),
(1, 140365),
(1, 140366),
(1, 140367),
(1, 140368),
(1, 140369),
(1, 140370),
(1, 140371),
(1, 140372),
(1, 140373),
(1, 140374),
(1, 140375),
(1, 140376),
(1, 140377),
(1, 140378),
(1, 140379),
(1, 140380),
(1, 140381);
INSERT INTO "sys_casbin_rule" ("id", "ptype", "v0", "v1", "v2", "v3", "v4", "v5") VALUES
(6266, 'g', 'user_1', 'role_1', '*', '', '', ''),
(7562, 'p', 'role_1', '/api/sysOperationLog/delete', 'DELETE', '*', '', ''),
(7564, 'p', 'role_1', '/api/sysJobs/delete', 'DELETE', '*', '', ''),
(7565, 'p', 'role_1', '/api/sysAffix/chunk/cancel', 'DELETE', '*', '', ''),
(7566, 'p', 'role_1', '/api/sysApi/list', 'GET', '*', '', ''),
(7567, 'p', 'role_1', '/api/sysRole/add', 'POST', '*', '', ''),
(7568, 'p', 'role_1', '/api/sysRole/edit', 'PUT', '*', '', ''),
(7569, 'p', 'role_1', '/api/sysRole/delete', 'DELETE', '*', '', ''),
(7570, 'p', 'role_1', '/api/sysRole/addRoleMenu', 'POST', '*', '', ''),
(7571, 'p', 'role_1', '/api/sysDepartment/delete', 'DELETE', '*', '', ''),
(7572, 'p', 'role_1', '/api/sysDictItem/getByDictId/:dictId', 'GET', '*', '', ''),
(7573, 'p', 'role_1', '/api/config/get', 'GET', '*', '', ''),
(7574, 'p', 'role_1', '/api/users/updateAccount', 'PUT', '*', '', ''),
(7575, 'p', 'role_1', '/api/sysMenu/import', 'POST', '*', '', ''),
(7577, 'p', 'role_1', '/api/sysGen/:id', 'GET', '*', '', ''),
(7578, 'p', 'role_1', '/api/pluginsmanager/exports', 'GET', '*', '', ''),
(7579, 'p', 'role_1', '/api/users/logout', 'POST', '*', '', ''),
(7580, 'p', 'role_1', '/api/users/delete', 'DELETE', '*', '', ''),
(7581, 'p', 'role_1', '/api/sysDictItem/edit', 'PUT', '*', '', ''),
(7582, 'p', 'role_1', '/api/sysAffix/upload', 'POST', '*', '', ''),
(7585, 'p', 'role_1', '/api/pluginsmanager/uninstall', 'DELETE', '*', '', ''),
(7586, 'p', 'role_1', '/api/sysOperationLog/list', 'GET', '*', '', ''),
(7587, 'p', 'role_1', '/api/sysMenu/delete', 'DELETE', '*', '', ''),
(7588, 'p', 'role_1', '/api/sysApi/add', 'POST', '*', '', ''),
(7589, 'p', 'role_1', '/api/sysDepartment/edit', 'PUT', '*', '', ''),
(7590, 'p', 'role_1', '/api/sysAffix/download/:id', 'GET', '*', '', ''),
(7591, 'p', 'role_1', '/api/sysRole/dataScope', 'PUT', '*', '', ''),
(7593, 'p', 'role_1', '/api/sysGen/batchInsert', 'POST', '*', '', ''),
(7594, 'p', 'role_1', '/api/sysRole/getUserPermission/:roleId', 'GET', '*', '', ''),
(7595, 'p', 'role_1', '/api/sysApi/:id', 'GET', '*', '', ''),
(7596, 'p', 'role_1', '/api/sysDict/add', 'POST', '*', '', ''),
(7597, 'p', 'role_1', '/api/codegen/generate', 'POST', '*', '', ''),
(7598, 'p', 'role_1', '/api/codegen/insertmenuandapi', 'POST', '*', '', ''),
(7599, 'p', 'role_1', '/api/pluginsmanager/export', 'POST', '*', '', ''),
(7600, 'p', 'role_1', '/api/sysJobs/list', 'GET', '*', '', ''),
(7601, 'p', 'role_1', '/api/sysAffix/chunk/merge', 'POST', '*', '', ''),
(7602, 'p', 'role_1', '/api/sysDepartment/:id', 'GET', '*', '', ''),
(7603, 'p', 'role_1', '/api/sysMenu/add', 'POST', '*', '', ''),
(7604, 'p', 'role_1', '/api/sysMenu/edit', 'PUT', '*', '', ''),
(7605, 'p', 'role_1', '/api/sysDict/edit', 'PUT', '*', '', ''),
(7609, 'p', 'role_1', '/api/sysGen/update', 'PUT', '*', '', ''),
(7610, 'p', 'role_1', '/api/config/update', 'PUT', '*', '', ''),
(7611, 'p', 'role_1', '/api/sysMenu/export', 'GET', '*', '', ''),
(7612, 'p', 'role_1', '/api/codegen/preview', 'GET', '*', '', ''),
(7613, 'p', 'role_1', '/api/pluginsmanager/import', 'POST', '*', '', ''),
(7614, 'p', 'role_1', '/api/sysJobs/setStatus', 'PUT', '*', '', ''),
(7615, 'p', 'role_1', '/api/sysAffix/chunk/upload', 'POST', '*', '', ''),
(7616, 'p', 'role_1', '/api/users/switchTenant/:tenantld', 'GET', '*', '', ''),
(7617, 'p', 'role_1', '/api/sysMenu/apis/:id', 'GET', '*', '', ''),
(7618, 'p', 'role_1', '/api/sysDictItem/add', 'POST', '*', '', ''),
(7620, 'p', 'role_1', '/api/sysJobs/executeNow', 'POST', '*', '', ''),
(7621, 'p', 'role_1', '/api/users/add', 'POST', '*', '', ''),
(7622, 'p', 'role_1', '/api/sysGen/list', 'GET', '*', '', ''),
(7623, 'p', 'role_1', '/api/codegen/tables', 'GET', '*', '', ''),
(7624, 'p', 'role_1', '/api/sysJobs/executors', 'GET', '*', '', ''),
(7625, 'p', 'role_1', '/api/users/profile', 'GET', '*', '', ''),
(7626, 'p', 'role_1', '/api/sysDict/getAllDicts', 'GET', '*', '', ''),
(7627, 'p', 'role_1', '/api/users/uploadAvatar', 'POST', '*', '', ''),
(7628, 'p', 'role_1', '/api/sysMenu/getMenuList', 'GET', '*', '', ''),
(7629, 'p', 'role_1', '/api/sysMenu/setApis', 'POST', '*', '', ''),
(7630, 'p', 'role_1', '/api/sysApi/delete', 'DELETE', '*', '', ''),
(7633, 'p', 'role_1', '/api/users/list', 'GET', '*', '', ''),
(7634, 'p', 'role_1', '/api/sysDict/list', 'GET', '*', '', ''),
(7635, 'p', 'role_1', '/api/sysAffix/delete', 'DELETE', '*', '', ''),
(7640, 'p', 'role_1', '/api/users/updateBasicInfo', 'PUT', '*', '', ''),
(7642, 'p', 'role_1', '/api/users/:id', 'GET', '*', '', ''),
(7643, 'p', 'role_1', '/api/sysRole/getRoles', 'GET', '*', '', ''),
(7644, 'p', 'role_1', '/api/config/viewCache', 'GET', '*', '', ''),
(7645, 'p', 'role_1', '/api/sysOperationLog/export', 'GET', '*', '', ''),
(7647, 'p', 'role_1', '/api/sysGen/refreshFields', 'PUT', '*', '', ''),
(7648, 'p', 'role_1', '/api/sysGen/:id', 'DELETE', '*', '', ''),
(7649, 'p', 'role_1', '/api/sysMenu/getRouters', 'GET', '*', '', ''),
(7650, 'p', 'role_1', '/api/sysApi/edit', 'PUT', '*', '', ''),
(7651, 'p', 'role_1', '/api/sysDict/delete', 'DELETE', '*', '', ''),
(7652, 'p', 'role_1', '/api/sysAffix/list', 'GET', '*', '', ''),
(7653, 'p', 'role_1', '/api/sysJobs/add', 'POST', '*', '', ''),
(7654, 'p', 'role_1', '/api/sysJobs/edit', 'PUT', '*', '', ''),
(7655, 'p', 'role_1', '/api/sysJobResults/list', 'GET', '*', '', ''),
(7656, 'p', 'role_1', '/api/sysAffix/chunk/init', 'POST', '*', '', ''),
(7657, 'p', 'role_1', '/api/sysJobs/:id', 'GET', '*', '', ''),
(7658, 'p', 'role_1', '/api/sysJobResults/delete', 'DELETE', '*', '', ''),
(7659, 'p', 'role_1', '/api/sysDepartment/getDivision', 'GET', '*', '', ''),
(7660, 'p', 'role_1', '/api/users/edit', 'PUT', '*', '', ''),
(7661, 'p', 'role_1', '/api/sysDepartment/add', 'POST', '*', '', ''),
(7662, 'p', 'role_1', '/api/sysDictItem/delete', 'DELETE', '*', '', ''),
(7664, 'p', 'role_1', '/api/sysMenu/batchDelete', 'DELETE', '*', '', ''),
(7665, 'p', 'role_1', '/api/sysAffix/updateName', 'PUT', '*', '', ''),
(7666, 'p', 'role_1', '/api/gb28181/sip/setup/status', 'GET', '*', '', ''),
(7667, 'p', 'role_1', '/api/gb28181/sip/setup/network-interfaces', 'GET', '*', '', ''),
(7668, 'p', 'role_1', '/api/gb28181/sip/platform', 'GET', '*', '', ''),
(7669, 'p', 'role_1', '/api/gb28181/sip/setup/config', 'PUT', '*', '', ''),
(7670, 'p', 'role_1', '/api/gb28181/sip/setup/skip', 'POST', '*', '', ''),
(7673, 'p', 'role_1', '/api/gb28181/sip-traces/health', 'GET', '*', '', ''),
(7674, 'p', 'role_1', '/api/gb28181/sip-traces/messages', 'GET', '*', '', ''),
(7675, 'p', 'role_1', '/api/gb28181/sip-traces/messages/:id', 'GET', '*', '', ''),
(7676, 'p', 'role_1', '/api/gb28181/sip-traces/sessions', 'GET', '*', '', ''),
(7677, 'p', 'role_1', '/api/gb28181/sip-traces/sessions/:callId/messages', 'GET', '*', '', ''),
(7678, 'p', 'role_1', '/api/gb28181/device-mgmt/device/:id/sip-trace-capture', 'GET', '*', '', ''),
(7679, 'p', 'role_1', '/api/gb28181/device-mgmt/device/:id/sip-trace-captures', 'POST', '*', '', ''),
(7680, 'p', 'role_1', '/api/gb28181/sip-traces/captures/:id/stop', 'POST', '*', '', ''),
(7688, 'p', 'role_1', '/api/gb28181/sip/qr/token', 'POST', '*', '', ''),
(7689, 'p', 'role_1', '/api/gb28181/device-mgmt/custom-groups', 'POST', '*', '', ''),
(7690, 'p', 'role_1', '/api/gb28181/device-mgmt/custom-groups/:id', 'PATCH', '*', '', ''),
(7691, 'p', 'role_1', '/api/gb28181/device-mgmt/custom-groups/:id/move', 'POST', '*', '', ''),
(7692, 'p', 'role_1', '/api/gb28181/device-mgmt/custom-groups/:id', 'DELETE', '*', '', ''),
(7693, 'p', 'role_1', '/api/gb28181/device-mgmt/custom-groups/:id/devices', 'POST', '*', '', ''),
(7694, 'p', 'role_1', '/api/gb28181/device-mgmt/custom-groups/:id/devices/remove', 'POST', '*', '', ''),
(7696, 'p', 'role_1', '/api/gb28181/alarms/:id', 'GET', '*', '', ''),
(7697, 'p', 'role_1', '/api/gb28181/alarms', 'GET', '*', '', ''),
(7699, 'p', 'role_1', '/api/gb28181/alarms/:id', 'DELETE', '*', '', ''),
(7700, 'p', 'role_1', '/api/gb28181/alarms/batch-delete', 'POST', '*', '', ''),
(7702, 'p', 'role_1', '/api/gb28181/sip/service-config/position-history', 'GET', '*', '', ''),
(7703, 'p', 'role_1', '/api/gb28181/sip/service-config/position-history', 'PUT', '*', '', ''),
(7705, 'p', 'role_1', '/api/gb28181/playback-schemes', 'GET', '*', '', ''),
(7706, 'p', 'role_1', '/api/gb28181/playback-schemes/:id', 'GET', '*', '', ''),
(7707, 'p', 'role_1', '/api/gb28181/playback-schemes', 'POST', '*', '', ''),
(7708, 'p', 'role_1', '/api/gb28181/playback-schemes/:id', 'PATCH', '*', '', ''),
(7709, 'p', 'role_1', '/api/gb28181/playback-schemes/:id/layout', 'PUT', '*', '', ''),
(7710, 'p', 'role_1', '/api/gb28181/playback-schemes/:id', 'DELETE', '*', '', ''),
(7712, 'p', 'role_1', '/api/gb28181/sip/service-config/ptz-default-speed', 'GET', '*', '', ''),
(7713, 'p', 'role_1', '/api/gb28181/sip/service-config/ptz-default-speed', 'PUT', '*', '', ''),
(7715, 'p', 'role_1', '/api/gb28181/sip/service-config/sync-channels-on-online', 'GET', '*', '', ''),
(7716, 'p', 'role_1', '/api/gb28181/sip/service-config/sync-channels-on-online', 'PUT', '*', '', ''),
(7718, 'p', 'role_1', '/api/gb28181/sip/service-config/sip-log', 'GET', '*', '', ''),
(7719, 'p', 'role_1', '/api/gb28181/sip/service-config/sip-log', 'PUT', '*', '', ''),
(7721, 'p', 'role_1', '/api/gb28181/sip/service-config/ignore-channel-offline-status-notify', 'GET', '*', '', ''),
(7722, 'p', 'role_1', '/api/gb28181/sip/service-config/ignore-channel-offline-status-notify', 'PUT', '*', '', ''),
(7724, 'p', 'role_1', '/api/gb28181/sip/service-config/online-on-heartbeat', 'GET', '*', '', ''),
(7725, 'p', 'role_1', '/api/gb28181/sip/service-config/online-on-heartbeat', 'PUT', '*', '', ''),
(7727, 'p', 'role_1', '/api/gb28181/sip/service-config/save-alarm-messages', 'GET', '*', '', ''),
(7728, 'p', 'role_1', '/api/gb28181/sip/service-config/save-alarm-messages', 'PUT', '*', '', ''),
(7730, 'p', 'role_1', '/api/gb28181/sip/service-config/sip-command-timeout', 'GET', '*', '', ''),
(7731, 'p', 'role_1', '/api/gb28181/sip/service-config/preallocation-mode', 'GET', '*', '', ''),
(7732, 'p', 'role_1', '/api/gb28181/sip/service-config/sip-command-timeout', 'PUT', '*', '', ''),
(7733, 'p', 'role_1', '/api/gb28181/sip/service-config/preallocation-mode', 'PUT', '*', '', ''),
(7737, 'p', 'role_1', '/api/gb28181/security/snapshot', 'GET', '*', '', ''),
(7738, 'p', 'role_1', '/api/gb28181/security/events', 'GET', '*', '', ''),
(7739, 'p', 'role_1', '/api/gb28181/security/bans', 'GET', '*', '', ''),
(7740, 'p', 'role_1', '/api/gb28181/security/bans/:id/unban', 'POST', '*', '', ''),
(7741, 'p', 'role_1', '/api/gb28181/security/policy', 'GET', '*', '', ''),
(7742, 'p', 'role_1', '/api/gb28181/security/policy', 'PUT', '*', '', ''),
(7743, 'p', 'role_1', '/api/gb28181/security/agent/health', 'GET', '*', '', ''),
(7744, 'p', 'role_1', '/api/gb28181/security/stream', 'GET', '*', '', ''),
(7752, 'p', 'role_1', '/api/gb28181/security/access-rules', 'GET', '*', '', ''),
(7753, 'p', 'role_1', '/api/gb28181/security/access-rules', 'POST', '*', '', ''),
(7754, 'p', 'role_1', '/api/gb28181/security/access-rules/:id', 'PUT', '*', '', ''),
(7755, 'p', 'role_1', '/api/gb28181/security/access-rules/:id', 'DELETE', '*', '', ''),
(7759, 'p', 'role_1', '/api/gb28181/sip/service-config/global-subscriptions', 'GET', '*', '', ''),
(7760, 'p', 'role_1', '/api/gb28181/sip/service-config/default-channel-audio', 'GET', '*', '', ''),
(7761, 'p', 'role_1', '/api/gb28181/sip/service-config/global-subscriptions', 'PUT', '*', '', ''),
(7762, 'p', 'role_1', '/api/gb28181/sip/service-config/default-channel-audio', 'PUT', '*', '', ''),
(7766, 'p', 'role_1', '/api/gb28181/sip/service-config/default-playback-protocol', 'GET', '*', '', ''),
(7767, 'p', 'role_1', '/api/gb28181/sip/service-config/default-playback-protocol', 'PUT', '*', '', ''),
(7769, 'p', 'role_1', '/api/sysDictItem/getByDictCode/:dictCode', 'GET', '*', '', ''),
(7770, 'p', 'role_1', '/api/gb28181/sip/service-config/fixed-address-playback', 'GET', '*', '', ''),
(7771, 'p', 'role_1', '/api/gb28181/sip/service-config/fixed-address-playback', 'PUT', '*', '', ''),
(7773, 'p', 'role_1', '/api/gb28181/cloud-recordings/active', 'GET', '*', '', ''),
(7774, 'p', 'role_1', '/api/gb28181/cloud-recordings/files/:id/access', 'POST', '*', '', ''),
(7775, 'p', 'role_1', '/api/gb28181/cloud-recordings/files/:id', 'GET', '*', '', ''),
(7776, 'p', 'role_1', '/api/gb28181/cloud-recordings/files/options', 'GET', '*', '', ''),
(7777, 'p', 'role_1', '/api/gb28181/cloud-recordings/files', 'GET', '*', '', ''),
(7780, 'p', 'role_1', '/api/gb28181/cloud-recordings/reconciliations', 'GET', '*', '', ''),
(7781, 'p', 'role_1', '/api/gb28181/cloud-recordings/reconciliations', 'POST', '*', '', ''),
(7783, 'p', 'role_1', '/api/gb28181/cascade/platforms', 'GET', '*', '', ''),
(7784, 'p', 'role_1', '/api/gb28181/cascade/platforms', 'POST', '*', '', ''),
(7785, 'p', 'role_1', '/api/gb28181/cascade/platforms/:id', 'GET', '*', '', ''),
(7786, 'p', 'role_1', '/api/gb28181/cascade/platforms/:id', 'PUT', '*', '', ''),
(7787, 'p', 'role_1', '/api/gb28181/cascade/platforms/:id', 'DELETE', '*', '', ''),
(7788, 'p', 'role_1', '/api/gb28181/cascade/platforms/:id/enable', 'POST', '*', '', ''),
(7789, 'p', 'role_1', '/api/gb28181/cascade/platforms/:id/disable', 'POST', '*', '', ''),
(7790, 'p', 'role_1', '/api/gb28181/cascade/platforms/:id/enabled', 'PUT', '*', '', ''),
(7791, 'p', 'role_1', '/api/gb28181/cascade/platforms/:id/reconnect', 'POST', '*', '', ''),
(7792, 'p', 'role_1', '/api/gb28181/cascade/platforms/:id/shares', 'GET', '*', '', ''),
(7793, 'p', 'role_1', '/api/gb28181/cascade/platforms/:id/shares', 'PUT', '*', '', ''),
(7798, 'p', 'role_1', '/api/gb28181/sip/service-config/play-auth', 'GET', '*', '', ''),
(7799, 'p', 'role_1', '/api/gb28181/sip/service-config/play-auth', 'PUT', '*', '', ''),
(7801, 'p', 'role_1', '/api/gb28181/play/:deviceId/:channelId/authorization', 'POST', '*', '', ''),
(7802, 'p', 'role_1', '/api/gb28181/play/:deviceId/:channelId', 'POST', '*', '', ''),
(7803, 'p', 'role_1', '/api/gb28181/cloud-recordings/files/:id/downloads', 'POST', '*', '', ''),
(7804, 'p', 'role_1', '/api/gb28181/cloud-recordings/downloads/:taskId', 'GET', '*', '', ''),
(7805, 'p', 'role_1', '/api/gb28181/cloud-recordings/downloads/:taskId', 'DELETE', '*', '', '');
INSERT INTO "sys_dict" ("id", "name", "code", "status", "description", "created_at", "updated_at", "deleted_at", "created_by") VALUES
(1, '性别', 'gender', 1, '这是一个性别字典', '2024-07-01 10:00:00', NULL, NULL, 1),
(2, '状态', 'status', 1, '状态字段可以用这个', '2024-07-01 10:00:00', NULL, NULL, 1),
(3, '岗位', 'post', 1, '岗位字段', '2024-07-01 10:00:00', NULL, NULL, 1),
(4, '任务状态', 'taskStatus', 1, '任务状态字段可以用它', '2024-07-01 10:00:00', NULL, NULL, 1),
(5, '摄像头类型', 'ptz_type', 1, 'GB28181 国标摄像头云台类型(PTZType)', '2026-07-18 18:31:33', '2026-07-18 18:31:33', NULL, 1),
(6, 'GB28181 默认播放协议', 'gb28181_playback_protocol', 1, '国标播放入口支持的默认协议选项', '2026-08-10 18:56:34', '2026-08-10 19:00:25', NULL, 1);
INSERT INTO "sys_dict_item" ("id", "name", "value", "status", "dict_id") VALUES
(11, '男', '1', 1, 1),
(12, '女', '0', 1, 1),
(13, '其它', '2', 1, 1),
(21, '禁用', '0', 1, 2),
(22, '启用', '1', 1, 2),
(31, '总经理', '1', 1, 3),
(32, '总监', '2', 1, 3),
(33, '人事主管', '3', 1, 3),
(34, '开发部主管', '4', 1, 3),
(35, '普通职员', '5', 1, 3),
(36, '其它', '999', 1, 3),
(41, '失败', '0', 1, 4),
(42, '成功', '1', 1, 4),
(43, '未知', '0', 1, 5),
(44, '球机', '1', 1, 5),
(45, '半球', '2', 1, 5),
(46, '固定枪机', '3', 1, 5),
(47, '遥控枪机', '4', 1, 5),
(48, 'WS-FLV', 'ws-flv', 1, 6),
(49, 'HTTP-FLV', 'http-flv', 1, 6),
(50, 'HLS', 'hls', 1, 6),
(51, 'WebRTC', 'webrtc', 1, 6);
INSERT INTO "gb_sip_security_policy" ("id", "scope_key", "mode", "window_seconds", "ban_score", "max_packet_bytes", "max_udp_per_window", "max_tcp_connections", "sample_per_source", "nonce_ttl_seconds", "ban_ttl_steps", "allowlist_text", "updated_by", "updated_at") VALUES
(1, 'global', 'protect', 10, 100, 65536, 120, 32, 3, 60, '100:0', '127.0.0.0/8\n10.0.0.0/8\n172.16.0.0/12\n192.168.0.0/16', 0, '2026-08-18 17:00:00');
INSERT INTO "sys_api" ("id","title","path","method","api_group","created_at","updated_at","deleted_at","created_by") VALUES
(339,'查询在线用户','/api/sysOnlineUser/list','GET','系统管理','2026-09-07 00:00:00','2026-09-07 00:00:00',NULL,1),
(340,'强制下线会话','/api/sysOnlineUser/forceLogout','POST','系统管理','2026-09-07 00:00:00','2026-09-07 00:00:00',NULL,1);
INSERT INTO "sys_menu" ("id","parent_id","path","name","component","title","hide","disable","sort","type","permission","icon","created_at","updated_at","created_by") VALUES
(140382,10,'/system/online-user','SystemOnlineUser','system/online-user/index','在线用户',0,0,8,2,'system:online-user:list','lucide:UsersRound','2026-09-07 00:00:00','2026-09-07 00:00:00',1),
(140383,140382,'','SystemOnlineUserForceLogout','','强制下线',1,0,1,3,'system:online-user:force-logout','','2026-09-07 00:00:00','2026-09-07 00:00:00',1);
INSERT INTO "sys_role_menu" ("role_id","menu_id") VALUES (1,140382),(1,140383);
INSERT INTO "sys_menu_api" ("menu_id","api_id") VALUES (140382,339),(140383,340);
INSERT INTO "sys_casbin_rule" ("id","ptype","v0","v1","v2","v3","v4","v5") VALUES
(7806,'p','role_1','/api/sysOnlineUser/list','GET','*','',''),
(7807,'p','role_1','/api/sysOnlineUser/forceLogout','POST','*','','');
INSERT INTO "sys_api" ("id","title","path","method","api_group","created_at","updated_at","deleted_at","created_by") VALUES
(341,'登录日志列表','/api/sysLoginLog/list','GET','日志管理','2026-09-07 00:00:00','2026-09-07 00:00:00',NULL,1),(342,'登录日志详情','/api/sysLoginLog/:id','GET','日志管理','2026-09-07 00:00:00','2026-09-07 00:00:00',NULL,1),(343,'删除登录日志','/api/sysLoginLog/delete','DELETE','日志管理','2026-09-07 00:00:00','2026-09-07 00:00:00',NULL,1),(344,'清空登录日志','/api/sysLoginLog/clear','POST','日志管理','2026-09-07 00:00:00','2026-09-07 00:00:00',NULL,1),(345,'解锁登录账号','/api/sysLoginLog/unlock','POST','日志管理','2026-09-07 00:00:00','2026-09-07 00:00:00',NULL,1);
INSERT INTO "sys_menu" ("id","parent_id","path","name","component","title","hide","disable","sort","type","permission","icon","created_at","updated_at","created_by") VALUES
(140384,10,'/system/login-log','SystemLoginLog','system/login-log/index','登录日志',0,0,1,2,'system:login-log:list','lucide:FileClock','2026-09-07 00:00:00','2026-09-07 00:00:00',1);
INSERT INTO "sys_menu" ("id","parent_id","path","name","component","title","hide","disable","sort","type","permission","icon","created_at","updated_at","created_by") VALUES
(140385,140384,'','SystemLoginLogDelete','','删除登录日志',1,0,1,3,'system:login-log:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1),(140386,140384,'','SystemLoginLogClear','','清空登录日志',1,0,2,3,'system:login-log:clear','','2026-09-07 00:00:00','2026-09-07 00:00:00',1),(140387,140384,'','SystemLoginLogUnlock','','解锁登录账号',1,0,3,3,'system:login-log:unlock','','2026-09-07 00:00:00','2026-09-07 00:00:00',1);
INSERT INTO "sys_role_menu" ("role_id","menu_id") VALUES (1,140384),(1,140385),(1,140386),(1,140387);
INSERT INTO "sys_menu_api" ("menu_id","api_id") VALUES (140384,341),(140384,342),(140385,343),(140386,344),(140387,345);
INSERT INTO "sys_casbin_rule" ("id","ptype","v0","v1","v2","v3","v4","v5") VALUES
(7808,'p','role_1','/api/sysLoginLog/list','GET','*','',''),(7809,'p','role_1','/api/sysLoginLog/:id','GET','*','',''),(7810,'p','role_1','/api/sysLoginLog/delete','DELETE','*','',''),(7811,'p','role_1','/api/sysLoginLog/clear','POST','*','',''),(7812,'p','role_1','/api/sysLoginLog/unlock','POST','*','','');
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","is_full","hide","disable","keep_alive","affix","is_link","link","iframe","svg_icon","icon","sort","type","permission","created_by","created_at","updated_at")
SELECT 0,'/gb28181/device-assignment','device-assignment','','gb28181/device-assignment/index','设备分配',0,0,0,0,0,0,'',0,'','lucide:KeyRound',9,2,'',1,'2026-09-07 00:00:00','2026-09-07 00:00:00'
WHERE NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "name"='device-assignment' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","is_full","hide","disable","keep_alive","affix","is_link","link","iframe","svg_icon","icon","sort","type","permission","created_by","created_at","updated_at")
SELECT m."id",'','device-assignment-assign','','','分配设备归属',0,0,0,0,0,0,'',0,'','',1,3,'gb28181:device:assign',1,'2026-09-07 00:00:00','2026-09-07 00:00:00'
FROM "sys_menu" m WHERE m."name"='device-assignment' AND m."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" x WHERE x."name"='device-assignment-assign' AND x."deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","is_full","hide","disable","keep_alive","affix","is_link","link","iframe","svg_icon","icon","sort","type","permission","created_by","created_at","updated_at")
SELECT m."id",'','device-assignment-share','','','共享设备',0,0,0,0,0,0,'',0,'','',2,3,'gb28181:device:share',1,'2026-09-07 00:00:00','2026-09-07 00:00:00'
FROM "sys_menu" m WHERE m."name"='device-assignment' AND m."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" x WHERE x."name"='device-assignment-share' AND x."deleted_at" IS NULL);
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT rm."role_id", m."id"
FROM "sys_role_menu" rm
JOIN "sys_menu" src ON src."id"=rm."menu_id" AND src."name"='device-mgmt-list' AND src."deleted_at" IS NULL
JOIN "sys_menu" m ON m."name" IN ('device-assignment','device-assignment-assign','device-assignment-share') AND m."deleted_at" IS NULL
WHERE NOT EXISTS (SELECT 1 FROM "sys_role_menu" x WHERE x."role_id"=rm."role_id" AND x."menu_id"=m."id");
UPDATE "sys_menu"
SET "title"='设备权限工作台', "updated_at"='2026-09-07 00:00:00'
WHERE "name"='device-assignment' AND "deleted_at" IS NULL;
UPDATE "sys_api"
SET "deleted_at"='2026-09-07 00:00:00', "updated_at"='2026-09-07 00:00:00'
WHERE "deleted_at" IS NULL
  AND "path" IN ('/api/gb28181/device-mgmt/assign',
                 '/api/gb28181/device-mgmt/assign-dept',
                 '/api/gb28181/device-mgmt/device/:id/grants',
                 '/api/gb28181/device-mgmt/device/:id/grants/:grantId');
DELETE FROM "sys_menu_api" AS ma WHERE EXISTS (SELECT 1 FROM "sys_api" a WHERE a."id"=ma."api_id" AND a."path" IN ('/api/gb28181/device-mgmt/assign',
                   '/api/gb28181/device-mgmt/assign-dept',
                   '/api/gb28181/device-mgmt/device/:id/grants',
                   '/api/gb28181/device-mgmt/device/:id/grants/:grantId'));
DELETE FROM "sys_casbin_rule"
WHERE "v1" IN ('/api/gb28181/device-mgmt/assign',
               '/api/gb28181/device-mgmt/assign-dept',
               '/api/gb28181/device-mgmt/device/:id/grants',
               '/api/gb28181/device-mgmt/device/:id/grants/:grantId');
UPDATE "sys_api" SET "deleted_at"=NULL, "title"='查询设备权限汇总', "api_group"='GB28181设备管理', "updated_at"='2026-09-07 00:00:00'
WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/summary' AND "method"='GET';
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","created_by")
SELECT '查询设备权限汇总','/api/gb28181/device-mgmt/permission-workbench/summary','GET','GB28181设备管理','2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE NOT EXISTS (SELECT 1 FROM "sys_api" WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/summary' AND "method"='GET' AND "deleted_at" IS NULL);
UPDATE "sys_api" SET "deleted_at"=NULL, "title"='解析工作台设备', "api_group"='GB28181设备管理', "updated_at"='2026-09-07 00:00:00'
WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND "method"='POST';
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","created_by")
SELECT '解析工作台设备','/api/gb28181/device-mgmt/permission-workbench/devices/resolve','POST','GB28181设备管理','2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE NOT EXISTS (SELECT 1 FROM "sys_api" WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND "method"='POST' AND "deleted_at" IS NULL);
UPDATE "sys_api" SET "deleted_at"=NULL, "title"='查询设备共享授权', "api_group"='GB28181设备管理', "updated_at"='2026-09-07 00:00:00'
WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND "method"='POST';
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","created_by")
SELECT '查询设备共享授权','/api/gb28181/device-mgmt/permission-workbench/grants/query','POST','GB28181设备管理','2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE NOT EXISTS (SELECT 1 FROM "sys_api" WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND "method"='POST' AND "deleted_at" IS NULL);
UPDATE "sys_api" SET "deleted_at"=NULL, "title"='查询共享目标', "api_group"='GB28181设备管理', "updated_at"='2026-09-07 00:00:00'
WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND "method"='GET';
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","created_by")
SELECT '查询共享目标','/api/gb28181/device-mgmt/permission-workbench/grant-targets','GET','GB28181设备管理','2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE NOT EXISTS (SELECT 1 FROM "sys_api" WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND "method"='GET' AND "deleted_at" IS NULL);
UPDATE "sys_api" SET "deleted_at"=NULL, "title"='调整设备归属', "api_group"='GB28181设备管理', "updated_at"='2026-09-07 00:00:00'
WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/assignments' AND "method"='POST';
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","created_by")
SELECT '调整设备归属','/api/gb28181/device-mgmt/permission-workbench/assignments','POST','GB28181设备管理','2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE NOT EXISTS (SELECT 1 FROM "sys_api" WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/assignments' AND "method"='POST' AND "deleted_at" IS NULL);
UPDATE "sys_api" SET "deleted_at"=NULL, "title"='整部门调整设备归属', "api_group"='GB28181设备管理', "updated_at"='2026-09-07 00:00:00'
WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND "method"='POST';
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","created_by")
SELECT '整部门调整设备归属','/api/gb28181/device-mgmt/permission-workbench/assignments/departments','POST','GB28181设备管理','2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE NOT EXISTS (SELECT 1 FROM "sys_api" WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND "method"='POST' AND "deleted_at" IS NULL);
UPDATE "sys_api" SET "deleted_at"=NULL, "title"='应用设备共享授权', "api_group"='GB28181设备管理', "updated_at"='2026-09-07 00:00:00'
WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND "method"='POST';
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","created_by")
SELECT '应用设备共享授权','/api/gb28181/device-mgmt/permission-workbench/grants/apply','POST','GB28181设备管理','2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE NOT EXISTS (SELECT 1 FROM "sys_api" WHERE "path"='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND "method"='POST' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT m."id",a."id" FROM "sys_menu" m JOIN "sys_api" a
ON 1=1
WHERE m."name"='device-assignment' AND m."deleted_at" IS NULL AND a."deleted_at" IS NULL
  AND ((a."path"='/api/gb28181/device-mgmt/permission-workbench/summary' AND a."method"='GET')
    OR (a."path"='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND a."method"='POST')
    OR (a."path"='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND a."method"='POST')
    OR (a."path"='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND a."method"='GET'))
  AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" x WHERE x."menu_id"=m."id" AND x."api_id"=a."id");
INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT m."id",a."id" FROM "sys_menu" m JOIN "sys_api" a
ON 1=1
WHERE m."permission"='gb28181:device:assign' AND m."deleted_at" IS NULL AND a."deleted_at" IS NULL
  AND a."path" IN ('/api/gb28181/device-mgmt/permission-workbench/assignments','/api/gb28181/device-mgmt/permission-workbench/assignments/departments')
  AND a."method"='POST'
  AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" x WHERE x."menu_id"=m."id" AND x."api_id"=a."id");
INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT m."id",a."id" FROM "sys_menu" m JOIN "sys_api" a
ON 1=1
WHERE m."permission"='gb28181:device:share' AND m."deleted_at" IS NULL AND a."deleted_at" IS NULL
  AND a."path"='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND a."method"='POST'
  AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" x WHERE x."menu_id"=m."id" AND x."api_id"=a."id");
INSERT INTO "sys_casbin_rule" ("ptype","v0","v1","v2","v3","v4","v5")
SELECT DISTINCT 'p',('role_' || rm."role_id"),a."path",a."method",'*','',''
FROM "sys_role_menu" rm
JOIN "sys_menu" m ON m."id"=rm."menu_id"
JOIN "sys_menu_api" ma ON ma."menu_id"=m."id"
JOIN "sys_api" a ON a."id"=ma."api_id"
WHERE m."name" IN ('device-assignment','device-assignment-assign','device-assignment-share')
  AND m."deleted_at" IS NULL AND a."deleted_at" IS NULL
  AND a."path" LIKE '/api/gb28181/device-mgmt/permission-workbench/%'
  AND NOT EXISTS (SELECT 1 FROM "sys_casbin_rule" c WHERE c."ptype"='p' AND c."v0"=('role_' || rm."role_id") AND c."v1"=a."path" AND c."v2"=a."method" AND c."v3"='*');
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/gb28181/cloud-recordings' AND "deleted_at" IS NULL)),'','','','删除录像文件',3,'gb28181:recording:delete',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/gb28181/cloud-recordings' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:recording:delete' AND "deleted_at" IS NULL);
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","created_by")
SELECT v.title,v.path,v.method,'GB28181 云端录像删除','2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM (
  SELECT '删除单个云端录像' title,'/api/gb28181/cloud-recordings/files/:id' path,'DELETE' method
  UNION ALL SELECT '批量删除云端录像','/api/gb28181/cloud-recordings/files/batch-delete','POST'
) v WHERE NOT EXISTS (SELECT 1 FROM "sys_api" a WHERE a."path"=v.path AND a."method"=v.method AND a."deleted_at" IS NULL);
INSERT INTO "sys_role_menu" ("role_id","menu_id") SELECT 1,((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:recording:delete' AND "deleted_at" IS NULL)) WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:recording:delete' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" WHERE "role_id"=1 AND "menu_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:recording:delete' AND "deleted_at" IS NULL)));
INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:recording:delete' AND "deleted_at" IS NULL)),a."id" FROM "sys_api" a WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:recording:delete' AND "deleted_at" IS NULL)) IS NOT NULL AND ((a."path"='/api/gb28181/cloud-recordings/files/:id' AND a."method"='DELETE') OR (a."path"='/api/gb28181/cloud-recordings/files/batch-delete' AND a."method"='POST')) AND a."deleted_at" IS NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" x WHERE x."menu_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:recording:delete' AND "deleted_at" IS NULL)) AND x."api_id"=a."id");
INSERT INTO "sys_casbin_rule" ("ptype","v0","v1","v2","v3","v4","v5")
SELECT 'p','role_1',a."path",a."method",'*','','' FROM "sys_api" a WHERE ((a."path"='/api/gb28181/cloud-recordings/files/:id' AND a."method"='DELETE') OR (a."path"='/api/gb28181/cloud-recordings/files/batch-delete' AND a."method"='POST')) AND a."deleted_at" IS NULL AND NOT EXISTS (SELECT 1 FROM "sys_casbin_rule" c WHERE c."ptype"='p' AND c."v0"='role_1' AND c."v1"=a."path" AND c."v2"=a."method" AND c."v3"='*');
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT m."id",'','','','停止录像',3,'gb28181:recording:stop',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" m WHERE m."path"='/gb28181/cloud-recordings' AND m."deleted_at" IS NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:recording:stop' AND "deleted_at" IS NULL);
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","created_by")
SELECT '停止云端录像','/api/gb28181/cloud-recordings/active/:id/stop','POST','GB28181 云端录像控制','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS (SELECT 1 FROM "sys_api" WHERE "path"='/api/gb28181/cloud-recordings/active/:id/stop' AND "method"='POST' AND "deleted_at" IS NULL);
INSERT INTO "sys_role_menu" ("role_id","menu_id") SELECT 1,m."id" FROM "sys_menu" m WHERE m."permission"='gb28181:recording:stop' AND m."deleted_at" IS NULL AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" x WHERE x."role_id"=1 AND x."menu_id"=m."id");
INSERT INTO "sys_menu_api" ("menu_id","api_id") SELECT m."id",a."id" FROM "sys_menu" m CROSS JOIN "sys_api" a WHERE m."permission"='gb28181:recording:stop' AND m."deleted_at" IS NULL AND a."path"='/api/gb28181/cloud-recordings/active/:id/stop' AND a."method"='POST' AND a."deleted_at" IS NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" x WHERE x."menu_id"=m."id" AND x."api_id"=a."id");
INSERT INTO "sys_casbin_rule" ("ptype","v0","v1","v2","v3","v4","v5") SELECT 'p','role_1',a."path",a."method",'*','','' FROM "sys_api" a WHERE a."path"='/api/gb28181/cloud-recordings/active/:id/stop' AND a."method"='POST' AND a."deleted_at" IS NULL AND NOT EXISTS (SELECT 1 FROM "sys_casbin_rule" c WHERE c."ptype"='p' AND c."v0"='role_1' AND c."v1"=a."path" AND c."v2"=a."method" AND c."v3"='*');
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","icon","sort","created_at","updated_at","created_by")
SELECT 0,'/gb28181/recording-schedules','gb28181-recording-schedules','gb28181/recording-schedules/index','录像计划',2,'gb28181:recording-plan:view','lucide:CalendarClock',34,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:recording-plan:view' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:recording-plan:view' AND "deleted_at" IS NULL)),'','','','维护录像计划',3,'gb28181:recording-plan:maintain',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:recording-plan:view' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:recording-plan:maintain' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:recording-plan:view' AND "deleted_at" IS NULL)),'','','','分配录像计划',3,'gb28181:recording-plan:assign',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:recording-plan:view' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:recording-plan:assign' AND "deleted_at" IS NULL);
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT 1,m."id" FROM "sys_menu" m WHERE m."permission" IN ('gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign') AND m."deleted_at" IS NULL AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" x WHERE x."role_id"=1 AND x."menu_id"=m."id");
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","created_by")
SELECT s.title,s.path,s.method,'GB28181 录像计划','2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM (
 SELECT '查询录像计划' title,'/api/gb28181/recording-plans' path,'GET' method UNION ALL SELECT '新建录像计划','/api/gb28181/recording-plans','POST'
 UNION ALL SELECT '查看录像计划','/api/gb28181/recording-plans/:id','GET' UNION ALL SELECT '编辑录像计划','/api/gb28181/recording-plans/:id','PUT'
 UNION ALL SELECT '删除录像计划','/api/gb28181/recording-plans/:id','DELETE' UNION ALL SELECT '启停录像计划','/api/gb28181/recording-plans/:id/status','PATCH'
 UNION ALL SELECT '搜索分配设备','/api/gb28181/recording-plans/:id/assignment-options/devices','GET' UNION ALL SELECT '搜索分配通道','/api/gb28181/recording-plans/:id/assignment-options/channels','GET'
 UNION ALL SELECT '分配录像计划','/api/gb28181/recording-plans/:id/assignments','POST' UNION ALL SELECT '切换通道录像模式','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH'
 UNION ALL SELECT '查询计划通道状态','/api/gb28181/recording-plans/:id/channels','GET' UNION ALL SELECT '诊断通道录像','/api/gb28181/recording-plans/channels/:channelId/diagnosis','GET'
 UNION ALL SELECT '查询通道执行时间线','/api/gb28181/recording-plans/channels/:channelId/timeline','GET'
) s WHERE NOT EXISTS (SELECT 1 FROM "sys_api" a WHERE a."path"=s.path AND a."method"=s.method AND a."deleted_at" IS NULL);
INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT m."id",a."id" FROM "sys_menu" m CROSS JOIN "sys_api" a WHERE m."permission"='gb28181:recording-plan:view' AND m."deleted_at" IS NULL AND a."deleted_at" IS NULL AND a."method"='GET' AND a."path" IN ('/api/gb28181/recording-plans','/api/gb28181/recording-plans/:id','/api/gb28181/recording-plans/:id/channels','/api/gb28181/recording-plans/channels/:channelId/diagnosis','/api/gb28181/recording-plans/channels/:channelId/timeline') AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" x WHERE x."menu_id"=m."id" AND x."api_id"=a."id");
INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT m."id",a."id" FROM "sys_menu" m CROSS JOIN "sys_api" a WHERE m."permission"='gb28181:recording-plan:maintain' AND m."deleted_at" IS NULL AND a."deleted_at" IS NULL AND ((a."path"='/api/gb28181/recording-plans' AND a."method"='POST') OR (a."path"='/api/gb28181/recording-plans/:id' AND a."method" IN ('PUT','DELETE')) OR (a."path"='/api/gb28181/recording-plans/:id/status' AND a."method"='PATCH')) AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" x WHERE x."menu_id"=m."id" AND x."api_id"=a."id");
INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT m."id",a."id" FROM "sys_menu" m CROSS JOIN "sys_api" a WHERE m."permission"='gb28181:recording-plan:assign' AND m."deleted_at" IS NULL AND a."deleted_at" IS NULL AND a."path" IN ('/api/gb28181/recording-plans/:id/assignment-options/devices','/api/gb28181/recording-plans/:id/assignment-options/channels','/api/gb28181/recording-plans/:id/assignments','/api/gb28181/recording-plans/channels/:channelId/recording-mode') AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" x WHERE x."menu_id"=m."id" AND x."api_id"=a."id");
INSERT INTO "sys_casbin_rule" ("ptype","v0","v1","v2","v3","v4","v5")
SELECT DISTINCT 'p',('role_' || rm."role_id"),a."path",a."method",'*','','' FROM "sys_role_menu" rm JOIN "sys_menu" m ON m."id"=rm."menu_id" JOIN "sys_menu_api" ma ON ma."menu_id"=m."id" JOIN "sys_api" a ON a."id"=ma."api_id" WHERE m."permission" IN ('gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign') AND m."deleted_at" IS NULL AND a."deleted_at" IS NULL AND NOT EXISTS (SELECT 1 FROM "sys_casbin_rule" c WHERE c."ptype"='p' AND c."v0"=('role_' || rm."role_id") AND c."v1"=a."path" AND c."v2"=a."method" AND c."v3"='*');
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","keep_alive","created_at","updated_at","created_by")
SELECT 0,'/media','Media','/gb28181/zlm/overview','','流媒体管理','lucide:Clapperboard',9,1,'',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "redirect"='/gb28181/zlm/overview',"title"='流媒体管理',"icon"='lucide:Clapperboard',"sort"=9,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/media' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='集群概览',"icon"='lucide:LayoutDashboard',"sort"=10,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/overview' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/zlm/overview','gb28181-zlm-overview','','gb28181/zlm/ClusterOverview','集群概览','lucide:LayoutDashboard',10,2,'',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/zlm/overview' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='节点管理',"icon"='lucide:Server',"sort"=11,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/nodes' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/zlm/nodes','gb28181-zlm-nodes','','gb28181/zlm/NodeList','节点管理','lucide:Server',11,2,'',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/zlm/nodes' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='调度策略',"icon"='lucide:Workflow',"sort"=12,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/scheduler' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/zlm/scheduler','gb28181-zlm-scheduler-strategy','','gb28181/zlm/SchedulerStrategy','调度策略','lucide:Workflow',12,2,'',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/zlm/scheduler' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='调度日志',"icon"='lucide:History',"sort"=13,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/scheduler/logs' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/zlm/scheduler/logs','gb28181-zlm-scheduler-log','','gb28181/zlm/SchedulerLog','调度日志','lucide:History',13,2,'',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/zlm/scheduler/logs' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='运行监控',"icon"='lucide:Activity',"sort"=20,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/runtime' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/zlm/runtime','gb28181-zlm-runtime','','gb28181/zlm/RuntimeOverview','运行监控','lucide:Activity',20,2,'',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/zlm/runtime' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='流媒体',"icon"='lucide:RadioTower',"sort"=21,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/streams' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/zlm/streams','gb28181-zlm-streams','','gb28181/zlm/StreamManagement','流媒体','lucide:RadioTower',21,2,'',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/zlm/streams' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='会话管理',"icon"='lucide:Users',"sort"=22,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/sessions' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/zlm/sessions','gb28181-zlm-sessions','','gb28181/zlm/SessionManagement','会话管理','lucide:Users',22,2,'',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/zlm/sessions' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='拉流代理',"icon"='lucide:Network',"sort"=30,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/proxies' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/zlm/proxies','gb28181-zlm-proxies','','gb28181/zlm/ProxyManagement','拉流代理','lucide:Network',30,2,'',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/zlm/proxies' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='FFmpeg 源',"icon"='lucide:Clapperboard',"sort"=31,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/ffmpeg-sources' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/zlm/ffmpeg-sources','gb28181-zlm-ffmpeg-sources','','gb28181/zlm/FFmpegSources','FFmpeg 源','lucide:Clapperboard',31,2,'',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/zlm/ffmpeg-sources' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='RTP 服务',"icon"='lucide:Waypoints',"sort"=32,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/rtp-servers' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/zlm/rtp-servers','gb28181-zlm-rtp-servers','','gb28181/zlm/RTPServices','RTP 服务','lucide:Waypoints',32,2,'',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/zlm/rtp-servers' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='录制管理',"icon"='lucide:Cloud',"sort"=40,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/cloud-recordings' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/cloud-recordings','gb28181-cloud-recordings','','gb28181/cloud-recordings/index','录制管理','lucide:Cloud',40,2,'gb28181:recording:view',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/cloud-recordings' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='录像计划',"icon"='lucide:CalendarClock',"sort"=41,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/recording-schedules' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/recording-schedules','gb28181-recording-schedules','','gb28181/recording-schedules/index','录像计划','lucide:CalendarClock',41,2,'gb28181:recording-plan:view',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/recording-schedules' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"redirect"='',"title"='服务配置',"icon"='lucide:Settings2',"sort"=42,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/config' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),'/gb28181/zlm/config','gb28181-zlm-config','','gb28181/zlm/ServerConfig','服务配置','lucide:Settings2',42,2,'',0,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/gb28181/zlm/config' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','管理节点',3,'gb28181:zlm:node:manage',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/nodes' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:node:manage' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','踢除节点会话',3,'gb28181:zlm:node:kick',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/nodes' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:node:kick' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','切换调度策略',3,'gb28181:zlm:scheduler:manage',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/scheduler' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:scheduler:manage' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','预览与截图',3,'gb28181:zlm:stream:preview',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/streams' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:stream:preview' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','关闭流',3,'gb28181:zlm:stream:close',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/streams' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:stream:close' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','强制关闭流',3,'gb28181:zlm:stream:force-close',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/streams' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:stream:force-close' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','踢除会话',3,'gb28181:zlm:session:kick',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/sessions' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:session:kick' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','管理代理',3,'gb28181:zlm:proxy:manage',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/proxies' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:proxy:manage' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','管理 FFmpeg 源',3,'gb28181:zlm:ffmpeg:manage',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/ffmpeg-sources' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:ffmpeg:manage' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','管理 RTP 服务',3,'gb28181:zlm:rtp:manage',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/rtp-servers' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:rtp:manage' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','强制关闭 RTP 服务',3,'gb28181:zlm:rtp:force-close',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/rtp-servers' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:rtp:force-close' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','手工录制控制',3,'gb28181:recording:control',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/cloud-recordings' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:recording:control' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','强制停止录制',3,'gb28181:recording:force-stop',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/cloud-recordings' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:recording:force-stop' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','更新服务配置',3,'gb28181:zlm:config:update',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/config' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:config:update' AND "deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT p."id",'','','','重启媒体服务',3,'gb28181:zlm:restart',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/gb28181/zlm/config' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:zlm:restart' AND "deleted_at" IS NULL);
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT 1,m."id" FROM "sys_menu" m
WHERE m."deleted_at" IS NULL AND (m."path" IN ('/media','/gb28181/zlm/overview','/gb28181/zlm/nodes','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/config')
OR m."permission" IN ('gb28181:zlm:node:manage','gb28181:zlm:node:kick','gb28181:zlm:scheduler:manage','gb28181:zlm:stream:preview','gb28181:zlm:stream:close','gb28181:zlm:stream:force-close','gb28181:zlm:session:kick','gb28181:zlm:proxy:manage','gb28181:zlm:ffmpeg:manage','gb28181:zlm:rtp:manage','gb28181:zlm:rtp:force-close','gb28181:recording:control','gb28181:recording:force-stop','gb28181:zlm:config:update','gb28181:zlm:restart','gb28181:recording:view','gb28181:recording:reconcile','gb28181:recording:delete','gb28181:recording:stop','gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign'))
AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" x WHERE x."role_id"=1 AND x."menu_id"=m."id");
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","created_by")
SELECT v."title",v."path",v."method",'GB28181 媒体管理','2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM (
  SELECT '媒体管理 GET zlm/overview' title,'/api/gb28181/zlm/overview' path,'GET' method
  UNION ALL SELECT '媒体管理 GET zlm/nodes','/api/gb28181/zlm/nodes','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id','/api/gb28181/zlm/nodes/:id','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/config','/api/gb28181/zlm/nodes/:id/config','GET'
  UNION ALL SELECT '媒体管理 GET zlm/scheduler','/api/gb28181/zlm/scheduler','GET'
  UNION ALL SELECT '媒体管理 GET zlm/scheduler/logs','/api/gb28181/zlm/scheduler/logs','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/runtime','/api/gb28181/zlm/nodes/:id/runtime','GET'
  UNION ALL SELECT '媒体管理 GET zlm/streams','/api/gb28181/zlm/streams','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/streams','/api/gb28181/zlm/nodes/:id/streams','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/streams/detail','/api/gb28181/zlm/nodes/:id/streams/detail','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/streams/viewers','/api/gb28181/zlm/nodes/:id/streams/viewers','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/sessions/network','/api/gb28181/zlm/nodes/:id/sessions/network','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/sessions/viewers','/api/gb28181/zlm/nodes/:id/sessions/viewers','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/proxies/pull','/api/gb28181/zlm/nodes/:id/proxies/pull','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/proxies/pull/:key','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/proxies/push','/api/gb28181/zlm/nodes/:id/proxies/push','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/proxies/push/:key','/api/gb28181/zlm/nodes/:id/proxies/push/:key','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/ffmpeg-sources','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/rtp-servers','/api/gb28181/zlm/nodes/:id/rtp-servers','GET'
  UNION ALL SELECT '媒体管理 GET cloud-recordings/files','/api/gb28181/cloud-recordings/files','GET'
  UNION ALL SELECT '媒体管理 GET cloud-recordings/files/options','/api/gb28181/cloud-recordings/files/options','GET'
  UNION ALL SELECT '媒体管理 GET cloud-recordings/files/:id','/api/gb28181/cloud-recordings/files/:id','GET'
  UNION ALL SELECT '媒体管理 POST cloud-recordings/files/:id/access','/api/gb28181/cloud-recordings/files/:id/access','POST'
  UNION ALL SELECT '媒体管理 POST cloud-recordings/files/:id/downloads','/api/gb28181/cloud-recordings/files/:id/downloads','POST'
  UNION ALL SELECT '媒体管理 GET cloud-recordings/downloads/:taskId','/api/gb28181/cloud-recordings/downloads/:taskId','GET'
  UNION ALL SELECT '媒体管理 DELETE cloud-recordings/downloads/:taskId','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE'
  UNION ALL SELECT '媒体管理 GET cloud-recordings/active','/api/gb28181/cloud-recordings/active','GET'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/recordings/runtime/status','/api/gb28181/zlm/nodes/:id/recordings/runtime/status','GET'
  UNION ALL SELECT '媒体管理 GET cloud-recordings/reconciliations','/api/gb28181/cloud-recordings/reconciliations','GET'
  UNION ALL SELECT '媒体管理 POST cloud-recordings/reconciliations','/api/gb28181/cloud-recordings/reconciliations','POST'
  UNION ALL SELECT '媒体管理 POST cloud-recordings/files/batch-delete','/api/gb28181/cloud-recordings/files/batch-delete','POST'
  UNION ALL SELECT '媒体管理 DELETE cloud-recordings/files/:id','/api/gb28181/cloud-recordings/files/:id','DELETE'
  UNION ALL SELECT '媒体管理 POST cloud-recordings/active/:id/stop','/api/gb28181/cloud-recordings/active/:id/stop','POST'
  UNION ALL SELECT '媒体管理 GET recording-plans','/api/gb28181/recording-plans','GET'
  UNION ALL SELECT '媒体管理 GET recording-plans/:id','/api/gb28181/recording-plans/:id','GET'
  UNION ALL SELECT '媒体管理 GET recording-plans/:id/channels','/api/gb28181/recording-plans/:id/channels','GET'
  UNION ALL SELECT '媒体管理 GET recording-plans/channels/:channelId/diagnosis','/api/gb28181/recording-plans/channels/:channelId/diagnosis','GET'
  UNION ALL SELECT '媒体管理 GET recording-plans/channels/:channelId/timeline','/api/gb28181/recording-plans/channels/:channelId/timeline','GET'
  UNION ALL SELECT '媒体管理 POST recording-plans','/api/gb28181/recording-plans','POST'
  UNION ALL SELECT '媒体管理 PUT recording-plans/:id','/api/gb28181/recording-plans/:id','PUT'
  UNION ALL SELECT '媒体管理 DELETE recording-plans/:id','/api/gb28181/recording-plans/:id','DELETE'
  UNION ALL SELECT '媒体管理 PATCH recording-plans/:id/status','/api/gb28181/recording-plans/:id/status','PATCH'
  UNION ALL SELECT '媒体管理 PATCH recording-plans/channels/:channelId/recording-mode','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH'
  UNION ALL SELECT '媒体管理 GET recording-plans/:id/assignment-options/devices','/api/gb28181/recording-plans/:id/assignment-options/devices','GET'
  UNION ALL SELECT '媒体管理 GET recording-plans/:id/assignment-options/channels','/api/gb28181/recording-plans/:id/assignment-options/channels','GET'
  UNION ALL SELECT '媒体管理 POST recording-plans/:id/assignments','/api/gb28181/recording-plans/:id/assignments','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes','/api/gb28181/zlm/nodes','POST'
  UNION ALL SELECT '媒体管理 PUT zlm/nodes/:id','/api/gb28181/zlm/nodes/:id','PUT'
  UNION ALL SELECT '媒体管理 DELETE zlm/nodes/:id','/api/gb28181/zlm/nodes/:id','DELETE'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/maintenance','/api/gb28181/zlm/nodes/:id/maintenance','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/activate','/api/gb28181/zlm/nodes/:id/activate','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/kick','/api/gb28181/zlm/nodes/:id/kick','POST'
  UNION ALL SELECT '媒体管理 PUT zlm/scheduler','/api/gb28181/zlm/scheduler','PUT'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/streams/playback-grant','/api/gb28181/zlm/nodes/:id/streams/playback-grant','POST'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/streams/snapshot','/api/gb28181/zlm/nodes/:id/streams/snapshot','GET'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/streams/close/preflight','/api/gb28181/zlm/nodes/:id/streams/close/preflight','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/streams/close','/api/gb28181/zlm/nodes/:id/streams/close','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/streams/close/batch/preflight','/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/streams/close/batch','/api/gb28181/zlm/nodes/:id/streams/close/batch','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/streams/force-close','/api/gb28181/zlm/nodes/:id/streams/force-close','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/sessions/kick','/api/gb28181/zlm/nodes/:id/sessions/kick','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/proxies/pull','/api/gb28181/zlm/nodes/:id/proxies/pull','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/proxies/pull/:key/preflight','/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight','POST'
  UNION ALL SELECT '媒体管理 DELETE zlm/nodes/:id/proxies/pull/:key','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','DELETE'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/proxies/push','/api/gb28181/zlm/nodes/:id/proxies/push','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/proxies/push/:key/preflight','/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight','POST'
  UNION ALL SELECT '媒体管理 DELETE zlm/nodes/:id/proxies/push/:key','/api/gb28181/zlm/nodes/:id/proxies/push/:key','DELETE'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/ffmpeg-sources','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/ffmpeg-sources/:key/preflight','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight','POST'
  UNION ALL SELECT '媒体管理 DELETE zlm/nodes/:id/ffmpeg-sources/:key','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key','DELETE'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/rtp-servers','/api/gb28181/zlm/nodes/:id/rtp-servers','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/rtp-servers/close/preflight','/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/rtp-servers/close','/api/gb28181/zlm/nodes/:id/rtp-servers/close','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/rtp-servers/force-close','/api/gb28181/zlm/nodes/:id/rtp-servers/force-close','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/recordings/runtime/start/preflight','/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/recordings/runtime/start','/api/gb28181/zlm/nodes/:id/recordings/runtime/start','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/recordings/runtime/stop/preflight','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/recordings/runtime/stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/recordings/runtime/force-stop/preflight','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight','POST'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/recordings/runtime/force-stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop','POST'
  UNION ALL SELECT '媒体管理 PUT zlm/nodes/:id/config','/api/gb28181/zlm/nodes/:id/config','PUT'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/config/test-connection','/api/gb28181/zlm/nodes/:id/config/test-connection','POST'
  UNION ALL SELECT '媒体管理 GET zlm/nodes/:id/restart','/api/gb28181/zlm/nodes/:id/restart','GET'
  UNION ALL SELECT '媒体管理 POST zlm/nodes/:id/restart','/api/gb28181/zlm/nodes/:id/restart','POST'
) v
WHERE NOT EXISTS (SELECT 1 FROM "sys_api" a WHERE a."path"=v."path" AND a."method"=v."method" AND a."deleted_at" IS NULL);
INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT DISTINCT m."id",a."id" FROM (
  SELECT 'path' selector_type,'/gb28181/zlm/overview' selector,'/api/gb28181/zlm/overview' api_path,'GET' method
  UNION ALL SELECT 'path','/gb28181/zlm/nodes','/api/gb28181/zlm/nodes','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/nodes','/api/gb28181/zlm/nodes/:id','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/nodes','/api/gb28181/zlm/nodes/:id/config','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/scheduler','/api/gb28181/zlm/scheduler','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/scheduler/logs','/api/gb28181/zlm/scheduler/logs','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/scheduler/logs','/api/gb28181/zlm/nodes','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/runtime','/api/gb28181/zlm/nodes','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/runtime','/api/gb28181/zlm/nodes/:id/runtime','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/streams','/api/gb28181/zlm/nodes','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/streams','/api/gb28181/zlm/streams','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/streams','/api/gb28181/zlm/nodes/:id/streams','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/streams','/api/gb28181/zlm/nodes/:id/streams/detail','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/streams','/api/gb28181/zlm/nodes/:id/streams/viewers','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/sessions','/api/gb28181/zlm/nodes','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/sessions','/api/gb28181/zlm/nodes/:id/sessions/network','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/sessions','/api/gb28181/zlm/nodes/:id/sessions/viewers','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes/:id/proxies/pull','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes/:id/proxies/push','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes/:id/proxies/push/:key','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/ffmpeg-sources','/api/gb28181/zlm/nodes','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/ffmpeg-sources','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/rtp-servers','/api/gb28181/zlm/nodes','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/rtp-servers','/api/gb28181/zlm/nodes/:id/rtp-servers','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/config','/api/gb28181/zlm/nodes','GET'
  UNION ALL SELECT 'path','/gb28181/zlm/config','/api/gb28181/zlm/nodes/:id/config','GET'
  UNION ALL SELECT 'permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files','GET'
  UNION ALL SELECT 'permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files/options','GET'
  UNION ALL SELECT 'permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files/:id','GET'
  UNION ALL SELECT 'permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files/:id/access','POST'
  UNION ALL SELECT 'permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files/:id/downloads','POST'
  UNION ALL SELECT 'permission','gb28181:recording:view','/api/gb28181/cloud-recordings/downloads/:taskId','GET'
  UNION ALL SELECT 'permission','gb28181:recording:view','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE'
  UNION ALL SELECT 'permission','gb28181:recording:view','/api/gb28181/cloud-recordings/active','GET'
  UNION ALL SELECT 'permission','gb28181:recording:view','/api/gb28181/zlm/nodes','GET'
  UNION ALL SELECT 'permission','gb28181:recording:view','/api/gb28181/zlm/nodes/:id/recordings/runtime/status','GET'
  UNION ALL SELECT 'permission','gb28181:recording:reconcile','/api/gb28181/cloud-recordings/reconciliations','GET'
  UNION ALL SELECT 'permission','gb28181:recording:reconcile','/api/gb28181/cloud-recordings/reconciliations','POST'
  UNION ALL SELECT 'permission','gb28181:recording:delete','/api/gb28181/cloud-recordings/files/batch-delete','POST'
  UNION ALL SELECT 'permission','gb28181:recording:delete','/api/gb28181/cloud-recordings/files/:id','DELETE'
  UNION ALL SELECT 'permission','gb28181:recording:stop','/api/gb28181/cloud-recordings/active/:id/stop','POST'
  UNION ALL SELECT 'permission','gb28181:recording-plan:view','/api/gb28181/recording-plans','GET'
  UNION ALL SELECT 'permission','gb28181:recording-plan:view','/api/gb28181/recording-plans/:id','GET'
  UNION ALL SELECT 'permission','gb28181:recording-plan:view','/api/gb28181/recording-plans/:id/channels','GET'
  UNION ALL SELECT 'permission','gb28181:recording-plan:view','/api/gb28181/recording-plans/channels/:channelId/diagnosis','GET'
  UNION ALL SELECT 'permission','gb28181:recording-plan:view','/api/gb28181/recording-plans/channels/:channelId/timeline','GET'
  UNION ALL SELECT 'permission','gb28181:recording-plan:maintain','/api/gb28181/recording-plans','POST'
  UNION ALL SELECT 'permission','gb28181:recording-plan:maintain','/api/gb28181/recording-plans/:id','PUT'
  UNION ALL SELECT 'permission','gb28181:recording-plan:maintain','/api/gb28181/recording-plans/:id','DELETE'
  UNION ALL SELECT 'permission','gb28181:recording-plan:maintain','/api/gb28181/recording-plans/:id/status','PATCH'
  UNION ALL SELECT 'permission','gb28181:recording-plan:assign','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH'
  UNION ALL SELECT 'permission','gb28181:recording-plan:assign','/api/gb28181/recording-plans/:id/assignment-options/devices','GET'
  UNION ALL SELECT 'permission','gb28181:recording-plan:assign','/api/gb28181/recording-plans/:id/assignment-options/channels','GET'
  UNION ALL SELECT 'permission','gb28181:recording-plan:assign','/api/gb28181/recording-plans/:id/assignments','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes/:id','PUT'
  UNION ALL SELECT 'permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes/:id','DELETE'
  UNION ALL SELECT 'permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes/:id/maintenance','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes/:id/activate','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:node:kick','/api/gb28181/zlm/nodes/:id/kick','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:scheduler:manage','/api/gb28181/zlm/scheduler','PUT'
  UNION ALL SELECT 'permission','gb28181:zlm:stream:preview','/api/gb28181/zlm/nodes/:id/streams/playback-grant','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:stream:preview','/api/gb28181/zlm/nodes/:id/streams/snapshot','GET'
  UNION ALL SELECT 'permission','gb28181:zlm:stream:close','/api/gb28181/zlm/nodes/:id/streams/close/preflight','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:stream:close','/api/gb28181/zlm/nodes/:id/streams/close','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:stream:close','/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:stream:close','/api/gb28181/zlm/nodes/:id/streams/close/batch','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:stream:force-close','/api/gb28181/zlm/nodes/:id/streams/force-close','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:session:kick','/api/gb28181/zlm/nodes/:id/sessions/kick','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/pull','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','DELETE'
  UNION ALL SELECT 'permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/push','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/push/:key','DELETE'
  UNION ALL SELECT 'permission','gb28181:zlm:ffmpeg:manage','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:ffmpeg:manage','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:ffmpeg:manage','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key','DELETE'
  UNION ALL SELECT 'permission','gb28181:zlm:rtp:manage','/api/gb28181/zlm/nodes/:id/rtp-servers','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:rtp:manage','/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:rtp:manage','/api/gb28181/zlm/nodes/:id/rtp-servers/close','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:rtp:force-close','/api/gb28181/zlm/nodes/:id/rtp-servers/force-close','POST'
  UNION ALL SELECT 'permission','gb28181:recording:control','/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight','POST'
  UNION ALL SELECT 'permission','gb28181:recording:control','/api/gb28181/zlm/nodes/:id/recordings/runtime/start','POST'
  UNION ALL SELECT 'permission','gb28181:recording:control','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight','POST'
  UNION ALL SELECT 'permission','gb28181:recording:control','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop','POST'
  UNION ALL SELECT 'permission','gb28181:recording:force-stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight','POST'
  UNION ALL SELECT 'permission','gb28181:recording:force-stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:config:update','/api/gb28181/zlm/nodes/:id/config','PUT'
  UNION ALL SELECT 'permission','gb28181:zlm:config:update','/api/gb28181/zlm/nodes/:id/config/test-connection','POST'
  UNION ALL SELECT 'permission','gb28181:zlm:restart','/api/gb28181/zlm/nodes/:id/restart','GET'
  UNION ALL SELECT 'permission','gb28181:zlm:restart','/api/gb28181/zlm/nodes/:id/restart','POST'
) b
JOIN "sys_menu" m ON ((b."selector_type"='path' AND m."path"=b."selector") OR (b."selector_type"='permission' AND m."permission"=b."selector")) AND m."deleted_at" IS NULL
JOIN "sys_api" a ON a."path"=b."api_path" AND a."method"=b."method" AND a."deleted_at" IS NULL
WHERE NOT EXISTS (SELECT 1 FROM "sys_menu_api" x WHERE x."menu_id"=m."id" AND x."api_id"=a."id");
INSERT INTO "sys_casbin_rule" ("ptype","v0","v1","v2","v3","v4","v5")
SELECT DISTINCT 'p',('role_' || rm."role_id"),a."path",a."method",'*','','' FROM "sys_role_menu" rm
JOIN "sys_menu" m ON m."id"=rm."menu_id"
JOIN "sys_menu_api" ma ON ma."menu_id"=m."id"
JOIN "sys_api" a ON a."id"=ma."api_id"
WHERE m."deleted_at" IS NULL AND a."deleted_at" IS NULL
AND (m."path" IN ('/gb28181/zlm/overview','/gb28181/zlm/nodes','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/config') OR m."permission" IN ('gb28181:zlm:node:manage','gb28181:zlm:node:kick','gb28181:zlm:scheduler:manage','gb28181:zlm:stream:preview','gb28181:zlm:stream:close','gb28181:zlm:stream:force-close','gb28181:zlm:session:kick','gb28181:zlm:proxy:manage','gb28181:zlm:ffmpeg:manage','gb28181:zlm:rtp:manage','gb28181:zlm:rtp:force-close','gb28181:recording:control','gb28181:recording:force-stop','gb28181:zlm:config:update','gb28181:zlm:restart','gb28181:recording:view','gb28181:recording:reconcile','gb28181:recording:delete','gb28181:recording:stop','gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign'))
AND (a."path" LIKE '/api/gb28181/zlm/%' OR a."path" LIKE '/api/gb28181/cloud-recordings/%' OR a."path" LIKE '/api/gb28181/recording-plans%')
AND NOT EXISTS (SELECT 1 FROM "sys_casbin_rule" c WHERE c."ptype"='p' AND c."v0"=('role_' || rm."role_id") AND c."v1"=a."path" AND c."v2"=a."method" AND c."v3"='*');
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","keep_alive","created_at","updated_at","created_by")
SELECT 0,'/media','Media','','gb28181/zlm/workbench/MediaEntry','流媒体管理','lucide:Clapperboard',9,1,'',0,1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=0,"name"='Media',"redirect"='',"component"='gb28181/zlm/workbench/MediaEntry',"title"='流媒体管理',"icon"='lucide:Clapperboard',"sort"=9,"type"=1,"permission"='',"hide"=0,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/media' AND "deleted_at" IS NULL;
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","keep_alive","created_at","updated_at","created_by")
SELECT p."id",'/media/overview','media-overview','','gb28181/zlm/workbench/MediaOverview','媒体总览','lucide:LayoutDashboard',10,2,'',0,1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/media' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/media/overview' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=(SELECT "p"."id" FROM "sys_menu" AS "p" WHERE p."path"='/media' AND p."deleted_at" IS NULL LIMIT 1),"name"='media-overview',"redirect"='',"component"='gb28181/zlm/workbench/MediaOverview',"title"='媒体总览',"icon"='lucide:LayoutDashboard',"sort"=10,"type"=2,"permission"='',"hide"=0,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/overview' AND "deleted_at" IS NULL AND EXISTS (SELECT 1 FROM "sys_menu" p WHERE p."path"='/media' AND p."deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","keep_alive","created_at","updated_at","created_by")
SELECT p."id",'/media/monitoring','media-monitoring','','gb28181/zlm/workbench/MediaMonitoring','媒体监控','lucide:Activity',20,2,'',0,1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/media' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/media/monitoring' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=(SELECT "p"."id" FROM "sys_menu" AS "p" WHERE p."path"='/media' AND p."deleted_at" IS NULL LIMIT 1),"name"='media-monitoring',"redirect"='',"component"='gb28181/zlm/workbench/MediaMonitoring',"title"='媒体监控',"icon"='lucide:Activity',"sort"=20,"type"=2,"permission"='',"hide"=0,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/monitoring' AND "deleted_at" IS NULL AND EXISTS (SELECT 1 FROM "sys_menu" p WHERE p."path"='/media' AND p."deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","keep_alive","created_at","updated_at","created_by")
SELECT p."id",'/media/ingress','media-ingress','','gb28181/zlm/workbench/IngressManagement','接入管理','lucide:RadioTower',30,2,'',0,1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/media' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/media/ingress' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=(SELECT "p"."id" FROM "sys_menu" AS "p" WHERE p."path"='/media' AND p."deleted_at" IS NULL LIMIT 1),"name"='media-ingress',"redirect"='',"component"='gb28181/zlm/workbench/IngressManagement',"title"='接入管理',"icon"='lucide:RadioTower',"sort"=30,"type"=2,"permission"='',"hide"=0,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/ingress' AND "deleted_at" IS NULL AND EXISTS (SELECT 1 FROM "sys_menu" p WHERE p."path"='/media' AND p."deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","keep_alive","created_at","updated_at","created_by")
SELECT p."id",'/media/recordings','media-recordings','','gb28181/zlm/workbench/RecordingCenter','录制中心','lucide:Cloud',40,2,'',0,1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/media' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/media/recordings' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=(SELECT "p"."id" FROM "sys_menu" AS "p" WHERE p."path"='/media' AND p."deleted_at" IS NULL LIMIT 1),"name"='media-recordings',"redirect"='',"component"='gb28181/zlm/workbench/RecordingCenter',"title"='录制中心',"icon"='lucide:Cloud',"sort"=40,"type"=2,"permission"='',"hide"=0,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/recordings' AND "deleted_at" IS NULL AND EXISTS (SELECT 1 FROM "sys_menu" p WHERE p."path"='/media' AND p."deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","keep_alive","created_at","updated_at","created_by")
SELECT p."id",'/media/nodes','media-nodes','','gb28181/zlm/workbench/NodeManagement','节点管理','lucide:Server',50,2,'',0,1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/media' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/media/nodes' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=(SELECT "p"."id" FROM "sys_menu" AS "p" WHERE p."path"='/media' AND p."deleted_at" IS NULL LIMIT 1),"name"='media-nodes',"redirect"='',"component"='gb28181/zlm/workbench/NodeManagement',"title"='节点管理',"icon"='lucide:Server',"sort"=50,"type"=2,"permission"='',"hide"=0,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/nodes' AND "deleted_at" IS NULL AND EXISTS (SELECT 1 FROM "sys_menu" p WHERE p."path"='/media' AND p."deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","keep_alive","created_at","updated_at","created_by")
SELECT p."id",'/media/scheduling','media-scheduling','','gb28181/zlm/workbench/SchedulingManagement','调度管理','lucide:Workflow',60,2,'',0,1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/media' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/media/scheduling' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=(SELECT "p"."id" FROM "sys_menu" AS "p" WHERE p."path"='/media' AND p."deleted_at" IS NULL LIMIT 1),"name"='media-scheduling',"redirect"='',"component"='gb28181/zlm/workbench/SchedulingManagement',"title"='调度管理',"icon"='lucide:Workflow',"sort"=60,"type"=2,"permission"='',"hide"=0,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/scheduling' AND "deleted_at" IS NULL AND EXISTS (SELECT 1 FROM "sys_menu" p WHERE p."path"='/media' AND p."deleted_at" IS NULL);
INSERT INTO "sys_menu" ("parent_id","path","name","redirect","component","title","icon","sort","type","permission","hide","keep_alive","created_at","updated_at","created_by")
SELECT p."id",'/media/nodes/:id','media-node-detail','','gb28181/zlm/workbench/nodes/NodeDetail','节点详情','lucide:Server',99,2,'',1,1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM "sys_menu" p
WHERE p."path"='/media/nodes' AND p."deleted_at" IS NULL
AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "path"='/media/nodes/:id' AND "deleted_at" IS NULL);
UPDATE "sys_menu" SET "parent_id"=(SELECT "p"."id" FROM "sys_menu" AS "p" WHERE p."path"='/media/nodes' AND p."deleted_at" IS NULL LIMIT 1),"name"='media-node-detail',"redirect"='',"component"='gb28181/zlm/workbench/nodes/NodeDetail',"title"='节点详情',"icon"='lucide:Server',"sort"=99,"type"=2,"permission"='',"hide"=1,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/nodes/:id' AND "deleted_at" IS NULL AND EXISTS (SELECT 1 FROM "sys_menu" p WHERE p."path"='/media/nodes' AND p."deleted_at" IS NULL);
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/overview' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/runtime' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/streams' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/sessions' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/proxies' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/ffmpeg-sources' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/rtp-servers' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/cloud-recordings' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/recording-schedules' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/nodes' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/nodes/:id' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/config' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/scheduler' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00'
WHERE "path"='/gb28181/zlm/scheduler/logs' AND "deleted_at" IS NULL;
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT DISTINCT rm."role_id",target."id"
FROM "sys_role_menu" rm
JOIN "sys_menu" source ON source."id"=rm."menu_id" AND source."deleted_at" IS NULL
JOIN "sys_menu" target ON target."path"='/media' AND target."deleted_at" IS NULL
WHERE source."path" IN ('/gb28181/zlm/overview','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs')
AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" existing WHERE existing."role_id"=rm."role_id" AND existing."menu_id"=target."id");
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT DISTINCT rm."role_id",target."id"
FROM "sys_role_menu" rm
JOIN "sys_menu" source ON source."id"=rm."menu_id" AND source."deleted_at" IS NULL
JOIN "sys_menu" target ON target."path"='/media/overview' AND target."deleted_at" IS NULL
WHERE source."path" IN ('/gb28181/zlm/overview')
AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" existing WHERE existing."role_id"=rm."role_id" AND existing."menu_id"=target."id");
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT DISTINCT rm."role_id",target."id"
FROM "sys_role_menu" rm
JOIN "sys_menu" source ON source."id"=rm."menu_id" AND source."deleted_at" IS NULL
JOIN "sys_menu" target ON target."path"='/media/monitoring' AND target."deleted_at" IS NULL
WHERE source."path" IN ('/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions')
AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" existing WHERE existing."role_id"=rm."role_id" AND existing."menu_id"=target."id");
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT DISTINCT rm."role_id",target."id"
FROM "sys_role_menu" rm
JOIN "sys_menu" source ON source."id"=rm."menu_id" AND source."deleted_at" IS NULL
JOIN "sys_menu" target ON target."path"='/media/ingress' AND target."deleted_at" IS NULL
WHERE source."path" IN ('/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers')
AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" existing WHERE existing."role_id"=rm."role_id" AND existing."menu_id"=target."id");
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT DISTINCT rm."role_id",target."id"
FROM "sys_role_menu" rm
JOIN "sys_menu" source ON source."id"=rm."menu_id" AND source."deleted_at" IS NULL
JOIN "sys_menu" target ON target."path"='/media/recordings' AND target."deleted_at" IS NULL
WHERE source."path" IN ('/gb28181/cloud-recordings','/gb28181/recording-schedules')
AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" existing WHERE existing."role_id"=rm."role_id" AND existing."menu_id"=target."id");
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT DISTINCT rm."role_id",target."id"
FROM "sys_role_menu" rm
JOIN "sys_menu" source ON source."id"=rm."menu_id" AND source."deleted_at" IS NULL
JOIN "sys_menu" target ON target."path"='/media/nodes' AND target."deleted_at" IS NULL
WHERE source."path" IN ('/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config')
AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" existing WHERE existing."role_id"=rm."role_id" AND existing."menu_id"=target."id");
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT DISTINCT rm."role_id",target."id"
FROM "sys_role_menu" rm
JOIN "sys_menu" source ON source."id"=rm."menu_id" AND source."deleted_at" IS NULL
JOIN "sys_menu" target ON target."path"='/media/nodes/:id' AND target."deleted_at" IS NULL
WHERE source."path" IN ('/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config')
AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" existing WHERE existing."role_id"=rm."role_id" AND existing."menu_id"=target."id");
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT DISTINCT rm."role_id",target."id"
FROM "sys_role_menu" rm
JOIN "sys_menu" source ON source."id"=rm."menu_id" AND source."deleted_at" IS NULL
JOIN "sys_menu" target ON target."path"='/media/scheduling' AND target."deleted_at" IS NULL
WHERE source."path" IN ('/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs')
AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" existing WHERE existing."role_id"=rm."role_id" AND existing."menu_id"=target."id");
UPDATE "sys_menu" SET "redirect"='/gb28181/zlm/overview',"component"='',"title"='流媒体管理',"icon"='lucide:Clapperboard',"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/ClusterOverview',"title"='集群总览',"icon"='lucide:LayoutDashboard',"sort"=10,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/overview' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/NodeList',"title"='节点管理',"icon"='lucide:Server',"sort"=20,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/nodes' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/RuntimeOverview',"title"='总览',"icon"='lucide:Gauge',"sort"=30,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/runtime' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/StreamManagement',"title"='流管理',"icon"='lucide:RadioTower',"sort"=40,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/streams' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/SessionManagement',"title"='会话管理',"icon"='lucide:Users',"sort"=50,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/sessions' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/ProxyManagement',"title"='拉流/推流代理',"icon"='lucide:Network',"sort"=60,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/proxies' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/FFmpegSources',"title"='FFmpeg 源',"icon"='lucide:Clapperboard',"sort"=70,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/ffmpeg-sources' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/RTPServices',"title"='RTP 服务',"icon"='lucide:Waypoints',"sort"=80,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/rtp-servers' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/ServerConfig',"title"='服务器配置',"icon"='lucide:Settings2',"sort"=90,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/config' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/SchedulerStrategy',"title"='调度策略',"icon"='lucide:Workflow',"sort"=100,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/scheduler' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/SchedulerLog',"title"='调度日志',"icon"='lucide:History',"sort"=110,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/scheduler/logs' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=0,"component"='gb28181/zlm/NodeDetail',"title"='节点详情',"hide"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/nodes/:id' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT "parent_id" FROM "sys_menu" WHERE "id"=((SELECT "id" FROM "sys_menu" WHERE "path" IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND "deleted_at" IS NULL ORDER BY CASE WHEN "path"='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,"id" LIMIT 1)))),"component"='gb28181/cloud-recordings/index',"title"='云端录像',"icon"='lucide:Cloud',"sort"=35,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/cloud-recordings' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT "parent_id" FROM "sys_menu" WHERE "id"=((SELECT "id" FROM "sys_menu" WHERE "path" IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND "deleted_at" IS NULL ORDER BY CASE WHEN "path"='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,"id" LIMIT 1)))),"component"='gb28181/recording-schedules/index',"title"='录像计划',"icon"='lucide:CalendarClock',"sort"=36,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/recording-schedules' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path" IN ('/media/overview','/media/monitoring','/media/ingress','/media/recordings','/media/nodes','/media/scheduling','/media/nodes/:id') AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/ClusterOverview',"title"='总览',"icon"='lucide:LayoutDashboard',"sort"=10,"hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/overview' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/RuntimeOverview',"hide"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/gb28181/zlm/runtime' AND "deleted_at" IS NULL;
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT rm."role_id",o."id" FROM "sys_role_menu" rm JOIN "sys_menu" r ON r."id"=rm."menu_id" AND r."path"='/gb28181/zlm/runtime' AND r."deleted_at" IS NULL CROSS JOIN "sys_menu" o
WHERE o."path"='/gb28181/zlm/overview' AND o."deleted_at" IS NULL AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" x WHERE x."role_id"=rm."role_id" AND x."menu_id"=o."id");
INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT o."id",a."id" FROM "sys_menu" o CROSS JOIN "sys_api" a WHERE o."path"='/gb28181/zlm/overview' AND o."deleted_at" IS NULL AND a."deleted_at" IS NULL AND a."method"='GET'
AND a."path" IN ('/api/gb28181/zlm/nodes','/api/gb28181/zlm/nodes/:id/runtime')
AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" ma WHERE ma."menu_id"=o."id" AND ma."api_id"=a."id");
INSERT INTO "sys_casbin_rule" ("ptype","v0","v1","v2","v3","v4","v5")
SELECT DISTINCT 'p',('role_' || rm."role_id"),a."path",a."method",'*','','' FROM "sys_role_menu" rm
JOIN "sys_menu" m ON m."id"=rm."menu_id" JOIN "sys_menu_api" ma ON ma."menu_id"=m."id" JOIN "sys_api" a ON a."id"=ma."api_id"
WHERE m."path"='/gb28181/zlm/overview' AND m."deleted_at" IS NULL AND a."deleted_at" IS NULL
AND a."path" IN ('/api/gb28181/zlm/overview','/api/gb28181/zlm/nodes','/api/gb28181/zlm/nodes/:id/runtime')
AND NOT EXISTS (SELECT 1 FROM "sys_casbin_rule" c WHERE c."ptype"='p' AND c."v0"=('role_' || rm."role_id") AND c."v1"=a."path" AND c."v2"=a."method" AND c."v3"='*');
UPDATE "sys_menu" SET "redirect"='/media/overview',"component"='',"title"='流媒体管理',"icon"='lucide:Clapperboard',"sort"=9,"type"=1,"hide"=0,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/workbench/MediaOverview',"title"='运行总览',"sort"=10,"type"=2,"hide"=1,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/overview' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/workbench/MediaMonitoring',"title"='流与会话',"sort"=20,"type"=2,"hide"=1,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/monitoring' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/workbench/IngressManagement',"title"='接入管理',"sort"=30,"type"=2,"hide"=1,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/ingress' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/workbench/NodeManagement',"title"='节点管理',"sort"=40,"type"=2,"hide"=1,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/nodes' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/workbench/SchedulingManagement',"title"='调度管理',"sort"=50,"type"=2,"hide"=1,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/scheduling' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "parent_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path"='/media/nodes' AND "deleted_at" IS NULL)),"component"='gb28181/zlm/workbench/nodes/NodeDetail',"title"='节点详情',"hide"=1,"keep_alive"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/nodes/:id' AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "component"='gb28181/zlm/workbench/LegacyMediaRoute',"hide"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path" IN ('/gb28181/zlm/overview','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs') AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "hide"=0,"updated_at"='2026-09-07 00:00:00' WHERE "path" IN ('/media/overview','/media/monitoring','/media/ingress','/media/nodes','/media/scheduling') AND "deleted_at" IS NULL;
UPDATE "sys_menu" SET "hide"=1,"updated_at"='2026-09-07 00:00:00' WHERE "path"='/media/nodes/:id' AND "deleted_at" IS NULL;
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看设备维护','/api/gb28181/device-mgmt/device/:id/maintenance-operations','GET','GB28181 设备维护','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceMaintenanceView','','查看设备维护',1,0,1,3,'gb28181:device:maintenance:view','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:maintenance:view' AND deleted_at IS NULL);
INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p',('role_' || rm.role_id),a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND a.method='GET' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=('role_' || rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重启设备','/api/gb28181/device-mgmt/device/:id/reboot','POST','GB28181 设备维护','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/reboot' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceReboot','','重启设备',1,0,1,3,'gb28181:device:reboot','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:reboot' AND deleted_at IS NULL);
INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:reboot' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:reboot' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/reboot' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p',('role_' || rm.role_id),a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:reboot' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/reboot' AND a.method='POST' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=('role_' || rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看设备升级','/api/gb28181/device-mgmt/device/:id/firmware-upgrades','GET','GB28181 设备维护','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceMaintenanceView','','查看设备维护',1,0,1,3,'gb28181:device:maintenance:view','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:maintenance:view' AND deleted_at IS NULL);
INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p',('role_' || rm.role_id),a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND a.method='GET' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=('role_' || rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '升级设备','/api/gb28181/device-mgmt/device/:id/firmware-upgrade','POST','GB28181 设备维护','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceUpgrade','','升级设备',1,0,1,3,'gb28181:device:upgrade','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:upgrade' AND deleted_at IS NULL);
INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:upgrade' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:upgrade' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p',('role_' || rm.role_id),a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:upgrade' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND a.method='POST' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=('role_' || rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'/system/sysparam','SystemSysparam','system/sysparam/sysparam','参数管理',1,1,100,2,'','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/system/sysparam' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '物理删除告警','/api/gb28181/alarms/:id','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/alarms/:id' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理国标级联','/api/gb28181/cascade/platforms/:id','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '通道收藏管理','/api/gb28181/channel-favorite-groups/:id','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/channel-favorite-groups/:id' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '通道收藏管理','/api/gb28181/channel-favorite-groups/:id/channels','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/channel-favorite-groups/:id/channels' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '下载云端录像','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/downloads/:taskId' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除录像文件','/api/gb28181/cloud-recordings/files/:id','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/files/:id' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除通道（单个/批量）','/api/gb28181/device-mgmt/channel/:id','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备录像回放会话（播放、暂停/续播、倍速、停止）','/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除预置位','/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '语音对讲与广播会话','/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理自定义分组','/api/gb28181/device-mgmt/custom-groups/:id','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/custom-groups/:id' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除设备（单个/批量）','/api/gb28181/device-mgmt/device/:id','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重置仪表盘布局','/api/gb28181/home/layout','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/layout' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强制停止当前直播/共享停播','/api/gb28181/play/:streamId','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/play/:streamId' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理播放方案','/api/gb28181/playback-schemes/:id','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/playback-schemes/:id' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '维护录像计划','/api/gb28181/recording-plans/:id','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除安全访问规则','/api/gb28181/security/access-rules/:id','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/security/access-rules/:id' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理节点','/api/gb28181/zlm/nodes/:id','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理 FFmpeg 源','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理代理','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/proxies/pull/:key' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理代理','/api/gb28181/zlm/nodes/:id/proxies/push/:key','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/proxies/push/:key' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/plugins/example/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/plugins/example/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '插件卸载','/api/pluginsmanager/uninstall','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/pluginsmanager/uninstall' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '大文件上传','/api/sysAffix/chunk/cancel','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/chunk/cancel' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除文件','/api/sysAffix/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysApi/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysApi/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除部门','/api/sysDepartment/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDepartment/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysDict/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDict/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除字典项','/api/sysDictItem/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDictItem/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysGen/:id','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysGen/:id' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysJobResults/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/joblog' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobResults/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysJobs/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobs/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除登录日志','/api/sysLoginLog/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysLoginLog/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysMenu/batchDelete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/batchDelete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysMenu/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysOperationLog/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysOperationLog/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除参数','/api/sysParam/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysParam/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysRole/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysRole/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/users/delete','DELETE','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/users/delete' AND method='DELETE' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '预览','/api/codegen/preview','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/codegen/preview' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导入表','/api/codegen/tables','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/codegen/tables' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看国标级联','/api/gb28181/cascade/platforms','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理国标级联','/api/gb28181/cascade/platforms/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '共享国标级联资源','/api/gb28181/cascade/platforms/:id/shares','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/shares' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '通道收藏管理','/api/gb28181/channel-favorite-groups','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/channel-favorite-groups' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '下载云端录像','/api/gb28181/cloud-recordings/downloads/:taskId','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/downloads/:taskId' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '执行录像对账','/api/gb28181/cloud-recordings/reconciliations','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/reconciliations' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/catalog/tree','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/catalog/tree' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/catalog/tree/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/catalog/tree/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/catalog/tree/:id/children','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/catalog/tree/:id/children' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/channel/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/control-capabilities','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/control-capabilities' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/device-status','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/device-status' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/channel/:id/mounts','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/mounts' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备录像回放会话（播放、暂停/续播、倍速、停止）','/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks/:trackId','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks/:trackId' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/ptz/home-position','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/home-position' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/ptz/operations/:operationId','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/operations/:operationId' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/ptz/precise-status','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/precise-status' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/ptz/presets','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/presets' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查询设备录像','/api/gb28181/device-mgmt/channel/:id/record-query/options','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/record-query/options' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备图像抓拍会话','/api/gb28181/device-mgmt/channel/:id/snapshot-sessions/:sessionId','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/snapshot-sessions/:sessionId' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '语音对讲与广播会话','/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/channel/:id/timeline','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/timeline' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/channels','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channels' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/device/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看设备维护','/api/gb28181/device-mgmt/device/:id/firmware-upgrades','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看设备维护','/api/gb28181/device-mgmt/device/:id/maintenance-operations','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/device/:id/status-events','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/status-events' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备订阅读取、更新与续订','/api/gb28181/device-mgmt/device/:id/subscriptions','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/subscriptions' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/devices','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/devices' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/directory/tree','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/directory/tree' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/map/clusters','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/map/clusters' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/map/markers','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/map/markers' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '共享设备','/api/gb28181/device-mgmt/permission-workbench/grant-targets','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看运行监控','/api/gb28181/device-traffic/coverage','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/coverage' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看运行监控','/api/gb28181/device-traffic/realtime','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/realtime' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看运行监控','/api/gb28181/device-traffic/sessions','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/sessions' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看运行监控','/api/gb28181/device-traffic/summary','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/summary' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看运行监控','/api/gb28181/device-traffic/trend','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/trend' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看运行监控','/api/gb28181/device-traffic/viewers','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/viewers' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看仪表盘','/api/gb28181/home/drilldown/play','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/drilldown/play' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看仪表盘','/api/gb28181/home/drilldown/sip','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/drilldown/sip' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看仪表盘','/api/gb28181/home/drilldown/traffic','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/drilldown/traffic' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看仪表盘','/api/gb28181/home/layout','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/layout' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看仪表盘','/api/gb28181/home/summary','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/summary' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '播放流监控读取','/api/gb28181/play/:streamId/monitor','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/play/:streamId/monitor' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理播放方案','/api/gb28181/playback-schemes','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/playback-schemes' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理播放方案','/api/gb28181/playback-schemes/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/playback-schemes/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '维护录像计划','/api/gb28181/recording-plans/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配录像计划','/api/gb28181/recording-plans/:id/assignment-options/channels','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id/assignment-options/channels' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配录像计划','/api/gb28181/recording-plans/:id/assignment-options/devices','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id/assignment-options/devices' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP Trace 报文及会话详情','/api/gb28181/sip-traces/messages/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip-traces/messages/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP Trace 报文及会话详情','/api/gb28181/sip-traces/sessions/:callId/messages','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip-traces/sessions/:callId/messages' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/platform','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/platform' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/default-channel-audio','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/default-channel-audio' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/default-playback-protocol','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/default-playback-protocol' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/fixed-address-playback','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/fixed-address-playback' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/global-subscriptions','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/global-subscriptions' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/ignore-channel-offline-status-notify','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/online-on-heartbeat','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/online-on-heartbeat' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/play-auth','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/play-auth' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/preallocation-mode','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/preallocation-mode' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/ptz-default-speed','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/ptz-default-speed' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/save-alarm-messages','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/save-alarm-messages' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/sip-command-timeout','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sip-command-timeout' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/sip-log','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sip-log' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/sync-channels-on-online','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sync-channels-on-online' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/setup/network-interfaces','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/setup/network-interfaces' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/setup/status','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/setup/status' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 ZLM 节点详情','/api/gb28181/zlm/nodes/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重启媒体服务','/api/gb28181/zlm/nodes/:id/restart','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/restart' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看媒体流详情','/api/gb28181/zlm/nodes/:id/streams/detail','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/detail' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '预览与截图','/api/gb28181/zlm/nodes/:id/streams/snapshot','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/snapshot' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看媒体流详情','/api/gb28181/zlm/nodes/:id/streams/viewers','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/viewers' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/plugins/example/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/plugins/example/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '复制链接','/api/sysAffix/download/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/download/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysApi/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysApi/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配权限','/api/sysApi/list','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysApi/list' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '数据权限','/api/sysDepartment/getDivision','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDepartment/getDivision' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/sysDictItem/getByDictCode/:dictCode','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDictItem/getByDictCode/:dictCode' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '字典项管理','/api/sysDictItem/getByDictId/:dictId','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDictItem/getByDictId/:dictId' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '配置','/api/sysGen/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysGen/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看定时任务日志','/api/sysJobResults/list','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobResults/list' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysJobs/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobs/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看登录日志详情','/api/sysLoginLog/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysLoginLog/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配权限','/api/sysMenu/apis/:id','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/apis/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导出','/api/sysMenu/export','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/export' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配权限','/api/sysMenu/getMenuList','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/getMenuList' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导出','/api/sysOperationLog/export','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysOperationLog/export' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配权限','/api/sysRole/getUserPermission/:roleId','GET','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysRole/getUserPermission/:roleId' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑通道参数','/api/gb28181/device-mgmt/channel/:id','PATCH','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id' AND method='PATCH' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '切换通道云端录制','/api/gb28181/device-mgmt/channel/:id/cloud-recording','PATCH','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/cloud-recording' AND method='PATCH' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台归位点更新','/api/gb28181/device-mgmt/channel/:id/ptz/home-position','PATCH','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/home-position' AND method='PATCH' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改通道传输模式','/api/gb28181/device-mgmt/channel/:id/stream-transport','PATCH','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/stream-transport' AND method='PATCH' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理自定义分组','/api/gb28181/device-mgmt/custom-groups/:id','PATCH','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/custom-groups/:id' AND method='PATCH' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备订阅读取、更新与续订','/api/gb28181/device-mgmt/device/:id/subscriptions/:kind','PATCH','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/subscriptions/:kind' AND method='PATCH' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑设备','/api/gb28181/device/:deviceId','PATCH','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device/:deviceId' AND method='PATCH' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理播放方案','/api/gb28181/playback-schemes/:id','PATCH','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/playback-schemes/:id' AND method='PATCH' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '维护录像计划','/api/gb28181/recording-plans/:id/status','PATCH','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id/status' AND method='PATCH' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配录像计划','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/channels/:channelId/recording-mode' AND method='PATCH' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '生成代码文件','/api/codegen/generate','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/codegen/generate' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '生成菜单','/api/codegen/insertmenuandapi','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/codegen/insertmenuandapi' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '物理删除告警','/api/gb28181/alarms/batch-delete','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/alarms/batch-delete' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '清空全部告警','/api/gb28181/alarms/clear-all','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/alarms/clear-all' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理国标级联','/api/gb28181/cascade/platforms','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '启停国标级联','/api/gb28181/cascade/platforms/:id/enable','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/enable' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重连国标级联','/api/gb28181/cascade/platforms/:id/reconnect','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/reconnect' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '通道收藏管理','/api/gb28181/channel-favorite-groups','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/channel-favorite-groups' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '通道收藏管理','/api/gb28181/channel-favorite-groups/:id/channels','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/channel-favorite-groups/:id/channels' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '停止录像','/api/gb28181/cloud-recordings/active/:id/stop','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/active/:id/stop' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '下载云端录像','/api/gb28181/cloud-recordings/files/:id/downloads','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/files/:id/downloads' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除录像文件','/api/gb28181/cloud-recordings/files/batch-delete','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/files/batch-delete' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '执行录像对账','/api/gb28181/cloud-recordings/reconciliations','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/reconciliations' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备复合控制（关键帧、录制、守望、告警、拖拽变焦等）','/api/gb28181/device-mgmt/channel/:id/device-control','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/device-control' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备录像下载','/api/gb28181/device-mgmt/channel/:id/download-sessions','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/download-sessions' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备录像回放会话（播放、暂停/续播、倍速、停止）','/api/gb28181/device-mgmt/channel/:id/playback-sessions','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/playback-sessions' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备录像回放会话（播放、暂停/续播、倍速、停止）','/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId/actions','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId/actions' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台移动、变倍、聚焦、光圈（基础/精准/扩展）','/api/gb28181/device-mgmt/channel/:id/ptz','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台巡航轨迹创建、启动、停止与删除','/api/gb28181/device-mgmt/channel/:id/ptz/cruise','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台巡航轨迹创建、启动、停止与删除','/api/gb28181/device-mgmt/channel/:id/ptz/cruise/tracks','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise/tracks' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台移动、变倍、聚焦、光圈（基础/精准/扩展）','/api/gb28181/device-mgmt/channel/:id/ptz/extended','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/extended' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台移动、变倍、聚焦、光圈（基础/精准/扩展）','/api/gb28181/device-mgmt/channel/:id/ptz/precise','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/precise' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '保存预置位','/api/gb28181/device-mgmt/channel/:id/ptz/presets','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/presets' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '调用预置位','/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId/call','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId/call' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查询设备录像','/api/gb28181/device-mgmt/channel/:id/record-query','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/record-query' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备图像抓拍会话','/api/gb28181/device-mgmt/channel/:id/snapshot-sessions','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/snapshot-sessions' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '语音对讲与广播会话','/api/gb28181/device-mgmt/channel/:id/talk-sessions','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/talk-sessions' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除通道（单个/批量）','/api/gb28181/device-mgmt/channel/batch-delete','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/batch-delete' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理自定义分组','/api/gb28181/device-mgmt/custom-groups','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/custom-groups' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理自定义分组','/api/gb28181/device-mgmt/custom-groups/:id/devices','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/custom-groups/:id/devices' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理自定义分组','/api/gb28181/device-mgmt/custom-groups/:id/devices/remove','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/custom-groups/:id/devices/remove' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理自定义分组','/api/gb28181/device-mgmt/custom-groups/:id/move','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/custom-groups/:id/move' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新建设备','/api/gb28181/device-mgmt/device','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '刷新设备目录（下发 SIP 目录查询）','/api/gb28181/device-mgmt/device/:id/catalog/refresh','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/catalog/refresh' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '升级设备','/api/gb28181/device-mgmt/device/:id/firmware-upgrade','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重启设备','/api/gb28181/device-mgmt/device/:id/reboot','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/reboot' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备订阅读取、更新与续订','/api/gb28181/device-mgmt/device/:id/subscriptions/:kind/renew','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/subscriptions/:kind/renew' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除设备（单个/批量）','/api/gb28181/device-mgmt/device/batch-delete','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/batch-delete' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配设备归属','/api/gb28181/device-mgmt/permission-workbench/assignments','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/assignments' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配设备归属','/api/gb28181/device-mgmt/permission-workbench/assignments/departments','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配设备归属','/api/gb28181/device-mgmt/permission-workbench/devices/resolve','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '共享设备','/api/gb28181/device-mgmt/permission-workbench/grants/apply','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '共享设备','/api/gb28181/device-mgmt/permission-workbench/grants/query','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强退观看连接','/api/gb28181/device-traffic/viewers/kick','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/viewers/kick' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '发起实时点播','/api/gb28181/play/:deviceId/:channelId','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/play/:deviceId/:channelId' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '发起实时点播','/api/gb28181/play/:deviceId/:channelId/authorization','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/play/:deviceId/:channelId/authorization' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理播放方案','/api/gb28181/playback-schemes','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/playback-schemes' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '维护录像计划','/api/gb28181/recording-plans','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配录像计划','/api/gb28181/recording-plans/:id/assignments','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id/assignments' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增访问规则（黑白名单）','/api/gb28181/security/access-rules','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/security/access-rules' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '解除自动封禁','/api/gb28181/security/bans/:id/unban','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/security/bans/:id/unban' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/qr/token','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL OR (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/platform' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/qr/token' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/setup/skip','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/setup/skip' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '视频探针诊断','/api/gb28181/stream-probes/:streamId','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/stream-probes/:streamId' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理节点','/api/gb28181/zlm/nodes','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理节点','/api/gb28181/zlm/nodes/:id/activate','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/activate' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '更新服务配置','/api/gb28181/zlm/nodes/:id/config/test-connection','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/config/test-connection' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理 FFmpeg 源','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/ffmpeg-sources' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理 FFmpeg 源','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '踢除节点会话','/api/gb28181/zlm/nodes/:id/kick','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/kick' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理节点','/api/gb28181/zlm/nodes/:id/maintenance','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/maintenance' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理代理','/api/gb28181/zlm/nodes/:id/proxies/pull','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/proxies/pull' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理代理','/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理代理','/api/gb28181/zlm/nodes/:id/proxies/push','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/proxies/push' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理代理','/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强制停止录制','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强制停止录制','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '手工录制控制','/api/gb28181/zlm/nodes/:id/recordings/runtime/start','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/recordings/runtime/start' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '手工录制控制','/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '手工录制控制','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/recordings/runtime/stop' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '手工录制控制','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重启媒体服务','/api/gb28181/zlm/nodes/:id/restart','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/restart' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理 RTP 服务','/api/gb28181/zlm/nodes/:id/rtp-servers','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/rtp-servers' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理 RTP 服务','/api/gb28181/zlm/nodes/:id/rtp-servers/close','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/rtp-servers/close' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理 RTP 服务','/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强制关闭 RTP 服务','/api/gb28181/zlm/nodes/:id/rtp-servers/force-close','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/rtp-servers/force-close' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '踢除会话','/api/gb28181/zlm/nodes/:id/sessions/kick','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/sessions/kick' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '关闭流','/api/gb28181/zlm/nodes/:id/streams/close','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/close' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '关闭流','/api/gb28181/zlm/nodes/:id/streams/close/batch','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/close/batch' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '关闭流','/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '关闭流','/api/gb28181/zlm/nodes/:id/streams/close/preflight','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/close/preflight' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强制关闭流','/api/gb28181/zlm/nodes/:id/streams/force-close','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/force-close' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '预览与截图','/api/gb28181/zlm/nodes/:id/streams/playback-grant','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/playback-grant' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理节点','/api/gb28181/zlm/nodes/probe','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/probe' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/plugins/example/add','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/plugins/example/add' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导出插件','/api/pluginsmanager/export','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/pluginsmanager/export' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导入插件','/api/pluginsmanager/import','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/pluginsmanager/import' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '大文件上传','/api/sysAffix/chunk/init','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/chunk/init' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '大文件上传','/api/sysAffix/chunk/merge','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/chunk/merge' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '大文件上传','/api/sysAffix/chunk/upload','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/chunk/upload' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '大文件上传','/api/sysAffix/upload','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/upload' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/sysApi/add','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysApi/add' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增部门','/api/sysDepartment/add','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDepartment/add' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/sysDict/add','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDict/add' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增字典项','/api/sysDictItem/add','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDictItem/add' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导入表','/api/sysGen/batchInsert','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysGen/batchInsert' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/sysJobs/add','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobs/add' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '执行一次','/api/sysJobs/executeNow','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobs/executeNow' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '清空登录日志','/api/sysLoginLog/clear','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysLoginLog/clear' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '解锁登录账号','/api/sysLoginLog/unlock','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysLoginLog/unlock' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/sysMenu/add','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/add' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导入','/api/sysMenu/import','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/import' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配权限','/api/sysMenu/setApis','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/setApis' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强制下线','/api/sysOnlineUser/forceLogout','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/online-user' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysOnlineUser/forceLogout' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增参数','/api/sysParam/add','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysParam/add' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/sysRole/add','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysRole/add' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配权限','/api/sysRole/addRoleMenu','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysRole/addRoleMenu' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/users/add','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/users/add' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '上传个人头像','/api/users/uploadAvatar','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/users/uploadAvatar' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改系统配置','/api/config/update','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysconfig' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/config/update' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理国标级联','/api/gb28181/cascade/platforms/:id','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '启停国标级联','/api/gb28181/cascade/platforms/:id/enabled','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/enabled' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '共享国标级联资源','/api/gb28181/cascade/platforms/:id/shares','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/shares' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '保存仪表盘布局','/api/gb28181/home/layout','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/layout' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理播放方案','/api/gb28181/playback-schemes/:id/layout','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/playback-schemes/:id/layout' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '维护录像计划','/api/gb28181/recording-plans/:id','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '启用或停用安全访问规则','/api/gb28181/security/access-rules/:id','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/security/access-rules/:id' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '保存安全防护策略','/api/gb28181/security/policy','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/security/policy' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/default-channel-audio','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/default-channel-audio' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/default-channel-stream-transport','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/default-channel-stream-transport' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/default-playback-protocol','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/default-playback-protocol' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/fixed-address-playback','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/fixed-address-playback' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/global-subscriptions','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/global-subscriptions' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/ignore-channel-offline-status-notify','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/online-on-heartbeat','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/online-on-heartbeat' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/play-auth','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/play-auth' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/playback-settings','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/playback-settings' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/position-history','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/position-history' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/preallocation-mode','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/preallocation-mode' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/ptz-default-speed','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/ptz-default-speed' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/save-alarm-messages','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/save-alarm-messages' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/sdp-extension','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sdp-extension' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/sip-command-timeout','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sip-command-timeout' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/sip-log','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sip-log' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/sync-channels-on-online','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sync-channels-on-online' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/setup/config','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/setup/config' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理节点','/api/gb28181/zlm/nodes/:id','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '更新服务配置','/api/gb28181/zlm/nodes/:id/config','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/config' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '切换调度策略','/api/gb28181/zlm/scheduler','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/scheduler' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/plugins/example/edit','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/plugins/example/edit' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改文件名','/api/sysAffix/updateName','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/updateName' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysApi/edit','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysApi/edit' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑部门','/api/sysDepartment/edit','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDepartment/edit' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysDict/edit','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDict/edit' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑字典项','/api/sysDictItem/edit','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDictItem/edit' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '同步数据库','/api/sysGen/refreshFields','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysGen/refreshFields' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '配置','/api/sysGen/update','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysGen/update' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysJobs/edit','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobs/edit' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysJobs/setStatus','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobs/setStatus' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysMenu/edit','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/edit' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑参数','/api/sysParam/edit','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysParam/edit' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '数据权限','/api/sysRole/dataScope','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysRole/dataScope' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysRole/edit','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysRole/edit' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/users/edit','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/users/edit' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改密码、手机号等','/api/users/updateAccount','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/users/updateAccount' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改用户基本信息','/api/users/updateBasicInfo','PUT','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/users/updateBasicInfo' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_alarm_clear','','清空全部告警',1,0,100,3,'gb28181:alarm:clear','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:alarm:clear' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:alarm:clear' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:alarm:clear' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/alarms/clear-all' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_alarm_delete','','物理删除告警',1,0,100,3,'gb28181:alarm:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:alarm:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:alarm:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:alarm:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/alarms/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:alarm:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/alarms/batch-delete' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_cascade_enable','','启停国标级联',1,0,100,3,'gb28181:cascade:enable','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:cascade:enable' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:cascade:enable' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:enable' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id/enable' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:enable' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id/enabled' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_cascade_manage','','管理国标级联',1,0,100,3,'gb28181:cascade:manage','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:cascade:manage' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:cascade:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_cascade_reconnect','','重连国标级联',1,0,100,3,'gb28181:cascade:reconnect','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:cascade:reconnect' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:cascade:reconnect' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:reconnect' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id/reconnect' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_cascade_share','','共享国标级联资源',1,0,100,3,'gb28181:cascade:share','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:cascade:share' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:cascade:share' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:share' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id/shares' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:share' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id/shares' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_cascade_view','','查看国标级联',1,0,100,3,'gb28181:cascade:view','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:cascade:view' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:cascade:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id/shares' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_channel_favorite_manage','','通道收藏管理',1,0,100,3,'gb28181:channel-favorite:manage','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:channel-favorite:manage' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:channel-favorite:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel-favorite:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/channel-favorite-groups' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel-favorite:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/channel-favorite-groups' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel-favorite:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/channel-favorite-groups/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel-favorite:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/channel-favorite-groups/:id/channels' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel-favorite:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/channel-favorite-groups/:id/channels' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_channel_delete','','删除通道（单个/批量）',1,0,100,3,'gb28181:channel:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:channel:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:channel:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/batch-delete' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_channel_edit','','编辑通道参数',1,0,100,3,'gb28181:channel:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:channel:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:channel:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_channel_recording_update','','切换通道云端录制',1,0,100,3,'gb28181:channel:recording:update','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:channel:recording:update' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:channel:recording:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel:recording:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/cloud-recording' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_channel_stream_transport_update','','修改通道传输模式',1,0,100,3,'gb28181:channel:stream-transport:update','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:channel:stream-transport:update' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:channel:stream-transport:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel:stream-transport:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/stream-transport' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_group_manage','','管理自定义分组',1,0,100,3,'gb28181:device-group:manage','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device-group:manage' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device-group:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/media')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-group:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/custom-groups' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-group:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/custom-groups/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-group:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/custom-groups/:id' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-group:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/custom-groups/:id/devices' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-group:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/custom-groups/:id/devices/remove' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-group:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/custom-groups/:id/move' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_record_download','','设备录像下载',1,0,100,3,'gb28181:device-record:download','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device-record:download' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device-record:download' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:download' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/download-sessions' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_record_play','','设备录像回放会话（播放、暂停/续播、倍速、停止）',1,0,100,3,'gb28181:device-record:play','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device-record:play' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device-record:play' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:play' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/playback-sessions' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:play' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:play' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:play' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId/actions' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_record_query','','查询设备录像',1,0,100,3,'gb28181:device-record:query','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device-record:query' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device-record:query' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:query' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/record-query' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:query' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/record-query/options' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_add','','新建设备',1,0,100,3,'gb28181:device:add','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:add' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_assign','','分配设备归属',1,0,100,3,'gb28181:device:assign','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:assign' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:assign' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/assignments' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_catalog_refresh','','刷新设备目录（下发 SIP 目录查询）',1,0,100,3,'gb28181:device:catalog:refresh','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:catalog:refresh' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:catalog:refresh' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:catalog:refresh' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/catalog/refresh' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_control','','设备复合控制（关键帧、录制、守望、告警、拖拽变焦等）',1,0,100,3,'gb28181:device:control','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:control' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:control' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/device-control' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_delete','','删除设备（单个/批量）',1,0,100,3,'gb28181:device:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/batch-delete' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_edit','','编辑设备',1,0,100,3,'gb28181:device:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device/:deviceId' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_maintenance_view','','查看设备维护',1,0,100,3,'gb28181:device:maintenance:view','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:maintenance:view' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:maintenance:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:maintenance:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:maintenance:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_reboot','','重启设备',1,0,100,3,'gb28181:device:reboot','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:reboot' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:reboot' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:reboot' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/reboot' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_share','','共享设备',1,0,100,3,'gb28181:device:share','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:share' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:share' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:share' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:share' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:share' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_snapshot','','设备图像抓拍会话',1,0,100,3,'gb28181:device:snapshot','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:snapshot' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:snapshot' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:snapshot' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/snapshot-sessions' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:snapshot' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/snapshot-sessions/:sessionId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_subscription_manage','','设备订阅读取、更新与续订',1,0,100,3,'gb28181:device:subscription:manage','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:subscription:manage' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:subscription:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:subscription:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/subscriptions' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:subscription:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/subscriptions/:kind' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:subscription:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/subscriptions/:kind/renew' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_upgrade','','升级设备',1,0,100,3,'gb28181:device:upgrade','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:upgrade' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:upgrade' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:upgrade' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_view','','设备、通道、目录与地图只读查询',1,0,100,3,'gb28181:device:view','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:view' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/catalog/tree' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/catalog/tree/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/catalog/tree/:id/children' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/mounts' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/timeline' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channels' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/status-events' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/devices' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/directory/tree' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/map/clusters' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/map/markers' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_home_layout_reset','','重置仪表盘布局',1,0,100,3,'gb28181:home:layout:reset','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:home:layout:reset' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:home:layout:reset' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:layout:reset' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/layout' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_home_layout_save','','保存仪表盘布局',1,0,100,3,'gb28181:home:layout:save','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:home:layout:save' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:home:layout:save' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:layout:save' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/layout' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_home_view','','查看仪表盘',1,0,100,3,'gb28181:home:view','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:home:view' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:home:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/drilldown/play' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/drilldown/sip' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/drilldown/traffic' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/layout' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/summary' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_play_diagnose','','视频探针诊断',1,0,100,3,'gb28181:play:diagnose','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:diagnose' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:play:diagnose' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:diagnose' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/stream-probes/:streamId' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_play_monitor','','播放流监控读取',1,0,100,3,'gb28181:play:monitor','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:monitor' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:play:monitor' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:monitor' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/play/:streamId/monitor' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_play_start','','发起实时点播',1,0,100,3,'gb28181:play:start','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:start' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:play:start' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/media')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:start' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/play/:deviceId/:channelId' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:start' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/play/:deviceId/:channelId/authorization' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_play_stop','','强制停止当前直播/共享停播',1,0,100,3,'gb28181:play:stop','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:stop' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:play:stop' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:stop' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/play/:streamId' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_playback_scheme_manage','','管理播放方案',1,0,100,3,'gb28181:playback-scheme:manage','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:playback-scheme:manage' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:playback-scheme:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/media')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:playback-scheme:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/playback-schemes' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:playback-scheme:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/playback-schemes' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:playback-scheme:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/playback-schemes/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:playback-scheme:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/playback-schemes/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:playback-scheme:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/playback-schemes/:id' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:playback-scheme:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/playback-schemes/:id/layout' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_control','','云台移动、变倍、聚焦、光圈（基础/精准/扩展）',1,0,100,3,'gb28181:ptz:control','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:control' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:control' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/extended' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/precise' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_cruise','','云台巡航轨迹创建、启动、停止与删除',1,0,100,3,'gb28181:ptz:cruise','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:cruise' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:cruise' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:cruise' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:cruise' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise/tracks' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_home','','云台归位点更新',1,0,100,3,'gb28181:ptz:home','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:home' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:home' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:home' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/home-position' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_preset_call','','调用预置位',1,0,100,3,'gb28181:ptz:preset:call','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:preset:call' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:preset:call' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:preset:call' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId/call' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_preset_delete','','删除预置位',1,0,100,3,'gb28181:ptz:preset:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:preset:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:preset:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:preset:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_preset_save','','保存预置位',1,0,100,3,'gb28181:ptz:preset:save','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:preset:save' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:preset:save' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:preset:save' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/presets' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_view','','云台能力、状态与预置/巡航资源读取',1,0,100,3,'gb28181:ptz:view','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:view' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/control-capabilities' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/device-status' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks/:trackId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/home-position' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/operations/:operationId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/precise-status' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/presets' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_plan_assign','','分配录像计划',1,0,100,3,'gb28181:recording-plan:assign','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording-plan:assign' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording-plan:assign' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id/assignment-options/channels' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id/assignment-options/devices' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id/assignments' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/channels/:channelId/recording-mode' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_plan_maintain','','维护录像计划',1,0,100,3,'gb28181:recording-plan:maintain','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording-plan:maintain' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording-plan:maintain' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:maintain' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:maintain' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:maintain' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:maintain' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:maintain' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id/status' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_control','','手工录制控制',1,0,100,3,'gb28181:recording:control','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:control' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording:control' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/recordings/runtime/start' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/recordings/runtime/stop' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_delete','','删除录像文件',1,0,100,3,'gb28181:recording:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/files/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/files/batch-delete' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_download','','下载云端录像',1,0,100,3,'gb28181:recording:download','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:download' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording:download' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:download' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/downloads/:taskId' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:download' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/downloads/:taskId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:download' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/files/:id/downloads' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_force_stop','','强制停止录制',1,0,100,3,'gb28181:recording:force-stop','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:force-stop' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording:force-stop' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:force-stop' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:force-stop' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_reconcile','','执行录像对账',1,0,100,3,'gb28181:recording:reconcile','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:reconcile' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording:reconcile' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:reconcile' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/reconciliations' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:reconcile' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/reconciliations' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_stop','','停止录像',1,0,100,3,'gb28181:recording:stop','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:stop' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording:stop' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:stop' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/active/:id/stop' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_security_ban_unban','','解除自动封禁',1,0,100,3,'gb28181:security:ban:unban','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:security:ban:unban' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:security:ban:unban' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:security:ban:unban' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/security/bans/:id/unban' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_security_policy_update','','保存安全防护策略',1,0,100,3,'gb28181:security:policy:update','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:security:policy:update' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:security:policy:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:security:policy:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/security/policy' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_security_rule_add','','新增访问规则（黑白名单）',1,0,100,3,'gb28181:security:rule:add','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:security:rule:add' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:security:rule:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:security:rule:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/security/access-rules' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_security_rule_delete','','删除安全访问规则',1,0,100,3,'gb28181:security:rule:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:security:rule:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:security:rule:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:security:rule:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/security/access-rules/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_security_rule_edit','','启用或停用安全访问规则',1,0,100,3,'gb28181:security:rule:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:security:rule:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:security:rule:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:security:rule:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/security/access-rules/:id' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_sip_config_update','','修改 SIP 配置',1,0,100,3,'gb28181:sip:config:update','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:sip:config:update' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:sip:config:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/media')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/default-channel-audio' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/default-channel-stream-transport' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/default-playback-protocol' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/fixed-address-playback' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/global-subscriptions' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/online-on-heartbeat' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/play-auth' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/playback-settings' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/position-history' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/preallocation-mode' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/ptz-default-speed' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/save-alarm-messages' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sdp-extension' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sip-command-timeout' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sip-log' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sync-channels-on-online' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/setup/config' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/setup/skip' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_sip_config_view','','查看 SIP 配置',1,0,100,3,'gb28181:sip:config:view','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:sip:config:view' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:sip:config:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/media')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/platform' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/qr/token' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/default-channel-audio' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/default-playback-protocol' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/fixed-address-playback' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/global-subscriptions' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/online-on-heartbeat' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/play-auth' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/preallocation-mode' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/ptz-default-speed' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/save-alarm-messages' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sip-command-timeout' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sip-log' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sync-channels-on-online' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/setup/network-interfaces' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/setup/status' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDictItem/getByDictCode/:dictCode' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/platform' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_sip_qr_create','','生成设备接入二维码',1,0,100,3,'gb28181:sip:qr:create','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/platform' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:sip:qr:create' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/platform' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:sip:qr:create' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/platform' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/platform' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:qr:create' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/qr/token' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_sip_trace_export','','下载SIP日志',1,0,100,3,'gb28181:sip:trace:export','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:sip:trace:export' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:sip:trace:export' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_sip_trace_view','','查看 SIP Trace 报文及会话详情',1,0,100,3,'gb28181:sip:trace:view','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:sip:trace:view' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:sip:trace:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:trace:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip-traces/messages/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:trace:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip-traces/sessions/:callId/messages' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_talk_control','','语音对讲与广播会话',1,0,100,3,'gb28181:talk:control','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:talk:control' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:talk:control' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:talk:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/talk-sessions' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:talk:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:talk:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_traffic_view','','查看运行监控',1,0,100,3,'gb28181:traffic:view','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:traffic:view' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:traffic:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/coverage' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/realtime' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/sessions' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/summary' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/trend' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/viewers' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_traffic_viewer_kick','','强退观看连接',1,0,100,3,'gb28181:traffic:viewer:kick','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:traffic:viewer:kick' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:traffic:viewer:kick' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:viewer:kick' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/viewers/kick' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_config_update','','更新服务配置',1,0,100,3,'gb28181:zlm:config:update','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:config:update' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:config:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/config')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/config' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/config/test-connection' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_ffmpeg_manage','','管理 FFmpeg 源',1,0,100,3,'gb28181:zlm:ffmpeg:manage','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:ffmpeg:manage' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:ffmpeg:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/ffmpeg-sources')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:ffmpeg:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/ffmpeg-sources' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:ffmpeg:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:ffmpeg:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_node_kick','','踢除节点会话',1,0,100,3,'gb28181:zlm:node:kick','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:node:kick' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:node:kick' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/nodes')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:kick' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/kick' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_node_manage','','管理节点',1,0,100,3,'gb28181:zlm:node:manage','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:node:manage' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:node:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/nodes')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/activate' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/config/test-connection' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/maintenance' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/probe' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_node_view','','查看 ZLM 节点详情',1,0,100,3,'gb28181:zlm:node:view','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:node:view' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:node:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_proxy_manage','','管理代理',1,0,100,3,'gb28181:zlm:proxy:manage','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:proxy:manage' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:proxy:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/proxies')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:proxy:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/proxies/pull' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:proxy:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/proxies/pull/:key' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:proxy:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:proxy:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/proxies/push' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:proxy:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/proxies/push/:key' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:proxy:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_restart','','重启媒体服务',1,0,100,3,'gb28181:zlm:restart','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:restart' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:restart' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/config')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:restart' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/restart' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:restart' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/restart' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_rtp_force_close','','强制关闭 RTP 服务',1,0,100,3,'gb28181:zlm:rtp:force-close','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:rtp:force-close' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:rtp:force-close' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/rtp-servers')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:rtp:force-close' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/rtp-servers/force-close' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_rtp_manage','','管理 RTP 服务',1,0,100,3,'gb28181:zlm:rtp:manage','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:rtp:manage' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:rtp:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/rtp-servers')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:rtp:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/rtp-servers' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:rtp:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/rtp-servers/close' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:rtp:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_scheduler_manage','','切换调度策略',1,0,100,3,'gb28181:zlm:scheduler:manage','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:scheduler:manage' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:scheduler:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/scheduler')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:scheduler:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/scheduler' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_session_kick','','踢除会话',1,0,100,3,'gb28181:zlm:session:kick','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:session:kick' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:session:kick' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/sessions')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:session:kick' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/sessions/kick' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_stream_close','','关闭流',1,0,100,3,'gb28181:zlm:stream:close','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:close' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:stream:close' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/streams')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:close' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/close' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:close' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/close/batch' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:close' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:close' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/close/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_stream_force_close','','强制关闭流',1,0,100,3,'gb28181:zlm:stream:force-close','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:force-close' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:stream:force-close' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/streams')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:force-close' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/force-close' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_stream_preview','','预览与截图',1,0,100,3,'gb28181:zlm:stream:preview','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:preview' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:stream:preview' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/streams')) AS catalog_legacy_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:preview' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/playback-grant' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:preview' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/snapshot' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_stream_view','','查看媒体流详情',1,0,100,3,'gb28181:zlm:stream:view','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:view' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:stream:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/detail' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/viewers' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_plugins_example_add','','新增',1,0,100,3,'plugins:example:add','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='plugins:example:add' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='plugins:example:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='plugins:example:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/plugins/example/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_plugins_example_delete','','删除',1,0,100,3,'plugins:example:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='plugins:example:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='plugins:example:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='plugins:example:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/plugins/example/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_plugins_example_edit','','编辑',1,0,100,3,'plugins:example:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='plugins:example:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='plugins:example:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='plugins:example:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/plugins/example/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='plugins:example:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/plugins/example/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_account_add','','新增',1,0,100,3,'system:account:add','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:account:add' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:account:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:account:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/users/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_account_delete','','删除',1,0,100,3,'system:account:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:account:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:account:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:account:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/users/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_account_edit','','编辑',1,0,100,3,'system:account:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:account:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:account:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:account:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/users/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_affix_bigupload','','大文件上传',1,0,100,3,'system:affix:bigupload','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:affix:bigupload' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:affix:bigupload' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:bigupload' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/chunk/cancel' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:bigupload' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/chunk/init' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:bigupload' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/chunk/merge' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:bigupload' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/chunk/upload' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:bigupload' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/upload' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_affix_copy','','复制链接',1,0,100,3,'system:affix:copy','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:affix:copy' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:affix:copy' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:copy' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/download/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_affix_delete','','删除文件',1,0,100,3,'system:affix:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:affix:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:affix:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_affix_download','','下载文件',1,0,100,3,'system:affix:download','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:affix:download' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:affix:download' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:download' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/download/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_affix_updateName','','修改文件名',1,0,100,3,'system:affix:updateName','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:affix:updateName' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:affix:updateName' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:updateName' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/updateName' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_affix_upload','','文件上传',1,0,100,3,'system:affix:upload','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:affix:upload' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:affix:upload' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:upload' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/upload' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_api_add','','新增',1,0,100,3,'system:api:add','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:api:add' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:api:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:api:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysApi/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_api_delete','','删除',1,0,100,3,'system:api:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:api:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:api:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:api:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysApi/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_api_edit','','编辑',1,0,100,3,'system:api:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:api:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:api:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:api:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysApi/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:api:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysApi/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_batchInsert','','导入表',1,0,100,3,'system:codegen:batchInsert','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:batchInsert' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:batchInsert' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:batchInsert' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/codegen/tables' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:batchInsert' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysGen/batchInsert' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_delete','','删除',1,0,100,3,'system:codegen:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysGen/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_generate','','生成代码文件',1,0,100,3,'system:codegen:generate','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:generate' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:generate' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:generate' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/codegen/generate' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_insertmenuandapi','','生成菜单',1,0,100,3,'system:codegen:insertmenuandapi','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:insertmenuandapi' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:insertmenuandapi' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:insertmenuandapi' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/codegen/insertmenuandapi' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_preview','','预览',1,0,100,3,'system:codegen:preview','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:preview' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:preview' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:preview' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/codegen/preview' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_refreshFields','','同步数据库',1,0,100,3,'system:codegen:refreshFields','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:refreshFields' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:refreshFields' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:refreshFields' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysGen/refreshFields' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_update','','配置',1,0,100,3,'system:codegen:update','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:update' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysGen/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysGen/update' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysconfig' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_config_update','','修改系统配置',1,0,100,3,'system:config:update','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysconfig' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:config:update' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysconfig' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:config:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysconfig' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysconfig' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/config/update' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dict_add','','新增',1,0,100,3,'system:dict:add','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dict:add' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dict:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dict:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDict/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dict_delete','','删除',1,0,100,3,'system:dict:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dict:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dict:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dict:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDict/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dict_edit','','编辑',1,0,100,3,'system:dict:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dict:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dict:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dict:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDict/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dictitem_add','','新增字典项',1,0,100,3,'system:dictitem:add','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dictitem:add' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dictitem:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dictitem:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDictItem/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dictitem_delete','','删除字典项',1,0,100,3,'system:dictitem:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dictitem:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dictitem:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dictitem:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDictItem/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dictitem_edit','','编辑字典项',1,0,100,3,'system:dictitem:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dictitem:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dictitem:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dictitem:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDictItem/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dictitem_list','','字典项管理',1,0,100,3,'system:dictitem:list','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dictitem:list' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dictitem:list' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dictitem:list' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDictItem/getByDictId/:dictId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_division_add','','新增部门',1,0,100,3,'system:division:add','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:division:add' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:division:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:division:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDepartment/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_division_delete','','删除部门',1,0,100,3,'system:division:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:division:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:division:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:division:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDepartment/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_division_edit','','编辑部门',1,0,100,3,'system:division:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:division:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:division:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:division:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDepartment/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_log_delete','','删除',1,0,100,3,'system:log:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:log:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:log:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:log:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysOperationLog/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_log_export','','导出',1,0,100,3,'system:log:export','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:log:export' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:log:export' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:log:export' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysOperationLog/export' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_login_log_clear','','清空登录日志',1,0,100,3,'system:login-log:clear','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:login-log:clear' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:login-log:clear' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:login-log:clear' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysLoginLog/clear' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_login_log_delete','','删除登录日志',1,0,100,3,'system:login-log:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:login-log:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:login-log:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:login-log:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysLoginLog/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_login_log_detail','','查看登录日志详情',1,0,100,3,'system:login-log:detail','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:login-log:detail' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:login-log:detail' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:login-log:detail' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysLoginLog/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_login_log_unlock','','解锁登录账号',1,0,100,3,'system:login-log:unlock','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:login-log:unlock' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:login-log:unlock' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:login-log:unlock' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysLoginLog/unlock' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_menu_add','','新增',1,0,100,3,'system:menu:add','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:menu:add' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:menu:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_menu_delete','','删除',1,0,100,3,'system:menu:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:menu:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:menu:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/batchDelete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_menu_edit','','编辑',1,0,100,3,'system:menu:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:menu:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:menu:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_menu_export','','导出',1,0,100,3,'system:menu:export','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:menu:export' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:menu:export' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:export' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/export' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_menu_import','','导入',1,0,100,3,'system:menu:import','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:menu:import' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:menu:import' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:import' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/import' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_menu_setMenuApis','','分配权限',1,0,100,3,'system:menu:setMenuApis','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:menu:setMenuApis' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:menu:setMenuApis' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:setMenuApis' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysApi/list' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:setMenuApis' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/apis/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:setMenuApis' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/setApis' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/online-user' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_online_user_force_logout','','强制下线',1,0,100,3,'system:online-user:force-logout','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/online-user' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:online-user:force-logout' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/online-user' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:online-user:force-logout' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/online-user' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/online-user' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:online-user:force-logout' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysOnlineUser/forceLogout' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_param_add','','新增参数',1,0,100,3,'system:param:add','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:param:add' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:param:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:param:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysParam/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_param_delete','','删除参数',1,0,100,3,'system:param:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:param:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:param:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:param:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysParam/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_param_edit','','编辑参数',1,0,100,3,'system:param:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:param:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:param:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:param:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysParam/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_pluginsmanager_export','','导出插件',1,0,100,3,'system:pluginsmanager:export','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:pluginsmanager:export' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:pluginsmanager:export' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:pluginsmanager:export' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/pluginsmanager/export' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_pluginsmanager_import','','导入插件',1,0,100,3,'system:pluginsmanager:import','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:pluginsmanager:import' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:pluginsmanager:import' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:pluginsmanager:import' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/pluginsmanager/import' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_pluginsmanager_uninstall','','插件卸载',1,0,100,3,'system:pluginsmanager:uninstall','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:pluginsmanager:uninstall' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:pluginsmanager:uninstall' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:pluginsmanager:uninstall' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/pluginsmanager/uninstall' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_role_add','','新增',1,0,100,3,'system:role:add','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:role:add' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:role:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysRole/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_role_addRoleMenu','','分配权限',1,0,100,3,'system:role:addRoleMenu','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:role:addRoleMenu' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:role:addRoleMenu' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:addRoleMenu' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/getMenuList' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:addRoleMenu' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysRole/addRoleMenu' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:addRoleMenu' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysRole/getUserPermission/:roleId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_role_dataScope','','数据权限',1,0,100,3,'system:role:dataScope','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:role:dataScope' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:role:dataScope' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:dataScope' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDepartment/getDivision' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:dataScope' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysRole/dataScope' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_role_delete','','删除',1,0,100,3,'system:role:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:role:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:role:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysRole/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_role_edit','','编辑',1,0,100,3,'system:role:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:role:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:role:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysRole/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/joblog' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobresults_delete','','删除',1,0,100,3,'system:sysjobresults:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/joblog' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobresults:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/joblog' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobresults:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/joblog' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/joblog' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobresults:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobResults/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobresults_list','','查看定时任务日志',1,0,100,3,'system:sysjobresults:list','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobresults:list' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobresults:list' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobresults:list' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobResults/list' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobs_add','','新增',1,0,100,3,'system:sysjobs:add','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobs:add' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobs:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobs_delete','','删除',1,0,100,3,'system:sysjobs:delete','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobs:delete' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobs:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobs_edit','','编辑',1,0,100,3,'system:sysjobs:edit','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobs:edit' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobs:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/setStatus' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobs_executeNow','','执行一次',1,0,100,3,'system:sysjobs:executeNow','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobs:executeNow' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobs:executeNow' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:executeNow' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/executeNow' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobs_setStatus','','切换定时任务状态',1,0,100,3,'system:sysjobs:setStatus','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobs:setStatus' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobs:setStatus' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:setStatus' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/setStatus' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_userinfo_updateAccount','','修改密码、手机号等',1,0,100,3,'system:userinfo:updateAccount','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:userinfo:updateAccount' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:userinfo:updateAccount' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:userinfo:updateAccount' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/users/updateAccount' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_userinfo_updateBasicInfo','','修改用户基本信息',1,0,100,3,'system:userinfo:updateBasicInfo','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:userinfo:updateBasicInfo' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:userinfo:updateBasicInfo' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:userinfo:updateBasicInfo' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/users/updateBasicInfo' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_userinfo_uploadAvatar','','上传个人头像',1,0,100,3,'system:userinfo:uploadAvatar','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:userinfo:uploadAvatar' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:userinfo:uploadAvatar' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:userinfo:uploadAvatar' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/users/uploadAvatar' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_play_share','','复制或分享播放地址',1,0,100,3,'gb28181:play:share','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:share' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:play:share' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_play_snapshot','','播放器本地图像截图',1,0,100,3,'gb28181:play:snapshot','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:snapshot' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:play:snapshot' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT m.id,'','Permission_gb28181_play_share','','复制或分享播放地址',1,0,100,3,'gb28181:play:share','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM sys_menu m WHERE m.path='/gb28181/multi-screen-playback' AND m.type=2 AND m.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu x WHERE x.permission='gb28181:play:share' AND x.deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT m.id,'','Permission_gb28181_play_snapshot','','播放器本地图像截图',1,0,100,3,'gb28181:play:snapshot','','2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM sys_menu m WHERE m.path='/gb28181/multi-screen-playback' AND m.type=2 AND m.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu x WHERE x.permission='gb28181:play:snapshot' AND x.deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '视频探针诊断','/api/gb28181/stream-probes/:streamId','POST','按钮权限目录','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/gb28181/stream-probes/:streamId' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT DISTINCT ma.menu_id,n.id FROM sys_menu_api ma JOIN sys_api o ON o.id=ma.api_id CROSS JOIN sys_api n WHERE o.path='/api/gb28181/play/:deviceId/probe' AND o.method='POST' AND n.path='/api/gb28181/stream-probes/:streamId' AND n.method='POST' AND n.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=ma.menu_id AND x.api_id=n.id);
DELETE FROM sys_menu_api WHERE api_id IN(SELECT id FROM sys_api WHERE path='/api/gb28181/play/:deviceId/probe' AND method='POST');
UPDATE sys_api SET deleted_at='2026-09-07 00:00:00',updated_at='2026-09-07 00:00:00' WHERE path='/api/gb28181/play/:deviceId/probe' AND method='POST' AND deleted_at IS NULL;
INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT DISTINCT p.ptype,p.v0,'/api/gb28181/stream-probes/:streamId',p.v2,p.v3,p.v4,p.v5 FROM (SELECT DISTINCT ptype,v0,v2,v3,v4,v5 FROM sys_casbin_rule WHERE ptype='p' AND v1='/api/gb28181/play/:deviceId/probe' AND v2='POST') p WHERE NOT EXISTS(SELECT 1 FROM sys_casbin_rule n WHERE n.ptype=p.ptype AND n.v0=p.v0 AND n.v1='/api/gb28181/stream-probes/:streamId' AND n.v2=p.v2 AND n.v3=p.v3);
DELETE FROM sys_casbin_rule WHERE ptype='p' AND v1='/api/gb28181/play/:deviceId/probe' AND v2='POST';
INSERT INTO sys_role_menu(role_id,menu_id) SELECT DISTINCT r.role_id,d.id FROM sys_role_menu r JOIN sys_menu v ON v.id=r.menu_id CROSS JOIN sys_menu d JOIN sys_role sr ON sr.id=r.role_id WHERE v.permission='gb28181:recording:view' AND v.deleted_at IS NULL AND d.permission='gb28181:recording:download' AND d.deleted_at IS NULL AND sr.name<>'游客' AND EXISTS(SELECT 1 FROM sys_menu_api ma JOIN sys_api a ON a.id=ma.api_id WHERE ma.menu_id=v.id AND a.path='/api/gb28181/cloud-recordings/files/:id/downloads') AND NOT EXISTS(SELECT 1 FROM sys_role_menu x WHERE x.role_id=r.role_id AND x.menu_id=d.id);
DELETE FROM sys_menu_api WHERE menu_id IN(SELECT id FROM sys_menu WHERE permission='gb28181:recording:view') AND api_id IN(SELECT id FROM sys_api WHERE (path='/api/gb28181/cloud-recordings/files/:id/downloads' AND method='POST') OR (path='/api/gb28181/cloud-recordings/downloads/:taskId' AND method IN('GET','DELETE')));
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/users/profile','GET','游客权限依赖','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/users/profile' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.path='/home' AND m.type=2 AND m.deleted_at IS NULL AND a.path='/api/users/profile' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/sysMenu/getRouters','GET','游客权限依赖','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/sysMenu/getRouters' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.path='/home' AND m.type=2 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/getRouters' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/users/logout','POST','游客权限依赖','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/users/logout' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.path='/home' AND m.type=2 AND m.deleted_at IS NULL AND a.path='/api/users/logout' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/gb28181/sip/dashboard/snapshot','GET','游客权限依赖','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/dashboard/snapshot' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/dashboard/snapshot' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/gb28181/zlm/overview','GET','游客权限依赖','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/overview' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/overview' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/gb28181/sip/service-config/default-playback-protocol','GET','游客权限依赖','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/default-playback-protocol' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:start' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/default-playback-protocol' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/gb28181/sip/service-config/playback-settings','GET','游客权限依赖','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/playback-settings' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:start' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/playback-settings' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/gb28181/sip/service-config/fixed-address-playback','GET','游客权限依赖','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/fixed-address-playback' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:start' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/fixed-address-playback' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_role(name,sort,status,description,parent_id,data_scope,checked_depts,created_at,updated_at,created_by) SELECT '游客',100,1,'只读游客：允许业务查看、实时观看和录像回放；禁止下载、控制与修改',0,4,'','2026-09-07 00:00:00','2026-09-07 00:00:00',1 WHERE NOT EXISTS(SELECT 1 FROM sys_role WHERE name='游客' AND deleted_at IS NULL);
UPDATE sys_role SET description='只读游客：允许业务查看、实时观看和录像回放；禁止下载、控制与修改',parent_id=0,status=1,updated_at='2026-09-07 00:00:00' WHERE name='游客' AND deleted_at IS NULL AND (COALESCE(description,'')<>'只读游客：允许业务查看、实时观看和录像回放；禁止下载、控制与修改' OR parent_id<>0 OR status<>1);
DELETE FROM sys_role_menu WHERE role_id=(SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL) AND menu_id NOT IN(SELECT m.id FROM sys_menu m WHERE m.deleted_at IS NULL AND m.disable=0 AND ((m.type=2 AND m.path IN ('/home','/gb28181/device-mgmt/index','/gb28181/multi-screen-playback','/gb28181/device-record-playback/:channelId','/gb28181/cloud-recordings','/gb28181/alarm-management')) OR (m.type=3 AND m.permission IN ('gb28181:home:view','gb28181:device:view','gb28181:play:start','gb28181:play:monitor','gb28181:device-record:query','gb28181:device-record:play'))));
INSERT INTO sys_role_menu(role_id,menu_id) SELECT (SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL),m.id FROM sys_menu m WHERE m.deleted_at IS NULL AND m.disable=0 AND ((m.type=2 AND m.path IN ('/home','/gb28181/device-mgmt/index','/gb28181/multi-screen-playback','/gb28181/device-record-playback/:channelId','/gb28181/cloud-recordings','/gb28181/alarm-management')) OR (m.type=3 AND m.permission IN ('gb28181:home:view','gb28181:device:view','gb28181:play:start','gb28181:play:monitor','gb28181:device-record:query','gb28181:device-record:play'))) AND NOT EXISTS(SELECT 1 FROM sys_role_menu x WHERE x.role_id=(SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL) AND x.menu_id=m.id);
DELETE FROM sys_casbin_rule WHERE v0=('role_' || (SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL)) AND ((ptype='p' AND (v3<>'*' OR NOT ((v1='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId' AND v2='DELETE') OR (v1='/api/gb28181/alarms' AND v2='GET') OR (v1='/api/gb28181/alarms/:id' AND v2='GET') OR (v1='/api/gb28181/cloud-recordings/active' AND v2='GET') OR (v1='/api/gb28181/cloud-recordings/files' AND v2='GET') OR (v1='/api/gb28181/cloud-recordings/files/:id' AND v2='GET') OR (v1='/api/gb28181/cloud-recordings/files/options' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/catalog/tree' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/catalog/tree/:id' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/catalog/tree/:id/children' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/channel/:id' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/channel/:id/mounts' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/channel/:id/record-query/options' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/channel/:id/timeline' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/channels' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/device/:id' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/device/:id/status-events' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/devices' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/directory/tree' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/map/clusters' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/map/markers' AND v2='GET') OR (v1='/api/gb28181/home/drilldown/play' AND v2='GET') OR (v1='/api/gb28181/home/drilldown/sip' AND v2='GET') OR (v1='/api/gb28181/home/drilldown/traffic' AND v2='GET') OR (v1='/api/gb28181/home/layout' AND v2='GET') OR (v1='/api/gb28181/home/summary' AND v2='GET') OR (v1='/api/gb28181/play/:streamId/monitor' AND v2='GET') OR (v1='/api/gb28181/sip/dashboard/snapshot' AND v2='GET') OR (v1='/api/gb28181/sip/service-config/default-playback-protocol' AND v2='GET') OR (v1='/api/gb28181/sip/service-config/fixed-address-playback' AND v2='GET') OR (v1='/api/gb28181/sip/service-config/playback-settings' AND v2='GET') OR (v1='/api/gb28181/zlm/nodes' AND v2='GET') OR (v1='/api/gb28181/zlm/nodes/:id/recordings/runtime/status' AND v2='GET') OR (v1='/api/gb28181/zlm/overview' AND v2='GET') OR (v1='/api/sysMenu/getRouters' AND v2='GET') OR (v1='/api/users/profile' AND v2='GET') OR (v1='/api/gb28181/cloud-recordings/files/:id/access' AND v2='POST') OR (v1='/api/gb28181/device-mgmt/channel/:id/playback-sessions' AND v2='POST') OR (v1='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId/actions' AND v2='POST') OR (v1='/api/gb28181/device-mgmt/channel/:id/record-query' AND v2='POST') OR (v1='/api/gb28181/play/:deviceId/:channelId' AND v2='POST') OR (v1='/api/gb28181/play/:deviceId/:channelId/authorization' AND v2='POST') OR (v1='/api/users/logout' AND v2='POST')))) OR ptype='g');
INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT DISTINCT 'p',('role_' || (SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL)),a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE rm.role_id=(SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL) AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=('role_' || (SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL)) AND p.v1=a.path AND p.v2=a.method AND p.v3='*');
INSERT INTO "sys_menu" ("parent_id","path","name","component","title","type","permission","hide","created_at","updated_at","created_by")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "path" IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND "deleted_at" IS NULL)),'','','','查看运行监控',3,'gb28181:traffic:view',1,'2026-09-07 00:00:00','2026-09-07 00:00:00',1
WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "path" IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND "deleted_at" IS NULL)) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "sys_menu" WHERE "permission"='gb28181:traffic:view' AND "deleted_at" IS NULL);
INSERT INTO "sys_api" ("title","path","method","api_group","created_at","updated_at","created_by")
SELECT v.title,v.path,v.method,'GB28181 运行监控','2026-09-07 00:00:00','2026-09-07 00:00:00',1 FROM (
  SELECT '查询流量汇总' title,'/api/gb28181/device-traffic/summary' path,'GET' method
  UNION ALL SELECT '查询流量趋势','/api/gb28181/device-traffic/trend','GET'
  UNION ALL SELECT '查询实时流量','/api/gb28181/device-traffic/realtime','GET'
  UNION ALL SELECT '查询流量会话','/api/gb28181/device-traffic/sessions','GET'
  UNION ALL SELECT '查询统计覆盖率','/api/gb28181/device-traffic/coverage','GET'
  UNION ALL SELECT '查询当前观看','/api/gb28181/device-traffic/viewers','GET'
  UNION ALL SELECT '强退观看连接','/api/gb28181/device-traffic/viewers/kick','POST'
) v WHERE NOT EXISTS (SELECT 1 FROM "sys_api" a WHERE a."path"=v.path AND a."method"=v.method AND a."deleted_at" IS NULL);
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT rm."role_id",((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:traffic:view' AND "deleted_at" IS NULL)) FROM "sys_role_menu" rm
WHERE rm."menu_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "path" IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND "deleted_at" IS NULL)) AND ((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:traffic:view' AND "deleted_at" IS NULL)) IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" x WHERE x."role_id"=rm."role_id" AND x."menu_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:traffic:view' AND "deleted_at" IS NULL)));
INSERT INTO "sys_role_menu" ("role_id","menu_id")
SELECT 1,((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:traffic:view' AND "deleted_at" IS NULL)) WHERE ((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:traffic:view' AND "deleted_at" IS NULL)) IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM "sys_role_menu" x WHERE x."role_id"=1 AND x."menu_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:traffic:view' AND "deleted_at" IS NULL)));
INSERT INTO "sys_menu_api" ("menu_id","api_id")
SELECT ((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:traffic:view' AND "deleted_at" IS NULL)),a."id" FROM "sys_api" a
WHERE a."path" LIKE '/api/gb28181/device-traffic/%' AND a."method"='GET' AND a."deleted_at" IS NULL
  AND NOT EXISTS (SELECT 1 FROM "sys_menu_api" x WHERE x."menu_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:traffic:view' AND "deleted_at" IS NULL)) AND x."api_id"=a."id");
INSERT INTO "sys_casbin_rule" ("ptype","v0","v1","v2","v3","v4","v5")
SELECT 'p',('role_' || rm."role_id"),a."path",a."method",'*','',''
FROM "sys_role_menu" rm CROSS JOIN "sys_api" a
WHERE rm."menu_id"=((SELECT MIN("id") FROM "sys_menu" WHERE "permission"='gb28181:traffic:view' AND "deleted_at" IS NULL)) AND a."path" LIKE '/api/gb28181/device-traffic/%' AND a."method"='GET' AND a."deleted_at" IS NULL
  AND NOT EXISTS (SELECT 1 FROM "sys_casbin_rule" c WHERE c."ptype"='p' AND c."v0"=('role_' || rm."role_id") AND c."v1"=a."path" AND c."v2"=a."method" AND c."v3"='*');
INSERT INTO "sys_casbin_rule" ("ptype","v0","v1","v2","v3","v4","v5")
SELECT 'p','role_1',a."path",a."method",'*','','' FROM "sys_api" a
WHERE a."path"='/api/gb28181/device-traffic/viewers/kick' AND a."method"='POST' AND a."deleted_at" IS NULL
  AND NOT EXISTS (SELECT 1 FROM "sys_casbin_rule" c WHERE c."ptype"='p' AND c."v0"='role_1' AND c."v1"=a."path" AND c."v2"=a."method" AND c."v3"='*');
