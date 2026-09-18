-- 默认播放协议字典（MySQL 5.7+，幂等）。

INSERT INTO `sys_dict` (`name`,`code`,`status`,`description`,`created_by`,`created_at`,`updated_at`)
SELECT 'GB28181 默认播放协议','gb28181_playback_protocol',1,'国标播放入口支持的默认协议选项',1,NOW(),NOW()
FROM DUAL
WHERE NOT EXISTS (
  SELECT 1 FROM `sys_dict` WHERE `code`='gb28181_playback_protocol' AND `deleted_at` IS NULL
);

SET @gb_playback_protocol_dict_id = (
  SELECT MIN(`id`) FROM `sys_dict` WHERE `code`='gb28181_playback_protocol' AND `deleted_at` IS NULL
);

UPDATE `sys_dict`
SET `name`='GB28181 默认播放协议',`status`=1,`description`='国标播放入口支持的默认协议选项',`updated_at`=NOW()
WHERE `id`=@gb_playback_protocol_dict_id;

INSERT INTO `sys_dict_item` (`name`,`value`,`status`,`dict_id`)
SELECT 'WS-FLV','ws-flv',1,@gb_playback_protocol_dict_id FROM DUAL
WHERE @gb_playback_protocol_dict_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_dict_item` WHERE `dict_id`=@gb_playback_protocol_dict_id AND `value`='ws-flv');
INSERT INTO `sys_dict_item` (`name`,`value`,`status`,`dict_id`)
SELECT 'HTTP-FLV','http-flv',1,@gb_playback_protocol_dict_id FROM DUAL
WHERE @gb_playback_protocol_dict_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_dict_item` WHERE `dict_id`=@gb_playback_protocol_dict_id AND `value`='http-flv');
INSERT INTO `sys_dict_item` (`name`,`value`,`status`,`dict_id`)
SELECT 'HLS','hls',1,@gb_playback_protocol_dict_id FROM DUAL
WHERE @gb_playback_protocol_dict_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_dict_item` WHERE `dict_id`=@gb_playback_protocol_dict_id AND `value`='hls');
INSERT INTO `sys_dict_item` (`name`,`value`,`status`,`dict_id`)
SELECT 'WebRTC','webrtc',1,@gb_playback_protocol_dict_id FROM DUAL
WHERE @gb_playback_protocol_dict_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_dict_item` WHERE `dict_id`=@gb_playback_protocol_dict_id AND `value`='webrtc');

UPDATE `sys_dict_item` SET `name`='WS-FLV',`status`=1 WHERE `dict_id`=@gb_playback_protocol_dict_id AND `value`='ws-flv';
UPDATE `sys_dict_item` SET `name`='HTTP-FLV',`status`=1 WHERE `dict_id`=@gb_playback_protocol_dict_id AND `value`='http-flv';
UPDATE `sys_dict_item` SET `name`='HLS',`status`=1 WHERE `dict_id`=@gb_playback_protocol_dict_id AND `value`='hls';
UPDATE `sys_dict_item` SET `name`='WebRTC',`status`=1 WHERE `dict_id`=@gb_playback_protocol_dict_id AND `value`='webrtc';
