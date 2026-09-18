-- GB/T 28181 目录通道属性落库(附录 A / §9.3.1)
--
-- 背景:云台类型/室内外/补光方式/方向/分辨率在标准里都位于 Catalog Item 的 <Info> 容器内,
-- 平台此前既没解析也没落库(PTZType 被错declare 在 Item 层,Go encoding/xml 不递归 → 收不到)。
-- 另外把 2016 独有的 PositionType/UseType 与 2022 独有的 PhotoelectricImagingType/
-- CapturePositionType 也一并落库 —— 这两组是"设备用的是哪一版目录形态"的直接证据。
-- 配套代码改动:manscdp.CatalogInfo 解析 + catalog/upsert 落库 + 通道详情展示。
--
-- 约定:0 / '' 一律表示"设备未上报该属性",不是"该属性为 0"。
--       落库侧据此判断是否覆盖,避免一次不带 <Info> 的 UPDATE 事件把已知属性清零。
--
-- 幂等:每列先查 information_schema 再决定是否执行。

SET @ch_room_type_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'room_type'
);
SET @ch_room_type_sql := IF(
  @ch_room_type_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `room_type` tinyint NOT NULL DEFAULT 0 COMMENT ''室内外 0未上报 1室外 2室内(两版编码一致)'' AFTER `ptz_type`',
  'SELECT 1'
);
PREPARE ch_room_type_stmt FROM @ch_room_type_sql;
EXECUTE ch_room_type_stmt;
DEALLOCATE PREPARE ch_room_type_stmt;

SET @ch_supply_light_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'supply_light_type'
);
SET @ch_supply_light_sql := IF(
  @ch_supply_light_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `supply_light_type` tinyint NOT NULL DEFAULT 0 COMMENT ''补光方式 0未上报 1无补光 2红外 3白光 4激光 9其他'' AFTER `room_type`',
  'SELECT 1'
);
PREPARE ch_supply_light_stmt FROM @ch_supply_light_sql;
EXECUTE ch_supply_light_stmt;
DEALLOCATE PREPARE ch_supply_light_stmt;

SET @ch_direction_type_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'direction_type'
);
SET @ch_direction_type_sql := IF(
  @ch_direction_type_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `direction_type` tinyint NOT NULL DEFAULT 0 COMMENT ''方向 0未上报'' AFTER `supply_light_type`',
  'SELECT 1'
);
PREPARE ch_direction_type_stmt FROM @ch_direction_type_sql;
EXECUTE ch_direction_type_stmt;
DEALLOCATE PREPARE ch_direction_type_stmt;

SET @ch_resolution_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'resolution'
);
SET @ch_resolution_sql := IF(
  @ch_resolution_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `resolution` varchar(32) NOT NULL DEFAULT '''' COMMENT ''分辨率,如 1920*1080'' AFTER `direction_type`',
  'SELECT 1'
);
PREPARE ch_resolution_stmt FROM @ch_resolution_sql;
EXECUTE ch_resolution_stmt;
DEALLOCATE PREPARE ch_resolution_stmt;

SET @ch_ip_address_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'ip_address'
);
SET @ch_ip_address_sql := IF(
  @ch_ip_address_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `ip_address` varchar(64) NOT NULL DEFAULT '''' COMMENT ''设备声明的通道 IP(Catalog Item 层)'' AFTER `resolution`',
  'SELECT 1'
);
PREPARE ch_ip_address_stmt FROM @ch_ip_address_sql;
EXECUTE ch_ip_address_stmt;
DEALLOCATE PREPARE ch_ip_address_stmt;

SET @ch_port_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'port'
);
SET @ch_port_sql := IF(
  @ch_port_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `port` int NOT NULL DEFAULT 0 COMMENT ''设备声明的通道端口(Catalog Item 层)'' AFTER `ip_address`',
  'SELECT 1'
);
PREPARE ch_port_stmt FROM @ch_port_sql;
EXECUTE ch_port_stmt;
DEALLOCATE PREPARE ch_port_stmt;

-- ---- 版本独有属性:2016 的 PositionType/UseType 与 2022 的 PhotoelectricImagingType/
--      CapturePositionType。两组在 XSD 上互斥,设备上报哪一组就落哪一组 —— 反过来说,
--      这两组的值本身就是"这台设备用的是哪一版目录形态"的证据,所以不能只解析不落库。
-- ⛔ 追加在末尾而不是插到 direction_type 后面:守卫式迁移无法重排**已存在**的列,
--    插队会让「已迁移过的库」与「新建库」列序不一致。列序对 GORM 按名映射无影响。
SET @ch_position_type_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'position_type'
);
SET @ch_position_type_sql := IF(
  @ch_position_type_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `position_type` tinyint NOT NULL DEFAULT 0 COMMENT ''位置类型 0未上报 2016独有 1省际检查站…10交通干线'' AFTER `port`',
  'SELECT 1'
);
PREPARE ch_position_type_stmt FROM @ch_position_type_sql;
EXECUTE ch_position_type_stmt;
DEALLOCATE PREPARE ch_position_type_stmt;

SET @ch_use_type_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'use_type'
);
SET @ch_use_type_sql := IF(
  @ch_use_type_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `use_type` tinyint NOT NULL DEFAULT 0 COMMENT ''用途 0未上报 2016独有 1治安 2交通 3重点'' AFTER `position_type`',
  'SELECT 1'
);
PREPARE ch_use_type_stmt FROM @ch_use_type_sql;
EXECUTE ch_use_type_stmt;
DEALLOCATE PREPARE ch_use_type_stmt;

SET @ch_photo_imaging_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'photoelectric_imaging_type'
);
SET @ch_photo_imaging_sql := IF(
  @ch_photo_imaging_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `photoelectric_imaging_type` varchar(32) NOT NULL DEFAULT '''' COMMENT ''光电成像类型 2022独有,可多值 / 分隔'' AFTER `use_type`',
  'SELECT 1'
);
PREPARE ch_photo_imaging_stmt FROM @ch_photo_imaging_sql;
EXECUTE ch_photo_imaging_stmt;
DEALLOCATE PREPARE ch_photo_imaging_stmt;

SET @ch_capture_position_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'capture_position_type'
);
SET @ch_capture_position_sql := IF(
  @ch_capture_position_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `capture_position_type` varchar(32) NOT NULL DEFAULT '''' COMMENT ''采集部位类型 2022独有,见附录O'' AFTER `photoelectric_imaging_type`',
  'SELECT 1'
);
PREPARE ch_capture_position_stmt FROM @ch_capture_position_sql;
EXECUTE ch_capture_position_stmt;
DEALLOCATE PREPARE ch_capture_position_stmt;

-- ptz_type 的列注释同步到 2022 值域(1-7)。⛔ 只更新注释,不改类型/默认值。
-- (该 UPDATE 属纯文档性质,不进本迁移的必过断言;PG/SQL Server 侧无等价轻量写法,对应的
--  新建库注释见 resource/database/gb28181/gb_channel.sql 与三个全量快照。)
SET @ch_ptz_comment_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'ptz_type'
);
SET @ch_ptz_comment_sql := IF(
  @ch_ptz_comment_exists = 1,
  'ALTER TABLE `gb_channel` MODIFY COLUMN `ptz_type` tinyint DEFAULT 0 COMMENT ''云台类型 0未知 1球机 2半球 3固定枪机 4遥控枪机 5遥控半球 6多目全景/拼接通道 7多目分割通道(5-7 为 2022 新增)''',
  'SELECT 1'
);
PREPARE ch_ptz_comment_stmt FROM @ch_ptz_comment_sql;
EXECUTE ch_ptz_comment_stmt;
DEALLOCATE PREPARE ch_ptz_comment_stmt;

-- ---------------------------------------------------------------------------
-- ptz_type 字典项补齐到 2022 值域(5/6/7)
--
-- `sys_dict_item`(dict code = ptz_type)是前端下拉的真源,此前只有 0-4(2016 值域),
-- 不同步会让人工编辑通道时根本选不到 2022 新增的三种类型。
-- 依据:GB/T 28181-2022 附录 A —— 1球机 2半球 3固定枪机 4遥控枪机 5遥控半球
--       6多目设备的全景/拼接通道 7多目设备的分割通道。
-- 配套代码改动:devicemgmt 手工编辑校验从 `> 4` 放到 `> 7`。
-- ---------------------------------------------------------------------------

SET @ptz_type_dict_id = (
  SELECT MIN(`id`) FROM `sys_dict` WHERE `code` = 'ptz_type' AND `deleted_at` IS NULL
);

UPDATE `sys_dict`
SET `description` = 'GB28181 国标摄像头云台类型(PTZType),2022 值域 1-7', `updated_at` = NOW()
WHERE `id` = @ptz_type_dict_id;

INSERT INTO `sys_dict_item` (`name`, `value`, `status`, `dict_id`)
SELECT '遥控半球', '5', 1, @ptz_type_dict_id FROM DUAL
WHERE @ptz_type_dict_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_dict_item` WHERE `dict_id` = @ptz_type_dict_id AND `value` = '5');

INSERT INTO `sys_dict_item` (`name`, `value`, `status`, `dict_id`)
SELECT '多目设备的全景/拼接通道', '6', 1, @ptz_type_dict_id FROM DUAL
WHERE @ptz_type_dict_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_dict_item` WHERE `dict_id` = @ptz_type_dict_id AND `value` = '6');

INSERT INTO `sys_dict_item` (`name`, `value`, `status`, `dict_id`)
SELECT '多目设备的分割通道', '7', 1, @ptz_type_dict_id FROM DUAL
WHERE @ptz_type_dict_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_dict_item` WHERE `dict_id` = @ptz_type_dict_id AND `value` = '7');
