-- SQLite down: restore the legacy 20-character limit; copying fails closed if a value would be lost.
CREATE TABLE "gb_channel_parent_id_down" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT CHECK ("id" IS NULL OR (typeof("id") = 'integer' AND "id" >= 0)),
  "channel_id" TEXT NOT NULL CHECK ("channel_id" IS NULL OR length("channel_id") <= 20) DEFAULT '',
  "device_id" TEXT NOT NULL CHECK ("device_id" IS NULL OR length("device_id") <= 20) DEFAULT '',
  "name" TEXT NOT NULL CHECK ("name" IS NULL OR length("name") <= 255) DEFAULT '',
  "alias" TEXT NOT NULL CHECK ("alias" IS NULL OR length("alias") <= 255) DEFAULT '',
  "manufacturer" TEXT NOT NULL CHECK ("manufacturer" IS NULL OR length("manufacturer") <= 255) DEFAULT '',
  "model" TEXT NOT NULL CHECK ("model" IS NULL OR length("model") <= 255) DEFAULT '',
  "owner" TEXT NOT NULL CHECK ("owner" IS NULL OR length("owner") <= 64) DEFAULT '',
  "civil_code" TEXT NOT NULL CHECK ("civil_code" IS NULL OR length("civil_code") <= 32) DEFAULT '',
  "parent_id" TEXT NOT NULL CHECK ("parent_id" IS NULL OR length("parent_id") <= 20) DEFAULT '',
  "ptz_type" INTEGER NULL CHECK ("ptz_type" IS NULL OR typeof("ptz_type") = 'integer') DEFAULT 0,
  "room_type" INTEGER NOT NULL CHECK ("room_type" IS NULL OR typeof("room_type") = 'integer') DEFAULT 0,
  "supply_light_type" INTEGER NOT NULL CHECK ("supply_light_type" IS NULL OR typeof("supply_light_type") = 'integer') DEFAULT 0,
  "direction_type" INTEGER NOT NULL CHECK ("direction_type" IS NULL OR typeof("direction_type") = 'integer') DEFAULT 0,
  "resolution" TEXT NOT NULL CHECK ("resolution" IS NULL OR length("resolution") <= 32) DEFAULT '',
  "ip_address" TEXT NOT NULL CHECK ("ip_address" IS NULL OR length("ip_address") <= 64) DEFAULT '',
  "port" INTEGER NOT NULL CHECK ("port" IS NULL OR typeof("port") = 'integer') DEFAULT 0,
  "position_type" INTEGER NOT NULL CHECK ("position_type" IS NULL OR typeof("position_type") = 'integer') DEFAULT 0,
  "use_type" INTEGER NOT NULL CHECK ("use_type" IS NULL OR typeof("use_type") = 'integer') DEFAULT 0,
  "photoelectric_imaging_type" TEXT NOT NULL CHECK ("photoelectric_imaging_type" IS NULL OR length("photoelectric_imaging_type") <= 32) DEFAULT '',
  "capture_position_type" TEXT NOT NULL CHECK ("capture_position_type" IS NULL OR length("capture_position_type") <= 32) DEFAULT '',
  "stream_number_list" TEXT NOT NULL CHECK ("stream_number_list" IS NULL OR length("stream_number_list") <= 32) DEFAULT '',
  "longitude" NUMERIC NULL DEFAULT 0.000000,
  "latitude" NUMERIC NULL DEFAULT 0.000000,
  "position_source" TEXT NOT NULL CHECK ("position_source" IS NULL OR length("position_source") <= 16) DEFAULT '',
  "position_updated_at" DATETIME NULL,
  "status" INTEGER NULL CHECK ("status" IS NULL OR typeof("status") = 'integer') CHECK ("status" IS NULL OR "status" IN (0, 1)) DEFAULT 0,
  "stream_id" TEXT NOT NULL CHECK ("stream_id" IS NULL OR length("stream_id") <= 64) DEFAULT '',
  "current_ssrc" TEXT NOT NULL CHECK ("current_ssrc" IS NULL OR length("current_ssrc") <= 10) DEFAULT '',
  "on_demand_live" INTEGER NOT NULL CHECK ("on_demand_live" IS NULL OR typeof("on_demand_live") = 'integer') CHECK ("on_demand_live" IS NULL OR "on_demand_live" IN (0, 1)) DEFAULT 1,
  "cloud_recording_enabled" INTEGER NOT NULL CHECK ("cloud_recording_enabled" IS NULL OR typeof("cloud_recording_enabled") = 'integer') CHECK ("cloud_recording_enabled" IS NULL OR "cloud_recording_enabled" IN (0, 1)) DEFAULT 0,
  "cloud_recording_state" TEXT NOT NULL CHECK ("cloud_recording_state" IS NULL OR length("cloud_recording_state") <= 20) DEFAULT 'disabled',
  "cloud_recording_error" TEXT NOT NULL CHECK ("cloud_recording_error" IS NULL OR length("cloud_recording_error") <= 500) DEFAULT '',
  "cloud_recording_updated_at" DATETIME NULL,
  "audio_enabled" INTEGER NOT NULL CHECK ("audio_enabled" IS NULL OR typeof("audio_enabled") = 'integer') CHECK ("audio_enabled" IS NULL OR "audio_enabled" IN (0, 1)) DEFAULT 1,
  "recording_mode" TEXT NOT NULL CHECK ("recording_mode" IS NULL OR length("recording_mode") <= 16) DEFAULT 'off',
  "stream_transport" TEXT NOT NULL CHECK ("stream_transport" IS NULL OR length("stream_transport") <= 16) DEFAULT 'TCP-Passive',
  "capabilities" TEXT NULL CHECK ("capabilities" IS NULL OR json_valid("capabilities")),
  "snapshot_url" TEXT NOT NULL CHECK ("snapshot_url" IS NULL OR length("snapshot_url") <= 500) DEFAULT '',
  "snapshot_at" DATETIME NULL,
  "created_at" DATETIME NULL,
  "updated_at" DATETIME NULL,
  "deleted_at" DATETIME NULL,
  "owner_dept_id" INTEGER NOT NULL CHECK ("owner_dept_id" IS NULL OR (typeof("owner_dept_id") = 'integer' AND "owner_dept_id" >= 0)) DEFAULT 0
);
INSERT INTO "gb_channel_parent_id_down" ("id", "channel_id", "device_id", "name", "alias", "manufacturer", "model", "owner", "civil_code", "parent_id", "ptz_type", "room_type", "supply_light_type", "direction_type", "resolution", "ip_address", "port", "position_type", "use_type", "photoelectric_imaging_type", "capture_position_type", "stream_number_list", "longitude", "latitude", "position_source", "position_updated_at", "status", "stream_id", "current_ssrc", "on_demand_live", "cloud_recording_enabled", "cloud_recording_state", "cloud_recording_error", "cloud_recording_updated_at", "audio_enabled", "recording_mode", "stream_transport", "capabilities", "snapshot_url", "snapshot_at", "created_at", "updated_at", "deleted_at", "owner_dept_id")
  SELECT "id", "channel_id", "device_id", "name", "alias", "manufacturer", "model", "owner", "civil_code", "parent_id", "ptz_type", "room_type", "supply_light_type", "direction_type", "resolution", "ip_address", "port", "position_type", "use_type", "photoelectric_imaging_type", "capture_position_type", "stream_number_list", "longitude", "latitude", "position_source", "position_updated_at", "status", "stream_id", "current_ssrc", "on_demand_live", "cloud_recording_enabled", "cloud_recording_state", "cloud_recording_error", "cloud_recording_updated_at", "audio_enabled", "recording_mode", "stream_transport", "capabilities", "snapshot_url", "snapshot_at", "created_at", "updated_at", "deleted_at", "owner_dept_id" FROM "gb_channel";
DROP TABLE "gb_channel";
ALTER TABLE "gb_channel_parent_id_down" RENAME TO "gb_channel";
CREATE UNIQUE INDEX IF NOT EXISTS "uk_device_channel" ON "gb_channel" ("device_id", "channel_id");
CREATE INDEX IF NOT EXISTS "idx_deleted_at" ON "gb_channel" ("deleted_at");
CREATE INDEX IF NOT EXISTS "idx_device_id" ON "gb_channel" ("device_id");
CREATE INDEX IF NOT EXISTS "idx_owner_dept_deleted" ON "gb_channel" ("owner_dept_id", "deleted_at");
CREATE INDEX IF NOT EXISTS "idx_stream_id" ON "gb_channel" ("stream_id");
CREATE INDEX IF NOT EXISTS "idx_stream_transport" ON "gb_channel" ("stream_transport");
