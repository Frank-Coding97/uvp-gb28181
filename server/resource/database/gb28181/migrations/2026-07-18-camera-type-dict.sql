-- 摄像头类型字典(GB28181 PTZType)
-- 插入字典主表
INSERT INTO `sys_dict` (`name`, `code`, `status`, `description`, `created_by`, `created_at`, `updated_at`)
VALUES ('摄像头类型', 'ptz_type', 1, 'GB28181 国标摄像头云台类型(PTZType)', 1, NOW(), NOW())
ON DUPLICATE KEY UPDATE `updated_at` = NOW();

-- 获取刚插入的字典ID(用于后续插入字典项)
SET @dict_id = (SELECT `id` FROM `sys_dict` WHERE `code` = 'ptz_type' LIMIT 1);

-- 插入字典项(5种类型)
INSERT INTO `sys_dict_item` (`name`, `value`, `status`, `dict_id`)
VALUES
    ('未知', '0', 1, @dict_id),
    ('球机', '1', 1, @dict_id),
    ('半球', '2', 1, @dict_id),
    ('固定枪机', '3', 1, @dict_id),
    ('遥控枪机', '4', 1, @dict_id)
ON DUPLICATE KEY UPDATE `status` = VALUES(`status`);
