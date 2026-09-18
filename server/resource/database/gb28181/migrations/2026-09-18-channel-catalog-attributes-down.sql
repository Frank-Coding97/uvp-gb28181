-- 回滚 2026-09-18-channel-catalog-attributes.sql:移除本次新增的十列。
--
-- ⛔ ptz_type 的列注释变更**不**回滚:那是纯文档修正(1-4 → 1-7),
--    回滚成 2016 值域只会让注释重新与实际值域不符。
-- ⚠️ 回滚会丢弃已落库的通道属性,执行前确认无回放需求。

ALTER TABLE `gb_channel`
  DROP COLUMN `room_type`,
  DROP COLUMN `supply_light_type`,
  DROP COLUMN `direction_type`,
  DROP COLUMN `resolution`,
  DROP COLUMN `ip_address`,
  DROP COLUMN `port`,
  DROP COLUMN `position_type`,
  DROP COLUMN `use_type`,
  DROP COLUMN `photoelectric_imaging_type`,
  DROP COLUMN `capture_position_type`;

-- 一并移除本次补齐的 ptz_type 字典项(5/6/7)。0-4 是历史项(2026-07-18 迁移写入),保持不变。
SET @ptz_type_dict_id = (
  SELECT MIN(`id`) FROM `sys_dict` WHERE `code` = 'ptz_type' AND `deleted_at` IS NULL
);
DELETE FROM `sys_dict_item`
WHERE `dict_id` = @ptz_type_dict_id AND `value` IN ('5', '6', '7');
