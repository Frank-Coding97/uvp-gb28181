CREATE TABLE IF NOT EXISTS gb_channel_favorite_group (id BIGSERIAL PRIMARY KEY, owner_user_id BIGINT NOT NULL, name VARCHAR(64) NOT NULL, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS uk_gb_channel_favorite_group_owner_name ON gb_channel_favorite_group(owner_user_id,name);
CREATE INDEX IF NOT EXISTS idx_gb_channel_favorite_group_owner ON gb_channel_favorite_group(owner_user_id);
CREATE TABLE IF NOT EXISTS gb_channel_favorite_item (id BIGSERIAL PRIMARY KEY, group_id BIGINT NOT NULL, device_code VARCHAR(64) NOT NULL, channel_code VARCHAR(64) NOT NULL, device_name VARCHAR(255) NOT NULL DEFAULT '', channel_name VARCHAR(255) NOT NULL DEFAULT '', created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS uk_gb_channel_favorite_item_code ON gb_channel_favorite_item(group_id,device_code,channel_code);
CREATE INDEX IF NOT EXISTS idx_gb_channel_favorite_item_group ON gb_channel_favorite_item(group_id);
