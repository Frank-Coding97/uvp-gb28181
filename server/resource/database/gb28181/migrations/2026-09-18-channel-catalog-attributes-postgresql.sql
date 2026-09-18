-- GB/T 28181 目录通道属性落库(附录 A / §9.3.1) —— PostgreSQL 方言
-- 与 2026-09-18-channel-catalog-attributes.sql 等价;0 / '' 表示"设备未上报"。

ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS room_type SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS supply_light_type SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS direction_type SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS resolution VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS ip_address VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS port INTEGER NOT NULL DEFAULT 0;

-- 版本独有属性:2016 的 PositionType/UseType 与 2022 的 PhotoelectricImagingType/
-- CapturePositionType。两组在 XSD 上互斥,是"设备用了哪一版目录形态"的直接证据。
ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS position_type SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS use_type SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS photoelectric_imaging_type VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS capture_position_type VARCHAR(32) NOT NULL DEFAULT '';

COMMENT ON COLUMN gb_channel.room_type IS '室内外 0未上报 1室外 2室内(两版编码一致)';
COMMENT ON COLUMN gb_channel.supply_light_type IS '补光方式 0未上报 1无补光 2红外 3白光 4激光 9其他';
COMMENT ON COLUMN gb_channel.direction_type IS '方向 0未上报';
COMMENT ON COLUMN gb_channel.resolution IS '分辨率,如 1920*1080';
COMMENT ON COLUMN gb_channel.ip_address IS '设备声明的通道 IP(Catalog Item 层)';
COMMENT ON COLUMN gb_channel.port IS '设备声明的通道端口(Catalog Item 层)';
COMMENT ON COLUMN gb_channel.position_type IS '位置类型 0未上报 2016独有 1省际检查站…10交通干线';
COMMENT ON COLUMN gb_channel.use_type IS '用途 0未上报 2016独有 1治安 2交通 3重点';
COMMENT ON COLUMN gb_channel.photoelectric_imaging_type IS '光电成像类型 2022独有,可多值 / 分隔';
COMMENT ON COLUMN gb_channel.capture_position_type IS '采集部位类型 2022独有,见附录O';
COMMENT ON COLUMN gb_channel.ptz_type IS '云台类型 0未知 1球机 2半球 3固定枪机 4遥控枪机 5遥控半球 6多目全景/拼接通道 7多目分割通道(5-7 为 2022 新增)';

-- ptz_type 字典项补齐到 2022 值域(5/6/7),依据 GB/T 28181-2022 附录 A。
UPDATE sys_dict
SET description = 'GB28181 国标摄像头云台类型(PTZType),2022 值域 1-7', updated_at = CURRENT_TIMESTAMP
WHERE code = 'ptz_type' AND deleted_at IS NULL;

INSERT INTO sys_dict_item (name,value,status,dict_id)
SELECT '遥控半球','5',TRUE,d.id
FROM sys_dict d
WHERE d.code='ptz_type' AND d.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_dict_item i WHERE i.dict_id=d.id AND i.value='5')
ORDER BY d.id LIMIT 1;

INSERT INTO sys_dict_item (name,value,status,dict_id)
SELECT '多目设备的全景/拼接通道','6',TRUE,d.id
FROM sys_dict d
WHERE d.code='ptz_type' AND d.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_dict_item i WHERE i.dict_id=d.id AND i.value='6')
ORDER BY d.id LIMIT 1;

INSERT INTO sys_dict_item (name,value,status,dict_id)
SELECT '多目设备的分割通道','7',TRUE,d.id
FROM sys_dict d
WHERE d.code='ptz_type' AND d.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_dict_item i WHERE i.dict_id=d.id AND i.value='7')
ORDER BY d.id LIMIT 1;
