-- GB28181 国标通道表(设备下的视频通道,Catalog 目录查询填充)
DROP TABLE IF EXISTS `gb_channel`;
CREATE TABLE `gb_channel` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `channel_id` varchar(20) NOT NULL DEFAULT '' COMMENT '通道国标编码',
  `device_id` varchar(20) NOT NULL DEFAULT '' COMMENT '所属设备国标编码',
  `name` varchar(255) NOT NULL DEFAULT '' COMMENT '通道名称',
  `manufacturer` varchar(255) NOT NULL DEFAULT '' COMMENT '厂商',
  `model` varchar(255) NOT NULL DEFAULT '' COMMENT '型号',
  `owner` varchar(64) NOT NULL DEFAULT '' COMMENT '设备归属',
  `civil_code` varchar(32) NOT NULL DEFAULT '' COMMENT '行政区划',
  `parent_id` varchar(20) NOT NULL DEFAULT '' COMMENT '父节点编码(目录树)',
  `ptz_type` tinyint DEFAULT '0' COMMENT '云台类型 0未知1球机2半球3固定枪机4遥控枪机',
  `longitude` decimal(10,6) DEFAULT '0.000000' COMMENT '经度',
  `latitude` decimal(10,6) DEFAULT '0.000000' COMMENT '纬度',
  `status` tinyint(1) DEFAULT '0' COMMENT '通道在线 0离线 1在线',
  `stream_id` varchar(64) NOT NULL DEFAULT '' COMMENT '当前播放流ID(ZLM app/stream),空=未点播',
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  `deleted_at` datetime DEFAULT NULL,
  `tenant_id` int unsigned DEFAULT '0' COMMENT '租户ID',
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE KEY `uk_device_channel` (`device_id`, `channel_id`) USING BTREE,
  KEY `idx_device_id` (`device_id`),
  KEY `idx_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='GB28181国标通道表';
