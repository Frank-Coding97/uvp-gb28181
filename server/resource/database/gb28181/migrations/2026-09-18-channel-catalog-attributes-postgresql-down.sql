-- 回滚 2026-09-18-channel-catalog-attributes-postgresql.sql —— PostgreSQL 方言
-- ⛔ COMMENT 变更不回滚(纯文档修正)。
-- ⚠️ 回滚会丢弃已落库的通道属性。

ALTER TABLE gb_channel
    DROP COLUMN IF EXISTS room_type,
    DROP COLUMN IF EXISTS supply_light_type,
    DROP COLUMN IF EXISTS direction_type,
    DROP COLUMN IF EXISTS resolution,
    DROP COLUMN IF EXISTS ip_address,
    DROP COLUMN IF EXISTS port,
    DROP COLUMN IF EXISTS position_type,
    DROP COLUMN IF EXISTS use_type,
    DROP COLUMN IF EXISTS photoelectric_imaging_type,
    DROP COLUMN IF EXISTS capture_position_type;

-- 一并移除本次补齐的 ptz_type 字典项(5/6/7)。0-4 是历史项,保持不变。
DELETE FROM sys_dict_item
WHERE value IN ('5', '6', '7')
  AND dict_id IN (SELECT id FROM sys_dict WHERE code = 'ptz_type' AND deleted_at IS NULL);
