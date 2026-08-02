-- Device custom groups. PostgreSQL 12+, repeatable.
CREATE TABLE IF NOT EXISTS gb_custom_group (
  id BIGSERIAL PRIMARY KEY,
  owner_dept_id BIGINT NOT NULL,
  parent_id BIGINT NOT NULL DEFAULT 0,
  path VARCHAR(1024) NOT NULL,
  depth SMALLINT NOT NULL DEFAULT 0,
  name VARCHAR(64) NOT NULL,
  created_by BIGINT NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_custom_group_sibling_name UNIQUE (owner_dept_id, parent_id, name)
);
CREATE INDEX IF NOT EXISTS idx_custom_group_parent ON gb_custom_group (parent_id);
CREATE INDEX IF NOT EXISTS idx_custom_group_dept_path ON gb_custom_group (owner_dept_id, path);

CREATE TABLE IF NOT EXISTS gb_custom_group_device (
  id BIGSERIAL PRIMARY KEY,
  group_id BIGINT NOT NULL,
  device_id BIGINT NOT NULL,
  created_by BIGINT NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_custom_group_device UNIQUE (group_id, device_id)
);
CREATE INDEX IF NOT EXISTS idx_custom_group_device_group ON gb_custom_group_device (group_id);
CREATE INDEX IF NOT EXISTS idx_custom_group_device_device ON gb_custom_group_device (device_id);
